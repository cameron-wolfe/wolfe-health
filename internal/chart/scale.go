// Package chart renders stored metrics to images for the e-ink display.
//
// Everything is drawn pixel by pixel onto a grayscale image rather than
// through a plotting library: an e-ink panel shows few gray levels, so exact
// control over line weight and where ink lands matters more than features.
// The display stage decides how to reduce the gray to what the panel shows.
package chart

import (
	"fmt"
	"image/color"
	"math"
	"time"
)

// Grays used for each role. Ink is for data and primary text; the grid stays
// recessive so the data reads first.
var (
	surface = color.Gray{Y: 255}
	ink     = color.Gray{Y: 0}
	muted   = color.Gray{Y: 90}  // axis labels, secondary text
	grid    = color.Gray{Y: 210} // hairline gridlines
)

// Point is one day's value.
type Point struct {
	Day   time.Time // midnight; only the date matters
	Value float64
}

// Mark sizes shared across charts, in pixels.
const (
	lineWidth = 2.0
	dotRadius = 4.0 // a marked point: >= 8px across
	dotRing   = 2.0 // surface ring around a marked point
)

// dayIndex is the number of calendar days from a to b. Dates are compared in
// UTC at midnight so a DST change cannot make a day 23 or 25 hours long.
func dayIndex(a, b time.Time) int {
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua).Hours() / 24)
}

// NightOf is the evening a night began, from the day Oura files it under:
// the day of waking. Oura's night for Tue Nov 24 is Monday night, Nov 23,
// and a chart of nights labels it so.
func NightOf(wake time.Time) time.Time { return wake.AddDate(0, 0, -1) }

// niceScale widens lo..hi to round tick values, with about n intervals.
// Steps are 1, 2 or 5 times a power of ten, so labels read 60 / 70 / 80
// rather than 61.3 / 68.9. A flat series gets a band around its value.
func niceScale(lo, hi float64, n int) (min, max, step float64) {
	if hi-lo < 1e-9 {
		lo, hi = lo-5, hi+5
	}
	raw := (hi - lo) / float64(n)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step = 10 * mag
	for _, m := range []float64{1, 2, 5} {
		if m*mag >= raw {
			step = m * mag
			break
		}
	}
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}

// formatValue prints whole numbers without a decimal point.
func formatValue(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
