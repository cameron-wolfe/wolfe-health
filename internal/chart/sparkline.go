package chart

import (
	"image"
	"time"
)

// sparkInset is how far a sparkline keeps its values from the top and bottom
// of its box: room for the end dot and its ring. Anything drawn to the same
// scale (reference lines) must use the same inset to line up.
const sparkInset = 3.5 + 2

// sparklineScaled is sparkline on a fixed value range, so several can share
// one scale and be compared directly. pts must already be inside from..to.
func sparklineScaled(img *image.Gray, r image.Rectangle, in []Point, from, to time.Time, lo, hi float64) {
	if len(in) == 0 {
		return
	}
	days := dayIndex(from, to) + 1
	const dot = 3.5
	// inset by the end dot so it is never clipped at the top or bottom
	top, bottom := float64(r.Min.Y)+sparkInset, float64(r.Max.Y)-sparkInset
	x := func(t time.Time) float64 {
		if days == 1 {
			return float64(r.Max.X)
		}
		return float64(r.Min.X) + float64(dayIndex(from, t))/float64(days-1)*float64(r.Dx())
	}
	y := func(v float64) float64 { return bottom - (v-lo)/(hi-lo)*(bottom-top) }

	for i := 1; i < len(in); i++ {
		a, b := in[i-1], in[i]
		if dayIndex(a.Day, b.Day) == 1 {
			thickLine(img, x(a.Day), y(a.Value), x(b.Day), y(b.Value), lineWidth, ink)
		}
	}
	for i, p := range in {
		prev := i > 0 && dayIndex(in[i-1].Day, p.Day) == 1
		next := i < len(in)-1 && dayIndex(p.Day, in[i+1].Day) == 1
		if !prev && !next {
			disc(img, x(p.Day), y(p.Value), lineWidth, ink)
		}
	}
	last := in[len(in)-1]
	disc(img, x(last.Day), y(last.Value), dot+dotRing, surface)
	disc(img, x(last.Day), y(last.Value), dot, ink)
}
