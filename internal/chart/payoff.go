package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// PayoffNight is one night: when it began and the sleep score that
// followed. Bed is a night-axis position (minutes after 6 PM).
type PayoffNight struct {
	Bed   float64
	Score float64
}

// PayoffSlot is the nights whose bedtime fell in one half-hour slot.
type PayoffSlot struct {
	Start float64 // night-axis position of the slot's start
	Avg   float64 // their average sleep score
	N     int
}

// Payoff is the content of the bedtime-payoff chart.
type Payoff struct {
	Title, Subtitle string
	Headline        string
	Nights          []PayoffNight
	Slots           []PayoffSlot
	SlotLen         float64 // minutes
	MinSlotNights   int     // fewer than this: the slot's average is drawn hollow, off the line
	UsualBed        float64
	HasUsual        bool
	BestStart       float64 // the user's best window, from their own nights
	BestEnd         float64
	HasBest         bool
	WinStart        float64 // Oura's latest bedtime window
	WinEnd          float64
	HasWindow       bool
	Stats           []StatLine
	// Compare is the earliest third of bedtimes against the latest: what
	// "early" and "late" mean, then rows of "84 vs 75".
	EarlyBy, LateAfter string // "9:21 PM", "10:28 PM"
	Compare            []StatLine
}

// Payoff layout, in pixels.
const (
	payoffPanelW = 196
	payoffCountH = 14 // the row of night counts under the plot
)

// PayoffView renders the bedtime payoff: every night a faint dot at its
// bedtime and the sleep score that followed, and each half hour's average
// as a ringed dot joined by a bold line, so the slope reads at once — does
// an earlier bedtime pay? Slots with too few nights to trust are hollow and
// off the line. Under the plot, each slot's night count; behind it, Oura's
// recommended window as a light gray column and the usual bedtime dashed.
// A side panel holds the key and the earliest-against-latest comparison.
func PayoffView(p Payoff, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: bedtime payoff needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, axisW, footH = 16, 34, 22
	drawText(img, face(true, 22), pad, 30, p.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, p.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, p.Headline, ink, alignRight)

	plotL, plotR := pad+axisW, width-pad-payoffPanelW-18
	top := 76
	plotB := height - footH - payoffCountH - 6

	if len(p.Nights) == 0 {
		drawText(img, labelFace(), plotL+8, (top+plotB)/2, "no nights with a bedtime and a score yet", muted, alignLeft)
		drawPayoffPanel(img, image.Rect(width-pad-payoffPanelW, top-4, width-pad, height-pad), p)
		return img, nil
	}

	// x: whole hours around the nights' bedtimes; y: round tens around the
	// scores
	lo, hi := math.Inf(1), math.Inf(-1)
	slo, shi := math.Inf(1), math.Inf(-1)
	for _, n := range p.Nights {
		lo, hi = math.Min(lo, n.Bed), math.Max(hi, n.Bed)
		slo, shi = math.Min(slo, n.Score), math.Max(shi, n.Score)
	}
	if p.HasWindow {
		lo, hi = math.Min(lo, p.WinStart), math.Max(hi, p.WinEnd)
	}
	if p.HasBest {
		lo, hi = math.Min(lo, p.BestStart), math.Max(hi, p.BestEnd)
	}
	xmin := math.Floor(lo/60) * 60
	xmax := math.Ceil((hi+1)/60) * 60
	ymin := math.Max(0, math.Floor((slo-2)/10)*10)
	ymax := math.Min(100, math.Ceil((shi+2)/10)*10)
	x := func(pos float64) float64 {
		return float64(plotL) + (pos-xmin)/(xmax-xmin)*float64(plotR-plotL)
	}
	y := func(v float64) float64 {
		return float64(plotB) - (v-ymin)/(ymax-ymin)*float64(plotB-top)
	}

	// the best window behind everything, a light gray column
	if p.HasBest {
		fillRect(img, image.Rect(int(math.Round(x(p.BestStart))), top, int(math.Round(x(p.BestEnd)))+1, plotB), windowShade)
	}

	for v := ymin; v <= ymax+0.01; v += 10 {
		yy := int(math.Round(y(v)))
		hline(img, plotL, plotR-1, yy, grid)
		drawText(img, face(false, 11), plotL-6, yy+4, fmt.Sprintf("%.0f", v), muted, alignRight)
	}
	hline(img, plotL, plotR-1, plotB, ink)
	// hour labels, every other hour where hours are narrow, and a tick each
	// half hour
	every := 60.0
	if x(xmin+60)-x(xmin) < 48 {
		every = 120
	}
	for pos := xmin; pos <= xmax+0.01; pos += 30 {
		xx := int(math.Round(x(pos)))
		vline(img, xx, plotB, plotB+3, muted)
		if math.Mod(pos-xmin, every) == 0 {
			a := alignCenter
			if pos == xmin {
				a = alignLeft
			} else if pos == xmax {
				a = alignRight
			}
			drawText(img, face(false, 11), xx, height-6, hourWords(pos), muted, a)
		}
	}
	drawText(img, face(false, 10), plotL-4, plotB+payoffCountH+2, "nights", muted, alignRight)

	// the usual bedtime, dashed
	if p.HasUsual {
		ux := int(math.Round(x(p.UsualBed)))
		for yy := top; yy < plotB; yy++ {
			if (yy/4)%2 == 0 {
				img.SetGray(ux, yy, ink)
			}
		}
	}

	// every night, faint
	for _, n := range p.Nights {
		disc(img, x(n.Bed), y(n.Score), 2, muted)
	}

	// the slots: counts under the axis, averages joined where trusted
	var line [][2]float64
	half := p.SlotLen / 2
	for _, s := range p.Slots {
		cx := x(s.Start + half)
		drawText(img, face(false, 10), int(cx), plotB+payoffCountH+2, fmt.Sprint(s.N), muted, alignCenter)
		if s.N >= p.MinSlotNights {
			line = append(line, [2]float64{cx, y(s.Avg)})
		}
	}
	for _, pass := range []struct {
		w float64
		c color.Gray
	}{{7, surface}, {3, ink}} {
		for k := 1; k < len(line); k++ {
			thickLine(img, line[k-1][0], line[k-1][1], line[k][0], line[k][1], pass.w, pass.c)
		}
	}
	for _, s := range p.Slots {
		cx, cy := x(s.Start+half), y(s.Avg)
		disc(img, cx, cy, dotRadius+dotRing, surface)
		disc(img, cx, cy, dotRadius, ink)
		if s.N < p.MinSlotNights { // too few nights to trust: hollow
			disc(img, cx, cy, dotRadius-1.5, surface)
		}
	}

	// Oura's window: a hatched bar along the top of the plot, a span marker
	// rather than a column, so it can't be mistaken for the usual bedtime's
	// dashed line
	if p.HasWindow {
		r := image.Rect(int(math.Round(x(p.WinStart))), top+3, int(math.Round(x(p.WinEnd)))+1, top+11)
		ouraBar(img, r)
		f := face(true, 10)
		if lx := r.Max.X + 4; lx+textWidth(f, "Oura") < plotR {
			fillRect(img, image.Rect(lx-1, r.Min.Y-2, lx+textWidth(f, "Oura")+1, r.Max.Y+2), surface)
			drawText(img, f, lx, r.Max.Y, "Oura", ink, alignLeft)
		}
	}

	// the window's label above the plot, centered over the column but kept
	// within the plot: a narrow window can't fit it inside
	if p.HasBest {
		f := face(true, 11)
		label := "your best window"
		tw := textWidth(f, label)
		cx := int(math.Round((x(p.BestStart) + x(p.BestEnd)) / 2))
		cx = max(plotL+tw/2, min(cx, plotR-tw/2))
		drawText(img, f, cx, top-6, label, ink, alignCenter)
	}

	drawPayoffPanel(img, image.Rect(width-pad-payoffPanelW, top-4, width-pad, height-pad), p)
	return img, nil
}

// ouraBar draws Oura's bedtime window as a span marker: a white bar,
// outlined, with diagonal hatching inside.
func ouraBar(img *image.Gray, r image.Rectangle) {
	fillRect(img, r, surface)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if (x+y)%4 == 0 {
				img.SetGray(x, y, ink)
			}
		}
	}
	outline(img, r, 1, ink)
}

// hourWords is a night-axis hour as a short label: "9 PM", "12 AM".
func hourWords(pos float64) string {
	m := (int(math.Round(pos)) + nightStartHr*60) % 1440
	h := m / 60
	switch {
	case h == 0:
		return "12 AM"
	case h == 12:
		return "noon"
	case h < 12:
		return fmt.Sprintf("%d AM", h)
	}
	return fmt.Sprintf("%d PM", h-12)
}

// drawPayoffPanel draws the key, each mark as on the chart, then the
// figures.
func drawPayoffPanel(img *image.Gray, r image.Rectangle, p Payoff) {
	x := r.Min.X
	f := face(false, 12)
	y := r.Min.Y + 8
	drawText(img, face(true, 13), x, y, "Key", ink, alignLeft)
	row := func(mark func(cx, cy int), text string) {
		y += 19
		mark(x+11, y-4)
		drawText(img, f, x+28, y, text, ink, alignLeft)
	}
	row(func(cx, cy int) { disc(img, float64(cx), float64(cy), 2, muted) }, "one night")
	row(func(cx, cy int) {
		thickLine(img, float64(cx-10), float64(cy+2), float64(cx+10), float64(cy-2), 3, ink)
		disc(img, float64(cx), float64(cy), dotRadius+dotRing, surface)
		disc(img, float64(cx), float64(cy), dotRadius, ink)
	}, "average sleep score")
	row(func(cx, cy int) {
		disc(img, float64(cx), float64(cy), dotRadius, ink)
		disc(img, float64(cx), float64(cy), dotRadius-1.5, surface)
	}, "too few nights to trust")
	row(func(cx, cy int) {
		for yy := cy - 7; yy <= cy+5; yy++ {
			if (yy/4)%2 == 0 {
				img.SetGray(cx, yy, ink)
			}
		}
	}, "your usual bedtime")
	if p.HasBest {
		row(func(cx, cy int) { fillRect(img, image.Rect(cx-10, cy-6, cx+11, cy+6), windowShade) }, "your best window")
	}
	if p.HasWindow {
		row(func(cx, cy int) { ouraBar(img, image.Rect(cx-10, cy-4, cx+11, cy+4)) }, "Oura's bedtime window")
	}

	y += 26
	drawText(img, face(true, 13), x, y, "Figures", ink, alignLeft)
	y += 2
	stat := func(s StatLine) {
		y += 18
		if y > r.Max.Y {
			return
		}
		drawText(img, labelFace(), x, y, s.Label, muted, alignLeft)
		drawText(img, face(true, 13), r.Max.X, y, s.Value, ink, alignRight)
	}
	for _, s := range p.Stats {
		stat(s)
	}
	if len(p.Compare) > 0 {
		y += 24
		drawText(img, face(true, 13), x, y, "Early vs late nights", ink, alignLeft)
		for _, note := range []string{"early: in bed by " + p.EarlyBy, "late: in bed after " + p.LateAfter} {
			y += 14
			drawText(img, face(false, 11), x, y, note, muted, alignLeft)
		}
		y -= 2
		for _, s := range p.Compare {
			stat(s)
		}
	}
}
