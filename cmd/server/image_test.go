package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeImageToBGR_FilePath(t *testing.T) {
	samplePath := filepath.Join("..", "..", "test", "fixtures", "sample.png")
	if _, err := os.Stat(samplePath); err != nil {
		t.Fatalf("sample image not found: %v", err)
	}

	bgr, err := DecodeImageToBGR(samplePath)
	if err != nil {
		t.Fatalf("DecodeImageToBGR from path failed: %v", err)
	}

	if bgr.Width != 254 || bgr.Height != 119 {
		t.Errorf("expected 254x119, got %dx%d", bgr.Width, bgr.Height)
	}

	expectedLen := 254 * 119 * 3
	if len(bgr.Pixels) != expectedLen {
		t.Errorf("expected pixel buffer length %d, got %d", expectedLen, len(bgr.Pixels))
	}
}

func TestDecodeImageToBGR_DataURI(t *testing.T) {
	samplePath := filepath.Join("..", "..", "test", "fixtures", "sample.png")
	data, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read sample failed: %v", err)
	}

	encoded := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	bgr, err := DecodeImageToBGR(encoded)
	if err != nil {
		t.Fatalf("DecodeImageToBGR from DataURI failed: %v", err)
	}

	if bgr.Width != 254 || bgr.Height != 119 {
		t.Errorf("expected 254x119, got %dx%d", bgr.Width, bgr.Height)
	}
}

func TestDecodeImageToBGR_RawBase64(t *testing.T) {
	samplePath := filepath.Join("..", "..", "test", "fixtures", "sample.png")
	data, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read sample failed: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	bgr, err := DecodeImageToBGR(encoded)
	if err != nil {
		t.Fatalf("DecodeImageToBGR from raw base64 failed: %v", err)
	}

	if bgr.Width != 254 || bgr.Height != 119 {
		t.Errorf("expected 254x119, got %dx%d", bgr.Width, bgr.Height)
	}
}

func TestDecodeImageToBGR_InvalidInput(t *testing.T) {
	_, err := DecodeImageToBGR("not_a_valid_image_string")
	if err == nil {
		t.Errorf("expected error for invalid input, got nil")
	}
}
