package chart

import (
	"fmt"
	"image"
	"math"
	"time"
)

// BandPoint is a day's normal range.
type BandPoint struct {
	Day    time.Time
	Lo, Hi float64
}

// SignalPanel is one body signal: nightly values against a normal range,
// formatted by the caller.
type SignalPanel struct {
	Title  string // "HRV"
	Unit   string // "ms, nightly average"
	Points []Point
	Band   []BandPoint // days with a range; may start partway through
	Value  string      // the latest night: "60 ms"
	Status string      // "within your normal range"
	Detail string      // "normal 41 to 71"
	Note   string      // "3 nights outside in 60 days"
}

// Signals is the content of the body-signals chart.
type Signals struct {
	Title, Subtitle string
	From, To        time.Time
	Panels          []SignalPanel
}

// SignalsView stacks one panel per signal on a shared run of days. In each,
// the normal range is a band — a light dot texture between thin boundary
// lines, so it survives a 1- or 2-bit panel where a gray fill would vanish —
// and every night is a dot, joined by a thin line. Nights outside the range
// are drawn larger with a white ring, so they stand out without labels; the
// right column words the latest night.
func SignalsView(s Signals, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: body signals need at least 600x360, got %dx%d", width, height)
	}
	if len(s.Panels) == 0 || s.To.Before(s.From) {
		return nil, fmt.Errorf("chart: nothing to draw")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, labelW, rightW, gap, footH = 16, 120, 168, 18, 24
	drawText(img, face(true, 22), pad, 30, s.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, s.Subtitle, muted, alignLeft)

	plotL, plotR := pad+labelW, width-pad-rightW
	top, bottom := 72, height-footH
	n := len(s.Panels)
	panelH := float64(bottom-top-(n-1)*gap) / float64(n)
	days := dayIndex(s.From, s.To) + 1
	slotW := float64(plotR-plotL) / float64(days)
	x := func(d time.Time) float64 { return float64(plotL) + (float64(dayIndex(s.From, d))+0.5)*slotW }

	for i, p := range s.Panels {
		y0 := top + int(float64(i)*(panelH+gap))
		r := image.Rect(plotL, y0, plotR, y0+int(panelH))
		if i > 0 {
			hline(img, pad, width-pad-1, y0-gap/2, rule) // panels separated like the night view's rows
		}
		drawSignalPanel(img, r, p, x, slotW, pad, width-pad)
	}

	// dates: each night by the evening it began
	ly := height - 6
	drawText(img, labelFace(), int(x(s.From)), ly, NightOf(s.From).Format("Jan 2"), muted, alignLeft)
	drawText(img, labelFace(), int(x(s.To)), ly, NightOf(s.To).Format("Jan 2"), muted, alignRight)
	if days >= 14 {
		mid := s.From.AddDate(0, 0, (days-1)/2)
		drawText(img, labelFace(), int(x(mid)), ly, NightOf(mid).Format("Jan 2"), muted, alignCenter)
	}
	return img, nil
}

func drawSignalPanel(img *image.Gray, r image.Rectangle, p SignalPanel, x func(time.Time) float64,
	slotW float64, left, right int) {

	drawText(img, face(true, 15), left, r.Min.Y+15, p.Title, ink, alignLeft)
	drawText(img, face(false, 11), left, r.Min.Y+31, p.Unit, muted, alignLeft)
	drawText(img, face(true, 18), right, r.Min.Y+17, p.Value, ink, alignRight)
	drawText(img, face(false, 13), right, r.Min.Y+35, p.Status, ink, alignRight)
	drawText(img, face(false, 11), right, r.Min.Y+51, p.Detail, muted, alignRight)
	drawText(img, face(false, 11), right, r.Min.Y+65, p.Note, muted, alignRight)

	if len(p.Points) == 0 && len(p.Band) == 0 {
		drawText(img, labelFace(), r.Min.X+8, r.Min.Y+r.Dy()/2+5, "no readings yet", muted, alignLeft)
		return
	}
	// the scale covers both the nights and their ranges
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, pt := range p.Points {
		lo, hi = math.Min(lo, pt.Value), math.Max(hi, pt.Value)
	}
	for _, b := range p.Band {
		lo, hi = math.Min(lo, b.Lo), math.Max(hi, b.Hi)
	}
	ymin, ymax, step := niceScale(lo, hi, 3)
	const inset = 6.0
	yOf := func(v float64) float64 {
		t, b := float64(r.Min.Y)+inset, float64(r.Max.Y)-inset
		return b - (v-ymin)/(ymax-ymin)*(b-t)
	}
	for v := ymin; v <= ymax+step/2; v += step {
		if v == ymin || v+step > ymax+step/2 {
			drawText(img, face(false, 11), r.Min.X-6, int(yOf(v))+4, formatValue(v), muted, alignRight)
		}
	}

	// the band: a dot texture per day, then its edges as hairlines
	for _, b := range p.Band {
		cx := x(b.Day)
		x0, x1 := int(math.Floor(cx-slotW/2)), int(math.Ceil(cx+slotW/2))
		y0, y1 := int(math.Round(yOf(b.Hi))), int(math.Round(yOf(b.Lo)))
		dotTexture(img, image.Rect(x0, y0, x1, y1+1))
	}
	for k := 1; k < len(p.Band); k++ {
		a, b := p.Band[k-1], p.Band[k]
		if dayIndex(a.Day, b.Day) != 1 {
			continue
		}
		thickLine(img, x(a.Day), yOf(a.Hi), x(b.Day), yOf(b.Hi), 1, grid)
		thickLine(img, x(a.Day), yOf(a.Lo), x(b.Day), yOf(b.Lo), 1, grid)
	}

	// the nights: a thin line through consecutive ones, then the dots
	for k := 1; k < len(p.Points); k++ {
		a, b := p.Points[k-1], p.Points[k]
		if dayIndex(a.Day, b.Day) == 1 {
			thickLine(img, x(a.Day), yOf(a.Value), x(b.Day), yOf(b.Value), 1.2, muted)
		}
	}
	bands := map[int]BandPoint{}
	for _, b := range p.Band {
		bands[dayIndex(time.Time{}, b.Day)] = b
	}
	for _, pt := range p.Points {
		cx, cy := x(pt.Day), yOf(pt.Value)
		b, ok := bands[dayIndex(time.Time{}, pt.Day)]
		if ok && (pt.Value < b.Lo || pt.Value > b.Hi) {
			disc(img, cx, cy, 6, surface) // outside the range: larger, ringed
			disc(img, cx, cy, 4.5, ink)
		} else {
			disc(img, cx, cy, 2.5, ink)
		}
	}
}

// dotTexture fills r with a light, even dot pattern anchored to absolute
// pixels: how a normal or typical range is shaded. Pure black and white, so
// it survives a 1- or 2-bit panel where a gray fill would vanish.
func dotTexture(img *image.Gray, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if x%3 == 0 && y%3 == 0 {
				img.SetGray(x, y, ink)
			}
		}
	}
}
