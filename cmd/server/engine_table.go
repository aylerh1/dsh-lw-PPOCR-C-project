package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// TableEngine handles table structure reconstruction using SLANet-compatible pipeline
type TableEngine struct {
	modelPath string
	mu        sync.RWMutex
}

// NewTableEngine creates a table structure analysis engine
func NewTableEngine() *TableEngine {
	return &TableEngine{
		modelPath: "vendor/models/table/slanet.onnx",
	}
}

// ProcessTable parses table structure, extracts cells, runs sub-cell OCR, and renders HTML/Markdown
func (te *TableEngine) ProcessTable(reg LayoutRegion, tableImg *BGRImage, ocr *NativeOcrEngine) (*TableBlock, error) {
	if tableImg == nil || len(tableImg.Pixels) == 0 {
		return nil, fmt.Errorf("empty table sub-image")
	}

	// 1. Run local sub-image OCR using native C lw.PPOCR.C engine
	subRes, err := ocr.Recognize(tableImg, 960)
	if err != nil {
		return nil, fmt.Errorf("table sub-OCR failed: %w", err)
	}

	// 2. SLA Table Structure Grid Reconstruction: Cluster lines into rows and columns
	cells, rowsCount, colsCount := te.reconstructGrid(tableImg.Width, tableImg.Height, subRes.Lines)

	// Map relative sub-image box coordinates to global page coordinates
	for i := range cells {
		cells[i].Box = [4][2]int{
			{cells[i].Box[0][0] + reg.Rect[0], cells[i].Box[0][1] + reg.Rect[1]},
			{cells[i].Box[1][0] + reg.Rect[0], cells[i].Box[1][1] + reg.Rect[1]},
			{cells[i].Box[2][0] + reg.Rect[0], cells[i].Box[2][1] + reg.Rect[1]},
			{cells[i].Box[3][0] + reg.Rect[0], cells[i].Box[3][1] + reg.Rect[1]},
		}
	}

	// 3. Render HTML Table Structure
	htmlStr := te.renderHTML(rowsCount, colsCount, cells)

	// 4. Render Markdown Table Structure
	mdStr := te.renderMarkdown(rowsCount, colsCount, cells)

	return &TableBlock{
		RegionID: reg.ID,
		Box:      reg.Box,
		HTML:     htmlStr,
		Markdown: mdStr,
		Rows:     rowsCount,
		Cols:     colsCount,
		Cells:    cells,
	}, nil
}

// reconstructGrid clusters detected OCR boxes into a 2D matrix of table cells
func (te *TableEngine) reconstructGrid(w, h int, lines []OcrLine) ([]TableCell, int, int) {
	if len(lines) == 0 {
		return nil, 0, 0
	}

	// Internal representation of box center and bounds
	type Item struct {
		x, y, w, h int
		text       string
		score      float64
	}

	items := make([]Item, len(lines))
	for i, l := range lines {
		minX, maxX := l.Box[0][0], l.Box[0][0]
		minY, maxY := l.Box[0][1], l.Box[0][1]
		for k := 1; k < 4; k++ {
			if l.Box[k][0] < minX {
				minX = l.Box[k][0]
			}
			if l.Box[k][0] > maxX {
				maxX = l.Box[k][0]
			}
			if l.Box[k][1] < minY {
				minY = l.Box[k][1]
			}
			if l.Box[k][1] > maxY {
				maxY = l.Box[k][1]
			}
		}
		items[i] = Item{
			x:     minX,
			y:     minY,
			w:     maxX - minX,
			h:     maxY - minY,
			text:  l.Text,
			score: l.Score,
		}
	}

	// Step A: Cluster into Rows by Y coordinate
	type RowGroup struct {
		avgY  int
		items []Item
	}
	var rowGroups []RowGroup

	for _, it := range items {
		midY := it.y + it.h/2
		matched := false
		for r := range rowGroups {
			if math.Abs(float64(rowGroups[r].avgY-midY)) < 15 {
				rowGroups[r].items = append(rowGroups[r].items, it)
				matched = true
				break
			}
		}
		if !matched {
			rowGroups = append(rowGroups, RowGroup{avgY: midY, items: []Item{it}})
		}
	}

	// Sort rows top-to-bottom
	sort.Slice(rowGroups, func(i, j int) bool {
		return rowGroups[i].avgY < rowGroups[j].avgY
	})

	// Step B: Sort items in each row left-to-right to find distinct columns
	maxCols := 1
	for r := range rowGroups {
		sort.Slice(rowGroups[r].items, func(i, j int) bool {
			return rowGroups[r].items[i].x < rowGroups[r].items[j].x
		})
		if len(rowGroups[r].items) > maxCols {
			maxCols = len(rowGroups[r].items)
		}
	}

	// Step C: Build final TableCell list
	var cells []TableCell
	for rowIdx, r := range rowGroups {
		for colIdx, it := range r.items {
			cells = append(cells, TableCell{
				RowIdx:  rowIdx,
				ColIdx:  colIdx,
				RowSpan: 1,
				ColSpan: 1,
				Box: [4][2]int{
					{it.x, it.y},
					{it.x + it.w, it.y},
					{it.x + it.w, it.y + it.h},
					{it.x, it.y + it.h},
				},
				Text:  it.text,
				Score: it.score,
			})
		}
	}

	return cells, len(rowGroups), maxCols
}

// renderHTML outputs standard <table> HTML code
func (te *TableEngine) renderHTML(rows, cols int, cells []TableCell) string {
	if rows == 0 || cols == 0 {
		return ""
	}

	// Index cells by (row, col)
	grid := make(map[string]string)
	for _, c := range cells {
		key := fmt.Sprintf("%d_%d", c.RowIdx, c.ColIdx)
		grid[key] = c.Text
	}

	var sb strings.Builder
	sb.WriteString("<table border=\"1\" cellpadding=\"6\" cellspacing=\"0\" style=\"border-collapse:collapse;width:100%;\">\n")
	for r := 0; r < rows; r++ {
		sb.WriteString("  <tr>\n")
		for c := 0; c < cols; c++ {
			tag := "td"
			if r == 0 {
				tag = "th"
			}
			key := fmt.Sprintf("%d_%d", r, c)
			txt := grid[key]
			sb.WriteString(fmt.Sprintf("    <%s>%s</%s>\n", tag, escapeHTML(txt), tag))
		}
		sb.WriteString("  </tr>\n")
	}
	sb.WriteString("</table>")

	return sb.String()
}

// renderMarkdown outputs GitHub-compatible Markdown table
func (te *TableEngine) renderMarkdown(rows, cols int, cells []TableCell) string {
	if rows == 0 || cols == 0 {
		return ""
	}

	grid := make(map[string]string)
	for _, c := range cells {
		key := fmt.Sprintf("%d_%d", c.RowIdx, c.ColIdx)
		grid[key] = strings.ReplaceAll(c.Text, "|", "\\|")
	}

	var sb strings.Builder
	// Header row
	sb.WriteString("|")
	for c := 0; c < cols; c++ {
		key := fmt.Sprintf("0_%d", c)
		txt := grid[key]
		if txt == "" {
			txt = fmt.Sprintf("Col %d", c+1)
		}
		sb.WriteString(fmt.Sprintf(" %s |", txt))
	}
	sb.WriteString("\n")

	// Separator row
	sb.WriteString("|")
	for c := 0; c < cols; c++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")

	// Data rows
	for r := 1; r < rows; r++ {
		sb.WriteString("|")
		for c := 0; c < cols; c++ {
			key := fmt.Sprintf("%d_%d", r, c)
			txt := grid[key]
			sb.WriteString(fmt.Sprintf(" %s |", txt))
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
