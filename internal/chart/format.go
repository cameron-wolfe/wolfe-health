package chart

import (
	"fmt"
	"image"
	"math"
	"time"
)

// Duration words a span of minutes the way every screen writes one: "45m",
// "8h", "7h 12m". The dashboard uses it too, so the figures it words and the
// ones the charts word match.
func Duration(minutes float64) string {
	m := int(math.Round(minutes))
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// hm is Duration for a time.Duration.
func hm(d time.Duration) string { return Duration(d.Minutes()) }

// hatch draws the house hatch, every third diagonal, over r: the mark for
// "caution" and "worse" on the console and WolfeSense screens.
func hatch(img *image.Gray, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if (x+y)%3 == 0 {
				img.SetGray(x, y, ink)
			}
		}
	}
}

// hatchPill hatches the inside of the pill shape r (a rectangle with fully
// rounded ends) from its left end up to x: a bar filled to a value.
func hatchPill(img *image.Gray, r image.Rectangle, upTo int) {
	rad := float64(r.Dy()) / 2
	cy := float64(r.Min.Y) + rad
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < min(upTo, r.Max.X); x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			cx := math.Max(float64(r.Min.X)+rad, math.Min(fx, float64(r.Max.X)-rad))
			if math.Hypot(fx-cx, fy-cy) <= rad && (x+y)%3 == 0 {
				img.SetGray(x, y, ink)
			}
		}
	}
}
