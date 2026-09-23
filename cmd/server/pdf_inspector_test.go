package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func createSampleTextPDF() []byte {
	return []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>
endobj
4 0 obj
<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>
endobj
5 0 obj
<< /Length 120 >>
stream
BT
/F1 16 Tf
1 0 0 1 50 750 Tm
(High-Performance Vector Document Analysis Engine with DeepSeek Harness) Tj
ET
BT
/F1 12 Tf
1 0 0 1 50 700 Tm
(AnyDoc PDF Inspector inspects streams directly without rasterization overhead.) Tj
ET
endstream
endobj
xref
0 6
0000000000 65535 f 
trailer
<< /Size 6 /Root 1 0 R >>
startxref
500
%%EOF`)
}

func createSampleScannedPDF() []byte {
	return []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /XObject << /Im1 4 0 R >> >> /Contents 5 0 R >>
endobj
4 0 obj
<< /Type /XObject /Subtype /Image /Width 600 /Height 800 /ColorSpace /DeviceRGB /BitsPerComponent 8 >>
stream
image-pixel-stream-placeholder
endstream
endobj
5 0 obj
<< /Length 20 >>
stream
q /Im1 Do Q
endstream
endobj
trailer
<< /Size 6 /Root 1 0 R >>
startxref
300
%%EOF`)
}

func TestPDFInspector(t *testing.T) {
	inspector := NewPDFInspector()

	t.Run("Inspect Vector Text-Based PDF", func(t *testing.T) {
		data := createSampleTextPDF()
		res, err := inspector.Inspect(data)
		if err != nil {
			t.Fatalf("Inspect text PDF failed: %v", err)
		}
		if !res.IsPDF {
			t.Errorf("Expected IsPDF=true, got false")
		}
		if res.NeedsOCR {
			t.Errorf("Expected NeedsOCR=false for text-based PDF, got true")
		}
		if res.PrimaryType != PageTypeTextBased {
			t.Errorf("Expected PageTypeTextBased, got %v", res.PrimaryType)
		}
		if res.TotalChars < 30 {
			t.Errorf("Expected chars >= 30, got %d", res.TotalChars)
		}
	})

	t.Run("Inspect Scanned Raster PDF", func(t *testing.T) {
		data := createSampleScannedPDF()
		res, err := inspector.Inspect(data)
		if err != nil {
			t.Fatalf("Inspect scanned PDF failed: %v", err)
		}
		if !res.IsPDF {
			t.Errorf("Expected IsPDF=true, got false")
		}
		if !res.NeedsOCR {
			t.Errorf("Expected NeedsOCR=true for scanned image PDF, got false")
		}
		if res.PrimaryType != PageTypeScanned {
			t.Errorf("Expected PageTypeScanned, got %v", res.PrimaryType)
		}
		if res.ImageCount == 0 {
			t.Errorf("Expected ImageCount > 0, got %d", res.ImageCount)
		}
	})

	t.Run("Extract Native Text Lines from Vector PDF", func(t *testing.T) {
		data := createSampleTextPDF()
		lines, fullText, err := inspector.ExtractNativeTextLines(data, 1190, 1684)
		if err != nil {
			t.Fatalf("ExtractNativeTextLines failed: %v", err)
		}
		if len(lines) == 0 {
			t.Fatalf("Expected lines > 0, got 0")
		}
		if !strings.Contains(fullText, "DeepSeek Harness") {
			t.Errorf("Expected text to contain 'DeepSeek Harness', got: %s", fullText)
		}
		t.Logf("Extracted %d lines directly from vector PDF, text sample: %s", len(lines), lines[0].Text)
	})
}

func TestAnyDocConverter(t *testing.T) {
	converter := NewAnyDocConverter()

	t.Run("Convert Plain Text / Markdown", func(t *testing.T) {
		sampleMD := "# Chapter 1: Architecture\n\nThis is paragraph one.\n\nThis is paragraph two."
		res, err := converter.Convert([]byte(sampleMD), "test.md", "text/markdown")
		if err != nil {
			t.Fatalf("Convert Markdown failed: %v", err)
		}
		if !strings.Contains(res.Markdown, "# Chapter 1: Architecture") {
			t.Errorf("Markdown conversion missing header: %s", res.Markdown)
		}
		if len(res.TextBlocks) < 3 {
			t.Errorf("Expected at least 3 text blocks, got %d", len(res.TextBlocks))
		}
	})

	t.Run("Convert HTML Document", func(t *testing.T) {
		sampleHTML := `<!DOCTYPE html>
<html>
<body>
  <h1>Technical Specification</h1>
  <p>First paragraph of the specification.</p>
  <table>
    <tr><th>Key</th><th>Value</th></tr>
    <tr><td>Engine</td><td>AVX2 C</td></tr>
  </table>
</body>
</html>`
		res, err := converter.Convert([]byte(sampleHTML), "doc.html", "text/html")
		if err != nil {
			t.Fatalf("Convert HTML failed: %v", err)
		}
		if !strings.Contains(res.Markdown, "# Technical Specification") {
			t.Errorf("HTML conversion missing header: %s", res.Markdown)
		}
		if len(res.Tables) == 0 {
			t.Fatalf("Expected at least 1 table extracted from HTML, got 0")
		}
		if !strings.Contains(res.Tables[0].Markdown, "AVX2 C") {
			t.Errorf("Expected table markdown to contain 'AVX2 C', got: %s", res.Tables[0].Markdown)
		}
	})

	t.Run("Convert DOCX Document", func(t *testing.T) {
		// Create in-memory minimal docx
		var docxBuf bytes.Buffer
		zw := zip.NewWriter(&docxBuf)
		w, err := zw.Create("word/document.xml")
		if err != nil {
			t.Fatalf("Failed to create zip entry: %v", err)
		}
		docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:pPr><w:pStyle w:val="Heading1"/></w:pPr>
      <w:r><w:t>Project Overview</w:t></w:r>
    </w:p>
    <w:p>
      <w:r><w:t>This is an automated AnyDoc document conversion pipeline.</w:t></w:r>
    </w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>Component</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>Status</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>AnyDoc</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>Active</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
  </w:body>
</w:document>`
		_, _ = w.Write([]byte(docXML))
		_ = zw.Close()

		res, err := converter.Convert(docxBuf.Bytes(), "spec.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		if err != nil {
			t.Fatalf("Convert DOCX failed: %v", err)
		}
		if !strings.Contains(res.Markdown, "# Project Overview") {
			t.Errorf("DOCX conversion missing title: %s", res.Markdown)
		}
		if len(res.Tables) == 0 {
			t.Fatalf("Expected 1 table in DOCX result, got 0")
		}
		t.Logf("DOCX successfully converted to Markdown:\n%s", res.Markdown)
	})

	t.Run("Convert CSV / TSV Document", func(t *testing.T) {
		sampleCSV := "Name,Role,Performance\nAlice,Architect,99.8\nBob,Engineer,98.5"
		res, err := converter.Convert([]byte(sampleCSV), "metrics.csv", "text/csv")
		if err != nil {
			t.Fatalf("Convert CSV failed: %v", err)
		}
		if len(res.Tables) == 0 {
			t.Fatalf("Expected 1 table block from CSV, got 0")
		}
		if !strings.Contains(res.Markdown, "Alice") || !strings.Contains(res.Markdown, "99.8") {
			t.Errorf("CSV Markdown missing data: %s", res.Markdown)
		}
		t.Logf("CSV Markdown output:\n%s", res.Markdown)
	})

	t.Run("Convert JSON Document", func(t *testing.T) {
		sampleJSON := `{"project":"dsh-lw-PPOCR-C","engine":"AVX2","features":["layout","ocr","anydoc"]}`
		res, err := converter.Convert([]byte(sampleJSON), "config.json", "application/json")
		if err != nil {
			t.Fatalf("Convert JSON failed: %v", err)
		}
		if !strings.Contains(res.Markdown, "```json") || !strings.Contains(res.Markdown, "dsh-lw-PPOCR-C") {
			t.Errorf("JSON Markdown missing formatted codeblock: %s", res.Markdown)
		}
		t.Logf("JSON Markdown output:\n%s", res.Markdown)
	})

	t.Run("Convert RTF Document", func(t *testing.T) {
		sampleRTF := `{\rtf1\ansi\deff0 {\fonttbl {\f0 Courier;}}\f0\fs24 Hello AnyDoc World!\par}`
		res, err := converter.Convert([]byte(sampleRTF), "memo.rtf", "application/rtf")
		if err != nil {
			t.Fatalf("Convert RTF failed: %v", err)
		}
		if !strings.Contains(res.Markdown, "Hello AnyDoc World!") {
			t.Errorf("RTF conversion missing text content: %s", res.Markdown)
		}
		t.Logf("RTF Markdown output:\n%s", res.Markdown)
	})

	t.Run("Convert XLSX Document", func(t *testing.T) {
		var xlsxBuf bytes.Buffer
		zw := zip.NewWriter(&xlsxBuf)

		// sharedStrings.xml
		sw, _ := zw.Create("xl/sharedStrings.xml")
		_, _ = sw.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="4" uniqueCount="4">
  <si><t>Model</t></si>
  <si><t>Latency</t></si>
  <si><t>PP-OCR</t></si>
  <si><t>15ms</t></si>
</sst>`))

		// sheet1.xml
		shw, _ := zw.Create("xl/worksheets/sheet1.xml")
		_, _ = shw.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="s"><v>0</v></c>
      <c r="B1" t="s"><v>1</v></c>
    </row>
    <row r="2">
      <c r="A2" t="s"><v>2</v></c>
      <c r="B2" t="s"><v>3</v></c>
    </row>
  </sheetData>
</worksheet>`))
		_ = zw.Close()

		res, err := converter.Convert(xlsxBuf.Bytes(), "bench.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		if err != nil {
			t.Fatalf("Convert XLSX failed: %v", err)
		}
		if len(res.Tables) == 0 {
			t.Fatalf("Expected 1 table in XLSX result, got 0")
		}
		if !strings.Contains(res.Markdown, "PP-OCR") || !strings.Contains(res.Markdown, "15ms") {
			t.Errorf("XLSX Markdown missing cell data: %s", res.Markdown)
		}
		t.Logf("XLSX Markdown output:\n%s", res.Markdown)
	})

	t.Run("Convert PPTX Document", func(t *testing.T) {
		var pptxBuf bytes.Buffer
		zw := zip.NewWriter(&pptxBuf)

		sw, _ := zw.Create("ppt/slides/slide1.xml")
		_, _ = sw.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld>
    <p:spTree>
      <p:sp>
        <p:txBody>
          <a:p><a:r><a:t>High-Performance OCR Architecture</a:t></a:r></a:p>
          <a:p><a:r><a:t>Ultra-low memory and sub-20ms inference speed.</a:t></a:r></a:p>
        </p:txBody>
      </p:sp>
    </p:spTree>
  </p:cSld>
</p:sld>`))
		_ = zw.Close()

		res, err := converter.Convert(pptxBuf.Bytes(), "slides.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
		if err != nil {
			t.Fatalf("Convert PPTX failed: %v", err)
		}
		if !strings.Contains(res.Markdown, "High-Performance OCR Architecture") {
			t.Errorf("PPTX Markdown missing slide text: %s", res.Markdown)
		}
		t.Logf("PPTX Markdown output:\n%s", res.Markdown)
	})
}
