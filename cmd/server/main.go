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
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

var (
	startTime  = time.Now()
	publicDir  = "./public"
	samplePath = "./test/fixtures/sample.png"
	cliScript  = "./src/cli.js"
	reqCounter uint64
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
			"service":     "dsh-lw-ppocr-web",
			"version":     "3.0.0",
			"mode":        "ultra-low-memory-on-demand",
			"engine":      "ready",
			"uptime":      uptime,
			"targetMem":   "~10MB resident",
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

// OCR Single Recognition Handler (On-demand execution, zero idle memory!)
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

	reqID := fmt.Sprintf("req_%d_%d", time.Now().UnixMilli(), atomic.AddUint64(&reqCounter, 1))
	rawMap["id"] = reqID
	reqBytes, _ := json.Marshal(rawMap)

	// Launch on-demand isolated Node CLI process (exits immediately after inference)
	cmd := exec.Command("node", "--max-old-space-size=48", "--optimize_for_size", cliScript)
	cmd.Stdin = bytes.NewReader(reqBytes)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		errMsg := stderrBuf.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		log.Printf("[ocr-err] CLI execution failed: %s\n", errMsg)
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("OCR 推理执行异常: %s", errMsg))
		return
	}

	// Immediate Memory Reclaim: force GC and return heap pages to OS
	go func() {
		runtime.GC()
		debug.FreeOSMemory()
	}()

	respBytes := bytes.TrimSpace(stdoutBuf.Bytes())
	var parsedResp JsonResponse
	if err := json.Unmarshal(respBytes, &parsedResp); err == nil && parsedResp.Code != 0 {
		sendJSON(w, parsedResp.Code, parsedResp)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBytes)
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
	if envCli := os.Getenv("CLI_SCRIPT"); envCli != "" {
		cliScript = envCli
	}

	log.Println("[server] Ultra-Low-Memory mode enabled: Zero persistent background workers.")
	log.Println("[server] Resident memory target: ~10MB. OCR inference executed on-demand.")

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
