package chart

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"
)

// WeekDay is one day's card on the week-in-review screen.
type WeekDay struct {
	Day                                 time.Time
	Sleep, Readiness, Activity          float64 // 0-100 scores
	HasSleep, HasReadiness, HasActivity bool
	Steps, Stress                       string // formatted: "6.2k", "1h 30m", or "—"
	Calories                            string // active calories: "420", or "—"
	Future                              bool   // not yet happened: drawn blank

	// ActivityGoal makes the inner ring progress toward Oura's daily
	// activity goal instead of the activity score: Activity is percent of
	// the goal, a full ring is the goal met, and the ring carries no 85
	// tick. The daily brief uses it, since the score is a judgment of the
	// whole day, not something that fills as the day goes.
	ActivityGoal bool
}

// Optimal is how many of the day's three scores reached optimalScore.
func (d WeekDay) Optimal() int {
	n := 0
	for i, sc := range []struct {
		v   float64
		has bool
	}{{d.Sleep, d.HasSleep}, {d.Readiness, d.HasReadiness}, {d.Activity, d.HasActivity}} {
		target := float64(optimalScore)
		if i == 2 && d.ActivityGoal {
			target = 100 // the goal met
		}
		if sc.has && sc.v >= target {
			n++
		}
	}
	return n
}

// AllOptimal is an optimal day: all three scores reached optimalScore. The
// Wolfe Health mark fills its rings.
func (d WeekDay) AllOptimal() bool { return d.Optimal() == 3 }

// StatLine3 is a figure with a label and a change: one cell of the "vs last
// week" strip.
type StatLine3 struct{ Label, Value, Change string }

// WeekReview is the content of the week-in-review screen.
type WeekReview struct {
	Title, Subtitle, Headline string
	Days                      [7]WeekDay // Sunday to Saturday
	Best, Worst               int        // day indexes to mark; -1 for none
	Stats                     []StatLine3
}

// optimalScore is where Oura's "optimal" band starts (85+ optimal, 70-84
// good, under 70 pay attention). Each ring carries a tick there, so a ring
// reads against a target, not just against full.
const optimalScore = 85

// ringStyle is a score ring's texture, so the three rings tell apart on a
// black-and-white panel without a key: sleep solid, readiness outlined,
// activity dashed.
type ringStyle int

const (
	ringSolid ringStyle = iota
	ringOutlined
	ringDashed
)

var scoreRings = []struct {
	label string
	style ringStyle
}{{"Sleep", ringSolid}, {"Readiness", ringOutlined}, {"Activity", ringDashed}}

// styledArc draws a ring band from the top clockwise to fraction f in the
// given texture: solid; outlined (a 1px edge all round, hollow inside); or
// dashed (4px dashes, 3px gaps, measured along the band's middle).
func styledArc(img *image.Gray, cx, cy, ri, ro, f float64, st ringStyle) {
	if f <= 0 {
		return
	}
	rm := (ri + ro) / 2
	circ := 2 * math.Pi * rm
	for y := int(math.Floor(cy - ro)); y <= int(math.Ceil(cy+ro)); y++ {
		for x := int(math.Floor(cx - ro)); x <= int(math.Ceil(cx+ro)); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			r := math.Hypot(dx, dy)
			if r < ri || r > ro {
				continue
			}
			a := math.Atan2(dx, -dy) / (2 * math.Pi)
			if a < 0 {
				a++
			}
			if a > f {
				continue
			}
			switch st {
			case ringOutlined:
				edge := r < ri+1.2 || r > ro-1.2 || a*circ < 1.2 || (f-a)*circ < 1.2
				if !edge {
					continue
				}
			case ringDashed:
				if math.Mod(a*circ, 7) >= 4 {
					continue
				}
			}
			img.SetGray(x, y, ink)
		}
	}
}

// targetTick draws a radial mark across a ring band at fraction f, 2px wide
// and kept inside the band (so the three rings' ticks don't join into one
// line), inverting whatever it crosses: white through a filled band, black
// over an empty track, so it shows either way.
func targetTick(img *image.Gray, cx, cy, ri, ro, f float64) {
	ang := f * 2 * math.Pi
	ux, uy := math.Sin(ang), -math.Cos(ang) // outward
	px, py := -uy, ux                       // across
	seen := map[image.Point]bool{}
	for r := ri + 0.25; r <= ro-0.25; r += 0.25 {
		for _, o := range []float64{-0.5, 0.5} {
			pt := image.Pt(int(math.Floor(cx+ux*r+px*o)), int(math.Floor(cy+uy*r+py*o)))
			if seen[pt] || !pt.In(img.Rect) {
				continue
			}
			seen[pt] = true
			if img.GrayAt(pt.X, pt.Y).Y < 128 {
				img.SetGray(pt.X, pt.Y, surface)
			} else {
				img.SetGray(pt.X, pt.Y, ink)
			}
		}
	}
}

// drawScoreRings draws three concentric 0-100 score rings centered at
// (cx, cy), outer radius R: sleep outside, readiness, then activity inside,
// each clockwise from 12 o'clock over a faint full track — the stress dial's
// construction — in its own texture, with a tick at the optimal score.
// Fuller is better on all three. The center holds the wolf on a day all
// three reach optimal, and otherwise how many did ("2/3").
func drawScoreRings(img *image.Gray, cx, cy, R float64, d WeekDay) {
	// bands thick enough to show a texture, gaps wide enough for the ticks
	th := math.Max(5, math.Round(R*0.14))
	gap := math.Max(4, math.Round(R*0.1))
	drawRings(img, cx, cy, R, th, gap, d, true)
}

// drawCompactRings is drawScoreRings for a small calendar cell: thinner
// bands and gaps so the center keeps room for its mark, and no pointer
// outside the rings, whose in-band ticks already mark the optimal score.
func drawCompactRings(img *image.Gray, cx, cy, R float64, d WeekDay) {
	th := math.Max(3, math.Round(R*0.15))
	gap := math.Max(2, math.Round(R*0.08))
	drawRings(img, cx, cy, R, th, gap, d, false)
}

// drawRings draws the three score rings with band thickness th and gap
// between bands, the center mark, and optionally the outer pointer.
func drawRings(img *image.Gray, cx, cy, R, th, gap float64, d WeekDay, pointer bool) {
	vals := []struct {
		v   float64
		has bool
	}{{d.Sleep, d.HasSleep}, {d.Readiness, d.HasReadiness}, {d.Activity, d.HasActivity}}
	ro := R
	for i, rg := range vals {
		ri := ro - th
		ring(img, cx, cy, (ro+ri)/2, grid)
		if rg.has {
			styledArc(img, cx, cy, ri, ro, math.Max(0, math.Min(1, rg.v/100)), scoreRings[i].style)
		}
		if i < 2 || !d.ActivityGoal { // a goal ring's target is full
			targetTick(img, cx, cy, ri, ro, optimalScore/100.0)
		}
		ro = ri - gap
	}
	// the center: the space inside the innermost ring, less the gap
	if free := ro; free >= 8 && (d.HasSleep || d.HasReadiness || d.HasActivity) {
		if d.AllOptimal() {
			sz := free * 1.55
			DrawWolfMark(img, cx-sz/2, cy-sz/2, sz, ink, surface)
		} else {
			f := face(true, math.Max(9, math.Round(free*0.75)))
			drawText(img, f, int(cx), int(cy+free*0.28), fmt.Sprintf("%d/3", d.Optimal()), muted, alignCenter)
		}
	}
	if !pointer {
		return
	}
	// one pointer outside the rings at the optimal score
	ang := optimalScore / 100.0 * 2 * math.Pi
	ux, uy := math.Sin(ang), -math.Cos(ang)
	px, py := -uy, ux
	tip, base := R+1, R+7
	fillPolygon(img, [][2]float64{
		{cx + ux*tip, cy + uy*tip},
		{cx + ux*base + px*3.5, cy + uy*base + py*3.5},
		{cx + ux*base - px*3.5, cy + uy*base - py*3.5},
	}, ink)
}

// ringSwatch is a short horizontal band in a ring's texture, for naming the
// rings in each card and in the key.
func ringSwatch(img *image.Gray, r image.Rectangle, st ringStyle) {
	switch st {
	case ringSolid:
		fillRect(img, r, ink)
	case ringOutlined:
		outline(img, r, 1, ink)
	case ringDashed:
		for x := r.Min.X; x < r.Max.X; x += 7 {
			fillRect(img, image.Rect(x, r.Min.Y, min(x+4, r.Max.X), r.Max.Y), ink)
		}
	}
}

// WeekView renders the week in review: seven day cards, Sunday to Saturday,
// each with its three score rings, named beneath with their scores, then
// steps and high-stress time; the
// best night's card has an inverted header and the toughest a heavy border,
// as in the monthly report, each named under its rings so neither reads as
// "today". Below, the week against the week before.
func WeekView(w WeekReview, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: week review needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad, gap = 16, 6
	drawText(img, face(true, 22), pad, 30, w.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, w.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, w.Headline, ink, alignRight)

	cardsT := 68
	statsH := 104
	cardsB := height - statsH - 10
	cardW := float64(width-2*pad-6*gap) / 7
	for i, d := range w.Days {
		x0 := pad + int(float64(i)*(cardW+gap))
		r := image.Rect(x0, cardsT, x0+int(cardW), cardsB)
		if !d.Future { // each day a zone a tap can open: "day:2026-11-21"
			addZone("day:"+d.Day.Format("2006-01-02"), r)
		}
		drawWeekCard(img, r, d, i == w.Best, i == w.Worst)
	}

	// the week against the week before: four across, two down
	top := cardsB + 10
	hline(img, pad, width-pad-1, top, rule)
	drawText(img, face(true, 11), pad, top+16, "VS LAST WEEK", muted, alignLeft)
	drawRingKey(img, width-pad, top+16)
	colW := (width - 2*pad) / 4
	for i, s := range w.Stats {
		x := pad + (i%4)*colW
		y := top + 40 + (i/4)*38
		drawText(img, face(false, 12), x, y, s.Label, muted, alignLeft)
		vf := face(true, 16)
		vx := x + 78
		drawText(img, vf, vx, y+1, s.Value, ink, alignLeft)
		drawText(img, face(false, 11), vx, y+16, s.Change, muted, alignLeft)
	}
	return img, nil
}

// drawRingKey draws, right-aligned at x on baseline y, each ring's swatch
// and name, then what the tick means.
func drawRingKey(img *image.Gray, x, y int) {
	f := face(false, 11)
	tail := "all three optimal"
	x -= textWidth(f, tail)
	drawText(img, f, x, y, tail, muted, alignLeft)
	x -= 18
	DrawWolfMark(img, float64(x), float64(y-11), 13, ink, surface)
	mid := "85 = optimal, fuller is better"
	x -= 10 + textWidth(f, mid)
	drawText(img, f, x, y, mid, muted, alignLeft)
	x -= 13 // the pointer, pointing in as on a ring
	fx, fy := float64(x), float64(y)
	fillPolygon(img, [][2]float64{{fx + 4, fy - 2}, {fx, fy - 9}, {fx + 8, fy - 9}}, ink)
	for i := len(scoreRings) - 1; i >= 0; i-- {
		x -= 14 + textWidth(f, scoreRings[i].label)
		drawText(img, f, x, y, scoreRings[i].label, muted, alignLeft)
		x -= 22
		ringSwatch(img, image.Rect(x, y-8, x+18, y-2), scoreRings[i].style)
	}
}

// dayCard draws a week screen's card frame for day in r: its header naming
// the day, inverted for the best one; a heavy border for the worst; a
// dotted outline only, for a day still to come. It returns the header, and
// whether the card has a body to fill.
func dayCard(img *image.Gray, r image.Rectangle, day time.Time, future, best, worst bool) (image.Rectangle, bool) {
	head := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+22)
	label := strings.ToUpper(day.Format("Mon")) + " " + day.Format("2")
	cx := (r.Min.X + r.Max.X) / 2
	if future {
		dottedOutline(img, r, grid)
		drawText(img, face(true, 12), cx, head.Min.Y+15, label, muted, alignCenter)
		return head, false
	}
	fg := ink
	if best {
		fillRect(img, head, ink)
		fg = surface
	}
	drawText(img, face(true, 12), cx, head.Min.Y+15, label, fg, alignCenter)
	if worst {
		outline(img, r, 3, ink)
	} else {
		outline(img, r, 1, ink)
	}
	hline(img, r.Min.X, r.Max.X-1, head.Max.Y, ink)
	return head, true
}

func drawWeekCard(img *image.Gray, r image.Rectangle, d WeekDay, best, worst bool) {
	head, ok := dayCard(img, r, d.Day, d.Future, best, worst)
	if !ok {
		return
	}
	cx := float64(r.Min.X+r.Max.X) / 2

	// rings fill the card's width, above the rows that name them; a line
	// under them names what the header or border marks, when the rings
	// still fit above it
	const rowH, tagH = 16, 14
	figH := 6*rowH + 12
	area := image.Rect(r.Min.X, head.Max.Y, r.Max.X, r.Max.Y-figH)
	ringR := func(a image.Rectangle) float64 { return math.Min(float64(a.Dx()), float64(a.Dy()))/2 - 8 }
	if short := image.Rect(area.Min.X, area.Min.Y, area.Max.X, area.Max.Y-tagH); ringR(short) >= 16 {
		area = short
		if tag := weekCardTag(r.Dx()-8, best, worst); tag != "" {
			drawText(img, face(true, 10), int(cx), r.Max.Y-figH-4, tag, ink, alignCenter)
		}
	}
	R := ringR(area)
	if R >= 16 {
		drawScoreRings(img, cx, float64(area.Min.Y+area.Max.Y)/2, R, d)
	}

	hline(img, r.Min.X+6, r.Max.X-7, r.Max.Y-figH, grid)
	x0, x1 := r.Min.X+7, r.Max.X-7
	y := r.Max.Y - figH + 6 + 12
	row := func(sw int, label, value string) {
		lx := x0
		if sw >= 0 {
			ringSwatch(img, image.Rect(x0, y-8, x0+12, y-2), ringStyle(sw))
			lx += 16
		}
		drawText(img, face(false, 10), lx, y, label, muted, alignLeft)
		drawText(img, face(true, 12), x1, y, value, ink, alignRight)
		y += rowH
	}
	scores := []struct {
		v   float64
		has bool
	}{{d.Sleep, d.HasSleep}, {d.Readiness, d.HasReadiness}, {d.Activity, d.HasActivity}}
	for i, sc := range scores {
		v := "—"
		if sc.has {
			v = fmt.Sprintf("%.0f", sc.v)
		}
		row(int(scoreRings[i].style), scoreRings[i].label, v)
	}
	row(-1, "Steps", d.Steps)
	row(-1, "Stress", d.Stress)
	row(-1, "Active cal", d.Calories)
}

// weekCardTag names what a card's inverted header or heavy border marks,
// shortened to fit within width pixels: "" when it marks neither.
func weekCardTag(width int, best, worst bool) string {
	var names []string
	switch {
	case best:
		names = []string{"BEST NIGHT", "BEST"}
	case worst:
		names = []string{"TOUGHEST NIGHT", "TOUGHEST", "WORST"}
	}
	for _, n := range names {
		if textWidth(face(true, 10), n) <= width {
			return n
		}
	}
	return ""
}
