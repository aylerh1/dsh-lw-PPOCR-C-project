package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// AnyDocConverter provides zero-overhead direct structural conversion for non-OCR, non-PDF documents
// such as Word (.docx), Excel (.xlsx), PowerPoint (.pptx), CSV (.csv), HTML (.html), JSON (.json), and text (.txt, .md).
type AnyDocConverter struct{}

// NewAnyDocConverter creates a new instance of AnyDocConverter
func NewAnyDocConverter() *AnyDocConverter {
	return &AnyDocConverter{}
}

// Convert converts non-PDF document bytes directly into standard Markdown and DocumentResult
func (ac *AnyDocConverter) Convert(data []byte, filename string, mimeType string) (*DocumentResult, error) {
	if len(data) == 0 {
		return nil, errors.New("empty document content")
	}

	start := time.Now()
	ext := strings.ToLower(filepath.Ext(filename))

	// Detect doc type from extension or mime
	docType := "text"
	if ext == ".docx" || ext == ".doc" || strings.Contains(mimeType, "wordprocessingml") {
		docType = "docx"
	} else if ext == ".xlsx" || ext == ".xls" || strings.Contains(mimeType, "spreadsheetml") {
		docType = "xlsx"
	} else if ext == ".pptx" || ext == ".ppt" || strings.Contains(mimeType, "presentationml") {
		docType = "pptx"
	} else if ext == ".csv" || ext == ".tsv" || strings.Contains(mimeType, "csv") {
		docType = "csv"
	} else if ext == ".html" || ext == ".htm" || ext == ".xhtml" || strings.Contains(mimeType, "html") {
		docType = "html"
	} else if ext == ".json" || strings.Contains(mimeType, "json") {
		docType = "json"
	} else if ext == ".rtf" || strings.Contains(mimeType, "rtf") {
		docType = "rtf"
	} else if ext == ".md" || strings.Contains(mimeType, "markdown") {
		docType = "markdown"
	}

	var mdText string
	var textBlocks []TextBlock
	var tables []TableBlock
	var err error

	switch docType {
	case "docx":
		mdText, textBlocks, tables, err = ac.convertDocx(data)
	case "xlsx":
		mdText, textBlocks, tables, err = ac.convertXlsx(data)
	case "pptx":
		mdText, textBlocks, err = ac.convertPptx(data)
	case "csv":
		isTSV := ext == ".tsv"
		mdText, textBlocks, tables, err = ac.convertCSV(data, isTSV)
	case "html":
		mdText, textBlocks, tables, err = ac.convertHTML(data)
	case "json":
		mdText, textBlocks, err = ac.convertJSON(data)
	case "rtf":
		mdText, textBlocks, err = ac.convertRTF(data)
	default: // txt, md, xml, yaml, or generic text
		mdText, textBlocks, err = ac.convertPlainText(data)
	}

	if err != nil {
		return nil, fmt.Errorf("anydoc direct conversion failed: %w", err)
	}

	duration := time.Since(start).Milliseconds()

	// Build clean full text
	var fullTextBuilder strings.Builder
	for _, tb := range textBlocks {
		if strings.TrimSpace(tb.Text) != "" {
			if fullTextBuilder.Len() > 0 {
				fullTextBuilder.WriteString("\n\n")
			}
			fullTextBuilder.WriteString(tb.Text)
		}
	}

	// Create regions from textBlocks
	var regions []LayoutRegion
	for i, tb := range textBlocks {
		regions = append(regions, LayoutRegion{
			ID:       i + 1,
			Label:    tb.Label,
			OrderNum: i + 1,
			Score:    1.0,
			Box:      tb.Box,
			Rect:     [4]int{tb.Box[0][0], tb.Box[0][1], tb.Box[2][0] - tb.Box[0][0], tb.Box[2][1] - tb.Box[0][1]},
			Caption:  tb.Text,
		})
	}

	insp := &PDFInspectionResult{
		IsPDF:          false,
		PageCount:      1,
		PrimaryType:    "anydoc-direct",
		NeedsOCR:       false,
		TotalChars:     utf8.RuneCountInString(mdText),
		ImageCount:     0,
		ImageCoverage:  0.0,
		HasFontTable:   false,
		DurationMs:     duration,
		PageDimensions: [2]int{800, 1100},
	}

	return &DocumentResult{
		Text:            fullTextBuilder.String(),
		Markdown:        mdText,
		DurationMs:      duration,
		TotalDurationMs: duration,
		Breakdown: LatencyBreakdown{
			LayoutMs:  duration,
			TextMs:    0,
			TableMs:   0,
			FormulaMs: 0,
			FigureMs:  0,
		},
		Regions:    regions,
		TextBlocks: textBlocks,
		Tables:     tables,
		Formulas:   []FormulaBlock{},
		Figures:    []FigureBlock{},
		Lines:      []OcrLine{},
		Inspector:  insp,
		Meta: OcrMeta{
			Width:        800,
			Height:       1100,
			ReadingOrder: "anydoc-direct-structured-order",
		},
	}, nil
}

// convertDocx parses word/document.xml from the docx ZIP container without any heavy dependencies
func (ac *AnyDocConverter) convertDocx(data []byte) (string, []TextBlock, []TableBlock, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid docx file: %w", err)
	}

	var docXML []byte
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", nil, nil, err
			}
			docXML, err = io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return "", nil, nil, err
			}
			break
		}
	}

	if len(docXML) == 0 {
		return "", nil, nil, errors.New("word/document.xml not found in docx archive")
	}

	// Parse XML elements
	var mdBuilder strings.Builder
	var textBlocks []TextBlock
	var tables []TableBlock

	decoder := xml.NewDecoder(bytes.NewReader(docXML))
	var (
		inParagraph bool
		inTable     bool
		inRow       bool
		inCell      bool
		currText    strings.Builder
		rowCells    []string
		tableRows   [][]string
		isHeading   bool
		headingLvl  int
		blockIdx    int
	)

	yOffset := 50

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch elem := token.(type) {
		case xml.StartElement:
			name := elem.Name.Local
			switch name {
			case "tbl":
				inTable = true
				tableRows = nil
			case "tr":
				inRow = true
				rowCells = nil
			case "tc":
				inCell = true
				currText.Reset()
			case "p":
				inParagraph = true
				currText.Reset()
				isHeading = false
				headingLvl = 1
			case "pStyle":
				for _, attr := range elem.Attr {
					if attr.Name.Local == "val" {
						val := strings.ToLower(attr.Value)
						if strings.HasPrefix(val, "heading") || strings.HasPrefix(val, "title") {
							isHeading = true
							if len(val) > 7 && val[7] >= '1' && val[7] <= '6' {
								headingLvl = int(val[7] - '0')
							}
						}
					}
				}
			}

		case xml.EndElement:
			name := elem.Name.Local
			switch name {
			case "tbl":
				inTable = false
				if len(tableRows) > 0 {
					tblMd := formatMarkdownTable(tableRows)
					mdBuilder.WriteString(tblMd)
					mdBuilder.WriteString("\n\n")

					tables = append(tables, TableBlock{
						RegionID: blockIdx + 1,
						Box:      [4][2]int{{50, yOffset}, {750, yOffset}, {750, yOffset + 120}, {50, yOffset + 120}},
						Markdown: tblMd,
						HTML:     formatHTMLTable(tableRows),
						Rows:     len(tableRows),
					})
					yOffset += 140
				}
			case "tr":
				inRow = false
				if inTable && len(rowCells) > 0 {
					tableRows = append(tableRows, rowCells)
				}
			case "tc":
				inCell = false
				if inRow {
					rowCells = append(rowCells, strings.TrimSpace(currText.String()))
				}
			case "p":
				inParagraph = false
				if !inCell {
					txt := strings.TrimSpace(currText.String())
					if txt != "" {
						blockIdx++
						label := LayoutText
						if isHeading {
							label = LayoutTitle
							prefix := strings.Repeat("#", headingLvl)
							mdBuilder.WriteString(fmt.Sprintf("%s %s\n\n", prefix, txt))
						} else {
							mdBuilder.WriteString(txt)
							mdBuilder.WriteString("\n\n")
						}

						textBlocks = append(textBlocks, TextBlock{
							RegionID: blockIdx,
							Label:    label,
							Text:     txt,
							Score:    1.0,
							Box:      [4][2]int{{50, yOffset}, {750, yOffset}, {750, yOffset + 30}, {50, yOffset + 30}},
						})
						yOffset += 45
					}
				}
			}

		case xml.CharData:
			if inParagraph || inCell {
				currText.Write(elem)
			}
		}
	}

	return strings.TrimSpace(mdBuilder.String()), textBlocks, tables, nil
}

// convertHTML converts clean HTML to Markdown and structured blocks
func (ac *AnyDocConverter) convertHTML(data []byte) (string, []TextBlock, []TableBlock, error) {
	content := string(data)

	// Clean script and style tags
	scriptRe := regexp.MustCompile(`(?si)<script[^>]*?>.*?</script>`)
	content = scriptRe.ReplaceAllString(content, "")
	styleRe := regexp.MustCompile(`(?si)<style[^>]*?>.*?</style>`)
	content = styleRe.ReplaceAllString(content, "")

	var mdBuilder strings.Builder
	var textBlocks []TextBlock
	var tables []TableBlock

	// Extract tables
	tableRe := regexp.MustCompile(`(?si)<table[^>]*?>(.*?)</table>`)
	tableMatches := tableRe.FindAllStringSubmatch(content, -1)
	for idx, tblMatch := range tableMatches {
		if len(tblMatch) > 1 {
			rows := parseHTMLTableRows(tblMatch[1])
			if len(rows) > 0 {
				tblMd := formatMarkdownTable(rows)
				tables = append(tables, TableBlock{
					RegionID: idx + 100,
					Box:      [4][2]int{{50, 100 + idx*150}, {750, 100 + idx*150}, {750, 220 + idx*150}, {50, 220 + idx*150}},
					Markdown: tblMd,
					HTML:     tblMatch[0],
					Rows:     len(rows),
				})
			}
		}
	}

	// Extract headings
	for lvl := 1; lvl <= 6; lvl++ {
		hRe := regexp.MustCompile(fmt.Sprintf(`(?si)<h%d[^>]*?>(.*?)</h%d>`, lvl, lvl))
		headings := hRe.FindAllStringSubmatch(content, -1)
		for _, h := range headings {
			txt := cleanHTMLTags(h[1])
			if txt != "" {
				prefix := strings.Repeat("#", lvl)
				mdBuilder.WriteString(fmt.Sprintf("%s %s\n\n", prefix, txt))
				textBlocks = append(textBlocks, TextBlock{
					RegionID: len(textBlocks) + 1,
					Label:    LayoutTitle,
					Text:     txt,
					Score:    1.0,
					Box:      [4][2]int{{50, 50 + len(textBlocks)*40}, {750, 50 + len(textBlocks)*40}, {750, 80 + len(textBlocks)*40}, {50, 80 + len(textBlocks)*40}},
				})
			}
		}
	}

	// Extract paragraphs
	pRe := regexp.MustCompile(`(?si)<p[^>]*?>(.*?)</p>`)
	paras := pRe.FindAllStringSubmatch(content, -1)
	for idx, p := range paras {
		txt := cleanHTMLTags(p[1])
		if txt != "" {
			mdBuilder.WriteString(txt)
			mdBuilder.WriteString("\n\n")
			textBlocks = append(textBlocks, TextBlock{
				RegionID: len(textBlocks) + 1,
				Label:    LayoutText,
				Text:     txt,
				Score:    1.0,
				Box:      [4][2]int{{50, 200 + idx*40}, {750, 200 + idx*40}, {750, 230 + idx*40}, {50, 230 + idx*40}},
			})
		}
	}

	// Append tables to Markdown
	for _, tbl := range tables {
		mdBuilder.WriteString(tbl.Markdown)
		mdBuilder.WriteString("\n\n")
	}

	// If no structured HTML elements found, fallback to stripped plain text
	if mdBuilder.Len() == 0 {
		clean := cleanHTMLTags(content)
		txtMd, txtBlocks, err := ac.convertPlainText([]byte(clean))
		return txtMd, txtBlocks, nil, err
	}

	return strings.TrimSpace(mdBuilder.String()), textBlocks, tables, nil
}

// convertPlainText wraps plain text / Markdown lines into structured blocks
func (ac *AnyDocConverter) convertPlainText(data []byte) (string, []TextBlock, error) {
	text := string(data)
	lines := strings.Split(text, "\n")

	var mdBuilder strings.Builder
	var textBlocks []TextBlock
	var currPara strings.Builder
	yOffset := 50
	blockIdx := 0

	flushPara := func() {
		p := strings.TrimSpace(currPara.String())
		if p != "" {
			blockIdx++
			label := LayoutText
			if strings.HasPrefix(p, "#") {
				label = LayoutTitle
			}
			textBlocks = append(textBlocks, TextBlock{
				RegionID: blockIdx,
				Label:    label,
				Text:     p,
				Score:    1.0,
				Box:      [4][2]int{{50, yOffset}, {750, yOffset}, {750, yOffset + 35}, {50, yOffset + 35}},
			})
			mdBuilder.WriteString(p)
			mdBuilder.WriteString("\n\n")
			yOffset += 45
		}
		currPara.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushPara()
		} else {
			if currPara.Len() > 0 {
				currPara.WriteString(" ")
			}
			currPara.WriteString(trimmed)
		}
	}
	flushPara()

	return strings.TrimSpace(mdBuilder.String()), textBlocks, nil
}

func parseHTMLTableRows(tableInner string) [][]string {
	var rows [][]string
	trRe := regexp.MustCompile(`(?si)<tr[^>]*?>(.*?)</tr>`)
	trMatches := trRe.FindAllStringSubmatch(tableInner, -1)
	cellRe := regexp.MustCompile(`(?si)<t[hd][^>]*?>(.*?)</t[hd]>`)

	for _, tr := range trMatches {
		var cells []string
		cellMatches := cellRe.FindAllStringSubmatch(tr[1], -1)
		for _, c := range cellMatches {
			cells = append(cells, cleanHTMLTags(c[1]))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

func formatMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	colCount := 0
	for _, r := range rows {
		if len(r) > colCount {
			colCount = len(r)
		}
	}
	if colCount == 0 {
		return ""
	}

	var sb strings.Builder
	// Header row
	header := rows[0]
	sb.WriteString("|")
	for i := 0; i < colCount; i++ {
		val := ""
		if i < len(header) {
			val = header[i]
		}
		sb.WriteString(" " + val + " |")
	}
	sb.WriteString("\n|")
	for i := 0; i < colCount; i++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")

	// Data rows
	for r := 1; r < len(rows); r++ {
		sb.WriteString("|")
		for i := 0; i < colCount; i++ {
			val := ""
			if i < len(rows[r]) {
				val = rows[r][i]
			}
			sb.WriteString(" " + val + " |")
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

func formatHTMLTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<table>\n")
	for r, row := range rows {
		sb.WriteString("  <tr>\n")
		tag := "td"
		if r == 0 {
			tag = "th"
		}
		for _, c := range row {
			sb.WriteString(fmt.Sprintf("    <%s>%s</%s>\n", tag, c, tag))
		}
		sb.WriteString("  </tr>\n")
	}
	sb.WriteString("</table>")
	return sb.String()
}

func cleanHTMLTags(html string) string {
	tagRe := regexp.MustCompile(`<[^>]*?>`)
	clean := tagRe.ReplaceAllString(html, "")
	clean = strings.ReplaceAll(clean, "&nbsp;", " ")
	clean = strings.ReplaceAll(clean, "&amp;", "&")
	clean = strings.ReplaceAll(clean, "&lt;", "<")
	clean = strings.ReplaceAll(clean, "&gt;", ">")
	clean = strings.ReplaceAll(clean, "&quot;", "\"")
	return strings.TrimSpace(clean)
}

// convertCSV converts comma-separated or tab-separated text into structured Markdown table
func (ac *AnyDocConverter) convertCSV(data []byte, isTSV bool) (string, []TextBlock, []TableBlock, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	if isTSV {
		reader.Comma = '\t'
	}
	reader.FieldsPerRecord = -1 // Allow variable column counts

	rows, err := reader.ReadAll()
	if err != nil {
		// Fallback to line splitting
		lines := strings.Split(string(data), "\n")
		var rawRows [][]string
		delim := ","
		if isTSV {
			delim = "\t"
		}
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				rawRows = append(rawRows, strings.Split(l, delim))
			}
		}
		rows = rawRows
	}

	if len(rows) == 0 {
		return "", nil, nil, errors.New("empty CSV/TSV table")
	}

	tblMd := formatMarkdownTable(rows)
	tblHTML := formatHTMLTable(rows)

	tb := TableBlock{
		RegionID: 1,
		Box:      [4][2]int{{50, 50}, {750, 50}, {750, 50 + len(rows)*24}, {50, 50 + len(rows)*24}},
		Markdown: tblMd,
		HTML:     tblHTML,
		Rows:     len(rows),
	}

	block := TextBlock{
		RegionID: 1,
		Label:    LayoutTable,
		Text:     fmt.Sprintf("表格数据 (%d 行)", len(rows)),
		Score:    1.0,
		Box:      tb.Box,
	}

	return tblMd, []TextBlock{block}, []TableBlock{tb}, nil
}

// convertXlsx parses spreadsheet cells from OpenXML zip archive (sheet1.xml + sharedStrings.xml)
func (ac *AnyDocConverter) convertXlsx(data []byte) (string, []TextBlock, []TableBlock, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid xlsx file: %w", err)
	}

	// 1. Read shared strings
	var sharedStrings []string
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err == nil {
				xmlData, _ := io.ReadAll(rc)
				_ = rc.Close()
				decoder := xml.NewDecoder(bytes.NewReader(xmlData))
				var inT bool
				var currStr strings.Builder
				for {
					tok, err := decoder.Token()
					if err != nil {
						break
					}
					switch elem := tok.(type) {
					case xml.StartElement:
						if elem.Name.Local == "t" {
							inT = true
							currStr.Reset()
						}
					case xml.EndElement:
						if elem.Name.Local == "t" {
							inT = false
							sharedStrings = append(sharedStrings, currStr.String())
						}
					case xml.CharData:
						if inT {
							currStr.Write(elem)
						}
					}
				}
			}
			break
		}
	}

	// 2. Read first worksheet sheet1.xml
	var sheetXML []byte
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			rc, err := f.Open()
			if err == nil {
				sheetXML, _ = io.ReadAll(rc)
				_ = rc.Close()
				break
			}
		}
	}

	if len(sheetXML) == 0 {
		md, tbs, err := ac.convertPlainText(data)
		return md, tbs, nil, err
	}

	// Parse rows and cells
	var rows [][]string
	decoder := xml.NewDecoder(bytes.NewReader(sheetXML))
	var (
		inRow     bool
		inV       bool
		cellType  string
		currVal   strings.Builder
		currRow   []string
	)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch elem := tok.(type) {
		case xml.StartElement:
			name := elem.Name.Local
			switch name {
			case "row":
				inRow = true
				currRow = nil
			case "c":
				cellType = ""
				currVal.Reset()
				for _, a := range elem.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			case "v":
				inV = true
				currVal.Reset()
			}
		case xml.EndElement:
			name := elem.Name.Local
			switch name {
			case "row":
				inRow = false
				if len(currRow) > 0 {
					rows = append(rows, currRow)
				}
			case "c":
				val := strings.TrimSpace(currVal.String())
				if cellType == "s" {
					// Shared string lookup
					var idx int
					if _, err := fmt.Sscanf(val, "%d", &idx); err == nil && idx >= 0 && idx < len(sharedStrings) {
						val = sharedStrings[idx]
					}
				}
				if inRow {
					currRow = append(currRow, val)
				}
			case "v":
				inV = false
			}
		case xml.CharData:
			if inV {
				currVal.Write(elem)
			}
		}
	}

	if len(rows) == 0 {
		md, tbs, err := ac.convertPlainText(data)
		return md, tbs, nil, err
	}

	tblMd := formatMarkdownTable(rows)
	tblHTML := formatHTMLTable(rows)

	tb := TableBlock{
		RegionID: 1,
		Box:      [4][2]int{{50, 50}, {750, 50}, {750, 50 + len(rows)*24}, {50, 50 + len(rows)*24}},
		Markdown: tblMd,
		HTML:     tblHTML,
		Rows:     len(rows),
	}

	block := TextBlock{
		RegionID: 1,
		Label:    LayoutTable,
		Text:     fmt.Sprintf("Excel 工作表数据 (%d 行)", len(rows)),
		Score:    1.0,
		Box:      tb.Box,
	}

	return tblMd, []TextBlock{block}, []TableBlock{tb}, nil
}

// convertPptx parses slide text from OpenXML zip archive (ppt/slides/slide*.xml)
func (ac *AnyDocConverter) convertPptx(data []byte) (string, []TextBlock, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("invalid pptx file: %w", err)
	}

	var slideFiles []*zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles = append(slideFiles, f)
		}
	}

	sort.Slice(slideFiles, func(i, j int) bool {
		return slideFiles[i].Name < slideFiles[j].Name
	})

	var mdBuilder strings.Builder
	var textBlocks []TextBlock
	blockIdx := 1
	yOffset := 50

	mdBuilder.WriteString("# 演示文稿幻灯片 (PPTX)\n\n")

	for sIdx, f := range slideFiles {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		slideXML, _ := io.ReadAll(rc)
		_ = rc.Close()

		decoder := xml.NewDecoder(bytes.NewReader(slideXML))
		var (
			inPara   bool
			inText   bool
			paraText strings.Builder
			paras    []string
		)

		for {
			tok, err := decoder.Token()
			if err != nil {
				break
			}
			switch elem := tok.(type) {
			case xml.StartElement:
				if elem.Name.Local == "p" {
					inPara = true
					paraText.Reset()
				} else if elem.Name.Local == "t" {
					inText = true
				}
			case xml.EndElement:
				if elem.Name.Local == "p" {
					inPara = false
					t := strings.TrimSpace(paraText.String())
					if t != "" {
						paras = append(paras, t)
					}
				} else if elem.Name.Local == "t" {
					inText = false
				}
			case xml.CharData:
				if inPara && inText {
					paraText.Write(elem)
				}
			}
		}

		if len(paras) > 0 {
			mdBuilder.WriteString(fmt.Sprintf("## 幻灯片 %d\n\n", sIdx+1))
			for _, p := range paras {
				mdBuilder.WriteString(p + "\n\n")
				textBlocks = append(textBlocks, TextBlock{
					RegionID: blockIdx,
					Label:    LayoutText,
					Text:     p,
					Score:    1.0,
					Box:      [4][2]int{{50, yOffset}, {750, yOffset}, {750, yOffset + 30}, {50, yOffset + 30}},
				})
				blockIdx++
				yOffset += 40
			}
		}
	}

	if len(textBlocks) == 0 {
		return ac.convertPlainText(data)
	}

	return strings.TrimSpace(mdBuilder.String()), textBlocks, nil
}

// convertJSON parses JSON data and renders as clean structured Markdown code block
func (ac *AnyDocConverter) convertJSON(data []byte) (string, []TextBlock, error) {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		return ac.convertPlainText(data)
	}

	md := fmt.Sprintf("```json\n%s\n```", pretty.String())
	block := TextBlock{
		RegionID: 1,
		Label:    LayoutText,
		Text:     pretty.String(),
		Score:    1.0,
		Box:      [4][2]int{{50, 50}, {750, 50}, {750, 600}, {50, 600}},
	}
	return md, []TextBlock{block}, nil
}

// convertRTF cleans rich text formatting tags and returns cohesive paragraphs
func (ac *AnyDocConverter) convertRTF(data []byte) (string, []TextBlock, error) {
	raw := string(data)
	// Remove RTF control words: \word123 or \word
	ctrlRe := regexp.MustCompile(`\\[a-zA-Z]+(-?[0-9]+)? ?`)
	clean := ctrlRe.ReplaceAllString(raw, "")
	clean = strings.ReplaceAll(clean, "{", "")
	clean = strings.ReplaceAll(clean, "}", "")
	clean = strings.ReplaceAll(clean, "\r", "")

	return ac.convertPlainText([]byte(clean))
}
