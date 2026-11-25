package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

// DeviationSpoke is one reading on the deviation radar: last night's value
// as a distance from the user's normal, in half-ranges — 0 the middle of
// the normal range, ±1 its edges.
type DeviationSpoke struct {
	Label  string
	Text   string  // last night's value, formatted: "57 ms"
	D      float64 // the deviation
	Has    bool
	AvgD   float64 // the nights before, averaged
	HasAvg bool
	Alert  bool   // off normal in a way that matters (the vital-signs rules)
	Change string // against the comparison, in the reading's units: "+4 ms", "same"
}

// DeviationRadar is the content of the deviation radar.
type DeviationRadar struct {
	Title, Subtitle string
	Headline        string
	Spokes          []DeviationSpoke
	// The key's wording; empty for the nightly radar's.
	ShapeLabel, AvgLabel string // "last night", "the 6 nights before"
	Inverted             string // what an inverted value means: "inverted: off normal"
	// The panel, in the sleep radar's layout: a header pair, a summary
	// figure, then a takeaway: a heading and the readings it names, or a
	// line for when it names none.
	PanelTitle, PanelVs   string // "LAST NIGHT", "vs before"
	SummaryLabel, Summary string // "In your normal", "3/6"
	TakeawayHead          string // "Off normal:"
	Takeaway              []string
	TakeawayNone          string // "Every reading within your normal"
}

// or returns s, or def when s is empty.
func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// The deviation scale, from the center out, in half-ranges: the rim, the
// caution rings, and how far in a spoke can go before the center.
const (
	devRim     = 2.6
	devCaution = 2.0
	devFloor   = -2.8
)

// DeviationView renders the deviation radar: one spoke per reading, and
// on each, last night's distance from the user's own normal. The normal
// range is a dotted band around a middle ring, so a night of normal readings draws
// a round shape inside the band; a spoke pushing outward is above normal,
// one pulled inward below it. Dashed rings mark the caution distance.
// Last night is a heavy unfilled line, the nights before a dashed outline,
// both over a lightly dotted band, so the shape reads cleanly. Labels carry each value; a value
// off normal in a way that matters is inverted.
func DeviationView(d DeviationRadar, width, height int) (*image.Gray, error) {
	if width < 600 || height < 300 {
		return nil, fmt.Errorf("chart: deviation radar needs at least 600x300, got %dx%d", width, height)
	}
	n := len(d.Spokes)
	if n < 3 {
		return nil, fmt.Errorf("chart: a radar needs at least 3 spokes, got %d", n)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad = 16
	drawText(img, face(true, 22), pad, 30, d.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, d.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, d.Headline, ink, alignRight)

	panelW := 236 // the sleep radar's column
	plot := image.Rect(pad, 64, width-pad-panelW-12, height-8)
	// room for the labels past the rim: two lines above the top spoke,
	// two below the bottom one
	const labelTop, labelBottom = 42, 50
	cx := float64(plot.Min.X+plot.Max.X) / 2
	rimFrac := 0.55 + 0.17*devRim // the rim's radius as a fraction of R
	R := math.Min((float64(plot.Dx())/2-80)/rimFrac, (float64(plot.Dy())-labelTop-labelBottom)/(2*rimFrac))
	cy := float64(plot.Min.Y) + labelTop + R*rimFrac
	// radius for a deviation: the middle ring at 55% of R, a half-range
	// 17% of R
	rad := func(dev float64) float64 {
		return R * (0.55 + 0.17*math.Max(devFloor, math.Min(devRim, dev)))
	}
	at := func(i int, dev float64) (float64, float64) {
		a := float64(i) / float64(n) * 2 * math.Pi
		r := rad(dev)
		return cx + r*math.Sin(a), cy - r*math.Cos(a)
	}
	poly := func(dev func(i int) float64) [][2]float64 {
		pts := make([][2]float64, n)
		for i := range pts {
			x, y := at(i, dev(i))
			pts[i] = [2]float64{x, y}
		}
		return pts
	}
	ringAt := func(dev float64, w float64, c color.Gray) {
		pts := poly(func(int) float64 { return dev })
		for i := range pts {
			j := (i + 1) % n
			thickLine(img, pts[i][0], pts[i][1], pts[j][0], pts[j][1], w, c)
		}
	}

	// the normal band: sparse dots between the -1 and +1 rings, a light
	// tint that last night's line reads over
	outer := polygonMask(img.Rect, poly(func(int) float64 { return 1 }))
	inner := polygonMask(img.Rect, poly(func(int) float64 { return -1 }))
	for p := range outer {
		if !inner[p] && bandDot(p.X, p.Y) {
			img.SetGray(p.X, p.Y, ink)
		}
	}
	ringAt(devRim, 1, rule)
	ringAt(1, 1, grid)
	ringAt(-1, 1, grid)
	ringAt(0, 1.5, muted)
	// the caution distance: fine dotted rings, so dashes mean only the
	// comparison shape
	for _, dev := range []float64{devCaution, -devCaution} {
		pts := poly(func(int) float64 { return dev })
		for i := range pts {
			j := (i + 1) % n
			dottedLine(img, pts[i][0], pts[i][1], pts[j][0], pts[j][1])
		}
	}
	// spokes from the inner caution ring out, leaving the middle for the
	// mark: the Wolfe Health outline, in light gray, under the data
	for i := 0; i < n; i++ {
		x0, y0 := at(i, -devCaution)
		x1, y1 := at(i, devRim)
		thickLine(img, x0, y0, x1, y1, 1, grid)
	}
	DrawWolfOutline(img, cx, cy, 1.45*rad(-devCaution), 1.5, rule)

	// a shape through only the spokes that have a reading: a missing one
	// is left out, not drawn as normal
	shape := func(has func(DeviationSpoke) bool, dev func(DeviationSpoke) float64) [][2]float64 {
		var pts [][2]float64
		for i, s := range d.Spokes {
			if has(s) {
				x, y := at(i, dev(s))
				pts = append(pts, [2]float64{x, y})
			}
		}
		return pts
	}
	closed := func(pts [][2]float64, draw func(a, b [2]float64)) {
		if len(pts) < 2 {
			return
		}
		for i := range pts {
			draw(pts[i], pts[(i+1)%len(pts)])
		}
	}

	// the nights before: dashed, with a white edge so it reads over the band
	avg := shape(func(s DeviationSpoke) bool { return s.HasAvg }, func(s DeviationSpoke) float64 { return s.AvgD })
	for _, pass := range []struct {
		w float64
		c color.Gray
	}{{5, surface}, {2.5, ink}} {
		closed(avg, func(a, b [2]float64) { dashedLine(img, a[0], a[1], b[0], b[1], pass.w, pass.c) })
	}
	// last night: a heavy line on a white edge, unfilled, so the band and
	// rings stay readable under it
	night := shape(func(s DeviationSpoke) bool { return s.Has }, func(s DeviationSpoke) float64 { return s.D })
	closed(night, func(a, b [2]float64) { thickLine(img, a[0], a[1], b[0], b[1], 6, surface) })
	closed(night, func(a, b [2]float64) { thickLine(img, a[0], a[1], b[0], b[1], 3, ink) })
	for _, pt := range night {
		disc(img, pt[0], pt[1], 4.5, surface)
		disc(img, pt[0], pt[1], 3.5, ink)
	}

	// labels round the rim: the reading, then last night's value
	for i, s := range d.Spokes {
		a := float64(i) / float64(n) * 2 * math.Pi
		lr := rad(devRim) + 14
		lx, ly := cx+lr*math.Sin(a), cy-lr*math.Cos(a)
		al := alignCenter
		switch {
		case math.Sin(a) > 0.25:
			al = alignLeft
		case math.Sin(a) < -0.25:
			al = alignRight
		}
		var top float64
		switch {
		case math.Cos(a) > 0.5:
			top = ly - 26
		case math.Cos(a) < -0.5:
			top = ly + 4
		default:
			top = ly - 12
		}
		drawTracked(img, face(true, 11), labelX(int(lx), trackedWidth(face(true, 11), strings.ToUpper(s.Label), 1), al), int(top)+11,
			strings.ToUpper(s.Label), 1, muted)
		v := "—"
		if s.Has {
			v = s.Text
		}
		vf := face(true, 14)
		vy := int(top) + 27
		if s.Alert {
			w := textWidth(vf, v)
			x0 := labelX(int(lx), w, al)
			fillRect(img, image.Rect(x0-4, vy-12, x0+w+4, vy+4), ink)
			drawText(img, vf, x0, vy, v, surface, alignLeft)
			continue
		}
		drawText(img, vf, int(lx), vy, v, ink, al)
	}

	drawDeviationPanel(img, image.Rect(width-pad-panelW, 64, width-pad, height-8), d)
	return img, nil
}

// labelX is the left edge of a label of width w anchored at x by al.
func labelX(x, w int, al align) int {
	switch al {
	case alignCenter:
		return x - w/2
	case alignRight:
		return x - w
	}
	return x
}

// drawDeviationPanel draws the right column in the sleep radar's layout:
// a header, the summary figure, every reading furthest from normal first
// with its value (inverted when off normal) and its change, the takeaway,
// and the key at the foot.
func drawDeviationPanel(img *image.Gray, r image.Rectangle, d DeviationRadar) {
	// the readings, furthest from normal first; those without one last
	order := make([]int, len(d.Spokes))
	for i := range order {
		order[i] = i
	}
	dist := func(s DeviationSpoke) float64 {
		if !s.Has {
			return -1
		}
		return math.Abs(s.D)
	}
	for i := 1; i < len(order); i++ { // insertion sort: stable
		for j := i; j > 0 && dist(d.Spokes[order[j]]) > dist(d.Spokes[order[j-1]]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var rows []PanelRow
	for _, i := range order {
		s := d.Spokes[i]
		row := PanelRow{Label: s.Label, Value: "—", Note: s.Change, Alert: s.Alert}
		if s.Has {
			row.Value = s.Text
		}
		rows = append(rows, row)
	}
	c := column{
		head: or(d.PanelTitle, "LAST NIGHT"), headNote: or(d.PanelVs, "vs before"),
		leadLabel: d.SummaryLabel, lead: d.Summary,
		rows: rows, rowMax: 24,
		key: radarKey(img, or(d.ShapeLabel, "last night"), or(d.AvgLabel, "the 6 nights before"),
			keyEntry{"caution distance", dottedSwatch(img)},
			keyEntry{"normal · " + or(d.Inverted, "inverted: off normal"), bandSwatch(img)}),
	}
	if len(d.Takeaway) == 0 {
		c.takeaway = d.TakeawayNone
	} else {
		c.takeawayHead, c.takeawayItems = d.TakeawayHead, d.Takeaway
	}
	drawColumn(img, r, c)
}

// bandDot is the normal band's texture: one dot every 4 pixels each way,
// lighter than the 3-pixel dots elsewhere, so a line over it stays clear.
func bandDot(x, y int) bool { return x%4 == 0 && y%4 == 0 }

// dottedLine draws a fine dotted line: a single dot every 4 pixels along
// it, the caution rings' style, unlike the comparison shape's dashes.
func dottedLine(img *image.Gray, x0, y0, x1, y1 float64) {
	l := math.Hypot(x1-x0, y1-y0)
	for t := 0.0; t <= l; t += 4 {
		f := t / math.Max(l, 1e-9)
		disc(img, x0+f*(x1-x0), y0+f*(y1-y0), 0.9, ink)
	}
}
