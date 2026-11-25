package chart

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"
)

// ReportRow is one metric's month: its average and the change from the
// month before, formatted by the caller.
type ReportRow struct {
	Label string // "Sleep score"
	Value string // "81"
	Delta string // "+3 vs Oct"; empty with nothing to compare
}

// MonthReport is the content of the month-in-review screen.
type MonthReport struct {
	Title, Subtitle string
	Month           time.Time // the first of the month
	Today           time.Time // later days are left blank
	Rows            []ReportRow
	Scores          map[int]int // day of month -> nightly sleep score
	Best, Worst     int         // days of month to mark; 0 for none
	Highlights      []StatLine
}

// MonthReportView renders a month in review: the month's averages against
// the month before on the left, a calendar of nightly sleep scores on the
// right (the best night inverted, the toughest outlined heavily), and the
// month's standout days along the bottom.
func MonthReportView(m MonthReport, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: month report needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad = 16
	drawText(img, face(true, 22), pad, 30, m.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, m.Subtitle, muted, alignLeft)

	highTop := height - 96
	hline(img, pad, width-pad-1, highTop, rule)

	// left: the averages, one row each
	tableR := width * 54 / 100
	top := 72
	drawText(img, face(true, 12), pad, top+12, "MONTHLY AVERAGE", muted, alignLeft)
	rowH := float64(highTop-top-26) / float64(max(1, len(m.Rows)))
	valueX := pad + 170
	for i, r := range m.Rows {
		y := top + 26 + int(rowH*float64(i)+rowH/2) + 6
		drawText(img, face(false, 15), pad, y, r.Label, ink, alignLeft)
		drawText(img, face(true, 20), valueX, y+1, r.Value, ink, alignRight)
		drawText(img, face(false, 13), valueX+16, y, r.Delta, muted, alignLeft)
		if i > 0 {
			hline(img, pad, tableR-24, int(float64(top+26)+rowH*float64(i)), grid)
		}
	}

	drawMonthCalendar(img, image.Rect(tableR, top, width-pad, highTop-12), m)

	// bottom: standout days, two to a row
	colW := (width - 2*pad) / 2
	for i, h := range m.Highlights {
		x := pad + (i%2)*colW
		y := highTop + 22 + (i/2)*38
		drawText(img, face(true, 11), x, y, strings.ToUpper(h.Label), muted, alignLeft)
		drawText(img, face(false, 14), x, y+18, h.Value, ink, alignLeft)
	}
	return img, nil
}

// drawMonthCalendar lays the month out Sunday to Saturday with each night's
// sleep score; days without a score show a dash, future days nothing.
func drawMonthCalendar(img *image.Gray, r image.Rectangle, m MonthReport) {
	drawText(img, face(true, 12), r.Min.X, r.Min.Y+12, "NIGHTLY SLEEP SCORE", muted, alignLeft)
	first := m.Month
	daysIn := first.AddDate(0, 1, -1).Day()
	offset := int(first.Weekday())
	rows := (offset + daysIn + 6) / 7

	legendH := 22
	gridT := r.Min.Y + 40
	cellW := float64(r.Dx()) / 7
	cellH := math.Min(cellW*0.78, float64(r.Max.Y-legendH-gridT)/float64(rows))
	for c, name := range []string{"S", "M", "T", "W", "T", "F", "S"} {
		drawText(img, face(false, 11), r.Min.X+int(cellW*(float64(c)+0.5)), gridT-6, name, muted, alignCenter)
	}
	today := date(m.Today)
	for dom := 1; dom <= daysIn; dom++ {
		i := offset + dom - 1
		x0 := r.Min.X + int(cellW*float64(i%7))
		y0 := gridT + int(cellH*float64(i/7))
		cell := image.Rect(x0+2, y0+2, x0+int(cellW)-2, y0+int(cellH)-2)
		cx, cy := (cell.Min.X+cell.Max.X)/2, (cell.Min.Y+cell.Max.Y)/2
		day := first.AddDate(0, 0, dom-1)
		if !day.After(today) { // each day that has come a zone: its daily brief
			addZone("day:"+day.Format("2006-01-02"), cell)
		}
		// the day of the month, small, in the corner
		dayColor := muted
		if day.After(today) {
			drawText(img, face(false, 9), cell.Min.X+3, cell.Min.Y+10, fmt.Sprint(dom), grid, alignLeft)
			continue
		}
		score, ok := m.Scores[dom]
		switch {
		case !ok:
			drawText(img, face(false, 13), cx, cy+6, "–", muted, alignCenter)
		case dom == m.Best:
			fillRect(img, cell, ink) // the best night, inverted
			drawText(img, face(true, 15), cx, cy+7, fmt.Sprint(score), surface, alignCenter)
			dayColor = surface
		case dom == m.Worst:
			outline(img, cell, 3, ink) // the toughest night, heavily outlined
			drawText(img, face(true, 15), cx, cy+7, fmt.Sprint(score), ink, alignCenter)
		default:
			outline(img, cell, 1, grid)
			drawText(img, face(false, 15), cx, cy+7, fmt.Sprint(score), ink, alignCenter)
		}
		drawText(img, face(false, 9), cell.Min.X+3, cell.Min.Y+10, fmt.Sprint(dom), dayColor, alignLeft)
	}

	// key
	ly := r.Max.Y - 4
	sw := image.Rect(r.Min.X, ly-11, r.Min.X+16, ly+1)
	fillRect(img, sw, ink)
	drawText(img, labelFace(), sw.Max.X+6, ly, "best night", ink, alignLeft)
	x := sw.Max.X + 6 + textWidth(labelFace(), "best night") + 18
	sw2 := image.Rect(x, ly-11, x+16, ly+1)
	outline(img, sw2, 3, ink)
	drawText(img, labelFace(), sw2.Max.X+6, ly, "toughest night", ink, alignLeft)
}
