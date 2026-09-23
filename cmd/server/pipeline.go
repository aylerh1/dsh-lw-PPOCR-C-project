package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// LayoutCategory defines supported document structure labels
type LayoutCategory string

const (
	LayoutText    LayoutCategory = "text"
	LayoutTitle   LayoutCategory = "title"
	LayoutTable   LayoutCategory = "table"
	LayoutFormula LayoutCategory = "formula"
	LayoutFigure  LayoutCategory = "figure"
	LayoutHeader  LayoutCategory = "header"
	LayoutFooter  LayoutCategory = "footer"
)

// LayoutRegion represents a detected bounding region on the document page
type LayoutRegion struct {
	ID       int            `json:"id"`
	Label    LayoutCategory `json:"label"`
	Score    float64        `json:"score"`
	Box      [4][2]int      `json:"box"`               // Polygon 4 corner points [[x1,y1], [x2,y2], [x3,y3], [x4,y4]]
	Rect     [4]int         `json:"rect"`              // Bounding rect [x, y, w, h]
	OrderNum int            `json:"orderNum"`          // Natural reading order index
	Caption  string         `json:"caption,omitempty"` // Associated caption text
}

// TableCell describes an individual cell within a structured table
type TableCell struct {
	RowIdx  int       `json:"rowIdx"`
	ColIdx  int       `json:"colIdx"`
	RowSpan int       `json:"rowSpan"`
	ColSpan int       `json:"colSpan"`
	Box     [4][2]int `json:"box"`
	Text    string    `json:"text"`
	Score   float64   `json:"score"`
}

// TableBlock contains the complete recognition output of a table
type TableBlock struct {
	RegionID   int         `json:"regionId"`
	Box        [4][2]int   `json:"box"`
	HTML       string      `json:"html"`
	Markdown   string      `json:"markdown"`
	Rows       int         `json:"rows"`
	Cols       int         `json:"cols"`
	Cells      []TableCell `json:"cells"`
	DurationMs int64       `json:"durationMs"`
}

// FormulaBlock contains the LaTeX output for a math formula
type FormulaBlock struct {
	RegionID   int       `json:"regionId"`
	Box        [4][2]int `json:"box"`
	LaTeX      string    `json:"latex"`
	Score      float64   `json:"score"`
	IsInline   bool      `json:"isInline"`
	DurationMs int64     `json:"durationMs"`
}

// FigureBlock contains a cropped illustration/chart image
type FigureBlock struct {
	RegionID   int       `json:"regionId"`
	Box        [4][2]int `json:"box"`
	Filename   string    `json:"filename"` // e.g. "images/figure_1.png"
	Caption    string    `json:"caption"`
	DataURL    string    `json:"dataUrl"` // data:image/png;base64,... for inline display
	DurationMs int64     `json:"durationMs"`
}

// TextBlock represents recognized structured text within a layout region
type TextBlock struct {
	RegionID   int            `json:"regionId"`
	Label      LayoutCategory `json:"label"`
	Text       string         `json:"text"` // Paragraph-merged cohesive text
	Score      float64        `json:"score"`
	Box        [4][2]int      `json:"box"`
	Lines      []OcrLine      `json:"lines"`
	DurationMs int64          `json:"durationMs"`
}

// LatencyBreakdown tracks execution time across pipeline stages
type LatencyBreakdown struct {
	LayoutMs  int64 `json:"layoutMs"`
	TextMs    int64 `json:"textMs"`
	TableMs   int64 `json:"tableMs"`
	FormulaMs int64 `json:"formulaMs"`
	FigureMs  int64 `json:"figureMs"`
}

// DocumentResult represents the full multi-modal document understanding output
type DocumentResult struct {
	Text            string               `json:"text"`
	Markdown        string               `json:"markdown"`
	DurationMs      int64                `json:"durationMs"`
	TotalDurationMs int64                `json:"totalDurationMs"`
	Breakdown       LatencyBreakdown     `json:"breakdown"`
	Regions         []LayoutRegion       `json:"regions"`
	TextBlocks      []TextBlock          `json:"textBlocks"`
	Tables          []TableBlock         `json:"tables"`
	Formulas        []FormulaBlock       `json:"formulas"`
	Figures         []FigureBlock        `json:"figures"`
	Lines           []OcrLine            `json:"lines"`
	Inspector       *PDFInspectionResult `json:"inspector,omitempty"`
	Meta            OcrMeta              `json:"meta"`
}

// DocumentPipeline orchestrates document layout analysis, routing, and concurrent execution
type DocumentPipeline struct {
	ocrEngine       *NativeOcrEngine
	layoutEngine    *LayoutEngine
	tableEngine     *TableEngine
	formulaEngine   *FormulaEngine
	pdfInspector    *PDFInspector
	anydocConverter *AnyDocConverter
}

// NewDocumentPipeline initializes the multi-modal document analysis pipeline
func NewDocumentPipeline(ocr *NativeOcrEngine) *DocumentPipeline {
	return &DocumentPipeline{
		ocrEngine:       ocr,
		layoutEngine:    NewLayoutEngine(),
		tableEngine:     NewTableEngine(),
		formulaEngine:   NewFormulaEngine(),
		pdfInspector:    NewPDFInspector(),
		anydocConverter: NewAnyDocConverter(),
	}
}

// Process parses a full document image with layout detection, figure cropping, and routed recognition
func (p *DocumentPipeline) Process(bgr *BGRImage) (*DocumentResult, error) {
	if bgr == nil || len(bgr.Pixels) == 0 {
		return nil, fmt.Errorf("empty input image")
	}

	startTotal := time.Now()
	var breakdown LatencyBreakdown

	// Step 1: Document Layout Detection (~15-22ms)
	startLayout := time.Now()
	regions, allLines, err := p.layoutEngine.Detect(bgr, p.ocrEngine)
	breakdown.LayoutMs = time.Since(startLayout).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("layout detection failed: %w", err)
	}

	// Sort regions by natural reading order
	SortRegionsByReadingOrder(regions)
	for i := range regions {
		regions[i].OrderNum = i + 1
	}

	// Step 2: Route regions to respective engines concurrently
	var (
		textBlocks   []TextBlock
		tables       []TableBlock
		formulas     []FormulaBlock
		figures      []FigureBlock
		mu           sync.Mutex
		wg           sync.WaitGroup
	)

	startRouting := time.Now()
	var (
		textDuration    int64
		tableDuration   int64
		formulaDuration int64
		figureDuration  int64
	)

	// Collect figure regions to exclude their internal lines from regular text paragraphs
	var figureRegions []LayoutRegion
	for _, reg := range regions {
		if reg.Label == LayoutFigure {
			figureRegions = append(figureRegions, reg)
		}
	}

	figCounter := 0

	for _, region := range regions {
		reg := region // capture loop variable
		rect := reg.Rect
		subImg, cropErr := bgr.CropBGR(rect[0], rect[1], rect[2], rect[3])
		if cropErr != nil {
			continue
		}

		switch reg.Label {
		case LayoutTable:
			wg.Add(1)
			go func(r LayoutRegion, img *BGRImage) {
				defer wg.Done()
				t0 := time.Now()
				tbl, tblErr := p.tableEngine.ProcessTable(r, img, p.ocrEngine)
				d := time.Since(t0).Milliseconds()
				if tblErr == nil && tbl != nil {
					tbl.DurationMs = d
					mu.Lock()
					tables = append(tables, *tbl)
					tableDuration += d
					mu.Unlock()
				}
			}(reg, subImg)

		case LayoutFormula:
			wg.Add(1)
			go func(r LayoutRegion, img *BGRImage) {
				defer wg.Done()
				t0 := time.Now()
				fml, fmlErr := p.formulaEngine.RecognizeFormula(r, img)
				d := time.Since(t0).Milliseconds()
				if fmlErr == nil && fml != nil {
					fml.DurationMs = d
					mu.Lock()
					formulas = append(formulas, *fml)
					formulaDuration += d
					mu.Unlock()
				}
			}(reg, subImg)

		case LayoutFigure:
			figCounter++
			currFigIdx := figCounter
			wg.Add(1)
			go func(r LayoutRegion, idx int) {
				defer wg.Done()
				t0 := time.Now()

				// Pixel-level tight boundary auto-fitting (<0.3ms)
				tightRect := bgr.FindTightContentBounds(r.Rect[0], r.Rect[1], r.Rect[2], r.Rect[3], 12)
				tightImg, cropErr := bgr.CropBGR(tightRect[0], tightRect[1], tightRect[2], tightRect[3])
				if cropErr != nil {
					tightImg, _ = bgr.CropBGR(r.Rect[0], r.Rect[1], r.Rect[2], r.Rect[3])
				}

				pngBytes, encErr := tightImg.EncodePNG()
				d := time.Since(t0).Milliseconds()
				if encErr == nil && len(pngBytes) > 0 {
					dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
					filename := fmt.Sprintf("images/figure_%d.png", idx)
					caption := r.Caption
					if caption == "" {
						caption = fmt.Sprintf("插图 %d", idx)
					}
					fig := FigureBlock{
						RegionID:   r.ID,
						Box: [4][2]int{
							{tightRect[0], tightRect[1]},
							{tightRect[0] + tightRect[2], tightRect[1]},
							{tightRect[0] + tightRect[2], tightRect[1] + tightRect[3]},
							{tightRect[0], tightRect[1] + tightRect[3]},
						},
						Filename:   filename,
						Caption:    caption,
						DataURL:    dataURL,
						DurationMs: d,
					}
					mu.Lock()
					figures = append(figures, fig)
					figureDuration += d
					mu.Unlock()
				}
			}(reg, currFigIdx)

		default:
			// Text, Title, Header, Footer: Directly assign lines from allLines falling in this region
			wg.Add(1)
			go func(r LayoutRegion) {
				defer wg.Done()
				t0 := time.Now()

				// Find lines falling into this region geometrically
				var regionLines []OcrLine
				for _, line := range allLines {
					midY := (line.Box[0][1] + line.Box[2][1]) / 2
					midX := (line.Box[0][0] + line.Box[2][0]) / 2

					// Ensure this line is not part of any Figure
					inFigure := false
					for _, fig := range figureRegions {
						if midY >= fig.Rect[1]-4 && midY <= fig.Rect[1]+fig.Rect[3]+4 &&
							midX >= fig.Rect[0]-10 && midX <= fig.Rect[0]+fig.Rect[2]+10 {
							inFigure = true
							break
						}
					}
					if inFigure {
						continue
					}

					// Check if inside current region
					if midY >= r.Rect[1]-6 && midY <= r.Rect[1]+r.Rect[3]+6 &&
						midX >= r.Rect[0]-15 && midX <= r.Rect[0]+r.Rect[2]+15 {
						regionLines = append(regionLines, line)
					}
				}

				// Sort lines vertically
				sort.Slice(regionLines, func(a, b int) bool {
					return regionLines[a].Box[0][1] < regionLines[b].Box[0][1]
				})

				mergedText := MergeParagraphLines(regionLines)
				d := time.Since(t0).Milliseconds()

				tb := TextBlock{
					RegionID:   r.ID,
					Label:      r.Label,
					Text:       mergedText,
					Score:      r.Score,
					Box:        r.Box,
					Lines:      regionLines,
					DurationMs: d,
				}
				mu.Lock()
				textBlocks = append(textBlocks, tb)
				textDuration += d
				mu.Unlock()
			}(reg)
		}
	}

	wg.Wait()
	_ = time.Since(startRouting)

	breakdown.TextMs = textDuration
	breakdown.TableMs = tableDuration
	breakdown.FormulaMs = formulaDuration
	breakdown.FigureMs = figureDuration

	// Step 3: Reconstruct integrated Markdown following reading order with smart paragraph flow
	fullMarkdown := ReconstructMarkdown(regions, textBlocks, tables, formulas, figures)

	// Combine all text blocks into clean full text with double newlines
	var fullTextBuilder strings.Builder
	for _, tb := range textBlocks {
		if strings.TrimSpace(tb.Text) != "" {
			if fullTextBuilder.Len() > 0 {
				fullTextBuilder.WriteString("\n\n")
			}
			fullTextBuilder.WriteString(tb.Text)
		}
	}

	totalMs := time.Since(startTotal).Milliseconds()
	return &DocumentResult{
		Text:            fullTextBuilder.String(),
		Markdown:        fullMarkdown,
		DurationMs:      totalMs,
		TotalDurationMs: totalMs,
		Breakdown:       breakdown,
		Regions:         regions,
		TextBlocks:      textBlocks,
		Tables:          tables,
		Formulas:        formulas,
		Figures:         figures,
		Lines:           allLines,
		Meta: OcrMeta{
			Width:        bgr.Width,
			Height:       bgr.Height,
			ReadingOrder: "top-down-reading-order",
		},
	}, nil
}

// ProcessPDF parses a PDF document with AnyDoc pdf-inspector pre-classification and smart dual-track routing
func (p *DocumentPipeline) ProcessPDF(pdfBytes []byte) (*DocumentResult, error) {
	if len(pdfBytes) == 0 {
		return nil, errors.New("empty PDF payload")
	}

	startTotal := time.Now()
	insp, err := p.pdfInspector.Inspect(pdfBytes)
	if err != nil {
		return nil, fmt.Errorf("pdf-inspector failed: %w", err)
	}
	insp.DurationMs = time.Since(startTotal).Milliseconds()

	// Track 1: Scanned image PDF -> Requires full OCR workflow
	if insp.NeedsOCR {
		img, err := p.pdfInspector.ExtractPrimaryImage(pdfBytes)
		if err != nil {
			// Fallback: If no embedded image could be cleanly extracted, create blank BGR with dimensions
			w, h := insp.PageDimensions[0]*2, insp.PageDimensions[1]*2
			if w <= 0 || h <= 0 {
				w, h = 1190, 1684
			}
			img = &BGRImage{
				Width:  w,
				Height: h,
				Pixels: make([]byte, w*h*3),
			}
		}

		docResult, err := p.Process(img)
		if err != nil {
			return nil, err
		}
		docResult.Inspector = insp
		return docResult, nil
	}

	// Track 2: Text-based PDF (Vector text) -> Skip OCR, extract native text stream directly!
	targetW, targetH := insp.PageDimensions[0]*2, insp.PageDimensions[1]*2
	if targetW <= 0 || targetH <= 0 {
		targetW, targetH = 1190, 1684
	}

	nativeLines, _, err := p.pdfInspector.ExtractNativeTextLines(pdfBytes, targetW, targetH)
	if err != nil || len(nativeLines) == 0 {
		// Fallback to OCR if native extraction got no tokens
		insp.NeedsOCR = true
		img, _ := p.pdfInspector.ExtractPrimaryImage(pdfBytes)
		if img != nil {
			docRes, err := p.Process(img)
			if err == nil {
				docRes.Inspector = insp
				return docRes, nil
			}
		}
	}

	// Perform Layout Analysis on the native lines directly
	regions := p.layoutEngine.clusterAndClassifyRegions(nil, targetW, targetH, nativeLines)
	SortRegionsByReadingOrder(regions)
	for i := range regions {
		regions[i].OrderNum = i + 1
	}

	// Route regions to appropriate structures
	var textBlocks []TextBlock
	var tables []TableBlock
	var formulas []FormulaBlock
	var figures []FigureBlock

	for _, reg := range regions {
		switch reg.Label {
		case LayoutTable:
			// Extract structured table directly from vector lines
			tbl := ExtractTableFromVectorLines(nativeLines, reg.Rect, reg.ID)
			tables = append(tables, tbl)
		case LayoutFormula:
			// Extract formula latex
			fmlText := strings.TrimSpace(reg.Caption)
			if fmlText == "" {
				// Search lines inside formula region
				for _, line := range nativeLines {
					midY := (line.Box[0][1] + line.Box[2][1]) / 2
					midX := (line.Box[0][0] + line.Box[2][0]) / 2
					if midY >= reg.Rect[1]-5 && midY <= reg.Rect[1]+reg.Rect[3]+5 &&
						midX >= reg.Rect[0]-10 && midX <= reg.Rect[0]+reg.Rect[2]+10 {
						fmlText += " " + strings.TrimSpace(line.Text)
					}
				}
				fmlText = strings.TrimSpace(fmlText)
			}
			formulas = append(formulas, FormulaBlock{
				RegionID: reg.ID,
				Box:      reg.Box,
				LaTeX:    fmlText,
				Score:    1.0,
			})
		case LayoutFigure:
			figures = append(figures, FigureBlock{
				RegionID: reg.ID,
				Box:      reg.Box,
				Filename: fmt.Sprintf("images/figure_%d.png", reg.ID),
				Caption:  reg.Caption,
			})
		default: // LayoutText, LayoutTitle, LayoutHeader, LayoutFooter
			var regLines []OcrLine
			for _, line := range nativeLines {
				midY := (line.Box[0][1] + line.Box[2][1]) / 2
				midX := (line.Box[0][0] + line.Box[2][0]) / 2
				if midY >= reg.Rect[1]-10 && midY <= reg.Rect[1]+reg.Rect[3]+10 &&
					midX >= reg.Rect[0]-15 && midX <= reg.Rect[0]+reg.Rect[2]+15 {
					regLines = append(regLines, line)
				}
			}
			sort.Slice(regLines, func(a, b int) bool {
				return regLines[a].Box[0][1] < regLines[b].Box[0][1]
			})

			mergedText := MergeParagraphLines(regLines)
			if mergedText == "" && len(regLines) > 0 {
				var sb strings.Builder
				for _, l := range regLines {
					sb.WriteString(l.Text)
					sb.WriteString(" ")
				}
				mergedText = strings.TrimSpace(sb.String())
			}

			textBlocks = append(textBlocks, TextBlock{
				RegionID: reg.ID,
				Label:    reg.Label,
				Text:     mergedText,
				Score:    reg.Score,
				Box:      reg.Box,
				Lines:    regLines,
			})
		}
	}

	fullMarkdown := ReconstructMarkdown(regions, textBlocks, tables, formulas, figures)

	var fullTextBuilder strings.Builder
	for _, tb := range textBlocks {
		if strings.TrimSpace(tb.Text) != "" {
			if fullTextBuilder.Len() > 0 {
				fullTextBuilder.WriteString("\n\n")
			}
			fullTextBuilder.WriteString(tb.Text)
		}
	}

	totalMs := time.Since(startTotal).Milliseconds()

	return &DocumentResult{
		Text:            fullTextBuilder.String(),
		Markdown:        fullMarkdown,
		DurationMs:      totalMs,
		TotalDurationMs: totalMs,
		Breakdown: LatencyBreakdown{
			LayoutMs:  insp.DurationMs,
			TextMs:    0, // 0ms OCR: Native direct vector extraction!
			TableMs:   0,
			FormulaMs: 0,
			FigureMs:  0,
		},
		Regions:    regions,
		TextBlocks: textBlocks,
		Tables:     tables,
		Formulas:   formulas,
		Figures:    figures,
		Lines:      nativeLines,
		Inspector:  insp,
		Meta: OcrMeta{
			Width:        targetW,
			Height:       targetH,
			ReadingOrder: "top-down-reading-order",
		},
	}, nil
}

// ProcessDocument routes any document format according to AnyDoc specification:
// 1. PDF Documents -> Inspect with AnyDoc pdf-inspector:
//    - If needs OCR (scanned): Layout analysis -> OCR & multi-model routing -> Markdown/JSON
//    - If no OCR needed (vector): Layout analysis & table/formula extraction -> direct vector text
// 2. Non-PDF Images -> Layout analysis -> OCR & multi-model routing
// 3. Other Documents (Word .docx, HTML, TXT, MD) -> Direct AnyDoc conversion without model overhead
func (p *DocumentPipeline) ProcessDocument(data []byte, filename string, mimeType string) (*DocumentResult, error) {
	if len(data) == 0 {
		return nil, errors.New("empty document payload")
	}

	// 1. PDF Detection
	if IsPDFData(data) || strings.HasSuffix(strings.ToLower(filename), ".pdf") || strings.Contains(mimeType, "application/pdf") {
		return p.ProcessPDF(data)
	}

	// 2. Non-PDF Image Detection
	ext := strings.ToLower(filepath.Ext(filename))
	isImg := strings.HasPrefix(mimeType, "image/") || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".bmp"
	if isImg || IsImageBytes(data) {
		bgr, err := DecodeRawBytesToBGR(data)
		if err == nil && bgr != nil {
			return p.Process(bgr)
		}
	}

	// 3. Other Non-OCR, Non-PDF Documents -> Direct AnyDoc Structured Conversion!
	return p.anydocConverter.Convert(data, filename, mimeType)
}

// ExtractTableFromVectorLines extracts 2D structured table rows from position-aware vector lines
func ExtractTableFromVectorLines(lines []OcrLine, rect [4]int, regionID int) TableBlock {
	var tableLines []OcrLine
	for _, l := range lines {
		midY := (l.Box[0][1] + l.Box[2][1]) / 2
		midX := (l.Box[0][0] + l.Box[2][0]) / 2
		if midY >= rect[1]-6 && midY <= rect[1]+rect[3]+6 &&
			midX >= rect[0]-10 && midX <= rect[0]+rect[2]+10 {
			tableLines = append(tableLines, l)
		}
	}

	if len(tableLines) == 0 {
		return TableBlock{
			RegionID: regionID,
			Box:      [4][2]int{{rect[0], rect[1]}, {rect[0] + rect[2], rect[1]}, {rect[0] + rect[2], rect[1] + rect[3]}, {rect[0], rect[1] + rect[3]}},
			Markdown: "| 表格 | 内容 |\n| --- | --- |\n| - | - |",
		}
	}

	sort.Slice(tableLines, func(i, j int) bool {
		return tableLines[i].Box[0][1] < tableLines[j].Box[0][1]
	})

	var rows [][]OcrLine
	var currRow []OcrLine
	var lastY = -999

	for _, l := range tableLines {
		y := l.Box[0][1]
		if lastY < 0 || y-lastY < 20 {
			currRow = append(currRow, l)
		} else {
			if len(currRow) > 0 {
				sort.Slice(currRow, func(a, b int) bool {
					return currRow[a].Box[0][0] < currRow[b].Box[0][0]
				})
				rows = append(rows, currRow)
			}
			currRow = []OcrLine{l}
		}
		lastY = y
	}
	if len(currRow) > 0 {
		sort.Slice(currRow, func(a, b int) bool {
			return currRow[a].Box[0][0] < currRow[b].Box[0][0]
		})
		rows = append(rows, currRow)
	}

	var strRows [][]string
	for _, r := range rows {
		var cells []string
		for _, cellLine := range r {
			cells = append(cells, strings.TrimSpace(cellLine.Text))
		}
		if len(cells) > 0 {
			strRows = append(strRows, cells)
		}
	}

	tblMd := formatMarkdownTable(strRows)
	return TableBlock{
		RegionID: regionID,
		Box:      [4][2]int{{rect[0], rect[1]}, {rect[0] + rect[2], rect[1]}, {rect[0] + rect[2], rect[1] + rect[3]}, {rect[0], rect[1] + rect[3]}},
		Markdown: tblMd,
		HTML:     formatHTMLTable(strRows),
		Rows:     len(strRows),
	}
}

// detectLineHeading checks if an OCR line is a section or structural heading
func detectLineHeading(txt string) (bool, string) {
	s := strings.TrimSpace(txt)
	if s == "" {
		return false, ""
	}
	// Sentences ending with full stops or commas are body text, not headings
	if strings.HasSuffix(s, "。") || strings.HasSuffix(s, "；") || strings.HasSuffix(s, "，") ||
		strings.HasSuffix(s, "！") || strings.HasSuffix(s, "？") || strings.HasSuffix(s, "”") || strings.HasSuffix(s, "\"") {
		return false, ""
	}
	runes := []rune(s)
	if len(runes) > 40 {
		return false, ""
	}

	// 1. Numbered section headings: "4.1.4 螺栓连接要求", "1.1 简介", "2. 系统架构"
	dotCount := 0
	i := 0
	for ; i < len(runes); i++ {
		r := runes[i]
		if unicode.IsDigit(r) {
			continue
		} else if r == '.' {
			dotCount++
		} else {
			break
		}
	}
	if i > 0 && i < len(runes) && (unicode.IsSpace(runes[i]) || runes[i] == '、' || runes[i] == ' ') {
		if dotCount >= 2 {
			return true, "### " + s
		} else if dotCount == 1 {
			return true, "## " + s
		}
		return true, "### " + s
	}

	// 2. Chinese chapter headings: "第一章 基础原理", "一、概述", "附录A ..."
	if strings.HasPrefix(s, "第") && (strings.Contains(s, "章") || strings.Contains(s, "节") || strings.Contains(s, "条")) {
		return true, "## " + s
	}
	chineseDigits := "一二三四五六七八九十"
	if len(runes) >= 2 && strings.ContainsRune(chineseDigits, runes[0]) && (runes[1] == '、' || runes[1] == '.') {
		return true, "## " + s
	}
	if strings.HasPrefix(s, "附录") {
		return true, "## " + s
	}

	// 3. Document metadata titles: "文件：法兰螺栓跨中分布.jpg"
	if strings.HasPrefix(s, "文件：") || strings.HasPrefix(s, "文件名：") {
		return true, "### " + s
	}

	return false, ""
}

// MergeParagraphLines merges segmented OCR lines of the same paragraph into cohesive text
func MergeParagraphLines(lines []OcrLine) string {
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		t := strings.TrimSpace(lines[0].Text)
		if isH, hStr := detectLineHeading(t); isH {
			return hStr
		}
		return t
	}

	var sb strings.Builder
	for i := 0; i < len(lines); i++ {
		curText := strings.TrimSpace(lines[i].Text)
		if curText == "" {
			continue
		}

		// Detect if this line is an embedded section heading
		if isHeading, headingStr := detectLineHeading(curText); isHeading {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(headingStr)
			sb.WriteString("\n\n")
			continue
		}

		if sb.Len() == 0 {
			sb.WriteString(curText)
			continue
		}

		// If current buffer ended with heading (ended with newlines), start fresh
		if strings.HasSuffix(sb.String(), "\n\n") {
			sb.WriteString(curText)
			continue
		}

		prevRune, _ := utf8.DecodeLastRuneInString(sb.String())
		curFirstRune, _ := utf8.DecodeRuneInString(curText)

		// Intentional paragraph break on sentence terminal punctuation: 。！？!?…
		// Never break on colons, commas, semicolons, or regular Chinese characters
		isParaBreak := prevRune == '。' || prevRune == '！' || prevRune == '？' ||
			prevRune == '!' || prevRune == '?' || prevRune == '…'

		if isParaBreak {
			// New paragraph
			sb.WriteString("\n\n")
			sb.WriteString(curText)
			continue
		}

		// Check hyphenation at line end (e.g. "con- \n tainer" -> "container")
		if prevRune == '-' {
			currentStr := sb.String()
			sb.Reset()
			sb.WriteString(strings.TrimSuffix(currentStr, "-"))
			sb.WriteString(curText)
			continue
		}

		// Chinese to Chinese: concatenate directly without space
		isPrevCJK := isCJK(prevRune)
		isCurCJK := isCJK(curFirstRune)

		if isPrevCJK && isCurCJK {
			sb.WriteString(curText)
		} else if !isPrevCJK && !isCurCJK {
			// English/Latin to English/Latin: add space
			sb.WriteString(" ")
			sb.WriteString(curText)
		} else {
			// Mixed CJK and Latin / Numbers (e.g. "壳体" and "0。", or "图" and "4.1"): concatenate directly
			sb.WriteString(curText)
		}
	}

	return sb.String()
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols and Punctuation
		(r >= 0xFF00 && r <= 0xFFEF) // Halfwidth and Fullwidth Forms
}

// SortRegionsByReadingOrder orders bounding boxes by vertical-then-horizontal geometry
func SortRegionsByReadingOrder(regions []LayoutRegion) {
	sort.Slice(regions, func(i, j int) bool {
		y1 := regions[i].Rect[1]
		y2 := regions[j].Rect[1]
		h1 := regions[i].Rect[3]
		h2 := regions[j].Rect[3]

		minH := h1
		if h2 < minH {
			minH = h2
		}
		threshold := minH / 2
		if threshold < 10 {
			threshold = 10
		}

		diff := y1 - y2
		if diff < 0 {
			diff = -diff
		}

		if diff < threshold {
			return regions[i].Rect[0] < regions[j].Rect[0]
		}
		return y1 < y2
	})
}

// ReconstructMarkdown aggregates text blocks, tables, formulas, and figures into a structured Markdown document
func ReconstructMarkdown(regions []LayoutRegion, texts []TextBlock, tables []TableBlock, formulas []FormulaBlock, figures []FigureBlock) string {
	textMap := make(map[int]TextBlock, len(texts))
	for _, t := range texts {
		textMap[t.RegionID] = t
	}
	tableMap := make(map[int]TableBlock, len(tables))
	for _, tb := range tables {
		tableMap[tb.RegionID] = tb
	}
	formulaMap := make(map[int]FormulaBlock, len(formulas))
	for _, f := range formulas {
		formulaMap[f.RegionID] = f
	}
	figureMap := make(map[int]FigureBlock, len(figures))
	for _, fig := range figures {
		figureMap[fig.RegionID] = fig
	}

	var sb strings.Builder
	for _, reg := range regions {
		switch reg.Label {
		case LayoutTitle:
			if tb, ok := textMap[reg.ID]; ok && strings.TrimSpace(tb.Text) != "" {
				titleTxt := strings.TrimSpace(tb.Text)
				if !strings.HasPrefix(titleTxt, "#") {
					titleTxt = "## " + titleTxt
				}
				sb.WriteString(titleTxt + "\n\n")
			}
		case LayoutTable:
			if tbl, ok := tableMap[reg.ID]; ok {
				if tbl.Markdown != "" {
					sb.WriteString("\n" + tbl.Markdown + "\n\n")
				} else if tbl.HTML != "" {
					sb.WriteString("\n" + tbl.HTML + "\n\n")
				}
			}
		case LayoutFormula:
			if fml, ok := formulaMap[reg.ID]; ok && fml.LaTeX != "" {
				if fml.IsInline {
					sb.WriteString("$" + fml.LaTeX + "$ ")
				} else {
					sb.WriteString("\n$$\n" + fml.LaTeX + "\n$$\n\n")
				}
			}
		case LayoutFigure:
			if fig, ok := figureMap[reg.ID]; ok {
				caption := fig.Caption
				if caption == "" {
					caption = fmt.Sprintf("插图 %d", fig.RegionID)
				}
				sb.WriteString(fmt.Sprintf("\n![%s](%s)\n\n", caption, fig.Filename))
			} else {
				sb.WriteString(fmt.Sprintf("\n![Figure %d](images/figure_%d.png)\n\n", reg.ID, reg.ID))
			}
		case LayoutHeader:
			if tb, ok := textMap[reg.ID]; ok && strings.TrimSpace(tb.Text) != "" {
				headerTxt := strings.TrimSpace(tb.Text)
				if !strings.HasPrefix(headerTxt, "#") {
					headerTxt = "# " + headerTxt
				}
				sb.WriteString(headerTxt + "\n\n")
			}
		case LayoutFooter:
			if tb, ok := textMap[reg.ID]; ok && strings.TrimSpace(tb.Text) != "" {
				sb.WriteString("\n---\n*" + strings.TrimSpace(tb.Text) + "*\n\n")
			}
		default: // LayoutText
			if tb, ok := textMap[reg.ID]; ok && strings.TrimSpace(tb.Text) != "" {
				sb.WriteString(strings.TrimSpace(tb.Text) + "\n\n")
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

// CreateDocumentZip packages document.md, cropped figure images, original image, and metadata.json into a ZIP archive
func CreateDocumentZip(docResult *DocumentResult, originalImg *BGRImage) ([]byte, error) {
	if docResult == nil {
		return nil, fmt.Errorf("nil document result")
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. Write document.md
	mdWriter, err := zw.Create("document.md")
	if err != nil {
		return nil, fmt.Errorf("failed to create document.md in zip: %w", err)
	}
	if _, err := mdWriter.Write([]byte(docResult.Markdown)); err != nil {
		return nil, err
	}

	// 2. Write cropped figure images
	for _, fig := range docResult.Figures {
		if fig.DataURL != "" && strings.HasPrefix(fig.DataURL, "data:image/png;base64,") {
			rawBase64 := strings.TrimPrefix(fig.DataURL, "data:image/png;base64,")
			imgData, decErr := base64.StdEncoding.DecodeString(rawBase64)
			if decErr == nil && len(imgData) > 0 {
				figPath := fig.Filename
				if !strings.HasPrefix(figPath, "images/") {
					figPath = "images/" + figPath
				}
				imgWriter, err := zw.Create(figPath)
				if err == nil {
					_, _ = imgWriter.Write(imgData)
				}
			}
		}
	}

	// 3. Write original image if available
	if originalImg != nil {
		if origBytes, err := originalImg.EncodePNG(); err == nil {
			origWriter, err := zw.Create("images/original.png")
			if err == nil {
				_, _ = origWriter.Write(origBytes)
			}
		}
	}

	// 4. Write metadata.json
	metaWriter, err := zw.Create("metadata.json")
	if err == nil {
		metaBytes, _ := json.MarshalIndent(docResult, "", "  ")
		_, _ = metaWriter.Write(metaBytes)
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize zip: %w", err)
	}

	return buf.Bytes(), nil
}
