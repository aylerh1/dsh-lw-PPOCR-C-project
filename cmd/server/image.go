package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"
)

// BGRImage represents an uncompressed image with interleaved BGR8 pixels.
type BGRImage struct {
	Width  int
	Height int
	Pixels []byte
}

// DecodeImageToBGR decodes an input (file path, data URI, or base64 string)
// into continuous interleaved BGR8 bytes required by lw.PPOCR.C ABI.
func DecodeImageToBGR(input string) (*BGRImage, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, errors.New("empty image input")
	}

	var rawBytes []byte
	var err error

	if strings.HasPrefix(trimmed, "data:") {
		// Handle Data URI format: data:image/png;base64,...
		commaIdx := strings.Index(trimmed, ",")
		if commaIdx == -1 {
			return nil, errors.New("invalid data URI format: missing comma")
		}
		rawBytes, err = base64.StdEncoding.DecodeString(trimmed[commaIdx+1:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 data URI: %w", err)
		}
	} else if fi, statErr := os.Stat(trimmed); statErr == nil && !fi.IsDir() {
		// Handle local file path
		rawBytes, err = os.ReadFile(trimmed)
		if err != nil {
			return nil, fmt.Errorf("failed to read image file %q: %w", trimmed, err)
		}
	} else {
		// Attempt standard Base64 decoding
		rawBytes, err = base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			// Try URL-safe base64
			rawBytes, err = base64.URLEncoding.DecodeString(trimmed)
			if err != nil {
				return nil, fmt.Errorf("input is neither a valid file path nor valid Base64: %w", err)
			}
		}
	}

	if len(rawBytes) == 0 {
		return nil, errors.New("decoded image payload is empty")
	}

	// 1. Check for BMP format magic "BM" (0x42, 0x4D)
	if len(rawBytes) >= 54 && rawBytes[0] == 0x42 && rawBytes[1] == 0x4D {
		return decodeBmpToBGR(rawBytes)
	}

	// 2. Decode standard formats (PNG, JPEG) via Go standard library
	img, _, err := image.Decode(bytes.NewReader(rawBytes))
	if err != nil {
		return nil, fmt.Errorf("image.Decode failed: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}

	bgr := make([]byte, width*height*3)

	// High-speed pixel extraction
	switch src := img.(type) {
	case *image.RGBA:
		for y := 0; y < height; y++ {
			srcRow := src.PixOffset(bounds.Min.X, bounds.Min.Y+y)
			dstRow := y * width * 3
			for x := 0; x < width; x++ {
				s := srcRow + x*4
				d := dstRow + x*3
				bgr[d] = src.Pix[s+2]     // B
				bgr[d+1] = src.Pix[s+1]   // G
				bgr[d+2] = src.Pix[s]     // R
			}
		}
	case *image.NRGBA:
		for y := 0; y < height; y++ {
			srcRow := src.PixOffset(bounds.Min.X, bounds.Min.Y+y)
			dstRow := y * width * 3
			for x := 0; x < width; x++ {
				s := srcRow + x*4
				d := dstRow + x*3
				bgr[d] = src.Pix[s+2]     // B
				bgr[d+1] = src.Pix[s+1]   // G
				bgr[d+2] = src.Pix[s]     // R
			}
		}
	default:
		// Generic fallback path for Gray, YCbCr, etc.
		for y := 0; y < height; y++ {
			dstRow := y * width * 3
			for x := 0; x < width; x++ {
				r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				d := dstRow + x*3
				bgr[d] = byte(b >> 8)
				bgr[d+1] = byte(g >> 8)
				bgr[d+2] = byte(r >> 8)
			}
		}
	}

	return &BGRImage{
		Width:  width,
		Height: height,
		Pixels: bgr,
	}, nil
}

// decodeBmpToBGR decodes uncompressed 24-bit / 32-bit BMP into BGR8 bytes
func decodeBmpToBGR(buf []byte) (*BGRImage, error) {
	if len(buf) < 54 {
		return nil, errors.New("BMP data too short")
	}

	dataOffset := int(uint32(buf[10]) | uint32(buf[11])<<8 | uint32(buf[12])<<16 | uint32(buf[13])<<24)
	width := int(int32(uint32(buf[18]) | uint32(buf[19])<<8 | uint32(buf[20])<<16 | uint32(buf[21])<<24))
	rawHeight := int(int32(uint32(buf[22]) | uint32(buf[23])<<8 | uint32(buf[24])<<16 | uint32(buf[25])<<24))
	bpp := int(uint16(buf[28]) | uint16(buf[29])<<8)

	isTopDown := rawHeight < 0
	height := rawHeight
	if height < 0 {
		height = -height
	}

	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid BMP dimensions: %dx%d", width, height)
	}

	if bpp != 24 && bpp != 32 {
		return nil, fmt.Errorf("unsupported BMP bits-per-pixel: %d (only 24 and 32 are supported)", bpp)
	}

	rowSize := ((bpp*width + 31) / 32) * 4
	bgr := make([]byte, width*height*3)

	for y := 0; y < height; y++ {
		srcY := y
		if !isTopDown {
			srcY = height - 1 - y
		}
		srcRow := dataOffset + srcY*rowSize
		dstRow := y * width * 3

		if bpp == 24 {
			for x := 0; x < width; x++ {
				s := srcRow + x*3
				d := dstRow + x*3
				if s+2 < len(buf) {
					bgr[d] = buf[s]     // B
					bgr[d+1] = buf[s+1] // G
					bgr[d+2] = buf[s+2] // R
				}
			}
		} else { // 32 bpp
			for x := 0; x < width; x++ {
				s := srcRow + x*4
				d := dstRow + x*3
				if s+2 < len(buf) {
					bgr[d] = buf[s]     // B
					bgr[d+1] = buf[s+1] // G
					bgr[d+2] = buf[s+2] // R
				}
			}
		}
	}

	return &BGRImage{
		Width:  width,
		Height: height,
		Pixels: bgr,
	}, nil
}
