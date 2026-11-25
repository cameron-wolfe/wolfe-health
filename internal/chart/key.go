package chart

import (
	"image"
	"math"
)

// keyEntry is one line of a key: what a mark means, and how to draw the mark
// as it appears on the chart, at x on baseline y. Every chart keys its marks
// so nobody has to guess what a tick or a fill means.
type keyEntry struct {
	label string
	mark  func(x, y int)
}

// drawKeyGrid draws a key inside a console bank, under a faint rule: two
// entries to a row, each mark in an 18-pixel slot (scaled by k) and its
// label beside it. Put a long label last so it gets a row to itself.
func drawKeyGrid(img *image.Gray, r image.Rectangle, k float64, entries []keyEntry) {
	f := face(false, math.Max(9, math.Round(10*k)))
	colW := r.Dx() / 2
	sw := int(18 * k)
	hline(img, r.Min.X, r.Max.X-1, r.Min.Y, grid)
	for i, e := range entries {
		x := r.Min.X + (i%2)*colW + int(2*k)
		y := r.Min.Y + int(15*k) + (i/2)*int(15*k)
		e.mark(x, y)
		drawText(img, f, x+sw+int(6*k), y, e.label, muted, alignLeft)
	}
}

// drawKeyAndFigures draws the older screens' side panel in r: a "Key"
// heading over each mark and its meaning, then "Figures" over the stats,
// as many as fit.
func drawKeyAndFigures(img *image.Gray, r image.Rectangle, key []keyEntry, stats []StatLine) {
	x := r.Min.X
	f := face(false, 12)
	y := r.Min.Y + 8
	drawText(img, face(true, 13), x, y, "Key", ink, alignLeft)
	for _, e := range key {
		y += 19
		e.mark(x, y)
		drawText(img, f, x+28, y, e.label, ink, alignLeft)
	}
	y += 26
	drawText(img, face(true, 13), x, y, "Figures", ink, alignLeft)
	y += 2
	for _, s := range stats {
		y += 18
		if y > r.Max.Y {
			break
		}
		drawText(img, labelFace(), x, y, s.Label, muted, alignLeft)
		drawText(img, face(true, 13), r.Max.X, y, s.Value, ink, alignRight)
	}
}

// rangeSwatch draws the key's sample of a typical range, centered at cx, cy:
// the band's dot texture between dashed edges, as the charts draw it.
func rangeSwatch(img *image.Gray, cx, cy int) {
	r := image.Rect(cx-10, cy-6, cx+11, cy+6)
	for yy := r.Min.Y; yy <= r.Max.Y; yy++ {
		for xx := r.Min.X; xx < r.Max.X; xx++ {
			if xx%3 == 0 && yy%3 == 0 {
				img.SetGray(xx, yy, ink)
			}
			if (yy == r.Min.Y || yy == r.Max.Y) && (xx/3)%2 == 0 {
				img.SetGray(xx, yy, ink)
			}
		}
	}
}

// centered adapts a mark drawn about a center point to a keyEntry: the
// older panels center their marks 11 pixels in and 4 above the baseline.
func centered(mark func(cx, cy int)) func(x, y int) {
	return func(x, y int) { mark(x+11, y-4) }
}
