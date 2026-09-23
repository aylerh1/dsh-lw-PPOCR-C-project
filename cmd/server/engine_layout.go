package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// LayoutEngine implements high-speed document layout detection and regional routing
type LayoutEngine struct {
	modelPath string
	mu        sync.RWMutex
}

// NewLayoutEngine creates a new layout detection engine
func NewLayoutEngine() *LayoutEngine {
	return &LayoutEngine{
		modelPath: "vendor/models/layout/pp_doclayout_s.onnx",
	}
}

// LineBox tracks bounding coordinates and classification state of text lines
type LineBox struct {
	x1, y1, x2, y2 int
	line           OcrLine
	classified     bool
}

// Detect analyzes the image layout and returns classified regions and all OCR lines
func (le *LayoutEngine) Detect(bgr *BGRImage, ocr *NativeOcrEngine) ([]LayoutRegion, []OcrLine, error) {
	if bgr == nil || len(bgr.Pixels) == 0 {
		return nil, nil, fmt.Errorf("empty image for layout detection")
	}

	// 1. Fast coarse-to-fine detection: Leverage native C DBNet detection & visual geometry
	ocrRes, err := ocr.Recognize(bgr, 960)
	if err != nil {
		return nil, nil, fmt.Errorf("OCR detection pass failed: %w", err)
	}

	regions := le.clusterAndClassifyRegions(bgr, bgr.Width, bgr.Height, ocrRes.Lines)
	return regions, ocrRes.Lines, nil
}

// isRegularBodyText checks if a text line belongs to regular body paragraph text rather than figure annotation
func isRegularBodyText(box LineBox, pageW int) bool {
	txt := strings.TrimSpace(box.line.Text)
	runes := []rune(txt)

	// 1. Long text (>= 12 runes) is regular body text
	if len(runes) >= 12 {
		return true
	}
	// 2. Sentences with commas, periods, colons, or common paragraph words
	if strings.ContainsAny(txt, "，。；？！“”：:") ||
		strings.Contains(txt, "如图") || strings.Contains(txt, "所示") ||
		strings.Contains(txt, "对于") || strings.Contains(txt, "因此") ||
		strings.Contains(txt, "此时") || strings.Contains(txt, "设置") ||
		strings.Contains(txt, "主轴线") {
		return true
	}
	// 3. Wide span text spanning more than 40% of page width
	if box.x2-box.x1 > int(float64(pageW)*0.40) && len(runes) >= 7 {
		return true
	}

	return false
}

// clusterAndClassifyRegions classifies layout regions using geometric structure, line densities, and symbols
func (le *LayoutEngine) clusterAndClassifyRegions(bgr *BGRImage, pageW, pageH int, lines []OcrLine) []LayoutRegion {
	if len(lines) == 0 {
		return []LayoutRegion{
			{
				ID:    1,
				Label: LayoutText,
				Score: 0.9,
				Box:   [4][2]int{{0, 0}, {pageW, 0}, {pageW, pageH}, {0, pageH}},
				Rect:  [4]int{0, 0, pageW, pageH},
			},
		}
	}

	// 1. Convert lines to bounding rects [x1, y1, x2, y2]
	boxes := make([]LineBox, len(lines))
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
		boxes[i] = LineBox{x1: minX, y1: minY, x2: maxX, y2: maxY, line: l}
	}

	var regions []LayoutRegion
	regionID := 1

	// 2. Identify potential Header and Footer (with strict paragraph continuity protection)
	headerThresh := int(float64(pageH) * 0.08)
	footerThresh := int(float64(pageH) * 0.93)

	for i := range boxes {
		if boxes[i].classified {
			continue
		}
		// Header check
		if boxes[i].y2 <= headerThresh {
			boxes[i].classified = true
			regions = append(regions, makeRegion(regionID, LayoutHeader, boxes[i].line.Score,
				boxes[i].x1, boxes[i].y1, boxes[i].x2-boxes[i].x1, boxes[i].y2-boxes[i].y1, ""))
			regionID++
			continue
		}
		// Footer check: Must be isolated from preceding text, not part of regular running paragraph
		if boxes[i].y1 >= footerThresh {
			txt := strings.TrimSpace(boxes[i].line.Text)
			runes := []rune(txt)
			isPureFooter := len(runes) <= 12 && !strings.HasSuffix(txt, "。") && !strings.Contains(txt, "所示")

			// Check vertical gap with preceding lines
			hasTightPrecedingLine := false
			for j := range boxes {
				if j != i && boxes[j].y2 < boxes[i].y1 && (boxes[i].y1-boxes[j].y2 < 18) {
					hasTightPrecedingLine = true
					break
				}
			}

			if isPureFooter && !hasTightPrecedingLine {
				boxes[i].classified = true
				regions = append(regions, makeRegion(regionID, LayoutFooter, boxes[i].line.Score,
					boxes[i].x1, boxes[i].y1, boxes[i].x2-boxes[i].x1, boxes[i].y2-boxes[i].y1, ""))
				regionID++
				continue
			}
		}
	}

	// 3. Identify Formula Regions (mathematical symbols: =, \int, \sum, \pm, ^, _, √, etc.)
	formulaSymbols := []string{"=", "+", "-", "×", "÷", "√", "∫", "∑", "∏", "lim", "sin", "cos", "tan", "log", "ln", "α", "β", "γ", "θ", "λ", "π", "σ", "ω", "Δ", "≠", "≤", "≥", "±", "∞"}
	for i := range boxes {
		if boxes[i].classified {
			continue
		}
		text := boxes[i].line.Text
		symbolCount := 0
		for _, sym := range formulaSymbols {
			if strings.Contains(text, sym) {
				symbolCount++
			}
		}
		if (symbolCount >= 2 && len(text) < 30) || (symbolCount >= 1 && (strings.Contains(text, "f(x)") || strings.Contains(text, "y=") || strings.Contains(text, "x="))) {
			boxes[i].classified = true
			regions = append(regions, makeRegion(regionID, LayoutFormula, boxes[i].line.Score,
				boxes[i].x1, boxes[i].y1, boxes[i].x2-boxes[i].x1, boxes[i].y2-boxes[i].y1, ""))
			regionID++
		}
	}

	// 4. Identify Figure / Illustration Regions & Captions
	// Look for figure captions like "图4.1.4-6 ...", "Figure 1", "附图"
	for i := range boxes {
		if boxes[i].classified {
			continue
		}
		txt := strings.TrimSpace(boxes[i].line.Text)
		isCaption := (strings.HasPrefix(txt, "图") || strings.HasPrefix(txt, "附图") ||
			strings.HasPrefix(txt, "Figure") || strings.HasPrefix(txt, "Fig.")) && len([]rune(txt)) <= 50

		if isCaption {
			captionBox := boxes[i]
			figTop := 0
			var linesAbove []int
			for j := range boxes {
				if j != i && boxes[j].y2 < captionBox.y1 {
					linesAbove = append(linesAbove, j)
				}
			}
			sort.Slice(linesAbove, func(a, b int) bool {
				return boxes[linesAbove[a]].y2 > boxes[linesAbove[b]].y2
			})

			// Collect internal scattered labels (short texts, numbers, labels like (a)正确, angles)
			var internalLabels []int
			for _, idx := range linesAbove {
				if !isRegularBodyText(boxes[idx], pageW) && (captionBox.y1-boxes[idx].y2 < int(float64(pageH)*0.75)) {
					internalLabels = append(internalLabels, idx)
				} else {
					// Reached preceding regular paragraph line (e.g. "口，正确和错误的设置如图4.1.4-6所示。")
					figTop = boxes[idx].y2 + 6
					break
				}
			}

			if figTop == 0 {
				if len(internalLabels) > 0 {
					minLabelY := captionBox.y1
					for _, lIdx := range internalLabels {
						if boxes[lIdx].y1 < minLabelY {
							minLabelY = boxes[lIdx].y1
						}
					}
					figTop = max(0, minLabelY-25)
				} else {
					figTop = max(0, captionBox.y1-int(float64(pageH)*0.35))
				}
			}

			figBottom := captionBox.y2 + 4
			figH := figBottom - figTop
			if figH >= 40 {
				boxes[i].classified = true

				// Calculate tighter horizontal span based strictly on internal labels and caption
				figX1 := captionBox.x1 - 16
				figX2 := captionBox.x2 + 16
				if len(internalLabels) > 0 {
					minLabelX := captionBox.x1
					maxLabelX := captionBox.x2
					for _, lIdx := range internalLabels {
						boxes[lIdx].classified = true
						if boxes[lIdx].x1 < minLabelX {
							minLabelX = boxes[lIdx].x1
						}
						if boxes[lIdx].x2 > maxLabelX {
							maxLabelX = boxes[lIdx].x2
						}
					}
					// Add comfortable padding around labels and caption
					figX1 = max(0, minLabelX-24)
					figX2 = min(pageW, maxLabelX+24)
				} else {
					figX1 = max(0, int(float64(pageW)*0.15))
					figX2 = min(pageW, int(float64(pageW)*0.85))
				}

				// If visual BGR image is available, perform pixel-level tight content boundary fitting!
				if bgr != nil {
					candTop := figTop
					candBottom := captionBox.y1 - 2
					candH := candBottom - candTop
					if candH > 20 {
						searchW := min(pageW-figX1, max(figX2-figX1, 300))
						searchX := max(0, (figX1+figX2)/2-searchW/2)
						tight := bgr.FindTightContentBounds(searchX, candTop, searchW, candH, 12)
						if tight[2] > 30 && tight[3] > 30 {
							figX1 = min(tight[0], captionBox.x1-12)
							figX2 = max(tight[0]+tight[2], captionBox.x2+12)
							figX1 = max(0, figX1)
							figX2 = min(pageW, figX2)
							figTop = max(figTop, tight[1]-6)
						}
					}
				}

				// Absorb all remaining unclassified boxes inside this figure bounding rectangle
				for k := range boxes {
					if !boxes[k].classified && boxes[k].y1 >= figTop-4 && boxes[k].y2 <= captionBox.y1+2 {
						boxes[k].classified = true
					}
				}

				regions = append(regions, makeRegion(regionID, LayoutFigure, captionBox.line.Score,
					figX1, figTop, figX2-figX1, figH, txt))
				regionID++
			}
		}
	}

	// 5. Identify Table Regions (groups of boxes arranged in rows and columns)
	unclassified := make([]int, 0)
	for i := range boxes {
		if !boxes[i].classified {
			unclassified = append(unclassified, i)
		}
	}

	tableGroups := findTableGroups(boxes, unclassified)
	for _, group := range tableGroups {
		if len(group) >= 4 {
			minX, minY := pageW, pageH
			maxX, maxY := 0, 0
			var totalScore float64
			for _, idx := range group {
				boxes[idx].classified = true
				if boxes[idx].x1 < minX {
					minX = boxes[idx].x1
				}
				if boxes[idx].y1 < minY {
					minY = boxes[idx].y1
				}
				if boxes[idx].x2 > maxX {
					maxX = boxes[idx].x2
				}
				if boxes[idx].y2 > maxY {
					maxY = boxes[idx].y2
				}
				totalScore += boxes[idx].line.Score
			}
			pad := 6
			minX = max(0, minX-pad)
			minY = max(0, minY-pad)
			maxX = min(pageW, maxX+pad)
			maxY = min(pageH, maxY+pad)

			avgScore := totalScore / float64(len(group))
			regions = append(regions, makeRegion(regionID, LayoutTable, avgScore,
				minX, minY, maxX-minX, maxY-minY, ""))
			regionID++
		}
	}

	// 6. Group remaining text boxes into Paragraphs and Titles
	remaining := make([]int, 0)
	for i := range boxes {
		if !boxes[i].classified {
			remaining = append(remaining, i)
		}
	}

	sort.Slice(remaining, func(i, j int) bool {
		return boxes[remaining[i]].y1 < boxes[remaining[j]].y1
	})

	for len(remaining) > 0 {
		currIdx := remaining[0]
		remaining = remaining[1:]
		boxes[currIdx].classified = true

		blockX1 := boxes[currIdx].x1
		blockY1 := boxes[currIdx].y1
		blockX2 := boxes[currIdx].x2
		blockY2 := boxes[currIdx].y2
		blockScore := boxes[currIdx].line.Score
		blockCount := 1

		h := boxes[currIdx].y2 - boxes[currIdx].y1
		currText := strings.TrimSpace(boxes[currIdx].line.Text)

		// Strict title checking: only if explicit heading prefix or distinctly large font with space around it
		isTitle := (strings.HasPrefix(currText, "#") || strings.HasPrefix(currText, "第") && strings.Contains(currText, "章")) &&
			len([]rune(currText)) < 30

		if isTitle {
			regions = append(regions, makeRegion(regionID, LayoutTitle, blockScore,
				blockX1, blockY1, blockX2-blockX1, blockY2-blockY1, ""))
			regionID++
			continue
		}

		// Cluster sequential lines that belong to the same paragraph
		var nextRemaining []int
		lineGapThreshold := int(float64(h) * 2.2)
		if lineGapThreshold < 20 {
			lineGapThreshold = 20
		}

		lastLineInBlock := boxes[currIdx]

		for _, otherIdx := range remaining {
			candBox := boxes[otherIdx]
			vertGap := candBox.y1 - lastLineInBlock.y2

			// If within normal vertical line spacing
			if vertGap <= lineGapThreshold && candBox.y1 >= lastLineInBlock.y1 {
				// Check if the previous line ended with a full stop, causing an intentional paragraph break
				prevText := strings.TrimSpace(lastLineInBlock.line.Text)
				prevEndedFullStop := strings.HasSuffix(prevText, "。") ||
					strings.HasSuffix(prevText, "！") || strings.HasSuffix(prevText, "？")

				// If previous line ended with sentence full stop AND there is a distinct gap, stop clustering
				if prevEndedFullStop && vertGap > int(float64(h)*1.4) {
					nextRemaining = append(nextRemaining, otherIdx)
					continue
				}

				// Merge into current paragraph block
				if candBox.x1 < blockX1 {
					blockX1 = candBox.x1
				}
				if candBox.y1 < blockY1 {
					blockY1 = candBox.y1
				}
				if candBox.x2 > blockX2 {
					blockX2 = candBox.x2
				}
				if candBox.y2 > blockY2 {
					blockY2 = candBox.y2
				}
				blockScore += candBox.line.Score
				blockCount++
				lastLineInBlock = candBox
				boxes[otherIdx].classified = true
			} else {
				nextRemaining = append(nextRemaining, otherIdx)
			}
		}
		remaining = nextRemaining

		avgScore := blockScore / float64(blockCount)
		regions = append(regions, makeRegion(regionID, LayoutText, avgScore,
			blockX1, blockY1, blockX2-blockX1, blockY2-blockY1, ""))
		regionID++
	}

	return regions
}

// findTableGroups identifies clusters of boxes that align as multiple columns in multiple rows
func findTableGroups(boxes []LineBox, unclassified []int) [][]int {
	if len(unclassified) < 4 {
		return nil
	}

	type RowCluster struct {
		indices []int
		avgY    int
	}
	var rows []RowCluster

	for _, idx := range unclassified {
		midY := (boxes[idx].y1 + boxes[idx].y2) / 2
		matched := false
		for r := range rows {
			if math.Abs(float64(rows[r].avgY-midY)) < 12 {
				rows[r].indices = append(rows[r].indices, idx)
				matched = true
				break
			}
		}
		if !matched {
			rows = append(rows, RowCluster{indices: []int{idx}, avgY: midY})
		}
	}

	var multiColRows []RowCluster
	for _, r := range rows {
		if len(r.indices) >= 2 {
			multiColRows = append(multiColRows, r)
		}
	}

	if len(multiColRows) >= 2 {
		var tableIndices []int
		for _, r := range multiColRows {
			tableIndices = append(tableIndices, r.indices...)
		}
		return [][]int{tableIndices}
	}

	return nil
}

func makeRegion(id int, label LayoutCategory, score float64, x, y, w, h int, caption string) LayoutRegion {
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return LayoutRegion{
		ID:      id,
		Label:   label,
		Score:   math.Round(score*1000) / 1000,
		Caption: caption,
		Box: [4][2]int{
			{x, y},
			{x + w, y},
			{x + w, y + h},
			{x, y + h},
		},
		Rect: [4]int{x, y, w, h},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
