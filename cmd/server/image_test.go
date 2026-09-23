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

func TestCropBGR(t *testing.T) {
	samplePath := filepath.Join("..", "..", "test", "fixtures", "sample.png")
	bgr, err := DecodeImageToBGR(samplePath)
	if err != nil {
		t.Fatalf("DecodeImageToBGR failed: %v", err)
	}

	// Crop 100x50 region starting from (20, 10)
	crop, err := bgr.CropBGR(20, 10, 100, 50)
	if err != nil {
		t.Fatalf("CropBGR failed: %v", err)
	}

	if crop.Width != 100 || crop.Height != 50 {
		t.Errorf("expected crop size 100x50, got %dx%d", crop.Width, crop.Height)
	}

	if len(crop.Pixels) != 100*50*3 {
		t.Errorf("expected crop pixels 15000, got %d", len(crop.Pixels))
	}

	// Boundary clamping test
	clampedCrop, err := bgr.CropBGR(200, 100, 100, 50)
	if err != nil {
		t.Fatalf("CropBGR clamped failed: %v", err)
	}
	expectedClampedW := 254 - 200 // 54
	expectedClampedH := 119 - 100 // 19
	if clampedCrop.Width != expectedClampedW || clampedCrop.Height != expectedClampedH {
		t.Errorf("expected clamped size %dx%d, got %dx%d", expectedClampedW, expectedClampedH, clampedCrop.Width, clampedCrop.Height)
	}
}

