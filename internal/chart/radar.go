package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"
)

// RadarPart is one spoke of the sleep radar: a contributor's score last
// night and its average over the window.
type RadarPart struct {
	Label       string
	Short       string  // a shorter name, for a small radar; Label when empty
	Value, Avg  float64 // 0-100
	Has, HasAvg bool
}

// SleepRadar is the content of the sleep breakdown: the seven parts of one
// night's sleep score against their average.
type SleepRadar struct {
	Title, Subtitle string
	Night           time.Time // the wake-up day of the night shown
	Score           string    // that night's sleep score: "66"
	AvgLabel        string    // "30-day average"
	Parts           []RadarPart

	// What the column calls the day and its score, and the key the shape;
	// empty for the sleep radar's: "LAST NIGHT", "Sleep score", "last night".
	PanelHead, ScoreLabel, ShapeLabel string
}

// radarLow is the score under which a part is named "pay attention" — Oura's
// own band (85+ optimal, 70-84 good, under 70 pay attention).
const radarLow = 70

// radarHub is where the spokes start, on the 0-100 scale, with a ring
// there: the middle of the radar is left open for the mark, framed, as on
// the deviation radar.
const radarHub = 25

// RadarView renders the sleep breakdown as a radar: one spoke per part of the
// sleep score, 0 at the center and 100 at the rim, in a fixed order so a
// night's shape compares with any other's. The optimal zone (85-100) is a
// dotted band round the rim, as typical ranges are elsewhere; the average is
// a dashed outline and last night a wide-hatched shape with a heavy edge, so a
// dent inside the dashes is a part that fell short of usual. Parts under 70
// get an inverted value. The right column lists the parts weakest first with
// their change from the average.
func RadarView(s SleepRadar, width, height int) (*image.Gray, error) {
	if width < 600 || height < 300 {
		return nil, fmt.Errorf("chart: sleep radar needs at least 600x300, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad = 16
	drawText(img, face(true, 22), pad, 30, s.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, s.Subtitle, muted, alignLeft)

	panelW := 236
	plot := image.Rect(pad, 64, width-pad-panelW-12, height-8)
	cx := float64(plot.Min.X+plot.Max.X) / 2
	cy := float64(plot.Min.Y+plot.Max.Y)/2 + 4
	R := math.Min(float64(plot.Dx())/2-96, float64(plot.Dy())/2-34)
	if n := len(s.Parts); n < 3 {
		return nil, fmt.Errorf("chart: a radar needs at least 3 parts, got %d", n)
	}
	drawRadarFigure(img, plot, cx, cy, R, s, radarFull)
	drawRadarPanel(img, image.Rect(width-pad-panelW, 64, width-pad, height-8), s)
	return img, nil
}

// radarStyle sizes a radar's lines and labels: radarFull for the WolfeSense
// screens, smaller for a radar set into another screen's bank.
type radarStyle struct {
	line         float64 // the shape's line widths, as multiples of the full radar's
	label, value float64 // label and value font sizes
	gap          float64 // from the rim to the labels
	short        bool    // label with each part's Short name
	mark         bool    // the Wolfe Health outline in the center
}

var radarFull = radarStyle{line: 1, label: 12, value: 15, gap: 14, mark: true}

// drawRadarFigure draws s as a radar centered at cx, cy with radius R,
// clipped to plot for its dotted zone: the zone, rings and spokes, the
// average dashed, the day's shape heavy, and each spoke's label and value.
func drawRadarFigure(img *image.Gray, plot image.Rectangle, cx, cy, R float64, s SleepRadar, st radarStyle) {
	n := len(s.Parts)
	if n < 3 {
		return
	}
	at := func(i int, v float64) (float64, float64) {
		a := float64(i) / float64(n) * 2 * math.Pi
		r := R * math.Max(0, math.Min(100, v)) / 100
		return cx + r*math.Sin(a), cy - r*math.Cos(a)
	}
	poly := func(v func(i int) float64) [][2]float64 {
		pts := make([][2]float64, n)
		for i := range pts {
			x, y := at(i, v(i))
			pts[i] = [2]float64{x, y}
		}
		return pts
	}

	// the optimal zone: dots between the 85 ring and the rim
	zone := polygonMask(img.Rect, poly(func(int) float64 { return 100 }))
	inner := polygonMask(img.Rect, poly(func(int) float64 { return 85 }))
	for y := plot.Min.Y; y < plot.Max.Y; y++ {
		for x := plot.Min.X; x < plot.Max.X; x++ {
			if zone[image.Pt(x, y)] && !inner[image.Pt(x, y)] && bandDot(x, y) {
				img.SetGray(x, y, ink)
			}
		}
	}
	// rings at 25 (framing the mark), 50, 85 and the rim; and 70, Oura's
	// "pay attention" line, dotted like the other radars' caution rings
	for _, v := range []float64{radarHub, 50, 85, 100} {
		pts := poly(func(int) float64 { return v })
		c := grid
		if v == 100 {
			c = rule
		}
		for i := range pts {
			j := (i + 1) % n
			thickLine(img, pts[i][0], pts[i][1], pts[j][0], pts[j][1], 1, c)
		}
	}
	caution := poly(func(int) float64 { return radarLow })
	for i := range caution {
		j := (i + 1) % n
		dottedLine(img, caution[i][0], caution[i][1], caution[j][0], caution[j][1])
	}
	// spokes from just off the center to the rim, leaving the middle open
	// for the mark, as on the deviation radar: the Wolfe Health outline, in
	// light gray, under the data
	for i := 0; i < n; i++ {
		x0, y0 := at(i, radarHub)
		x, y := at(i, 100)
		thickLine(img, x0, y0, x, y, 1, grid)
	}
	if st.mark {
		DrawWolfOutline(img, cx, cy, 1.45*R*radarHub/100, 1.5, rule)
	}

	// the average: dashed, with a white edge so it reads over the zone
	avg := poly(func(i int) float64 { return s.Parts[i].Avg })
	for i := range avg {
		j := (i + 1) % n
		dashedLine(img, avg[i][0], avg[i][1], avg[j][0], avg[j][1], 5*st.line, surface)
	}
	for i := range avg {
		j := (i + 1) % n
		dashedLine(img, avg[i][0], avg[i][1], avg[j][0], avg[j][1], 2.5*st.line, ink)
	}
	// last night: a heavy line on a white edge, unfilled and on top, as on
	// the deviation radar, so the zone and rings stay readable under it
	night := poly(func(i int) float64 {
		if !s.Parts[i].Has {
			return 0
		}
		return s.Parts[i].Value
	})
	for _, pass := range []struct {
		w float64
		c color.Gray
	}{{6 * st.line, surface}, {3 * st.line, ink}} {
		for i := range night {
			j := (i + 1) % n
			thickLine(img, night[i][0], night[i][1], night[j][0], night[j][1], pass.w, pass.c)
		}
	}
	for i := range night {
		disc(img, night[i][0], night[i][1], 4.5*st.line, surface)
		disc(img, night[i][0], night[i][1], 3.5*st.line, ink)
	}

	// labels round the rim: the part, then last night's value
	for i, p := range s.Parts {
		a := float64(i) / float64(n) * 2 * math.Pi
		lx, ly := cx+(R+st.gap)*math.Sin(a), cy-(R+st.gap)*math.Cos(a)
		al := alignCenter
		switch {
		case math.Sin(a) > 0.25:
			al = alignLeft
		case math.Sin(a) < -0.25:
			al = alignRight
		}
		bh := st.label + st.value - 1 // label block of two lines, placed outward
		top := ly - bh/2
		if math.Cos(a) > 0.5 {
			top = ly - bh
		} else if math.Cos(a) < -0.5 {
			top = ly + 4
		}
		label := p.Label
		if st.short && p.Short != "" {
			label = p.Short
		}
		drawText(img, face(false, st.label), int(lx), int(top+st.label-1), label, muted, al)
		v := "—"
		if p.Has {
			v = fmt.Sprintf("%.0f", p.Value)
		}
		vf := face(true, st.value)
		vy := int(top + st.label + st.value + 1)
		if p.Has && p.Value < radarLow {
			// pay attention: the value inverted
			w := textWidth(vf, v)
			x0 := int(lx) - w/2
			switch al {
			case alignLeft:
				x0 = int(lx)
			case alignRight:
				x0 = int(lx) - w
			}
			fillRect(img, image.Rect(x0-3, vy-int(st.value*0.87), x0+w+3, vy+int(st.value*0.27)), ink)
			drawText(img, vf, int(lx), vy, v, surface, al)
			continue
		}
		drawText(img, vf, int(lx), vy, v, ink, al)
	}

}

// drawRadarPanel lists the parts weakest first with last night's value and
// its change from the average, then keys the marks.
func drawRadarPanel(img *image.Gray, r image.Rectangle, s SleepRadar) {
	order := make([]int, len(s.Parts))
	for i := range order {
		order[i] = i
	}
	key := func(p RadarPart) float64 {
		if !p.Has {
			return 1000
		}
		return p.Value
	}
	for i := 1; i < len(order); i++ { // insertion sort: weakest first, stable
		for j := i; j > 0 && key(s.Parts[order[j]]) < key(s.Parts[order[j-1]]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var rows []PanelRow
	var short []string // the parts well short of their usual, weakest first
	for _, i := range order {
		p := s.Parts[i]
		row := PanelRow{Label: p.Label, Value: "—"}
		if p.Has {
			row.Value, row.Alert = fmt.Sprintf("%.0f", p.Value), p.Value < radarLow
			if p.HasAvg {
				row.Note = signedOrSame(p.Value - p.Avg)
				if p.Avg-p.Value >= radarShortfall {
					short = append(short, p.Label)
				}
			}
		}
		rows = append(rows, row)
	}
	c := column{
		head: or(s.PanelHead, "LAST NIGHT"), headNote: "vs avg",
		leadLabel: or(s.ScoreLabel, "Sleep score"), lead: s.Score,
		rows: rows, rowMax: 24, valueRight: 48, noteSize: 12,
		key: radarKey(img, or(s.ShapeLabel, "last night"), s.AvgLabel,
			keyEntry{"optimal, 85+", bandSwatch(img)},
			keyEntry{"under 70: pay attention, inverted", dottedSwatch(img)}),
	}
	if len(short) == 0 {
		c.takeaway = "Every part within 10 of usual"
	} else {
		c.takeawayHead, c.takeawayItems = "Well short of usual:", short
	}
	drawColumn(img, r, c)
}

// signedOrSame words a rounded difference: "+4", "−11", "same".
func signedOrSame(d float64) string {
	switch d = math.Round(d); {
	case d > 0:
		return fmt.Sprintf("+%.0f", d)
	case d < 0:
		return fmt.Sprintf("−%.0f", -d)
	}
	return "same"
}

// radarKey is the key the radars share: the day's shape, the dashed
// comparison, then their own entries.
func radarKey(img *image.Gray, shape, avg string, more ...keyEntry) []keyEntry {
	return append([]keyEntry{
		{shape, func(x, y int) {
			thickLine(img, float64(x), float64(y-4), float64(x+22), float64(y-4), 3, ink)
			disc(img, float64(x+11), float64(y-4), 3.5, ink)
		}},
		{avg, func(x, y int) { dashedLine(img, float64(x), float64(y-4), float64(x+22), float64(y-4), 2, ink) }},
	}, more...)
}

// bandSwatch draws the dotted zone's texture, the radars' normal or optimal
// band; dottedSwatch the dotted caution line.
func bandSwatch(img *image.Gray) func(x, y int) {
	return func(x, y int) {
		for yy := y - 9; yy < y+1; yy++ {
			for xx := x; xx < x+22; xx++ {
				if bandDot(xx, yy) {
					img.SetGray(xx, yy, ink)
				}
			}
		}
	}
}

func dottedSwatch(img *image.Gray) func(x, y int) {
	return func(x, y int) { dottedLine(img, float64(x), float64(y-4), float64(x+22), float64(y-4)) }
}

// radarShortfall is how far under its average a part must be, last night,
// to be named as well short of usual.
const radarShortfall = 10

// joinAnd lists names as prose: "a", "a and b", "a, b and c". Long lists are
// shortened so they fit one line of the panel.
func joinAnd(names []string) string {
	if len(names) > 3 {
		names = append(names[:2:2], fmt.Sprintf("%d more", len(names)-2))
	}
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// polygonMask is the set of pixels whose centers lie inside the polygon,
// by the even-odd rule, within bounds.
func polygonMask(bounds image.Rectangle, pts [][2]float64) map[image.Point]bool {
	m := map[image.Point]bool{}
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		fy := float64(y) + 0.5
		var xs []float64
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= fy) != (b[1] <= fy) {
				xs = append(xs, a[0]+(fy-a[1])/(b[1]-a[1])*(b[0]-a[0]))
			}
		}
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
			}
		}
		for k := 0; k+1 < len(xs); k += 2 {
			for x := int(math.Ceil(xs[k] - 0.5)); float64(x)+0.5 <= xs[k+1]; x++ {
				if p := image.Pt(x, y); p.In(bounds) {
					m[p] = true
				}
			}
		}
	}
	return m
}

// dashedLine draws a line of 6px dashes and 4px gaps, width w.
func dashedLine(img *image.Gray, x0, y0, x1, y1, w float64, c color.Gray) {
	l := math.Hypot(x1-x0, y1-y0)
	for t := 0.0; t <= l; t += 0.5 {
		if math.Mod(t, 10) < 6 {
			disc(img, x0+(x1-x0)*t/l, y0+(y1-y0)*t/l, w/2, c)
		}
	}
}
