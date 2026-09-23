package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"
)

// BGRImage represents an uncompressed image with interleaved BGR8 pixels.
type BGRImage struct {
	Width  int
	Height int
	Pixels []byte
}

// IsImageBytes checks if the raw bytes start with common image format magic bytes (PNG, JPEG, GIF, BMP, WebP)
func IsImageBytes(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// PNG: 0x89 0x50 0x4E 0x47
	if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return true
	}
	// JPEG: 0xFF 0xD8 0xFF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	// BMP: "BM"
	if data[0] == 0x42 && data[1] == 0x4D {
		return true
	}
	// GIF: "GIF"
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return true
	}
	// WebP: "RIFF....WEBP"
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	return false
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

	return DecodeRawBytesToBGR(rawBytes)
}

// DecodeRawBytesToBGR decodes uncompressed/compressed raw byte streams (PNG, JPEG, BMP) into BGRImage
func DecodeRawBytesToBGR(rawBytes []byte) (*BGRImage, error) {
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

// CropBGR extracts a rectangular region [x, y, w, h] from BGRImage into a new BGRImage.
// Coordinates outside image bounds are automatically clamped.
func (img *BGRImage) CropBGR(x, y, w, h int) (*BGRImage, error) {
	if img == nil || len(img.Pixels) == 0 {
		return nil, errors.New("cannot crop from empty BGR image")
	}

	// Clamp boundaries
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x >= img.Width || y >= img.Height {
		return nil, fmt.Errorf("crop origin (%d, %d) is outside image bounds (%dx%d)", x, y, img.Width, img.Height)
	}
	if x+w > img.Width {
		w = img.Width - x
	}
	if y+h > img.Height {
		h = img.Height - y
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid crop dimensions: %dx%d", w, h)
	}

	croppedPixels := make([]byte, w*h*3)
	srcStride := img.Width * 3
	dstStride := w * 3

	for row := 0; row < h; row++ {
		srcOffset := (y+row)*srcStride + x*3
		dstOffset := row * dstStride
		copy(croppedPixels[dstOffset:dstOffset+dstStride], img.Pixels[srcOffset:srcOffset+dstStride])
	}

	return &BGRImage{
		Width:  w,
		Height: h,
		Pixels: croppedPixels,
	}, nil
}

// ToRGBA converts continuous BGR8 bytes back to standard Go *image.RGBA
func (img *BGRImage) ToRGBA() *image.RGBA {
	if img == nil || img.Width <= 0 || img.Height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}

	rgba := image.NewRGBA(image.Rect(0, 0, img.Width, img.Height))
	for y := 0; y < img.Height; y++ {
		srcRow := y * img.Width * 3
		dstRow := y * rgba.Stride
		for x := 0; x < img.Width; x++ {
			s := srcRow + x*3
			d := dstRow + x*4
			rgba.Pix[d] = img.Pixels[s+2]   // R
			rgba.Pix[d+1] = img.Pixels[s+1] // G
			rgba.Pix[d+2] = img.Pixels[s]   // B
			rgba.Pix[d+3] = 255            // A
		}
	}
	return rgba
}

// EncodePNG encodes BGRImage to high-quality PNG bytes
func (img *BGRImage) EncodePNG() ([]byte, error) {
	if img == nil || len(img.Pixels) == 0 {
		return nil, errors.New("cannot encode empty BGR image to PNG")
	}

	rgba := img.ToRGBA()
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, fmt.Errorf("png.Encode failed: %w", err)
	}
	return buf.Bytes(), nil
}

// FindTightContentBounds scans the specified sub-rectangle for foreground pixels (lines, shapes, drawings)
// and returns a content-fitted bounding box [tightX, tightY, tightW, tightH] with breathing padding
func (img *BGRImage) FindTightContentBounds(x, y, w, h int, padding int) [4]int {
	if img == nil || len(img.Pixels) == 0 || w <= 0 || h <= 0 {
		return [4]int{x, y, w, h}
	}

	startX := max(0, x)
	startY := max(0, y)
	endX := min(img.Width, x+w)
	endY := min(img.Height, y+h)

	if startX >= endX || startY >= endY {
		return [4]int{x, y, w, h}
	}

	minX, minY := endX, endY
	maxX, maxY := startX, startY
	hasForeground := false
	stride := img.Width * 3

	// Step size 2 for sub-millisecond execution (<0.3ms)
	step := 2
	for r := startY; r < endY; r += step {
		rowOffset := r * stride
		for c := startX; c < endX; c += step {
			off := rowOffset + c*3
			b := int(img.Pixels[off])
			g := int(img.Pixels[off+1])
			red := int(img.Pixels[off+2])

			// Foreground pixel: non-white content (lines, curves, labels, ink)
			if (b < 238 || g < 238 || red < 238) && (b+g+red < 705) {
				hasForeground = true
				if c < minX {
					minX = c
				}
				if c > maxX {
					maxX = c
				}
				if r < minY {
					minY = r
				}
				if r > maxY {
					maxY = r
				}
			}
		}
	}

	if !hasForeground || minX >= maxX || minY >= maxY {
		return [4]int{x, y, w, h}
	}

	tightX := max(0, minX-padding)
	tightY := max(0, minY-padding)
	tightW := min(img.Width-tightX, (maxX-minX)+padding*2)
	tightH := min(img.Height-tightY, (maxY-minY)+padding*2)

	return [4]int{tightX, tightY, tightW, tightH}
}


