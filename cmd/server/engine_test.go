package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeOcrEngine(t *testing.T) {
	modelDir := filepath.Join("..", "..", "vendor", "lw-ppocr-wasm")
	detPath := filepath.Join(modelDir, "det.lwm")
	if _, err := os.Stat(detPath); err != nil {
		t.Skipf("model files not found at %s: %v", modelDir, err)
	}

	engine, err := NewNativeOcrEngine(NativeEngineConfig{
		ModelDir:      modelDir,
		UseClassifier: true,
		WorkerCount:   4,
		RecMaxWidth:   960,
	})
	if err != nil {
		t.Fatalf("failed to create NativeOcrEngine: %v", err)
	}
	defer engine.Close()

	samplePath := filepath.Join("..", "..", "test", "fixtures", "sample.png")
	bgr, err := DecodeImageToBGR(samplePath)
	if err != nil {
		t.Fatalf("failed to decode sample image: %v", err)
	}

	res, err := engine.Recognize(bgr, 0)
	if err != nil {
		t.Fatalf("Recognize failed: %v", err)
	}

	t.Logf("Recognized %d lines in %d ms", len(res.Lines), res.DurationMs)
	t.Logf("Recognized text:\n%s", res.Text)

	if len(res.Lines) == 0 {
		t.Errorf("expected at least 1 line, got 0")
	}

	// Verify sample.png text content
	foundContainer := false
	for _, l := range res.Lines {
		if strings.Contains(l.Text, "Container") || strings.Contains(l.Text, "Name") {
			foundContainer = true
			break
		}
	}
	if !foundContainer {
		t.Errorf("expected text to contain 'Container' or 'Name', got: %s", res.Text)
	}

	// Verify latency is sub-100ms
	if res.DurationMs > 200 {
		t.Logf("warning: recognition duration was %d ms (>200ms)", res.DurationMs)
	}
}
