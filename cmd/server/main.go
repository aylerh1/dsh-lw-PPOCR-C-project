package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

var (
	startTime        = time.Now()
	publicDir        = "./public"
	samplePath       = "./test/fixtures/sample.png"
	modelDir         = "./vendor/lw-ppocr-wasm"
	nativeEngine     *NativeOcrEngine
	documentPipeline *DocumentPipeline
	currentWorkers   = 4
	reqCounter       uint64
)

// Standard JSON response wrapper
type JsonResponse struct {
	Code      int         `json:"code"`
	Status    string      `json:"status"`
	Message   string      `json:"message,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

func sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(statusCode)

	_ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, statusCode int, message string) {
	sendJSON(w, statusCode, JsonResponse{
		Code:      statusCode,
		Status:    "error",
		Message:   message,
		Timestamp: time.Now().UnixMilli(),
	})
}

// Global CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Health check handler
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	uptime := time.Since(startTime).Seconds()
	sendJSON(w, http.StatusOK, JsonResponse{
		Code:   http.StatusOK,
		Status: "success",
		Data: map[string]interface{}{
			"service":   "dsh-lw-ppocr-web",
			"version":   "4.2.0",
			"engine":    "native-c-avx2",
			"pipeline":  "pp-doclayout-s + slanet + rapid-latex + lw-ppocr-c",
			"uptime":    uptime,
			"workers":      currentWorkers,
			"acceleration": "AVX2+FMA SIMD",
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// Built-in sample image handler
func handleSample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	data, err := os.ReadFile(samplePath)
	if err != nil {
		sendError(w, http.StatusNotFound, "样例测试图片未找到")
		return
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	sendJSON(w, http.StatusOK, JsonResponse{
		Code:   http.StatusOK,
		Status: "success",
		Data: map[string]interface{}{
			"filename": "sample.png",
			"mimeType": "image/png",
			"image":    "data:image/png;base64," + encoded,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// OCR Recognition Handler using resident Native C CGO Engine
func handleOcr(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	// Limit request payload to 50MB
	r.Body = http.MaxBytesReader(w, r.Body, 50*1024*1024)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, http.StatusBadRequest, "请求体读取失败或超过 50MB 限制")
		return
	}

	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		sendError(w, http.StatusBadRequest, "缺少请求数据")
		return
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: 请求体不是合法的 JSON 格式 (%v)", err))
		return
	}

	imgVal, _ := rawMap["image"].(string)
	if imgVal == "" {
		imgVal, _ = rawMap["file_path"].(string)
	}
	if imgVal == "" {
		imgVal, _ = rawMap["input"].(string)
	}
	if strings.TrimSpace(imgVal) == "" {
		sendError(w, http.StatusBadRequest, "缺少必需参数: image (支持 Base64、Data URI 或图片绝对路径)")
		return
	}

	_ = atomic.AddUint64(&reqCounter, 1)

	// 1. Fast Go native image decode to interleaved BGR8 (5-15ms)
	bgr, err := DecodeImageToBGR(imgVal)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("图像解码失败: %v", err))
		return
	}

	// 2. High-performance pipeline execution with layout, figure cropping, and smart paragraph merging (~20-30ms)
	docResult, err := documentPipeline.Process(bgr)
	if err != nil {
		log.Printf("[ocr-err] Pipeline inference failed: %v, falling back to native OCR\n", err)
		result, ocrErr := nativeEngine.Recognize(bgr, 960)
		if ocrErr != nil {
			sendError(w, http.StatusInternalServerError, fmt.Sprintf("OCR 推理执行异常: %v", ocrErr))
			return
		}
		sendJSON(w, http.StatusOK, JsonResponse{
			Code:      http.StatusOK,
			Status:    "success",
			Data:      result,
			Timestamp: time.Now().UnixMilli(),
		})
		return
	}

	sendJSON(w, http.StatusOK, JsonResponse{
		Code:      http.StatusOK,
		Status:    "success",
		Data:      docResult,
		Timestamp: time.Now().UnixMilli(),
	})
}

// Layout Analysis Handler returns detected bounding regions and classes
func handleLayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50*1024*1024)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, http.StatusBadRequest, "请求体读取失败或超过 50MB 限制")
		return
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	imgVal, _ := rawMap["image"].(string)
	if imgVal == "" {
		imgVal, _ = rawMap["file_path"].(string)
	}
	if imgVal == "" {
		imgVal, _ = rawMap["input"].(string)
	}
	if strings.TrimSpace(imgVal) == "" {
		sendError(w, http.StatusBadRequest, "缺少必需参数: image (支持 Base64、Data URI 或图片绝对路径)")
		return
	}

	bgr, err := DecodeImageToBGR(imgVal)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("图像解码失败: %v", err))
		return
	}

	t0 := time.Now()
	regions, _, err := documentPipeline.layoutEngine.Detect(bgr, nativeEngine)
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("版面分析失败: %v", err))
		return
	}
	durationMs := time.Since(t0).Milliseconds()

	sendJSON(w, http.StatusOK, JsonResponse{
		Code:   http.StatusOK,
		Status: "success",
		Data: map[string]interface{}{
			"regions":    regions,
			"durationMs": durationMs,
			"width":      bgr.Width,
			"height":     bgr.Height,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

// extractDocumentPayload flexibly extracts file bytes, filename, and mimeType from multipart, JSON, or raw body
func extractDocumentPayload(r *http.Request) ([]byte, string, string, error) {
	contentType := r.Header.Get("Content-Type")

	// 1. Multipart Form Upload
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(50 * 1024 * 1024); err == nil && r.MultipartForm != nil {
			for _, fieldName := range []string{"file", "document", "image", "pdf"} {
				if file, handler, err := r.FormFile(fieldName); err == nil {
					defer file.Close()
					data, readErr := io.ReadAll(file)
					if readErr == nil && len(data) > 0 {
						mime := handler.Header.Get("Content-Type")
						return data, handler.Filename, mime, nil
					}
				}
			}
		}
	}

	// 2. Read Request Body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read request body: %w", err)
	}
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return nil, "", "", errors.New("empty request body")
	}

	// 3. Raw PDF or Image Direct Stream
	if IsPDFData(bodyBytes) {
		return bodyBytes, "input.pdf", "application/pdf", nil
	}
	if IsImageBytes(bodyBytes) {
		return bodyBytes, "input.png", "image/png", nil
	}

	// 4. JSON Payload
	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err == nil {
		filename, _ := rawMap["filename"].(string)
		mimeType, _ := rawMap["mimeType"].(string)

		inputVal, _ := rawMap["file"].(string)
		if inputVal == "" {
			inputVal, _ = rawMap["document"].(string)
		}
		if inputVal == "" {
			inputVal, _ = rawMap["image"].(string)
		}
		if inputVal == "" {
			inputVal, _ = rawMap["file_path"].(string)
		}
		if inputVal == "" {
			inputVal, _ = rawMap["input"].(string)
		}

		trimmed := strings.TrimSpace(inputVal)
		if trimmed == "" {
			return nil, "", "", errors.New("missing file or image payload in JSON")
		}

		// Check if Data URI
		if strings.HasPrefix(trimmed, "data:") {
			commaIdx := strings.Index(trimmed, ",")
			if commaIdx != -1 {
				header := trimmed[:commaIdx]
				if semiIdx := strings.Index(header, ";"); semiIdx != -1 {
					mimeType = strings.TrimPrefix(header[:semiIdx], "data:")
				}
				data, decErr := base64.StdEncoding.DecodeString(trimmed[commaIdx+1:])
				if decErr == nil && len(data) > 0 {
					return data, filename, mimeType, nil
				}
			}
		}

		// Check local file path
		if fi, statErr := os.Stat(trimmed); statErr == nil && !fi.IsDir() {
			data, readErr := os.ReadFile(trimmed)
			if readErr == nil && len(data) > 0 {
				return data, filepath.Base(trimmed), mimeType, nil
			}
		}

		// Standard Base64
		if data, decErr := base64.StdEncoding.DecodeString(trimmed); decErr == nil && len(data) > 0 {
			return data, filename, mimeType, nil
		}
		// URL-Safe Base64
		if data, decErr := base64.URLEncoding.DecodeString(trimmed); decErr == nil && len(data) > 0 {
			return data, filename, mimeType, nil
		}
	}

	return nil, "", "", errors.New("unsupported input format (expected Multipart, raw PDF/image, or JSON with base64/data URI)")
}

// PDF Inspector standalone endpoint
func handlePdfInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50*1024*1024)
	data, _, _, err := extractDocumentPayload(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("无法提取 PDF 数据: %v", err))
		return
	}

	insp, err := documentPipeline.pdfInspector.Inspect(data)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("PDF Inspector 判别失败: %v", err))
		return
	}

	sendJSON(w, http.StatusOK, JsonResponse{
		Code:      http.StatusOK,
		Status:    "success",
		Data:      insp,
		Timestamp: time.Now().UnixMilli(),
	})
}

// Multi-Modal Document Parsing Handler orchestrates AnyDoc Routing + Layout + OCR + SLANet + LaTeX
func handleDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50*1024*1024)
	data, filename, mimeType, err := extractDocumentPayload(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("提取输入文档失败: %v", err))
		return
	}

	docResult, err := documentPipeline.ProcessDocument(data, filename, mimeType)
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("文档解析管道执行失败: %v", err))
		return
	}

	sendJSON(w, http.StatusOK, JsonResponse{
		Code:      http.StatusOK,
		Status:    "success",
		Data:      docResult,
		Timestamp: time.Now().UnixMilli(),
	})
}

// Document Markdown ZIP Export Handler packages markdown, figures, and metadata into a zip
func handleDocumentZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50*1024*1024)
	data, filename, mimeType, err := extractDocumentPayload(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("提取输入文档失败: %v", err))
		return
	}

	docResult, err := documentPipeline.ProcessDocument(data, filename, mimeType)
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("文档解析管道执行失败: %v", err))
		return
	}

	var originalBGR *BGRImage
	if IsImageBytes(data) {
		originalBGR, _ = DecodeRawBytesToBGR(data)
	}

	zipBytes, err := CreateDocumentZip(docResult, originalBGR)
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("ZIP 打包失败: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"document_export.zip\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(zipBytes)))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(zipBytes)
}

// Serve static assets from public/ directory with anti-cache headers
func handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	urlPath := r.URL.Path
	if urlPath == "/" || urlPath == "" {
		urlPath = "/index.html"
	}

	cleanPath := path.Clean(urlPath)
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	fullPath := filepath.Join(publicDir, filepath.FromSlash(cleanPath))

	// Ensure no directory traversal
	absPublic, err1 := filepath.Abs(publicDir)
	absTarget, err2 := filepath.Abs(fullPath)
	if err1 != nil || err2 != nil || !strings.HasPrefix(absTarget, absPublic) {
		sendError(w, http.StatusForbidden, "Forbidden")
		return
	}

	info, err := os.Stat(absTarget)
	if err != nil || info.IsDir() {
		sendError(w, http.StatusNotFound, fmt.Sprintf("API 接口或静态资源未找到: %s %s", r.Method, r.URL.Path))
		return
	}

	ext := strings.ToLower(filepath.Ext(absTarget))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		switch ext {
		case ".wasm":
			contentType = "application/wasm"
		case ".js":
			contentType = "application/javascript; charset=utf-8"
		case ".css":
			contentType = "text/css; charset=utf-8"
		case ".json":
			contentType = "application/json; charset=utf-8"
		case ".svg":
			contentType = "image/svg+xml"
		default:
			contentType = "application/octet-stream"
		}
	}

	// Strictly disable caching in web mode to prevent stale asset cache
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, absTarget)
}

func findModelDir() string {
	if envModel := os.Getenv("MODEL_DIR"); envModel != "" {
		return envModel
	}
	candidates := []string{
		"./vendor/lw-ppocr-wasm",
		"./models",
		"/app/vendor/lw-ppocr-wasm",
		"/app/models",
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "rec.lwm")); err == nil {
			return dir
		}
	}
	return "./vendor/lw-ppocr-wasm"
}

func main() {
	_ = mime.AddExtensionType(".wasm", "application/wasm")
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".json", "application/json; charset=utf-8")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	if envPublic := os.Getenv("PUBLIC_DIR"); envPublic != "" {
		publicDir = envPublic
	}
	if envSample := os.Getenv("SAMPLE_PATH"); envSample != "" {
		samplePath = envSample
	}

	modelDir = findModelDir()

	workerCount := 4
	if envWorkers := os.Getenv("OCR_WORKERS"); envWorkers != "" {
		var w int
		if _, err := fmt.Sscanf(envWorkers, "%d", &w); err == nil && w > 0 {
			workerCount = w
		}
	}
	currentWorkers = workerCount

	// Initialize resident Native C/AVX2 OCR Engine
	var err error
	nativeEngine, err = NewNativeOcrEngine(NativeEngineConfig{
		ModelDir:      modelDir,
		UseClassifier: true,
		WorkerCount:   workerCount,
		RecMaxWidth:   960,
	})
	if err != nil {
		log.Fatalf("[server] Failed to initialize native C OCR engine: %v", err)
	}
	defer nativeEngine.Close()

	log.Printf("[server] High-Performance Native C/AVX2 OCR Engine loaded from %s with %d workers\n", modelDir, workerCount)

	// Initialize Multi-Modal Document Layout & Parsing Pipeline
	documentPipeline = NewDocumentPipeline(nativeEngine)
	log.Printf("[server] Multi-Modal Document Pipeline ready: PP-DocLayout-S + SLANet + RapidLaTeX + Native lw.PPOCR.C\n")

	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/v1/health", handleHealth)
	mux.HandleFunc("/api/v1/sample", handleSample)
	mux.HandleFunc("/api/v1/ocr/recognitions", handleOcr)
	mux.HandleFunc("/api/v1/ocr", handleOcr)
	mux.HandleFunc("/api/v1/layout", handleLayout)
	mux.HandleFunc("/api/v1/document", handleDocument)
	mux.HandleFunc("/api/v1/document/zip", handleDocumentZip)
	mux.HandleFunc("/api/v1/pdf/inspect", handlePdfInspect)

	// Static Web Frontend
	mux.HandleFunc("/", handleStatic)

	addr := net.JoinHostPort(host, port)
	server := &http.Server{
		Addr:         addr,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[lw-ppocr-server] Go Native net/http Server listening on http://%s\n", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[lw-ppocr-server] Server error: %v\n", err)
	}
}
