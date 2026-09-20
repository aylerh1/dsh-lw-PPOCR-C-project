package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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
	startTime    = time.Now()
	publicDir    = "./public"
	samplePath   = "./test/fixtures/sample.png"
	modelDir       = "./vendor/lw-ppocr-wasm"
	nativeEngine   *NativeOcrEngine
	currentWorkers = 4
	reqCounter     uint64
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
			"version":   "4.0.0",
			"engine":    "native-c-avx2",
			"uptime":    uptime,
			"workers":   currentWorkers,
			"targetMem": "~25MB resident",
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

	// 2. Direct high-performance CGO inference (~15-25ms)
	result, err := nativeEngine.Recognize(bgr, 960)
	if err != nil {
		log.Printf("[ocr-err] Native CGO inference failed: %v\n", err)
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("OCR 推理执行异常: %v", err))
		return
	}

	// 3. Filter by confidenceThreshold if requested
	threshold := 0.0
	if thVal, ok := rawMap["confidenceThreshold"].(float64); ok {
		threshold = thVal
	}
	if threshold > 0 {
		filteredLines := make([]OcrLine, 0, len(result.Lines))
		var filteredText strings.Builder
		for _, l := range result.Lines {
			if l.Score >= threshold {
				filteredLines = append(filteredLines, l)
				if filteredText.Len() > 0 {
					filteredText.WriteString("\n")
				}
				filteredText.WriteString(l.Text)
			}
		}
		result.Lines = filteredLines
		result.Text = filteredText.String()
	}

	sendJSON(w, http.StatusOK, JsonResponse{
		Code:      http.StatusOK,
		Status:    "success",
		Data:      result,
		Timestamp: time.Now().UnixMilli(),
	})
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

	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/v1/health", handleHealth)
	mux.HandleFunc("/api/v1/sample", handleSample)
	mux.HandleFunc("/api/v1/ocr/recognitions", handleOcr)
	mux.HandleFunc("/api/v1/ocr", handleOcr)

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
