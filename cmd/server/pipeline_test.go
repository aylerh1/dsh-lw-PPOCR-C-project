package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestPipeline_DataStructures(t *testing.T) {
	// Verify region reading order sorting
	regions := []LayoutRegion{
		{ID: 1, Label: LayoutText, Rect: [4]int{10, 100, 200, 30}},
		{ID: 2, Label: LayoutTitle, Rect: [4]int{10, 20, 150, 40}},
		{ID: 3, Label: LayoutTable, Rect: [4]int{10, 200, 400, 120}},
	}

	SortRegionsByReadingOrder(regions)
	if regions[0].ID != 2 {
		t.Errorf("expected Title (ID 2) to be first in reading order, got ID %d", regions[0].ID)
	}
	if regions[1].ID != 1 {
		t.Errorf("expected Text (ID 1) to be second in reading order, got ID %d", regions[1].ID)
	}
	if regions[2].ID != 3 {
		t.Errorf("expected Table (ID 3) to be third in reading order, got ID %d", regions[2].ID)
	}
}

func TestPipeline_MarkdownReconstruction(t *testing.T) {
	regions := []LayoutRegion{
		{ID: 1, Label: LayoutTitle, Rect: [4]int{10, 10, 100, 20}},
		{ID: 2, Label: LayoutText, Rect: [4]int{10, 40, 200, 30}},
		{ID: 3, Label: LayoutTable, Rect: [4]int{10, 80, 200, 80}},
		{ID: 4, Label: LayoutFormula, Rect: [4]int{10, 180, 150, 40}},
		{ID: 5, Label: LayoutFigure, Rect: [4]int{10, 240, 300, 150}},
	}

	texts := []TextBlock{
		{RegionID: 1, Text: "Document Title"},
		{RegionID: 2, Text: "This is paragraph text in the document."},
	}

	tables := []TableBlock{
		{
			RegionID: 3,
			Markdown: "| Item | Price |\n| --- | --- |\n| Apple | $1.50 |",
		},
	}

	formulas := []FormulaBlock{
		{
			RegionID: 4,
			LaTeX:    "E = mc^2",
			IsInline: false,
		},
	}

	figures := []FigureBlock{
		{
			RegionID: 5,
			Filename: "images/figure_1.png",
			Caption:  "图4.1.4-6 主轴线上的管口设置",
		},
	}

	md := ReconstructMarkdown(regions, texts, tables, formulas, figures)

	if !strings.Contains(md, "## Document Title") {
		t.Errorf("expected title in markdown, got: %s", md)
	}
	if !strings.Contains(md, "This is paragraph text") {
		t.Errorf("expected text in markdown, got: %s", md)
	}
	if !strings.Contains(md, "| Item | Price |") {
		t.Errorf("expected table in markdown, got: %s", md)
	}
	if !strings.Contains(md, "$$\nE = mc^2\n$$") {
		t.Errorf("expected formula in markdown, got: %s", md)
	}
	if !strings.Contains(md, "![图4.1.4-6 主轴线上的管口设置](images/figure_1.png)") {
		t.Errorf("expected figure in markdown, got: %s", md)
	}
}

func TestMergeParagraphLines_ChineseAndEnglish(t *testing.T) {
	// Scenario: Fragmented Chinese sentence with continuation
	chineseLines := []OcrLine{
		{Text: "接管法兰螺栓孔应跨过壳体0。-180。及90。-270。的主轴线。对于不开设在"},
		{Text: "上述主轴线上的管"},
		{Text: "口，正确和错误的设置如图4.1.4-6所示。"},
	}

	merged := MergeParagraphLines(chineseLines)
	expectedSubstring := "对于不开设在上述主轴线上的管口，正确和错误的设置"
	if !strings.Contains(merged, expectedSubstring) {
		t.Errorf("expected merged Chinese sentence, got: %s", merged)
	}

	// Scenario: English words with line break and hyphen
	englishLines := []OcrLine{
		{Text: "This is a con-"},
		{Text: "tainer resident"},
		{Text: "C engine."},
	}
	mergedEn := MergeParagraphLines(englishLines)
	if !strings.Contains(mergedEn, "container resident C engine.") {
		t.Errorf("expected merged English with hyphen removed, got: %s", mergedEn)
	}
}

func TestCreateDocumentZip(t *testing.T) {
	doc := &DocumentResult{
		Markdown: "# Test Document\n\nContent here.",
		Figures: []FigureBlock{
			{
				RegionID: 1,
				Filename: "images/fig_1.png",
				Caption:  "Test Fig",
				DataURL:  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			},
		},
	}

	zipBytes, err := CreateDocumentZip(doc, nil)
	if err != nil {
		t.Fatalf("CreateDocumentZip failed: %v", err)
	}
	if len(zipBytes) == 0 {
		t.Fatalf("expected non-empty zip bytes")
	}

	// Verify zip contents
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader failed: %v", err)
	}

	foundMD := false
	foundFig := false
	foundMeta := false

	for _, f := range zr.File {
		if f.Name == "document.md" {
			foundMD = true
		}
		if f.Name == "images/fig_1.png" {
			foundFig = true
		}
		if f.Name == "metadata.json" {
			foundMeta = true
		}
	}

	if !foundMD {
		t.Errorf("expected document.md in zip")
	}
	if !foundFig {
		t.Errorf("expected images/fig_1.png in zip")
	}
	if !foundMeta {
		t.Errorf("expected metadata.json in zip")
	}
}

func TestFormula_ConvertToLaTeX(t *testing.T) {
	raw := "a/b + sqrt(x^2 + y_1) <= alpha"
	latex := ConvertToLaTeX(raw)

	if !strings.Contains(latex, "\\frac{a}{b}") {
		t.Errorf("expected fraction conversion, got: %s", latex)
	}
	if !strings.Contains(latex, "\\sqrt") {
		t.Errorf("expected sqrt conversion, got: %s", latex)
	}
	if !strings.Contains(latex, "\\alpha") {
		t.Errorf("expected alpha conversion, got: %s", latex)
	}
	if !strings.Contains(latex, "\\le") {
		t.Errorf("expected le conversion, got: %s", latex)
	}

	eulerRaw := "eiπ +1l= 0"
	eulerLatex := ConvertToLaTeX(eulerRaw)
	if eulerLatex != "e^{i\\pi} + 1 = 0" {
		t.Errorf("expected Euler formula 'e^{i\\pi} + 1 = 0', got: %s", eulerLatex)
	}
}

func TestTableEngine_Render(t *testing.T) {
	te := NewTableEngine()
	cells := []TableCell{
		{RowIdx: 0, ColIdx: 0, Text: "Col A"},
		{RowIdx: 0, ColIdx: 1, Text: "Col B"},
		{RowIdx: 1, ColIdx: 0, Text: "Data 1"},
		{RowIdx: 1, ColIdx: 1, Text: "Data 2"},
	}

	html := te.renderHTML(2, 2, cells)
	if !strings.Contains(html, "<th>Col A</th>") {
		t.Errorf("expected th in HTML, got: %s", html)
	}
	if !strings.Contains(html, "<td>Data 2</td>") {
		t.Errorf("expected td in HTML, got: %s", html)
	}

	md := te.renderMarkdown(2, 2, cells)
	if !strings.Contains(md, "| Col A | Col B |") {
		t.Errorf("expected header row in markdown, got: %s", md)
	}
}

func TestFigureDetectionAndParagraphMerge(t *testing.T) {
	le := NewLayoutEngine()
	pageW, pageH := 800, 1200

	lines := []OcrLine{
		// First paragraph (3 lines)
		{
			Text:  "接管法兰螺栓孔应跨过壳体0。-180。及90。-270。的主轴线。对于不开设在",
			Box:   [4][2]int{{100, 100}, {700, 100}, {700, 120}, {100, 120}},
			Score: 0.95,
		},
		{
			Text:  "上述主轴线上的管",
			Box:   [4][2]int{{100, 130}, {300, 130}, {300, 150}, {100, 150}},
			Score: 0.95,
		},
		{
			Text:  "口，正确和错误的设置如图4.1.4-6所示。",
			Box:   [4][2]int{{100, 160}, {400, 160}, {400, 180}, {100, 180}},
			Score: 0.95,
		},
		// Figure internal annotations
		{
			Text:  "0",
			Box:   [4][2]int{{380, 240}, {400, 240}, {400, 255}, {380, 255}},
			Score: 0.90,
		},
		{
			Text:  "(a)正确",
			Box:   [4][2]int{{360, 350}, {420, 350}, {420, 370}, {360, 370}},
			Score: 0.92,
		},
		{
			Text:  "(b)错误",
			Box:   [4][2]int{{360, 520}, {420, 520}, {420, 540}, {360, 540}},
			Score: 0.91,
		},
		// Figure caption
		{
			Text:  "图4.1.4-6 主轴线上的管口设置",
			Box:   [4][2]int{{250, 560}, {550, 560}, {550, 580}, {250, 580}},
			Score: 0.96,
		},
		// Second paragraph (4 lines)
		{
			Text:  "此时，跨中的目的是为了在与外部接管相连接时，螺栓受外部载荷作用时较为均匀，且有利于扳",
			Box:   [4][2]int{{100, 620}, {700, 620}, {700, 640}, {100, 640}},
			Score: 0.95,
		},
		{
			Text:  "手紧固操作。对于立面安装除了上述优点外，还有一个很重要的目的：当管内介质通过螺栓向外",
			Box:   [4][2]int{{100, 650}, {700, 650}, {700, 670}, {100, 670}},
			Score: 0.95,
		},
		{
			Text:  "泄漏时，不会使介质直接喷射到螺栓上，特别是存在强腐蚀介质对螺栓的腐蚀的场合，如图",
			Box:   [4][2]int{{100, 680}, {700, 680}, {700, 700}, {100, 700}},
			Score: 0.95,
		},
		{
			Text:  "4.1.4-6所示，螺栓孔跨壳体主轴线显得更为必要了[18]。",
			Box:   [4][2]int{{100, 710}, {500, 710}, {500, 730}, {100, 730}},
			Score: 0.95,
		},
	}

	regions := le.clusterAndClassifyRegions(nil, pageW, pageH, lines)

	foundFigure := false
	for _, reg := range regions {
		if reg.Label == LayoutFigure {
			foundFigure = true
			if !strings.Contains(reg.Caption, "4.1.4-6") {
				t.Errorf("expected caption to have 4.1.4-6, got: %s", reg.Caption)
			}
		}
	}
	if !foundFigure {
		t.Fatalf("expected LayoutFigure to be detected")
	}

	// Test paragraph merging of first paragraph
	p1Merged := MergeParagraphLines(lines[0:3])
	expectedP1 := "接管法兰螺栓孔应跨过壳体0。-180。及90。-270。的主轴线。对于不开设在上述主轴线上的管口，正确和错误的设置如图4.1.4-6所示。"
	if p1Merged != expectedP1 {
		t.Errorf("expected p1 to be seamlessly merged:\nExpected: %s\nGot:      %s", expectedP1, p1Merged)
	}

	// Test paragraph merging of second paragraph
	p2Merged := MergeParagraphLines(lines[7:11])
	expectedP2 := "此时，跨中的目的是为了在与外部接管相连接时，螺栓受外部载荷作用时较为均匀，且有利于扳手紧固操作。对于立面安装除了上述优点外，还有一个很重要的目的：当管内介质通过螺栓向外泄漏时，不会使介质直接喷射到螺栓上，特别是存在强腐蚀介质对螺栓的腐蚀的场合，如图4.1.4-6所示，螺栓孔跨壳体主轴线显得更为必要了[18]。"
	if p2Merged != expectedP2 {
		t.Errorf("expected p2 to be seamlessly merged:\nExpected: %s\nGot:      %s", expectedP2, p2Merged)
	}
}
