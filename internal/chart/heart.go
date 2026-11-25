package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

// HeartPoint is one heart-rate reading.
type HeartPoint struct {
	Time time.Time
	BPM  float64
}

// HeartSlot is the usual heart rate for one slot of the day: the middle
// half of past readings at that time of day.
type HeartSlot struct {
	Start  time.Time // the slot's start, on the day being drawn
	Lo, Hi float64   // 25th and 75th percentiles
}

// Span is a stretch of the day: asleep, or an activity.
type Span struct {
	Start, End time.Time
	Label      string // "walking", "meditation"; empty for sleep
}

// HeartDay is the content of the heart-rate-today chart.
type HeartDay struct {
	Title, Subtitle string
	Headline        string    // top right: "72 bpm at 2:40 PM"
	Start, End      time.Time // local midnight to the next
	Now             time.Time // readings stop here; after it the day is to come
	SlotLen         time.Duration
	Readings        []HeartPoint
	Usual           []HeartSlot
	Asleep          []Span
	Activities      []Span // workouts and sessions
	Peak            HeartPoint
	HasPeak         bool
	Stats           []StatLine
}

// heartGap is the longest gap a line joins across: Oura reads daytime heart
// rate only when the wearer is still, so the line breaks where it stops.
const heartGap = 16 * time.Minute

// Heart layout, in pixels.
const (
	heartPanelW = 176
	heartLaneH  = 16 // above the plot: asleep and activities
)

// HeartDayView renders today's heart rate over 24 hours, midnight to
// midnight: each reading as a line (broken where readings stop) on a white
// halo, over a dotted band of the usual — the middle half of the past four
// weeks' readings at that time of day. A lane above marks sleep (solid) and
// workouts and sessions (outlined, named, and hatched down through the
// plot), so a rise has its reason beside it. The rest of the day, after
// now, keeps the usual band for what is to come. A side panel holds the key
// and the figures.
func HeartDayView(h HeartDay, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: heart rate needs at least 600x360, got %dx%d", width, height)
	}
	if !h.End.After(h.Start) {
		return nil, fmt.Errorf("chart: nothing to draw")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, axisW, footH = 16, 34, 24
	drawText(img, face(true, 22), pad, 30, h.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, h.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, h.Headline, ink, alignRight)

	plotL, plotR := pad+axisW, width-pad-heartPanelW-18
	laneT := 70
	top := laneT + heartLaneH + 6
	plotB := height - footH
	span := h.End.Sub(h.Start).Seconds()
	x := func(t time.Time) float64 {
		return float64(plotL) + t.Sub(h.Start).Seconds()/span*float64(plotR-plotL)
	}
	clampX := func(t time.Time) int {
		return int(math.Round(math.Max(float64(plotL), math.Min(float64(plotR), x(t)))))
	}

	// the scale covers the readings and the usual band
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range h.Readings {
		lo, hi = math.Min(lo, p.BPM), math.Max(hi, p.BPM)
	}
	for _, s := range h.Usual {
		lo, hi = math.Min(lo, s.Lo), math.Max(hi, s.Hi)
	}
	if math.IsInf(lo, 0) {
		lo, hi = 50, 100
	}
	// a few beats of room each way, on round tens; heart rate never nears 0,
	// so the axis doesn't start there
	ymin := math.Floor((lo-5)/10) * 10
	ymax := math.Ceil((hi+5)/10) * 10
	step := 10.0
	switch {
	case ymax-ymin > 120:
		step = 40
	case ymax-ymin > 60:
		step = 20
	}
	yOf := func(v float64) float64 {
		return float64(plotB) - (v-ymin)/(ymax-ymin)*float64(plotB-top)
	}
	for v := ymin; v <= ymax+step/2; v += step {
		y := int(math.Round(yOf(v)))
		hline(img, plotL, plotR-1, y, grid)
		drawText(img, face(false, 11), plotL-6, y+4, formatValue(v), muted, alignRight)
	}

	// hours along the bottom, every 3, with 6-hourly labels
	for hr := 0; hr <= 24; hr += 3 {
		t := h.Start.Add(time.Duration(hr) * time.Hour)
		cx := int(math.Round(x(t)))
		vline(img, cx, plotB, plotB+3, muted)
		if hr%6 == 0 {
			a := alignCenter
			if hr == 0 {
				a = alignLeft
			} else if hr == 24 {
				a = alignRight
			}
			label := map[int]string{0: "12 AM", 6: "6 AM", 12: "noon", 18: "6 PM", 24: "12 AM"}[hr]
			drawText(img, face(false, 11), cx, plotB+16, label, muted, a)
		}
	}

	// activities: hatched columns through the plot, under everything else
	for _, a := range h.Activities {
		x0, x1 := clampX(a.Start), clampX(a.End)
		for y := top; y < plotB; y++ {
			for c := x0; c <= x1; c++ {
				if (c+y)%6 == 0 {
					img.SetGray(c, y, ink)
				}
			}
		}
	}

	// the usual: a dotted band per slot, its edges dashed
	for _, s := range h.Usual {
		x0, x1 := clampX(s.Start), clampX(s.Start.Add(h.SlotLen))
		y0, y1 := int(math.Round(yOf(s.Hi))), int(math.Round(yOf(s.Lo)))
		for c := x0; c < x1; c++ {
			for y := y0; y <= y1; y++ {
				if c%3 == 0 && y%3 == 0 {
					img.SetGray(c, y, ink)
				}
			}
			if (c/3)%2 == 0 {
				img.SetGray(c, y0, ink)
				img.SetGray(c, y1, ink)
			}
		}
	}

	// the lane: sleep as a solid bar, activities as outlined, named boxes;
	// a name too long for its box goes beside it, if the lane is free there
	type taken struct{ x0, x1 int }
	var used []taken
	for _, sp := range append(append([]Span{}, h.Asleep...), h.Activities...) {
		used = append(used, taken{clampX(sp.Start), max(clampX(sp.End), clampX(sp.Start)+2)})
	}
	free := func(x0, x1 int) bool {
		if x0 < plotL || x1 > plotR {
			return false
		}
		for _, u := range used {
			if x0 < u.x1+3 && x1 > u.x0-3 {
				return false
			}
		}
		return true
	}
	lane := func(sp Span, solid bool) {
		x0, x1 := clampX(sp.Start), clampX(sp.End)
		if x1-x0 < 2 {
			x1 = x0 + 2
		}
		r := image.Rect(x0, laneT, x1, laneT+heartLaneH)
		if solid {
			fillRect(img, r, ink)
		} else {
			fillRect(img, r, surface)
			outline(img, r, 1, ink)
		}
		label, fg := sp.Label, ink
		if solid {
			label, fg = "asleep", surface
		}
		f := face(false, 11)
		tw := textWidth(f, label)
		switch {
		case tw+6 <= r.Dx():
			drawText(img, f, (r.Min.X+r.Max.X)/2, r.Max.Y-4, label, fg, alignCenter)
		case !solid && free(r.Max.X+4, r.Max.X+4+tw):
			drawText(img, f, r.Max.X+4, r.Max.Y-4, label, ink, alignLeft)
			used = append(used, taken{r.Max.X + 4, r.Max.X + 4 + tw})
		case !solid && free(r.Min.X-4-tw, r.Min.X-4):
			drawText(img, f, r.Min.X-4, r.Max.Y-4, label, ink, alignRight)
			used = append(used, taken{r.Min.X - 4 - tw, r.Min.X - 4})
		}
	}
	for _, sp := range h.Asleep {
		lane(sp, true)
	}
	for _, a := range h.Activities {
		lane(a, false)
	}

	// now: a dashed rule; the rest of the day is to come
	if h.Now.After(h.Start) && h.Now.Before(h.End) {
		nx := int(math.Round(x(h.Now)))
		for y := top; y < plotB; y++ {
			if (y/3)%2 == 0 {
				img.SetGray(nx, y, ink)
			}
		}
		// labeled at the foot, clear of the peak label at the top
		f := face(true, 11)
		fillRect(img, image.Rect(nx+2, plotB-17, nx+6+textWidth(f, "now"), plotB-2), surface)
		drawText(img, f, nx+4, plotB-5, "now", ink, alignLeft)
	}

	// the readings: a line on a white halo, broken across gaps, and a dot
	// for a reading with no neighbor
	for _, pass := range []struct {
		w float64
		c color.Gray
	}{{5, surface}, {2, ink}} {
		for k, p := range h.Readings {
			joined := false
			if k > 0 && p.Time.Sub(h.Readings[k-1].Time) <= heartGap {
				q := h.Readings[k-1]
				thickLine(img, x(q.Time), yOf(q.BPM), x(p.Time), yOf(p.BPM), pass.w, pass.c)
				joined = true
			}
			if k+1 < len(h.Readings) && h.Readings[k+1].Time.Sub(p.Time) <= heartGap {
				joined = true
			}
			if !joined {
				disc(img, x(p.Time), yOf(p.BPM), pass.w/2+0.6, pass.c)
			}
		}
	}

	// the peak: ringed and labeled
	if h.HasPeak {
		px, py := x(h.Peak.Time), yOf(h.Peak.BPM)
		disc(img, px, py, dotRadius+dotRing, surface)
		disc(img, px, py, dotRadius, ink)
		label := fmt.Sprintf("peak %.0f · %s", h.Peak.BPM, clock(h.Peak.Time))
		f := face(true, 12)
		tw := textWidth(f, label)
		tx := max(plotL+2, min(int(px)-tw/2, plotR-2-tw))
		ty := int(py) - 12
		if ty-12 < top {
			// no room above: beside the dot, on whichever side has room,
			// never below it, where its box would blank out the line
			ty = int(py) + 4
			tx = int(px) + int(dotRadius+dotRing) + 4
			if tx+tw > plotR-2 {
				tx = int(px) - int(dotRadius+dotRing) - 4 - tw
			}
		}
		fillRect(img, image.Rect(tx-3, ty-12, tx+tw+3, ty+4), surface)
		drawText(img, f, tx, ty, label, ink, alignLeft)
	}

	if len(h.Readings) == 0 {
		drawText(img, labelFace(), plotL+8, (top+plotB)/2, "no heart-rate readings yet today", muted, alignLeft)
	}

	drawHeartPanel(img, image.Rect(width-pad-heartPanelW, laneT, width-pad, height-pad), h.Stats)
	return img, nil
}

// clock is a time of day: "7:10 AM".
func clock(t time.Time) string { return t.Format("3:04 PM") }

// drawHeartPanel draws the key, each mark as on the chart, then the figures.
func drawHeartPanel(img *image.Gray, r image.Rectangle, stats []StatLine) {
	drawKeyAndFigures(img, r, []keyEntry{
		{"heart rate readings", centered(func(x, y int) {
			cx, cy := float64(x), float64(y)
			thickLine(img, cx-10, cy+2, cx-3, cy-3, 2, ink)
			thickLine(img, cx-3, cy-3, cx+3, cy+1, 2, ink)
			thickLine(img, cx+3, cy+1, cx+10, cy-2, 2, ink)
		})},
		{"usual at that hour", centered(func(cx, cy int) { rangeSwatch(img, cx, cy) })},
		{"asleep", centered(func(cx, cy int) { fillRect(img, image.Rect(cx-10, cy-6, cx+11, cy+5), ink) })},
		{"workout or session", centered(func(cx, cy int) {
			br := image.Rect(cx-10, cy-6, cx+11, cy+5)
			for yy := br.Min.Y; yy < br.Max.Y; yy++ {
				for xx := br.Min.X; xx < br.Max.X; xx++ {
					if (xx+yy)%6 == 0 {
						img.SetGray(xx, yy, ink)
					}
				}
			}
			outline(img, br, 1, ink)
		})},
	}, stats)
}
