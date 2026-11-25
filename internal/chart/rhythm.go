package chart

import (
	"fmt"
	"image"
	"math"
)

// RhythmPanel is one metric's average for each day of the week, Monday
// first, formatted by the caller.
type RhythmPanel struct {
	Title         string
	Values        [7]float64 // averages, Monday..Sunday
	Has           [7]bool    // false where no day of that weekday had data
	Labels        [7]string  // the value printed at each bar's end
	Avg           float64    // overall average: the baseline bars grow from
	AvgLabel      string     // "avg 81"
	Extent        float64    // the scale runs Avg±Extent
	ExtentLabel   string     // "±5"
	LowerIsBetter bool       // for stress: below average is the good side
	Caption       string     // "best Sat 83 · worst Tue 77"
}

// Rhythm is the content of the weekly-rhythm chart.
type Rhythm struct {
	Title, Subtitle string
	Panels          []RhythmPanel
}

var weekdayInitials = [7]string{"M", "T", "W", "T", "F", "S", "S"}

// RhythmView draws each panel as seven bars, Monday to Sunday, growing up or
// down from the metric's overall average: the question is which days run
// better or worse than normal, so normal is the baseline. A zero baseline
// buried the signal — weekday sleep scores span a few points out of 100.
// Solid bars are better than average and outlined ones worse, the same in
// every panel (for stress, less is better), and every bar carries its actual
// value, so nothing depends on reading the scale.
func RhythmView(rh Rhythm, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: weekly rhythm needs at least 600x360, got %dx%d", width, height)
	}
	if len(rh.Panels) == 0 {
		return nil, fmt.Errorf("chart: no panels to draw")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, between = 16, 28
	drawText(img, face(true, 22), pad, 30, rh.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, rh.Subtitle, muted, alignLeft)

	n := len(rh.Panels)
	panelW := float64(width-2*pad-(n-1)*between) / float64(n)
	for i, p := range rh.Panels {
		x0 := pad + int(float64(i)*(panelW+between))
		drawRhythmPanel(img, image.Rect(x0, 72, x0+int(panelW), height-pad), p)
	}
	return img, nil
}

func drawRhythmPanel(img *image.Gray, r image.Rectangle, p RhythmPanel) {
	drawText(img, face(true, 15), r.Min.X, r.Min.Y+14, p.Title, ink, alignLeft)

	plotT, plotB := r.Min.Y+40, r.Max.Y-40 // value labels inside, axis and caption below
	mid := (plotT + plotB) / 2
	half := float64(plotB-plotT)/2 - 16 // leave room for labels beyond the longest bar
	slotW := float64(r.Dx()) / 7
	barW := math.Round(slotW * 0.62)
	yOf := func(v float64) int {
		if p.Extent <= 0 {
			return mid
		}
		return mid - int(math.Round((v-p.Avg)/p.Extent*half))
	}

	for d := 0; d < 7; d++ {
		c := float64(r.Min.X) + (float64(d)+0.5)*slotW
		x0, x1 := int(math.Round(c-barW/2)), int(math.Round(c+barW/2))
		drawText(img, face(false, 12), int(c), plotB+16, weekdayInitials[d], muted, alignCenter)
		if !p.Has[d] {
			drawText(img, labelFace(), int(c), mid-4, "—", muted, alignCenter)
			continue
		}
		y := yOf(p.Values[d])
		top, bot := min(y, mid), max(y, mid)
		if bot-top < 2 {
			top, bot = mid-1, mid+1 // at the average: a stub, not nothing
		}
		above := p.Values[d] >= p.Avg
		better := above != p.LowerIsBetter
		bar := image.Rect(x0, top, x1, bot)
		if better {
			fillRect(img, bar, ink)
		} else {
			outline(img, bar, 1, ink)
		}
		if above {
			drawText(img, face(true, 12), int(c), top-5, p.Labels[d], ink, alignCenter)
		} else {
			drawText(img, face(true, 12), int(c), bot+14, p.Labels[d], ink, alignCenter)
		}
	}

	// the baseline: the average, across the panel and labeled at its end
	hline(img, r.Min.X, r.Max.X-1, mid, ink)
	drawText(img, face(false, 11), r.Max.X, r.Min.Y+30, p.AvgLabel+" · scale "+p.ExtentLabel, muted, alignRight)
	hline(img, r.Min.X, r.Max.X-1, plotB, grid)
	drawText(img, labelFace(), r.Min.X, r.Max.Y-4, p.Caption, muted, alignLeft)
}
