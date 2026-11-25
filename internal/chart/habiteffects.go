package chart

import (
	"fmt"
	"image"
	"math"
	"strings"

	"golang.org/x/image/font"
)

// EffectMetric is one column of the habit effects table: a figure of the
// night, how big a difference fills its bar, and which way is better.
type EffectMetric struct {
	Label       string  // "Sleep score"
	Unit        string  // "pts", "ms", "bpm", "min"
	Full        float64 // a difference this big fills the bar
	LowerBetter bool    // lowest heart rate: lower is better
}

// HabitEffect is one habit's row: how many nights followed it, and for each
// metric the average after it less the average of nights without it.
type HabitEffect struct {
	Code, Name      string
	Nights, Without int
	Enough          bool // enough nights both ways to compare
	Late            bool // the late form of a meal, sweets or workout: the badge inverted
	Diff            [4]float64
	Has             [4]bool
}

// HabitEffects is the content of the habit effects screen.
type HabitEffects struct {
	Title, Subtitle, Headline string
	Metrics                   [4]EffectMetric
	Rows                      []HabitEffect
	MinNights                 int // nights needed with a habit, and without, to compare

	PanelHead, PanelNote string
	LeadLabel, LeadValue string
	Panel                []PanelRow
	Conclusion           string
}

// HabitEffectsView renders the habit effects in the WolfeSense layout: a
// table, one row per habit, of how the nights after it differed from the
// nights without it, for sleep score, HRV, lowest heart rate and deep
// sleep. Each cell is a bar from a center line: solid to the right when the
// night was better, hatched to the left when worse, its length the size of
// the difference against the column's scale, the difference written beside
// it. A habit without enough nights either way shows how many it has. The
// right-hand column gives the window, the most-logged habits, the clearest
// effect, and the key.
func HabitEffectsView(e HabitEffects, width, height int) (*image.Gray, error) {
	if width < 600 || height < 300 {
		return nil, fmt.Errorf("chart: habit effects need at least 600x300, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad = 16
	drawText(img, face(true, 22), pad, 30, e.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, e.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, e.Headline, ink, alignRight)

	panelW := 236
	if width < 760 {
		panelW = 200
	}
	tbl := image.Rect(pad, 70, width-pad-panelW-12, height-8)
	nameW := 128
	nightsW := 40
	cellW := (tbl.Dx() - nameW - nightsW) / 4
	hf := face(true, 10)
	drawTracked(img, hf, tbl.Min.X, tbl.Min.Y+10, "HABIT", 1, muted)
	drawText(img, hf, tbl.Min.X+nameW+nightsW-6, tbl.Min.Y+10, "NIGHTS", muted, alignRight)
	for i, m := range e.Metrics {
		cx := tbl.Min.X + nameW + nightsW + i*cellW + cellW/2
		drawText(img, hf, cx, tbl.Min.Y+10, strings.ToUpper(m.Label), muted, alignCenter)
		drawText(img, face(false, 9), cx, tbl.Min.Y+22, m.Unit, muted, alignCenter)
	}
	hline(img, tbl.Min.X, tbl.Max.X-1, tbl.Min.Y+28, rule)

	if len(e.Rows) == 0 {
		drawText(img, face(false, 13), tbl.Min.X, tbl.Min.Y+60, "Nothing logged yet. Say \"Hey Siri, log a habit\" after a coffee,", muted, alignLeft)
		drawText(img, face(false, 13), tbl.Min.X, tbl.Min.Y+80, "a drink or a workout; each habit shows here once it has a few nights.", muted, alignLeft)
	}
	top := tbl.Min.Y + 32
	rowH := min(42, max(26, (tbl.Max.Y-top)/max(1, len(e.Rows))))
	for i, row := range e.Rows {
		y0 := top + i*rowH
		if y0+rowH > tbl.Max.Y+4 {
			break
		}
		mid := y0 + rowH/2
		px := habitPill(img, tbl.Min.X, mid+4, row.Code, row.Late) + 6
		drawText(img, face(false, 12), px, mid+4, row.Name, ink, alignLeft)
		drawText(img, face(true, 13), tbl.Min.X+nameW+nightsW-6, mid+5, fmt.Sprint(row.Nights), ink, alignRight)
		for j, m := range e.Metrics {
			c := image.Rect(tbl.Min.X+nameW+nightsW+j*cellW+6, y0, tbl.Min.X+nameW+nightsW+(j+1)*cellW-6, y0+rowH)
			drawEffectCell(img, c, row, j, m, e.MinNights)
		}
		if i < len(e.Rows)-1 {
			hline(img, tbl.Min.X, tbl.Max.X-1, y0+rowH, grid)
		}
	}

	drawEffectsPanel(img, image.Rect(width-pad-panelW, 64, width-pad, height-8), e)
	return img, nil
}

// drawEffectCell draws one habit's difference on one metric in c.
func drawEffectCell(img *image.Gray, c image.Rectangle, row HabitEffect, j int, m EffectMetric, need int) {
	cx := (c.Min.X + c.Max.X) / 2
	mid := (c.Min.Y + c.Max.Y) / 2
	small := face(false, 10)
	if !row.Enough || !row.Has[j] {
		for y := c.Min.Y + 6; y < c.Max.Y-6; y += 3 {
			img.SetGray(cx, y, grid)
		}
		if j == 0 {
			short := max(0, need-row.Nights)
			if row.Nights >= need {
				short = max(0, need-row.Without)
			}
			drawText(img, small, cx+5, mid+4, fmt.Sprintf("need %d more", short), muted, alignLeft)
		}
		return
	}
	vline(img, cx, c.Min.Y+4, c.Max.Y-5, rule)
	d := row.Diff[j]
	better := d > 0
	if m.LowerBetter {
		better = d < 0
	}
	half := float64(c.Dx()/2 - 4)
	l := int(math.Round(math.Min(1, math.Abs(d)/m.Full) * half))
	bar := image.Rect(cx+1, mid-5, cx+1+l, mid+5)
	if !better {
		bar = image.Rect(cx-l, mid-5, cx, mid+5)
	}
	if better {
		fillRect(img, bar, ink)
	} else {
		outline(img, bar, 1, ink)
		hatch(img, bar)
	}
	num := signed(d)
	nf := face(true, 10)
	if better {
		drawText(img, nf, cx-4, mid+4, num, ink, alignRight)
	} else {
		drawText(img, nf, cx+5, mid+4, num, ink, alignLeft)
	}
}

// signed writes a difference with its sign: "+6", "−2", "0".
func signed(d float64) string {
	r := math.Round(d)
	switch {
	case r > 0:
		return fmt.Sprintf("+%.0f", r)
	case r < 0:
		return fmt.Sprintf("−%.0f", -r)
	}
	return "0"
}

// drawEffectsPanel draws the right-hand column and its key.
func drawEffectsPanel(img *image.Gray, r image.Rectangle, e HabitEffects) {
	drawColumn(img, r, column{
		head: strings.ToUpper(e.PanelHead), headNote: e.PanelNote,
		leadLabel: e.LeadLabel, lead: e.LeadValue,
		rows: e.Panel, takeaway: e.Conclusion,
		keyWidth: 36, // the badge, the widest mark
		key: []keyEntry{
			{"better than nights without", func(x, y int) {
				vline(img, x+5, y-10, y+2, rule)
				fillRect(img, image.Rect(x+6, y-8, x+23, y+1), ink)
			}},
			{"worse than nights without", func(x, y int) {
				vline(img, x+17, y-10, y+2, rule)
				b := image.Rect(x, y-8, x+17, y+1)
				outline(img, b, 1, ink)
				hatch(img, b)
			}},
			{fmt.Sprintf("too few nights: %d each way", e.MinNights), func(x, y int) {
				for yy := y - 10; yy < y+3; yy += 3 {
					img.SetGray(x+11, yy, grid)
				}
			}},
			{"the difference, in the column's unit", func(x, y int) {
				drawText(img, face(true, 10), x+11, y, "+6", ink, alignCenter)
			}},
			{"solid: close to bed", func(x, y int) { habitPill(img, x, y, "MEAL", true) }},
		},
	})
}

// wrapText breaks s into lines no wider than w.
func wrapText(f font.Face, s string, w int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		try := strings.TrimSpace(line + " " + word)
		if line != "" && textWidth(f, try) > w {
			lines = append(lines, line)
			line = word
			continue
		}
		line = try
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
