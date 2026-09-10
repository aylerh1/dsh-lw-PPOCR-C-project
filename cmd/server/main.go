package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	startTime  = time.Now()
	publicDir  = "./public"
	samplePath = "./test/fixtures/sample.png"
	cliScript  = "./src/cli.js"
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

// Global CORS & logging middleware
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
			"service": "dsh-lw-ppocr-web",
			"version": "1.0.0",
			"engine":  "ready",
			"uptime":  uptime,
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

// OCR recognition request payload
type OcrRequest struct {
	Image               string   `json:"image"`
	FilePath            string   `json:"file_path"`
	Input               string   `json:"input"`
	Det                 *bool    `json:"det"`
	Cls                 *bool    `json:"cls"`
	Rec                 *bool    `json:"rec"`
	ReadingOrder        string   `json:"readingOrder"`
	ConfidenceThreshold *float64 `json:"confidenceThreshold"`
}

// OCR recognition handler
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

	var req OcrRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: 请求体不是合法的 JSON 格式 (%v)", err))
		return
	}

	targetImage := req.Image
	if targetImage == "" {
		targetImage = req.FilePath
	}
	if targetImage == "" {
		targetImage = req.Input
	}
	if strings.TrimSpace(targetImage) == "" {
		sendError(w, http.StatusBadRequest, "缺少必需参数: image (支持 Base64、Data URI 或图片绝对路径)")
		return
	}

	// Call CLI worker with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", cliScript)
	cmd.Stdin = bytes.NewReader(bodyBytes)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	execErr := cmd.Run()
	outputBytes := stdoutBuf.Bytes()

	if execErr != nil {
		// Attempt to parse structured error from stdout
		if len(outputBytes) > 0 {
			var errResp JsonResponse
			if json.Unmarshal(outputBytes, &errResp) == nil && errResp.Code != 0 {
				sendJSON(w, errResp.Code, errResp)
				return
			}
		}
		errMsg := stderrBuf.String()
		if errMsg == "" {
			errMsg = execErr.Error()
		}
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("OCR 推理执行异常: %s", errMsg))
		return
	}

	// Directly forward JSON response
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(outputBytes)
}

// Serve static assets from public/ directory
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

	if ext == ".html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, absTarget)
}

func main() {
	// Initialize MIME types
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

	// Resolve public dir from environment if specified
	if envPublic := os.Getenv("PUBLIC_DIR"); envPublic != "" {
		publicDir = envPublic
	}
	if envSample := os.Getenv("SAMPLE_PATH"); envSample != "" {
		samplePath = envSample
	}
	if envCli := os.Getenv("CLI_SCRIPT"); envCli != "" {
		cliScript = envCli
	}

	mux := http.NewServeMux()

	// 1. API Endpoints
	mux.HandleFunc("/api/v1/health", handleHealth)
	mux.HandleFunc("/api/v1/sample", handleSample)
	mux.HandleFunc("/api/v1/ocr/recognitions", handleOcr)
	mux.HandleFunc("/api/v1/ocr", handleOcr)

	// 2. Static Web Frontend
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
