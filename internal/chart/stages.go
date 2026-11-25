package chart

import (
	"fmt"
	"image"
	"math"
	"time"
)

// StageNight is one night's time in each stage.
type StageNight struct {
	Day                     time.Time // the wake-up day
	Deep, Light, REM, Awake time.Duration
}

// Asleep is time in any sleep stage, awake excluded.
func (n StageNight) Asleep() time.Duration { return n.Deep + n.Light + n.REM }

// InBed is the whole bar: every stage including awake.
func (n StageNight) InBed() time.Duration { return n.Asleep() + n.Awake }

// StageNights is the content of the stacked stage-bar chart.
type StageNights struct {
	Title, Subtitle string
	From, To        time.Time // one bar slot per day
	Nights          []StageNight
}

// How each stage fills its segment. Light sleep is nearly half of every
// night, so it is plain — white inside a thin outline — which keeps a chart
// of thirty stacked bars calm; texture is spent only on the smaller stages:
// deep solid, REM a single diagonal hatch, awake sparse dots in an outline.
// All pure black on white, anchored to absolute pixels so neighbouring bars'
// patterns line up.
func fillStage(img *image.Gray, r image.Rectangle, s Stage) {
	if r.Empty() {
		return
	}
	switch s {
	case StageDeep:
		fillRect(img, r, ink)
	case StageLight:
		outline(img, r, 1, ink)
	case StageREM:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if (x+y)%4 == 0 {
					img.SetGray(x, y, ink)
				}
			}
		}
		outline(img, r, 1, ink)
	case StageAwake:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if x%3 == 0 && y%3 == 0 {
					img.SetGray(x, y, ink)
				}
			}
		}
		outline(img, r, 1, ink)
	}
}

// Stack order, bottom to top: deep anchors the baseline and awake sits on
// top, the same top-to-bottom order as the night chart's lanes.
var stackOrder = []Stage{StageDeep, StageLight, StageREM, StageAwake}

func (n StageNight) of(s Stage) time.Duration {
	switch s {
	case StageDeep:
		return n.Deep
	case StageLight:
		return n.Light
	case StageREM:
		return n.REM
	case StageAwake:
		return n.Awake
	}
	return 0
}

// StageNightsView draws one stacked bar per night: its height is time in
// bed, split into stages. Segments are separated by a 2px white gap taken
// out of the upper segment, so a bar's total height stays exact. Missing
// nights are empty slots. The latest night is labeled with its time asleep;
// the side panel gives each stage's average night.
func StageNightsView(sn StageNights, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: stage nights need at least 600x360, got %dx%d", width, height)
	}
	if sn.To.Before(sn.From) {
		return nil, fmt.Errorf("chart: To is before From")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, panelW, gap = 16, 176, 2
	drawText(img, face(true, 22), pad, 30, sn.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, sn.Subtitle, muted, alignLeft)

	plotL, plotR := pad+40, width-pad-panelW-20
	plotT, plotB := 88, height-44
	days := dayIndex(sn.From, sn.To) + 1
	slotW := float64(plotR-plotL) / float64(days)
	barW := math.Max(3, math.Round(slotW*0.68))

	// y scale in hours, from zero: bar height is the quantity
	var maxH float64
	for _, n := range sn.Nights {
		maxH = math.Max(maxH, n.InBed().Hours())
	}
	if maxH == 0 {
		maxH = 8
	}
	_, ymax, step := niceScale(0, maxH, 6)
	pxPerHour := float64(plotB-plotT) / ymax
	yOf := func(h float64) int { return plotB - int(math.Round(h*pxPerHour)) }
	for h := 0.0; h <= ymax+step/2; h += step {
		y := yOf(h)
		if h > 0 {
			hline(img, plotL, plotR, y, grid)
		}
		label := fmt.Sprintf("%gh", h)
		if h == 0 {
			label = "0"
		}
		drawText(img, face(false, 11), plotL-8, y+4, label, muted, alignRight)
	}
	hline(img, plotL, plotR, plotB, ink) // the shared baseline

	slotX := func(day time.Time) (x0, x1 int, center float64) {
		c := float64(plotL) + (float64(dayIndex(sn.From, day))+0.5)*slotW
		return int(math.Round(c - barW/2)), int(math.Round(c + barW/2)), c
	}

	// weekday initials under every slot; a date under the latest and every
	// seventh night before it, so the latest is always labeled. Each slot
	// is labeled by the evening its night began, not the day of waking
	for i := 0; i < days; i++ {
		day := sn.From.AddDate(0, 0, i)
		_, _, c := slotX(day)
		night := NightOf(day)
		drawText(img, face(false, 11), int(c), plotB+15, night.Format("Mon")[:1], muted, alignCenter)
		if (days-1-i)%7 == 0 {
			drawText(img, labelFace(), int(c), plotB+32, night.Format("Jan 2"), muted, alignCenter)
		}
	}

	var latest *StageNight // labeled in the side panel, where it can't collide
	for i := range sn.Nights {
		n := sn.Nights[i]
		d := dayIndex(sn.From, n.Day)
		if d < 0 || d >= days {
			continue
		}
		x0, x1, _ := slotX(n.Day)
		var cum time.Duration
		for k, s := range stackOrder {
			seg := n.of(s)
			if seg <= 0 {
				continue
			}
			bottom := yOf(cum.Hours())
			cum += seg
			top := yOf(cum.Hours())
			if k > 0 && cum > seg { // not the lowest drawn segment
				bottom -= gap
			}
			if bottom <= top {
				bottom = top + 1 // a sliver still shows
			}
			fillStage(img, image.Rect(x0, top, x1, bottom), s)
		}
		if latest == nil || n.Day.After(latest.Day) {
			latest = &sn.Nights[i]
		}
	}
	drawStagePanel(img, image.Rect(width-pad-panelW, plotT-8, width-pad, plotB), sn.Nights, latest)
	return img, nil
}

// drawStagePanel is the legend, doubling as each stage's average night,
// listed top to bottom in the same order as the stack.
//
// The key — title and the four stage rows — sits in a box so it reads as the
// chart's legend at a glance; the summary figures below it stay outside.
func drawStagePanel(img *image.Gray, r image.Rectangle, nights []StageNight, latest *StageNight) {
	const boxPad = 10
	boxTop := r.Min.Y - 10
	key := image.Rect(r.Min.X+boxPad, 0, r.Max.X-boxPad, 0) // content edges inside the box
	y := boxTop + boxPad + 12
	drawText(img, face(true, 13), key.Min.X, y, "Average night", ink, alignLeft)
	n := len(nights)
	var sum StageNight
	for _, x := range nights {
		sum.Deep += x.Deep
		sum.Light += x.Light
		sum.REM += x.REM
		sum.Awake += x.Awake
	}
	avg := func(d time.Duration) time.Duration {
		if n == 0 {
			return 0
		}
		return d / time.Duration(n)
	}
	y += 12
	for i := len(stackOrder) - 1; i >= 0; i-- {
		s := stackOrder[i]
		sw := image.Rect(key.Min.X, y, key.Min.X+26, y+18)
		fillStage(img, sw, s)
		if s == StageDeep {
			outline(img, sw, 1, ink)
		}
		drawText(img, face(false, 14), sw.Max.X+8, y+14, stageNames[s], ink, alignLeft)
		value, share := "—", ""
		if n > 0 {
			value = hm(avg(sum.of(s)))
			if in := sum.InBed(); in > 0 {
				share = fmt.Sprintf("%.0f%%", 100*float64(sum.of(s))/float64(in))
			}
		}
		drawText(img, face(true, 14), key.Max.X, y+12, value, ink, alignRight)
		drawText(img, face(false, 11), key.Max.X, y+25, share, muted, alignRight)
		y += 34
	}
	// close the box just under the last row's share line
	outline(img, image.Rect(r.Min.X, boxTop, r.Max.X, y-2+boxPad/2), 1, ink)
	y += 24
	rows := []StatLine{
		{"Asleep", hmOrDash(avg(sum.Asleep()), n)},
		{"In bed", hmOrDash(avg(sum.InBed()), n)},
		{"Nights", fmt.Sprint(n)},
	}
	if latest != nil {
		rows = append(rows, StatLine{"Last night asleep", hm(latest.Asleep())})
	}
	for _, row := range rows {
		drawText(img, labelFace(), r.Min.X, y, row.Label, muted, alignLeft)
		drawText(img, face(true, 13), r.Max.X, y, row.Value, ink, alignRight)
		y += 19
	}
}

func hmOrDash(d time.Duration, n int) string {
	if n == 0 {
		return "—"
	}
	return hm(d)
}
