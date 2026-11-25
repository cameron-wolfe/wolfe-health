package chart

import (
	"fmt"
	"image"
	"math"
	"sort"
	"time"
)

// TrendPanel is one metric over a period, formatted by the caller.
type TrendPanel struct {
	Title, Unit    string  // "Sleep score", "nightly"
	Points         []Point // daily values
	Average        []Point // the rolling average, where there is one
	BandLo, BandHi float64 // the typical range: the middle half of the period's days
	HasBand        bool
	Value          string // "avg 82"
	Change         string // "+2 vs the 60 days before"
	Detail         string // "typical 76 to 88"
	Note           string // "7-day average now 79"
}

// Trends is the content of the trends screen.
type Trends struct {
	Title, Subtitle string
	From, To        time.Time
	Panels          []TrendPanel
}

// TrendsView stacks one panel per metric on a shared run of days, in the
// body-signals style: the typical range as a dot-textured band between
// hairlines, every day a small faint dot, and the rolling average as a bold
// line that ends in a ringed dot — the trend reads first, the daily noise
// stays in the background. The scale fits the typical spread, with rare
// off-scale days pinned to the edge as hollow dots. The right column carries
// the figures.
func TrendsView(t Trends, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: trends need at least 600x360, got %dx%d", width, height)
	}
	if len(t.Panels) == 0 || t.To.Before(t.From) {
		return nil, fmt.Errorf("chart: nothing to draw")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, labelW, rightW, gap, footH = 16, 120, 176, 18, 24
	drawText(img, face(true, 22), pad, 30, t.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, t.Subtitle, muted, alignLeft)

	plotL, plotR := pad+labelW, width-pad-rightW
	top, bottom := 72, height-footH
	n := len(t.Panels)
	panelH := float64(bottom-top-(n-1)*gap) / float64(n)
	days := dayIndex(t.From, t.To) + 1
	slotW := float64(plotR-plotL) / float64(days)
	x := func(d time.Time) float64 { return float64(plotL) + (float64(dayIndex(t.From, d))+0.5)*slotW }

	for i, p := range t.Panels {
		y0 := top + int(float64(i)*(panelH+gap))
		if i > 0 {
			hline(img, pad, width-pad-1, y0-gap/2, rule)
		}
		drawTrendPanel(img, image.Rect(plotL, y0, plotR, y0+int(panelH)), p, x, pad, width-pad)
	}

	ly := height - 6
	drawText(img, labelFace(), int(x(t.From)), ly, t.From.Format("Jan 2"), muted, alignLeft)
	drawText(img, labelFace(), int(x(t.To)), ly, t.To.Format("Jan 2"), muted, alignRight)
	if days >= 14 {
		mid := t.From.AddDate(0, 0, (days-1)/2)
		drawText(img, labelFace(), int(x(mid)), ly, mid.Format("Jan 2"), muted, alignCenter)
	}
	return img, nil
}

func drawTrendPanel(img *image.Gray, r image.Rectangle, p TrendPanel, x func(time.Time) float64, left, right int) {
	drawText(img, face(true, 15), left, r.Min.Y+15, p.Title, ink, alignLeft)
	drawText(img, face(false, 11), left, r.Min.Y+31, p.Unit, muted, alignLeft)
	drawText(img, face(true, 18), right, r.Min.Y+17, p.Value, ink, alignRight)
	drawText(img, face(false, 13), right, r.Min.Y+35, p.Change, ink, alignRight)
	drawText(img, face(false, 11), right, r.Min.Y+51, p.Detail, muted, alignRight)
	drawText(img, face(false, 11), right, r.Min.Y+65, p.Note, muted, alignRight)

	if len(p.Points) == 0 {
		drawText(img, labelFace(), r.Min.X+8, r.Min.Y+r.Dy()/2+5, "no data yet", muted, alignLeft)
		return
	}
	// The scale fits the typical spread — the average line, the band, and
	// the 5th to 95th percentile of days — not the extremes: one 23,000-step
	// day would otherwise squash every other day into the bottom of the panel.
	// Days beyond the scale are pinned to its edge as hollow dots.
	vals := make([]float64, len(p.Points))
	for i, pt := range p.Points {
		vals[i] = pt.Value
	}
	sort.Float64s(vals)
	pct := func(q float64) float64 { return vals[int(math.Round(q*float64(len(vals)-1)))] }
	lo, hi := pct(0.05), pct(0.95)
	for _, a := range p.Average {
		lo, hi = math.Min(lo, a.Value), math.Max(hi, a.Value)
	}
	if p.HasBand {
		lo, hi = math.Min(lo, p.BandLo), math.Max(hi, p.BandHi)
	}
	ymin, ymax, step := niceScale(lo, hi, 4)
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

	// the typical range: a flat band across the period
	if p.HasBand {
		y0, y1 := int(math.Round(yOf(p.BandHi))), int(math.Round(yOf(p.BandLo)))
		dotTexture(img, image.Rect(r.Min.X, y0, r.Max.X, y1+1))
		hline(img, r.Min.X, r.Max.X-1, y0, grid)
		hline(img, r.Min.X, r.Max.X-1, y1, grid)
	}
	// every day, small and faint; off-scale days hollow, at the edge
	for _, pt := range p.Points {
		v := math.Max(ymin, math.Min(ymax, pt.Value))
		if v != pt.Value {
			disc(img, x(pt.Day), yOf(v), 3, muted)
			disc(img, x(pt.Day), yOf(v), 1.6, surface)
			continue
		}
		disc(img, x(pt.Day), yOf(v), 2, muted)
	}
	// the rolling average, bold, broken where a day has none
	for k := 1; k < len(p.Average); k++ {
		a, b := p.Average[k-1], p.Average[k]
		if dayIndex(a.Day, b.Day) == 1 {
			thickLine(img, x(a.Day), yOf(a.Value), x(b.Day), yOf(b.Value), 3, ink)
		}
	}
	if n := len(p.Average); n > 0 {
		last := p.Average[n-1]
		disc(img, x(last.Day), yOf(last.Value), dotRadius+dotRing, surface)
		disc(img, x(last.Day), yOf(last.Value), dotRadius, ink)
	}
}
