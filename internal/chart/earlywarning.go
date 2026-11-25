package chart

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"
)

// WarnNight is one night of one signal: S is how far it sat from the
// user's normal, in half-widths of their normal range, signed so that +
// is the direction that comes before illness (warmer, a faster heart,
// lower HRV, faster breathing). The range's edges are ±1; Up is S > 1.
type WarnNight struct {
	Day time.Time
	S   float64
	Up  bool
}

// WarnLane is one signal's row of the early-warning chart.
type WarnLane struct {
	Title, Hint   string      // "Temperature", "up: warmer"
	Nights        []WarnNight // oldest first
	Avg           []Point     // the 3-night average of S
	Value, Status string      // last reading, as measured, and how it sits: "+0.29 °C", "high"
}

// WarnDay is one night across the signals: how many were Up out of how
// many had a reading and a range, and the habits logged before it that can
// move them on their own (by code: ALC, OUT).
type WarnDay struct {
	Day    time.Time
	Up, Of int
	Habits []string
}

// EarlyWarning is the content of the early-warning chart.
type EarlyWarning struct {
	Title, Subtitle, Headline string
	From, To                  time.Time
	Lanes                     []WarnLane
	Days                      []WarnDay // every night From..To
	Watch                     int       // signals up at once that shade a night
	HabitKey                  []string  // "ALC alcohol", for the codes shown
}

// the strain scale every lane shares, in range half-widths: room for a
// night well above normal, less below (better than normal isn't the story)
const warnLo, warnHi = -2.5, 3.0

// EarlyWarningView renders the early-warning chart: a row per signal, each
// night a dot at how far it sat from the user's normal range (shaded),
// with up always the direction that comes before illness, and its 3-night
// average as a line. Nights with Watch or more signals up are shaded
// across every row. Under the rows, each night's count of signals up, then
// the habits logged before it that could explain it, then the dates and
// the key.
func EarlyWarningView(e EarlyWarning, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: early warning needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, labelW, valueW = 16, 112, 128
	const countH, habitH, dateH, keyH = 30, 26, 18, 52
	drawText(img, face(true, 22), pad, 30, e.Title, ink, alignLeft)
	drawText(img, face(false, 13), pad, 50, e.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, e.Headline, ink, alignRight)

	plotL, plotR := pad+labelW, width-pad-valueW
	laneTop := 64
	keyTop := height - keyH
	dateY := keyTop - 6
	habitTop := dateY - dateH - habitH
	countTop := habitTop - countH
	laneH := (countTop - laneTop) / max(1, len(e.Lanes))

	n := len(e.Days)
	if n == 0 {
		drawText(img, labelFace(), plotL+8, (laneTop+countTop)/2, "no nights in this window", muted, alignLeft)
		drawWarnKey(img, image.Rect(pad, keyTop, width-pad, height), e)
		return img, nil
	}
	colW := float64(plotR-plotL) / float64(n)
	col := map[string]int{}
	for i, d := range e.Days {
		col[d.Day.Format("2006-01-02")] = i
	}
	cx := func(d time.Time) float64 {
		return float64(plotL) + (float64(col[d.Format("2006-01-02")])+0.5)*colW
	}

	// nights to watch: a light column through the rows and the count
	for i, d := range e.Days {
		if e.Watch > 0 && d.Up >= e.Watch {
			x0 := plotL + int(math.Round(float64(i)*colW))
			x1 := plotL + int(math.Round(float64(i+1)*colW))
			fillRect(img, image.Rect(x0, laneTop, x1, countTop+countH-4), windowShade)
		}
	}

	for li, lane := range e.Lanes {
		top := laneTop + li*laneH
		inT, inB := top+5, top+laneH-5
		y := func(s float64) float64 {
			s = math.Max(warnLo, math.Min(warnHi, s))
			return float64(inB) - (s-warnLo)/(warnHi-warnLo)*float64(inB-inT)
		}
		if li > 0 {
			hline(img, pad, width-pad-1, top, grid)
		}
		// the normal range: a dot texture between dashed edges, as on body
		// signals
		bt, bb := int(math.Round(y(1))), int(math.Round(y(-1)))
		for yy := bt; yy <= bb; yy++ {
			for xx := plotL; xx < plotR; xx++ {
				if xx%3 == 0 && yy%3 == 0 {
					img.SetGray(xx, yy, ink)
				}
				if (yy == bt || yy == bb) && (xx/3)%2 == 0 {
					img.SetGray(xx, yy, ink)
				}
			}
		}

		drawText(img, face(true, 13), pad, top+20, lane.Title, ink, alignLeft)
		drawText(img, face(false, 10), pad, top+34, lane.Hint, muted, alignLeft)
		drawText(img, face(true, 15), width-pad, top+20, lane.Value, ink, alignRight)
		drawText(img, face(false, 11), width-pad, top+35, lane.Status, muted, alignRight)

		// the 3-night average, under the dots
		for k := 1; k < len(lane.Avg); k++ {
			a, b := lane.Avg[k-1], lane.Avg[k]
			thickLine(img, cx(a.Day), y(a.Value), cx(b.Day), y(b.Value), 2.5, ink)
		}
		for _, nt := range lane.Nights {
			px, py := cx(nt.Day), y(nt.S)
			if nt.Up {
				disc(img, px, py, dotRadius+dotRing, surface)
				disc(img, px, py, dotRadius, ink)
			} else {
				disc(img, px, py, 2.5, muted)
			}
		}
	}
	hline(img, pad, width-pad-1, countTop, grid)

	// signals up each night: a stack of small boxes, one per signal read
	drawText(img, face(true, 12), pad, countTop+18, "Signals up", ink, alignLeft)
	bw := int(math.Min(12, math.Max(4, colW-6)))
	const bh = 4
	for _, d := range e.Days {
		x0 := int(math.Round(cx(d.Day))) - bw/2
		for k := 0; k < d.Of; k++ {
			b := countTop + countH - 6 - (k+1)*(bh+1)
			r := image.Rect(x0, b, x0+bw, b+bh)
			if k < d.Up {
				fillRect(img, r, ink)
			} else {
				outline(img, r, 1, muted)
			}
		}
	}

	// what was logged before each night that can move these on its own
	drawText(img, face(true, 12), pad, habitTop+16, "Logged", ink, alignLeft)
	// each code takes the first of two lines it fits on without touching
	// the code before it; where neither has room, as when nights are
	// narrow, a small square marks that something more was logged
	hf := face(false, 9)
	right := [2]int{math.MinInt, math.MinInt}
	for _, d := range e.Days {
		x := int(math.Round(cx(d.Day)))
		for _, code := range d.Habits {
			w := textWidth(hf, code)
			placed := false
			for row := range right {
				if x-w/2 > right[row]+2 {
					drawText(img, hf, x, habitTop+11+row*11, code, ink, alignCenter)
					right[row] = x + w/2
					placed = true
					break
				}
			}
			if !placed {
				fillRect(img, image.Rect(x-1, habitTop+habitH-4, x+2, habitTop+habitH-1), ink)
			}
		}
	}

	// dates: as many as fit without touching, always last night's, each
	// night by the evening it began
	lf := face(false, 11)
	every := int(math.Ceil(float64(textWidth(lf, "Nov 30")+10) / colW))
	for i := n - 1; i >= 0; i -= every {
		d := e.Days[i].Day
		xx := int(math.Round(cx(d)))
		vline(img, xx, dateY-dateH+2, dateY-dateH+5, muted)
		drawText(img, lf, xx, dateY, NightOf(d).Format("Jan 2"), muted, alignCenter)
	}

	drawWarnKey(img, image.Rect(pad, keyTop, width-pad, height), e)
	return img, nil
}

// drawWarnKey draws the key across r: each mark as drawn, three to a row,
// then the habit codes shown.
func drawWarnKey(img *image.Gray, r image.Rectangle, e EarlyWarning) {
	hline(img, r.Min.X, r.Max.X-1, r.Min.Y, grid)
	f := face(false, 11)
	entries := []keyEntry{
		{"beyond normal, toward illness", centered(func(cx, cy int) {
			disc(img, float64(cx), float64(cy), dotRadius+dotRing, surface)
			disc(img, float64(cx), float64(cy), dotRadius, ink)
		})},
		{"within normal, or better", centered(func(cx, cy int) { disc(img, float64(cx), float64(cy), 2.5, muted) })},
		{"your normal (prior 30 nights)", centered(func(cx, cy int) { rangeSwatch(img, cx, cy) })},
		{"3-night average", centered(func(cx, cy int) {
			thickLine(img, float64(cx-10), float64(cy+2), float64(cx+10), float64(cy-2), 2.5, ink)
		})},
		{fmt.Sprintf("%d+ signals up that night", e.Watch), centered(func(cx, cy int) {
			fillRect(img, image.Rect(cx-10, cy-6, cx+11, cy+6), windowShade)
		})},
		{"a signal up / not up", centered(func(cx, cy int) {
			fillRect(img, image.Rect(cx-10, cy-5, cx-1, cy-1), ink)
			outline(img, image.Rect(cx+1, cy-5, cx+10, cy-1), 1, muted)
		})},
	}
	colW := r.Dx() / 3
	for i, en := range entries {
		x := r.Min.X + (i%3)*colW
		y := r.Min.Y + 16 + (i/3)*16
		en.mark(x, y)
		drawText(img, f, x+28, y, en.label, ink, alignLeft)
	}
	if len(e.HabitKey) > 0 {
		drawText(img, f, r.Min.X, r.Min.Y+48, "Logged: "+strings.Join(e.HabitKey, " · ")+" · ▪ one more that didn't fit", muted, alignLeft)
	}
}
