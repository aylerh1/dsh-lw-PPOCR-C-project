# Native C/AVX2 OCR Acceleration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the on-demand Node.js WASM CLI execution with a native C/AVX2 compiled engine linked via Go CGO, achieving a 10x-15x speedup (from 1.x seconds down to ~50-100ms per page), resident memory < 30MB, and a stripped Docker image size (~25MB).

**Architecture:** Integrate pure C runtime `lw.PPOCR.C` compiled with `-O3 -mavx2 -mfma -pthread` into a static library `liblw_ppocr_c_static.a`. In Go, implement fast native image decoding (5-15ms) to interleaved BGR8, and bind the native C ABI `lw_ocr_run_bgr_u8` via CGO with zero-copy memory pointers. Wrap this in the existing Go HTTP server to maintain 100% API and Web UI compatibility.

**Tech Stack:** C11, AVX2 SIMD, Go 1.23+ (CGO, net/http, image/png, image/jpeg), Alpine Linux, Docker.

## Global Constraints
- All debugging and tests MUST run inside Docker containers (Docker 优先调试).
- Never use `docker system prune` or `docker image prune -a`.
- Do not run local host `go` or `node` directly; run via Docker containers.
- PowerShell syntax: no `&&` or `||`, use `;` for chaining.
- Output conclusions and reports in Chinese.

---

### Task 1: Integrate `lw.PPOCR.C` C Sources and Setup Native Build (`csrc/`)

**Files:**
- Create: `csrc/` (containing `include/`, `src/`, `CMakeLists.txt`)
- Test: Docker command building `csrc/build/liblw_ppocr_c_static.a`

**Interfaces:**
- Produces: `csrc/build/liblw_ppocr_c_static.a` and `csrc/include/lw_infer.h`

- [ ] **Step 1: Fetch and vendor `lw.PPOCR.C` sources into `csrc/`**

Run container command to clone and copy `include/`, `src/`, and `CMakeLists.txt` to `./csrc`:
```powershell
docker run --rm -v "${PWD}:/workspace" alpine sh -c "apk add --no-cache git; git clone --depth 1 https://github.com/lxw112190/lw.PPOCR.C.git /tmp/lwppocr; rm -rf /workspace/csrc; mkdir -p /workspace/csrc; cp -r /tmp/lwppocr/include /workspace/csrc/; cp -r /tmp/lwppocr/src /workspace/csrc/; cp /tmp/lwppocr/CMakeLists.txt /workspace/csrc/"
```

- [ ] **Step 2: Verify `csrc/` compilation inside Docker**

Run:
```powershell
docker run --rm -v "${PWD}:/workspace" alpine sh -c "apk add --no-cache build-base cmake; cd /workspace/csrc; cmake -B build -DCMAKE_BUILD_TYPE=Release -DLW_RUNTIME_ONLY=ON -DLW_BUILD_HTTP_DEMO=OFF; cmake --build build --target lw_ppocr_c -j4; ls -lh build/liblw_ppocr_c_static.a"
```
Expected: `liblw_ppocr_c_static.a` successfully built (~1MB static archive).

- [ ] **Step 3: Commit vendor sources**

```powershell
git add csrc/
git commit -m "feat: vendor lw.PPOCR.C source code in csrc/"
```

---

### Task 2: Implement High-Speed Native Go Image Decoder (`cmd/server/image.go`)

**Files:**
- Create: `cmd/server/image.go`
- Test: `cmd/server/image_test.go`

**Interfaces:**
- Produces:
  - `DecodeImageToBGR(input string) (*BGRImage, error)`
  - `type BGRImage struct { Width int; Height int; Pixels []byte }`
- Consumes: Base64 string, Data URI (`data:image/png;base64,...`), or local image file path.

- [ ] **Step 1: Write test `cmd/server/image_test.go`**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeImageToBGR(t *testing.T) {
	samplePath := "../../test/fixtures/sample.png"
	if _, err := os.Stat(samplePath); err != nil {
		t.Fatalf("sample image not found: %v", err)
	}

	bgr, err := DecodeImageToBGR(samplePath)
	if err != nil {
		t.Fatalf("DecodeImageToBGR failed: %v", err)
	}

	if bgr.Width <= 0 || bgr.Height <= 0 {
		t.Errorf("invalid dimensions: %dx%d", bgr.Width, bgr.Height)
	}

	expectedLen := bgr.Width * bgr.Height * 3
	if len(bgr.Pixels) != expectedLen {
		t.Errorf("expected pixel buffer length %d, got %d", expectedLen, len(bgr.Pixels))
	}
}
```

- [ ] **Step 2: Implement `cmd/server/image.go`**

Implement image decoding with standard `image/png`, `image/jpeg`, `image/draw` and BMP handling, extracting continuous interleaved BGR8 slice.

- [ ] **Step 3: Run test in Docker**

```powershell
docker run --rm -v "${PWD}:/app" -w /app golang:1.24-alpine go test -v ./cmd/server/... -run TestDecodeImageToBGR
```
Expected: PASS

- [ ] **Step 4: Commit**

```powershell
git add cmd/server/image.go cmd/server/image_test.go
git commit -m "feat: implement high-speed Go image decoder to interleaved BGR8"
```

---

### Task 3: Implement Go CGO Native OCR Engine Wrapper (`cmd/server/engine.go`)

**Files:**
- Create: `cmd/server/engine.go`
- Test: `cmd/server/engine_test.go`

**Interfaces:**
- Produces:
  - `type NativeOcrEngine struct`
  - `NewNativeOcrEngine(modelDir string, workers int, useCls bool) (*NativeOcrEngine, error)`
  - `(e *NativeOcrEngine) Recognize(bgr *BGRImage, recMaxWidth int) (*OcrResult, error)`
  - `(e *NativeOcrEngine) Close()`
  - `type OcrResult struct { Text string; Lines []OcrLine; DurationMs int64 }`
  - `type OcrLine struct { Text string; Score float64; Box [4][2]int }`
- Consumes: `BGRImage` from Task 2, `csrc/include/lw_infer.h`, `csrc/build/liblw_ppocr_c_static.a`.

- [ ] **Step 1: Write test `cmd/server/engine_test.go`**

Test engine initialization, inference on `sample.png`, and verification of recognized lines, scores, and bounding boxes.

- [ ] **Step 2: Implement `cmd/server/engine.go` with CGO flags**

```go
package main

/*
#cgo CFLAGS: -I../../csrc/include -O3 -mavx2 -mfma
#cgo LDFLAGS: -L../../csrc/build -llw_ppocr_c_static -lpthread -lm
#include "lw_infer.h"
#include <stdlib.h>
*/
import "C"
// ... CGO implementation
```

- [ ] **Step 3: Run test in Docker**

```powershell
docker run --rm -v "${PWD}:/app" -w /app alpine sh -c "apk add --no-cache build-base go cmake; cd /app/csrc; cmake -B build -DCMAKE_BUILD_TYPE=Release -DLW_RUNTIME_ONLY=ON -DLW_BUILD_HTTP_DEMO=OFF; cmake --build build --target lw_ppocr_c -j4; cd /app; go test -v ./cmd/server/... -run TestNativeOcrEngine"
```
Expected: PASS (inference duration ~15-25ms).

- [ ] **Step 4: Commit**

```powershell
git add cmd/server/engine.go cmd/server/engine_test.go
git commit -m "feat: implement Go CGO native OCR engine wrapper"
```

---

### Task 4: Integrate Native Engine into HTTP Server (`cmd/server/main.go`)

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `NativeOcrEngine` from Task 3, `DecodeImageToBGR` from Task 2.
- Produces: Updated `/api/v1/ocr/recognitions` handler returning instant JSON responses without spawning any CLI subprocesses.

- [ ] **Step 1: Update `cmd/server/main.go`**
  - Initialize singleton `NativeOcrEngine` in `main()` with graceful cleanup on shutdown.
  - In `handleOcr()`, parse request JSON, decode image via `DecodeImageToBGR()`, call `engine.Recognize()`, and return JSON response.
  - In `handleHealth()`, report `"engine": "native-c-avx2"`.

- [ ] **Step 2: Test HTTP Server in Docker**

Compile and run test against `/api/v1/health`, `/api/v1/sample`, and `/api/v1/ocr/recognitions`. Verify `durationMs < 50ms`.

- [ ] **Step 3: Commit**

```powershell
git add cmd/server/main.go
git commit -m "feat: integrate native CGO engine into HTTP server replacing Node CLI"
```

---

### Task 5: Multi-Stage Dockerfile & Full Benchmark

**Files:**
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Rewrite `Dockerfile` with multi-stage build**
  - Stage 1: `golang:1.24-alpine` + `build-base cmake git`. Builds `csrc/build/liblw_ppocr_c_static.a` and statically compiles `/build/server`.
  - Stage 2: Minimal `alpine:3.21` runtime (~7MB base). Copies `/build/server`, `models/` (or `vendor/lw-ppocr-wasm/`), and `public/`.
  - Eliminates Node.js completely from runtime image!

- [ ] **Step 2: Build container image with docker compose**

```powershell
docker compose build web-service
```
Expected: Build succeeds, image size is ~25MB (down from 155MB).

- [ ] **Step 3: Start container and run end-to-end benchmark**

```powershell
docker compose up -d web-service; curl.exe -s http://localhost:3003/api/v1/health
```
Send 10 requests to `/api/v1/ocr/recognitions`, measure latency and memory via `docker stats --no-stream`.

- [ ] **Step 4: Commit**

```powershell
git add Dockerfile docker-compose.yml
git commit -m "build: optimize Dockerfile with multi-stage native compilation and test"
```

---

### Task 6: Documentation and Optimization Logs Update

**Files:**
- Modify: `readme.md`
- Modify: `docs-files/优化.md`

- [ ] **Step 1: Update `docs-files/优化.md`**
  - Move the completed task to `## g1.5` under `# 已实现`.

- [ ] **Step 2: Update `readme.md`**
  - Update latency: from `1.x秒每页` to `18~100ms` (提速 10+ 倍).
  - Update image size: from `155M` to `~25M`.
  - Update memory: `~25MB` resident with AVX2 multi-threading.

- [ ] **Step 3: Commit**

```powershell
git add readme.md docs-files/优化.md
git commit -m "docs: update performance metrics, image size, and changelog"
```
