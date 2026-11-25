package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

// WeekAvg is one week's average, for the row of weekly figures.
type WeekAvg struct {
	Start time.Time // the week's Sunday
	Value float64
}

// OxygenTide is the content of the oxygen tide chart.
type OxygenTide struct {
	Title, Subtitle string
	Headline        string // top right: "95.1% · steady over 2 weeks"
	From, To        time.Time
	SpO2            []Point     // each night's average blood oxygen, %
	Smooth          []Point     // the 7-night average: the tide itself
	Band            []BandPoint // the normal range, from the nights before each
	BDI             []Point     // nights with more breathing dips than usual: a crest over the dot
	Weeks           []WeekAvg   // weekly averages, Sunday to Saturday
	Low             Point       // the lowest night below its range: "low tide"
	HasLow          bool
	Stats           []StatLine
}

// Tide layout, in pixels.
const (
	tidePanelW = 176
	tideWeekH  = 20 // the weekly averages row
)

// crest draws a breathing-dips mark: half a sine wave of half-width
// hw and height h standing on y, on a white halo so it reads over the band
// and the trend.
func crest(img *image.Gray, cx, y, hw, h float64) {
	for _, pass := range []struct {
		w float64
		c color.Gray
	}{{4, surface}, {1.6, ink}} {
		px, py := cx-hw, y
		for t := 0.1; t <= 1.0001; t += 0.1 {
			nx := cx - hw + t*2*hw
			ny := y - h*math.Sin(t*math.Pi)
			thickLine(img, px, py, nx, ny, pass.w, pass.c)
			px, py = nx, ny
		}
	}
}

// Crest sizes over a night's dot, in pixels.
const (
	crestMinH = 4.0 // the fewest marked dips
	crestMaxH = 9.0 // the period's most
	crestHW   = 3.5
)

// OxygenView renders the oxygen tide. Single nights' blood oxygen jitters by
// a point or two — mostly measurement noise — so the tide is the 7-night
// average, a bold line on a white halo: the trend's shape. Each night is a
// small dot, and nights below the user's own normal range (a dotted band,
// built from the 30 nights before each) are ringed;
// the lowest of them is labeled low tide. A crest over a night's dot marks
// more breathing dips than usual, taller for more. A row of
// weekly averages runs beneath, and a side panel holds the key and figures.
func OxygenView(o OxygenTide, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: oxygen tide needs at least 600x360, got %dx%d", width, height)
	}
	if o.To.Before(o.From) {
		return nil, fmt.Errorf("chart: nothing to draw")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, axisW, footH = 16, 36, 22
	drawText(img, face(true, 22), pad, 30, o.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, o.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, o.Headline, ink, alignRight)

	plotL, plotR := pad+axisW, width-pad-tidePanelW-18
	top := 76
	weekY := height - footH - 6 // baseline of the weekly row
	plotB := weekY - tideWeekH  // the axis
	days := dayIndex(o.From, o.To) + 1
	slotW := float64(plotR-plotL) / float64(days)
	x := func(d time.Time) float64 { return float64(plotL) + (float64(dayIndex(o.From, d))+0.5)*slotW }

	// The scale fits the nights and the band, a point of headroom each way
	// and never past 100%.
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range o.SpO2 {
		lo, hi = math.Min(lo, p.Value), math.Max(hi, p.Value)
	}
	for _, b := range o.Band {
		lo, hi = math.Min(lo, b.Lo), math.Max(hi, b.Hi)
	}
	if math.IsInf(lo, 0) {
		lo, hi = 94, 99
	}
	lo, hi = math.Max(80, math.Floor(lo)-1), math.Min(100, math.Ceil(hi)+1)
	if hi-lo < 4 {
		lo = hi - 4
	}
	step := 1.0
	if hi-lo > 7 {
		step = 2
	}
	yOf := func(v float64) float64 {
		return float64(plotB) - (v-lo)/(hi-lo)*float64(plotB-top)
	}
	for v := lo; v <= hi+0.01; v += step {
		y := int(math.Round(yOf(v)))
		hline(img, plotL, plotR-1, y, grid)
		drawText(img, face(false, 11), plotL-6, y+4, fmt.Sprintf("%.0f%%", v), muted, alignRight)
	}

	if len(o.SpO2) == 0 {
		drawText(img, labelFace(), plotL+8, (top+plotB)/2, "no oxygen readings yet", muted, alignLeft)
	}

	// the normal range per column, joining consecutive days
	type span struct{ hi, lo float64 }
	band := map[int]span{}
	for k, b := range o.Band {
		c0 := int(math.Round(x(b.Day)))
		band[c0] = span{yOf(b.Hi), yOf(b.Lo)}
		if k > 0 && dayIndex(o.Band[k-1].Day, b.Day) == 1 {
			a := o.Band[k-1]
			xa, xb := x(a.Day), x(b.Day)
			for c := int(math.Round(xa)); c <= c0; c++ {
				t := (float64(c) - xa) / (xb - xa)
				band[c] = span{yOf(a.Hi) + t*(yOf(b.Hi)-yOf(a.Hi)), yOf(a.Lo) + t*(yOf(b.Lo)-yOf(a.Lo))}
			}
		}
	}
	for c := plotL; c < plotR; c++ {
		bs, ok := band[c]
		if !ok {
			continue
		}
		for y := int(math.Ceil(bs.hi)); y <= int(bs.lo); y++ {
			if c%3 == 0 && y%3 == 0 {
				img.SetGray(c, y, ink)
			}
		}
		if (c/3)%2 == 0 { // the range's edges, dashed
			img.SetGray(c, int(math.Round(bs.hi)), ink)
			img.SetGray(c, int(math.Round(bs.lo)), ink)
		}
	}
	hline(img, plotL, plotR-1, plotB, ink) // the axis

	// the nights first, so the trend draws over them: small dots with a
	// white ring so they read over the band; nights below it larger, ringed
	bandAt := map[int]BandPoint{}
	for _, b := range o.Band {
		bandAt[dayIndex(o.From, b.Day)] = b
	}
	for _, p := range o.SpO2 {
		cx, cy := x(p.Day), yOf(p.Value)
		if b, ok := bandAt[dayIndex(o.From, p.Day)]; ok && p.Value < b.Lo {
			disc(img, cx, cy, dotRadius+dotRing, surface)
			disc(img, cx, cy, dotRadius, ink)
			disc(img, cx, cy, 1.5, surface)
			continue
		}
		disc(img, cx, cy, 3.6, surface)
		disc(img, cx, cy, 2.3, ink)
	}

	// the trend: bold, on a white halo so it stands clear of the band's
	// texture and the dots; broken where the average has a gap
	for _, pass := range []struct {
		w float64
		c color.Gray
	}{{7, surface}, {3.5, ink}} {
		for k := 1; k < len(o.Smooth); k++ {
			a, b := o.Smooth[k-1], o.Smooth[k]
			if dayIndex(a.Day, b.Day) == 1 {
				thickLine(img, x(a.Day), yOf(a.Value), x(b.Day), yOf(b.Value), pass.w, pass.c)
			}
		}
	}

	// extra breathing dips: a crest standing just above the night's dot,
	// taller for more, so a low night that also had extra dips shows as one
	// mark. Drawn after the trend so the trend can't hide it.
	maxBDI := 5.0
	for _, p := range o.BDI {
		maxBDI = math.Max(maxBDI, p.Value)
	}
	crestTop := map[int]float64{} // day -> how far above the dot its crest reaches
	nights := map[int]Point{}
	for _, p := range o.SpO2 {
		nights[dayIndex(o.From, p.Day)] = p
	}
	for _, p := range o.BDI {
		n, ok := nights[dayIndex(o.From, p.Day)]
		if !ok || p.Value <= 0 {
			continue // no reading to stand on, or a calm night
		}
		cx, cy := x(n.Day), yOf(n.Value)
		gap := 4.0 // clear of the dot's white ring
		if b, ok := bandAt[dayIndex(o.From, n.Day)]; ok && n.Value < b.Lo {
			gap = dotRadius + dotRing + 1
		}
		h := crestMinH + (p.Value/maxBDI)*(crestMaxH-crestMinH)
		crest(img, cx, cy-gap, crestHW, h)
		crestTop[dayIndex(o.From, n.Day)] = gap + h + 6
	}

	// low tide: labeled from the air at the top, a leader down to the night
	if o.HasLow {
		lx, ly := x(o.Low.Day), yOf(o.Low.Value)
		label := fmt.Sprintf("low tide · night of %s · %.1f%%", NightOf(o.Low.Day).Format("Jan 2"), o.Low.Value)
		f := face(true, 12)
		tw := textWidth(f, label)
		tx := max(plotL+2, min(int(lx)-tw/2, plotR-2-tw))
		ty := top + 12
		box := image.Rect(tx-3, ty-12, tx+tw+3, ty+4)
		clear := dotRadius + dotRing + 1
		if t, ok := crestTop[dayIndex(o.From, o.Low.Day)]; ok {
			clear = t // stop above the night's crest
		}
		if bot := int(ly - clear); bot > box.Max.Y {
			for y := box.Max.Y; y <= bot; y++ { // a leader with a white edge
				img.SetGray(int(lx)-1, y, surface)
				img.SetGray(int(lx), y, ink)
				img.SetGray(int(lx)+1, y, surface)
			}
		}
		fillRect(img, box, surface)
		drawText(img, f, tx, ty, label, ink, alignLeft)
	}

	// the weekly averages, each centered on its week's days in the window
	drawText(img, face(true, 10), plotL-6, weekY, "week", muted, alignRight)
	wf := face(true, 11)
	for _, w := range o.Weeks {
		a, b := w.Start, w.Start.AddDate(0, 0, 6)
		if a.Before(o.From) {
			a = o.From
		}
		if b.After(o.To) {
			b = o.To
		}
		x0, x1 := x(a)-slotW/2, x(b)+slotW/2
		txt := fmt.Sprintf("%.1f", w.Value)
		if x1-x0 < float64(textWidth(wf, txt))+4 {
			continue // a sliver of a week at the window's edge
		}
		drawText(img, wf, int((x0+x1)/2), weekY, txt, ink, alignCenter)
		if !w.Start.Before(o.From) {
			vline(img, int(x0), plotB+2, weekY, grid) // where each week starts
		}
	}

	// dates: each night by the evening it began
	ly := height - 6
	drawText(img, labelFace(), int(x(o.From)), ly, NightOf(o.From).Format("Jan 2"), muted, alignLeft)
	drawText(img, labelFace(), int(x(o.To)), ly, NightOf(o.To).Format("Jan 2"), muted, alignRight)
	if days >= 14 {
		mid := o.From.AddDate(0, 0, (days-1)/2)
		drawText(img, labelFace(), int(x(mid)), ly, NightOf(mid).Format("Jan 2"), muted, alignCenter)
	}

	drawTidePanel(img, image.Rect(width-pad-tidePanelW, top-4, width-pad, height-pad), o.Stats)
	return img, nil
}

// drawTidePanel draws the key — each mark drawn exactly as on the chart,
// named in plain words — and then the figures.
func drawTidePanel(img *image.Gray, r image.Rectangle, stats []StatLine) {
	drawKeyAndFigures(img, r, []keyEntry{
		{"trend, 7-night average", centered(func(x, y int) {
			thickLine(img, float64(x)-10, float64(y), float64(x)+10, float64(y), 3.5, ink)
		})},
		{"one night's reading", centered(func(x, y int) { disc(img, float64(x), float64(y), 2.3, ink) })},
		{"a night low for you", centered(func(x, y int) {
			disc(img, float64(x), float64(y), dotRadius, ink)
			disc(img, float64(x), float64(y), 1.5, surface)
		})},
		{"your normal range", centered(func(cx, cy int) { rangeSwatch(img, cx, cy) })},
		{"more dips than usual", centered(func(x, y int) {
			cx, cy := float64(x), float64(y)
			disc(img, cx, cy+4, 3.6, surface)
			disc(img, cx, cy+4, 2.3, ink)
			crest(img, cx, cy, crestHW, crestMaxH-1)
		})},
	}, stats)
}
