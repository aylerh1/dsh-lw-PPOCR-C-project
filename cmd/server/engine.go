package main

/*
#cgo CFLAGS: -I${SRCDIR}/../../csrc/include
#cgo LDFLAGS: -L${SRCDIR}/../../csrc/build -llw_ppocr_c_static -lpthread -lm
#include "lw_infer.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

type NativeEngineConfig struct {
	ModelDir      string
	UseClassifier bool
	WorkerCount   int
	RecMaxWidth   int
}

type OcrLine struct {
	Text  string    `json:"text"`
	Score float64   `json:"score"`
	Box   [4][2]int `json:"box"`
}

type OcrMeta struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ReadingOrder string `json:"readingOrder"`
}

type OcrResult struct {
	Text       string    `json:"text"`
	DurationMs int64     `json:"durationMs"`
	Lines      []OcrLine `json:"lines"`
	Meta       OcrMeta   `json:"meta"`
}

type NativeOcrEngine struct {
	handle   *C.lw_ocr
	info     C.lw_ocr_info
	linesBuf []C.lw_ocr_line
	textBuf  []C.char
	mu       sync.Mutex
}

// NewNativeOcrEngine initializes the C native lw.PPOCR.C runtime
func NewNativeOcrEngine(cfg NativeEngineConfig) (*NativeOcrEngine, error) {
	detPath := filepath.Join(cfg.ModelDir, "det.lwm")
	clsPath := filepath.Join(cfg.ModelDir, "cls.lwm")
	recPath := filepath.Join(cfg.ModelDir, "rec.lwm")
	dictPath := filepath.Join(cfg.ModelDir, "ppocr_keys.txt")

	cDetPath := C.CString(detPath)
	defer C.free(unsafe.Pointer(cDetPath))

	var cClsPath *C.char
	if cfg.UseClassifier {
		cClsPath = C.CString(clsPath)
		defer C.free(unsafe.Pointer(cClsPath))
	}

	cRecPath := C.CString(recPath)
	defer C.free(unsafe.Pointer(cRecPath))

	cDictPath := C.CString(dictPath)
	defer C.free(unsafe.Pointer(cDictPath))

	var options C.lw_ocr_options
	C.lw_ocr_options_init(&options)

	if cfg.UseClassifier {
		options.use_direction_classification = 1
	} else {
		options.use_direction_classification = 0
	}

	if cfg.WorkerCount > 0 {
		options.worker_count = C.uint32_t(cfg.WorkerCount)
	}

	if cfg.RecMaxWidth > 0 {
		options.recognizer.target_width = C.uint32_t(cfg.RecMaxWidth)
	}

	var handle *C.lw_ocr
	var errObj C.lw_error
	C.lw_error_init(&errObj)

	status := C.lw_ocr_create(cDetPath, cClsPath, cRecPath, cDictPath, &options, &handle, &errObj)
	if status != C.LW_STATUS_OK || handle == nil {
		errMsg := C.GoString(&errObj.message[0])
		statusStr := C.GoString(C.lw_status_string(status))
		return nil, fmt.Errorf("native lw_ocr_create failed (%s): %s", statusStr, errMsg)
	}

	var info C.lw_ocr_info
	C.lw_ocr_info_init(&info)
	status = C.lw_ocr_get_info(handle, &info)
	if status != C.LW_STATUS_OK {
		C.lw_ocr_free(handle)
		return nil, fmt.Errorf("lw_ocr_get_info failed with status %d", int(status))
	}

	maxLines := int(info.max_line_capacity)
	maxText := int(info.max_text_capacity)
	if maxLines <= 0 {
		maxLines = 1024
	}
	if maxText <= 0 {
		maxText = 1024 * 1024
	}

	return &NativeOcrEngine{
		handle:   handle,
		info:     info,
		linesBuf: make([]C.lw_ocr_line, maxLines),
		textBuf:  make([]C.char, maxText),
	}, nil
}

// Recognize runs high-performance native inference on an interleaved BGR8 image
func (e *NativeOcrEngine) Recognize(bgr *BGRImage, recMaxWidth int) (*OcrResult, error) {
	if bgr == nil || len(bgr.Pixels) == 0 {
		return nil, errors.New("empty BGR image buffer")
	}

	expectedLen := bgr.Width * bgr.Height * 3
	if len(bgr.Pixels) != expectedLen {
		return nil, fmt.Errorf("BGR buffer size mismatch: expected %d, got %d", expectedLen, len(bgr.Pixels))
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handle == nil {
		return nil, errors.New("native OCR engine is closed")
	}

	startTime := time.Now()

	var result C.lw_ocr_result
	var errObj C.lw_error
	C.lw_ocr_result_init(&result)
	C.lw_error_init(&errObj)

	cSource := (*C.uint8_t)(unsafe.Pointer(&bgr.Pixels[0]))
	cSourceLen := C.uint64_t(len(bgr.Pixels))
	cWidth := C.uint32_t(bgr.Width)
	cHeight := C.uint32_t(bgr.Height)
	cStride := C.uint32_t(bgr.Width * 3)

	cLines := (*C.lw_ocr_line)(unsafe.Pointer(&e.linesBuf[0]))
	cLineCap := C.uint32_t(len(e.linesBuf))

	cText := (*C.char)(unsafe.Pointer(&e.textBuf[0]))
	cTextCap := C.uint64_t(len(e.textBuf))

	status := C.lw_ocr_run_bgr_u8(
		e.handle,
		cSource,
		cSourceLen,
		cWidth,
		cHeight,
		cStride,
		cLines,
		cLineCap,
		cText,
		cTextCap,
		&result,
		&errObj,
	)

	if status != C.LW_STATUS_OK {
		errMsg := C.GoString(&errObj.message[0])
		statusStr := C.GoString(C.lw_status_string(status))
		return nil, fmt.Errorf("native lw_ocr_run_bgr_u8 failed (%s): %s", statusStr, errMsg)
	}

	durationMs := time.Since(startTime).Milliseconds()

	lineCount := int(result.line_count)
	lines := make([]OcrLine, 0, lineCount)
	var fullTextBuilder strings.Builder

	for i := 0; i < lineCount; i++ {
		cLine := e.linesBuf[i]

		var lineText string
		if cLine.text_length > 0 && cLine.text_offset < C.uint64_t(len(e.textBuf)) {
			lineText = C.GoStringN((*C.char)(unsafe.Pointer(&e.textBuf[cLine.text_offset])), C.int(cLine.text_length))
		}

		// Calculate confidence score (average of DET and REC scores)
		detScore := float64(cLine.box.score)
		recScore := float64(cLine.recognition_score)
		confidence := math.Round(((detScore+recScore)/2.0)*1000) / 1000

		// 4 corner bounding box: [[x1, y1], [x2, y2], [x3, y3], [x4, y4]]
		box := [4][2]int{
			{int(math.Round(float64(cLine.box.x1))), int(math.Round(float64(cLine.box.y1)))},
			{int(math.Round(float64(cLine.box.x2))), int(math.Round(float64(cLine.box.y2)))},
			{int(math.Round(float64(cLine.box.x3))), int(math.Round(float64(cLine.box.y3)))},
			{int(math.Round(float64(cLine.box.x4))), int(math.Round(float64(cLine.box.y4)))},
		}

		lines = append(lines, OcrLine{
			Text:  lineText,
			Score: confidence,
			Box:   box,
		})

		if i > 0 {
			fullTextBuilder.WriteString("\n")
		}
		fullTextBuilder.WriteString(lineText)
	}

	return &OcrResult{
		Text:       fullTextBuilder.String(),
		DurationMs: durationMs,
		Lines:      lines,
		Meta: OcrMeta{
			Width:        bgr.Width,
			Height:       bgr.Height,
			ReadingOrder: "horizontal-ltr",
		},
	}, nil
}

// Close releases native C memory handles
func (e *NativeOcrEngine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handle != nil {
		C.lw_ocr_free(e.handle)
		e.handle = nil
	}
}
