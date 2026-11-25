package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"golang.org/x/image/font"
)

// BriefView renders the daily brief as a console, in the manner of the
// vital-signs panel and the WolfeSense screens: framed banks under black
// header tabs, round instruments, and a status box with an inverted header.
//
// Readiness gets the left bank: the score large with its verdict, then the
// score's parts as a small WolfeSense · Readiness radar against their
// average, and a status box naming the weakest. Until the day's parts are
// stored, a round gauge filled to the score stands in, with the month's
// range as an arc outside it and a status box on what held sleep back.
// Last night gets the middle bank: a WolfePhase moon, time asleep beside the
// night's sleep score, and the stages and heart figures beneath. On the
// right, today's three rings with their scores, and under them yesterday's
// finished figures, each on a pill of its last month's range. Each bank
// keys its own marks, and each lays out from the bottom up, so a taller
// panel gets larger dials rather than empty space.
func BriefView(d Daily, width, height int) (*image.Gray, error) {
	if width < 600 || height < 340 {
		return nil, fmt.Errorf("chart: daily brief needs at least 600x340, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	k := math.Min(float64(width)/800, float64(height)/432)
	px := func(v float64) int { return int(math.Round(v * k)) }

	const pad = 12
	tf := face(true, math.Round(18*k))
	tx := drawTracked(img, tf, pad+4, px(26), "DAILY BRIEF", 2.5, ink)
	sub := face(false, math.Max(10, math.Round(12*k)))
	drawText(img, sub, tx+px(10), px(26), "· "+d.Date.Format("Monday, January 2"), muted, alignLeft)
	if d.HasRings {
		drawBriefKey(img, width-pad-4, px(26), k)
	}

	gap := px(8)
	top, bottom := px(38), height-pad
	colW := (width - 2*pad - 2*gap) / 3
	left := image.Rect(pad, top, pad+colW, bottom)
	mid := image.Rect(left.Max.X+gap, top, left.Max.X+gap+colW, bottom)
	right := image.Rect(mid.Max.X+gap, top, width-pad, bottom)

	// each bank a zone a tap can open: readiness, last night, today
	addZone("readiness", left)
	addZone("night", mid)
	if d.HasParts {
		briefReadinessRadar(img, drawBank(img, left, "READINESS", k), d, k)
	} else {
		briefReadiness(img, drawBank(img, left, "READINESS", k), d, k)
	}
	briefNight(img, drawBank(img, mid, "LAST NIGHT", k), d, k)
	split := right.Min.Y + px(136)
	if !d.HasRings {
		split = right.Min.Y
	} else {
		today := image.Rect(right.Min.X, right.Min.Y, right.Max.X, split)
		addZone("today", today)
		briefRings(img, drawBank(img, today, "TODAY", k), d, k)
		split += gap
	}
	briefYesterday(img, drawBank(img, image.Rect(right.Min.X, split, right.Max.X, right.Max.Y), "YESTERDAY", k), d, k)
	return img, nil
}

// drawBriefKey draws the rings' key right-aligned at x on baseline y, in the
// vital-signs legend's tracked capitals.
func drawBriefKey(img *image.Gray, x, y int, k float64) {
	f := face(true, math.Max(9, math.Round(10*k)))
	sw := int(18 * k)
	for i := len(scoreRings) - 1; i >= 0; i-- {
		label := strings.ToUpper(scoreRings[i].label)
		x -= trackedWidth(f, label, 1)
		drawTracked(img, f, x, y, label, 1, ink)
		x -= sw + int(5*k)
		ringSwatch(img, image.Rect(x, y-int(8*k), x+sw, y-int(2*k)), scoreRings[i].style)
		x -= int(14 * k)
	}
}

// briefReadiness draws the readiness bank from the bottom up: the status
// box, the key to the gauge's marks, and the verdict, with the gauge
// filling the height left above them.
func briefReadiness(img *image.Gray, r image.Rectangle, d Daily, k float64) {
	cx := float64(r.Min.X+r.Max.X) / 2
	h := d.Hero
	small := face(false, math.Max(9, math.Round(10*k)))

	// the status box: what's worth knowing about last night's score
	var lines []string
	if s := d.Metrics[0]; s.Value != "—" {
		l := "SLEEP " + s.Value
		if s.Delta != "" {
			l += " · " + strings.ToUpper(s.Delta)
		}
		lines = append(lines, l)
	}
	if hb, ok := strings.CutPrefix(d.HeldBack, "Held back by: "); ok {
		lines = append(lines, "HELD BACK BY")
		for _, part := range strings.Split(hb, " · ") {
			lines = append(lines, strings.ToUpper(part))
		}
	} else if d.HeldBack != "" {
		lines = append(lines, strings.ToUpper(d.HeldBack))
	}
	boxH := int(float64(16+12+14*max(2, len(lines))) * k)
	box := image.Rect(r.Min.X+int(4*k), r.Max.Y-boxH, r.Max.X-int(4*k), r.Max.Y)
	statusBox(img, box, "STATUS", strings.ToUpper(d.Date.Format("Jan 2")), lines, k)

	// the key: each of the gauge's marks as drawn, two to a row
	keyTop := box.Min.Y - int(40*k)
	if h.Range.Has {
		drawGaugeKey(img, image.Rect(r.Min.X+int(4*k), keyTop, r.Max.X-int(4*k), box.Min.Y-int(6*k)), k)
	}

	// the verdict, over the key
	lf := face(true, math.Max(10, math.Round(12*k)))
	vy := keyTop - int(24*k)
	verdict := strings.ToUpper(d.Verdict)
	if verdict == "" {
		verdict = "NO SIGNAL"
	}
	drawTracked(img, lf, int(cx)-trackedWidth(lf, verdict, 1.5)/2, vy, verdict, 1.5, ink)
	if d.VerdictDetail != "" {
		drawText(img, small, int(cx), vy+int(14*k), d.VerdictDetail, muted, alignCenter)
	}

	// the gauge, as large as the space above the verdict allows, leaving
	// room outside it for the range arc and the pointer
	space := float64(vy-r.Min.Y) - 22*k
	R := math.Min(float64(r.Dx())/2-24*k, space/2-20*k)
	cy := float64(r.Min.Y) + space/2 + 2*k
	if R < 30*k {
		return
	}
	drawReadinessGauge(img, cx, cy, R, h, k)
}

// briefReadinessRadar draws the readiness bank around the readiness radar:
// the score and verdict across the top; the score's parts as a radar
// against their 30-day average, as on WolfeSense · Readiness; its key; and
// the status box naming the weakest parts.
func briefReadinessRadar(img *image.Gray, r image.Rectangle, d Daily, k float64) {
	small := face(false, math.Max(9, math.Round(10*k)))

	// the score large at the left, the verdict beside it
	nf := face(true, math.Round(40*k))
	x0 := r.Min.X + int(6*k)
	top := r.Min.Y + int(36*k)
	drawText(img, nf, x0, top, d.Hero.Value, ink, alignLeft)
	vx := x0 + textWidth(nf, d.Hero.Value) + int(12*k)
	lf := face(true, math.Max(9, math.Round(11*k)))
	verdict := strings.ToUpper(d.Verdict)
	if verdict == "" {
		verdict = "NO SIGNAL"
	}
	drawTracked(img, lf, vx, top-int(16*k), verdict, 1, ink)
	if d.VerdictDetail != "" {
		drawText(img, small, vx, top-int(2*k), d.VerdictDetail, muted, alignLeft)
	}

	// the status box: the parts that pulled the score down
	type part struct {
		name string
		v    float64
	}
	var weak []part
	for _, p := range d.Parts.Parts {
		if p.Has {
			weak = append(weak, part{strings.ToUpper(or(p.Short, p.Label)), p.Value})
		}
	}
	for i := 1; i < len(weak); i++ { // weakest first
		for j := i; j > 0 && weak[j].v < weak[j-1].v; j-- {
			weak[j], weak[j-1] = weak[j-1], weak[j]
		}
	}
	var lines []string
	low := 0
	for _, w := range weak {
		if w.v < radarLow {
			low++
		}
	}
	switch {
	case low > 0:
		lines = append(lines, "PAY ATTENTION")
		for _, w := range weak[:low] {
			lines = append(lines, fmt.Sprintf("%s %.0f", w.name, w.v))
		}
	case len(weak) > 0:
		lines = append(lines, "EVERY PART 70 OR BETTER", fmt.Sprintf("LOWEST: %s %.0f", weak[0].name, weak[0].v))
	}
	if len(lines) > 4 {
		lines = lines[:4]
	}
	boxH := int(float64(16+12+14*max(2, len(lines))) * k)
	box := image.Rect(r.Min.X+int(4*k), r.Max.Y-boxH, r.Max.X-int(4*k), r.Max.Y)
	statusBox(img, box, "STATUS", strings.ToUpper(d.Date.Format("Jan 2")), lines, k)

	// the key, as the radar draws each mark
	keyTop := box.Min.Y - int(40*k)
	drawRadarKey(img, image.Rect(r.Min.X+int(4*k), keyTop, r.Max.X-int(4*k), box.Min.Y-int(6*k)), k)

	// the radar in the space between, its labels outside the rim
	st := radarStyle{line: 0.7, label: math.Max(8, math.Round(9*k)), value: math.Max(9, math.Round(11*k)), gap: 7 * k, short: true}
	area := image.Rect(r.Min.X, top+int(10*k), r.Max.X, keyTop-int(4*k))
	R := math.Min(float64(area.Dx())/2-58*k, float64(area.Dy())/2-24*k)
	if R < 30*k {
		return
	}
	drawRadarFigure(img, area, float64(area.Min.X+area.Max.X)/2, float64(area.Min.Y+area.Max.Y)/2, R, d.Parts, st)
}

// drawRadarKey draws the key to the brief's readiness radar in r, two
// entries to a row, each mark as the radar draws it.
func drawRadarKey(img *image.Gray, r image.Rectangle, k float64) {
	sw := int(18 * k)
	entries := []keyEntry{
		{"today", func(x, y int) {
			my := float64(y) - 4*k
			thickLine(img, float64(x), my, float64(x+sw), my, 2.2*k, ink)
			disc(img, float64(x+sw/2), my, 2.6*k, ink)
		}},
		{"30-day average", func(x, y int) {
			my := float64(y) - 4*k
			dashedLine(img, float64(x), my, float64(x+sw), my, 1.8*k, ink)
		}},
		{"optimal, 85+", func(x, y int) {
			for yy := y - int(8*k); yy < y; yy++ {
				for xx := x; xx < x+sw; xx++ {
					if bandDot(xx, yy) {
						img.SetGray(xx, yy, ink)
					}
				}
			}
		}},
		{"under 70", func(x, y int) {
			my := float64(y) - 4*k
			dottedLine(img, float64(x), my, float64(x+sw), my)
		}},
	}
	drawKeyGrid(img, r, k, entries)
}

// drawReadinessGauge draws readiness as a round gauge at cx, cy: a faint
// track filled solid to the score from 12 o'clock, a tick in it and a
// pointer outside at optimal, the month's range as a thin arc outside with
// its ends ticked and a heavier tick at typical, and the score in the
// center.
func drawReadinessGauge(img *image.Gray, cx, cy, R float64, h Metric, k float64) {
	th := math.Round(R * 0.2)
	ring(img, cx, cy, R-th/2, grid)
	ring(img, cx, cy, R, rule)
	ring(img, cx, cy, R-th, rule)
	if h.Range.Has {
		rr := R + 7*k
		arcBand(img, cx, cy, rr-1, rr+1, clamp01(h.Range.Low/100), clamp01(h.Range.High/100), ink)
		for _, v := range []float64{h.Range.Low, h.Range.High} {
			radialTick(img, cx, cy, rr-3*k, rr+3*k, clamp01(v/100), 1.2, ink)
		}
		radialTick(img, cx, cy, rr-4*k, rr+5*k, clamp01(h.Range.Typical/100), 2.4, ink)
		arcBand(img, cx, cy, R-th, R, 0, clamp01(h.Range.Now/100), ink)
	}
	targetTick(img, cx, cy, R-th, R, optimalScore/100.0)
	ang := optimalScore / 100.0 * 2 * math.Pi
	ux, uy := math.Sin(ang), -math.Cos(ang)
	pxv, pyv := -uy, ux
	tip, base := R+11*k, R+19*k
	fillPolygon(img, [][2]float64{
		{cx + ux*tip, cy + uy*tip},
		{cx + ux*base + pxv*4*k, cy + uy*base + pyv*4*k},
		{cx + ux*base - pxv*4*k, cy + uy*base - pyv*4*k},
	}, ink)

	num := math.Round(math.Min(54*k, R*0.6))
	drawText(img, face(true, num), int(cx), int(cy+num*0.35), h.Value, ink, alignCenter)
	f := face(true, math.Max(8, math.Round(9*k)))
	drawTracked(img, f, int(cx)-trackedWidth(f, "READINESS", 1.5)/2, int(cy+num*0.35+16*k), "READINESS", 1.5, muted)
}

// drawGaugeKey draws the key to the readiness gauge in r, two entries to a
// row, each mark drawn as on the gauge.
func drawGaugeKey(img *image.Gray, r image.Rectangle, k float64) {
	sw := int(18 * k)
	entries := []keyEntry{
		{"today's score", func(x, y int) { fillRect(img, image.Rect(x, y-int(7*k), x+sw, y-int(1*k)), ink) }},
		{"85: optimal", func(x, y int) {
			mx, ty := float64(x+sw/2), float64(y)-1
			fillPolygon(img, [][2]float64{{mx, ty}, {mx - 4*k, ty - 8*k}, {mx + 4*k, ty - 8*k}}, ink)
		}},
		{"last 30 days", func(x, y int) {
			my := y - int(4*k)
			fillRect(img, image.Rect(x, my-1, x+sw, my+1), ink)
			fillRect(img, image.Rect(x, my-int(3*k), x+1, my+int(3*k)), ink)
			fillRect(img, image.Rect(x+sw-1, my-int(3*k), x+sw, my+int(3*k)), ink)
		}},
		{"typical day", func(x, y int) {
			my := y - int(4*k)
			hline(img, x, x+sw-1, my, rule)
			fillRect(img, image.Rect(x+sw/2-1, my-int(5*k), x+sw/2+2, my+int(5*k)), ink)
		}},
	}
	drawKeyGrid(img, r, k, entries)
}

// statusBox draws the vital-signs status box in r: a rounded frame with an
// inverted header carrying title and, at the right, note; then lines,
// centered, as many as fit.
func statusBox(img *image.Gray, r image.Rectangle, title, note string, lines []string, k float64) {
	if r.Dy() < int(36*k) {
		return
	}
	roundRect(img, r, 6*k, 1, false)
	head := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+int(16*k))
	roundRect(img, head, 6*k, 0, true)
	fillRect(img, image.Rect(head.Min.X, head.Max.Y-int(6*k), head.Max.X, head.Max.Y), ink)
	hf := face(true, math.Max(9, math.Round(10*k)))
	drawTracked(img, hf, head.Min.X+int(8*k), head.Max.Y-int(4*k), title, 2, surface)
	if note != "" {
		drawTracked(img, hf, head.Max.X-int(8*k)-trackedWidth(hf, note, 1), head.Max.Y-int(4*k), note, 1, surface)
	}
	f := face(false, math.Max(9, math.Round(10*k)))
	cx := (r.Min.X + r.Max.X) / 2
	y := head.Max.Y + int(15*k)
	for _, l := range lines {
		if y > r.Max.Y-int(4*k) {
			return
		}
		drawTracked(img, f, cx-trackedWidth(f, l, 0.5)/2, y, l, 0.5, ink)
		y += int(14 * k)
	}
}

// briefNight draws the last-night bank from the bottom up: a grid of the
// stages and the night's heart figures, then time asleep and the times,
// with the WolfePhase moon filling the height left above them.
func briefNight(img *image.Gray, r image.Rectangle, d Daily, k float64) {
	cx := float64(r.Min.X+r.Max.X) / 2
	n := d.Phase
	if !d.HasPhase || !n.End.After(n.Start) {
		f := face(true, math.Max(10, math.Round(12*k)))
		drawTracked(img, f, int(cx)-trackedWidth(f, "NO SLEEP RECORDED", 1.5)/2, (r.Min.Y+r.Max.Y)/2, "NO SLEEP RECORDED", 1.5, muted)
		return
	}
	var tot = map[Stage]time.Duration{}
	for _, s := range n.Stages {
		tot[s] += n.StageEpoch
	}
	asleep := tot[StageDeep] + tot[StageLight] + tot[StageREM]
	small := face(false, math.Max(9, math.Round(10*k)))
	vf := face(true, math.Max(10, math.Round(12*k)))

	// the grid, two to a row: the stages on chips of the moon's grays,
	// then the lowest heart rate and average HRV
	type cell struct {
		chip       Stage
		hasChip    bool
		label, val string
	}
	cells := []cell{
		{StageDeep, true, "Deep", Duration(tot[StageDeep].Minutes())},
		{StageREM, true, "REM", Duration(tot[StageREM].Minutes())},
		{StageLight, true, "Light", Duration(tot[StageLight].Minutes())},
		{StageAwake, true, "Awake", Duration(tot[StageAwake].Minutes())},
	}
	if lo, ok := seriesMin(n.HeartRate); ok {
		cells = append(cells, cell{label: "Lowest HR", val: fmt.Sprintf("%.0f bpm", lo)})
	}
	if avg, ok := seriesMean(n.HRV); ok {
		cells = append(cells, cell{label: "HRV, avg", val: fmt.Sprintf("%.0f ms", avg)})
	}
	rowH := int(20 * k)
	rows := (len(cells) + 1) / 2
	x0, x1 := r.Min.X+int(6*k), r.Max.X-int(6*k)
	half := (x1 - x0) / 2
	gridTop := r.Max.Y - rows*rowH - int(2*k)
	hline(img, x0, x1-1, gridTop-int(4*k), grid)
	for i, c := range cells {
		x := x0 + (i%2)*half
		y := gridTop + (i/2)*rowH + int(14*k)
		lx := x
		if c.hasChip {
			stageChip(img, x, y-int(9*k), int(18*k), int(10*k), c.chip)
			lx += int(23 * k)
		}
		drawText(img, small, lx, y, c.label, muted, alignLeft)
		drawText(img, vf, x+half-int(8*k), y, c.val, ink, alignRight)
	}

	// over the grid, one line: time asleep and the times at the left, the
	// night's sleep score large at the right, named, with its change
	loc := n.Start.Location()
	lf := face(true, math.Max(10, math.Round(12*k)))
	hy := gridTop - int(28*k)
	head := strings.ToUpper(Duration(asleep.Minutes()) + " asleep")
	drawTracked(img, lf, x0, hy, head, 1.5, ink)
	drawText(img, small, x0, hy+int(14*k), n.Start.In(loc).Format("3:04 PM")+" → "+n.End.In(loc).Format("3:04 PM"), muted, alignLeft)
	if sc := d.Metrics[0]; sc.Value != "—" {
		nf := face(true, math.Round(30*k))
		drawText(img, nf, x1, hy+int(14*k), sc.Value, ink, alignRight)
		lx := x1 - textWidth(nf, sc.Value) - int(7*k)
		// named in full where it clears time asleep, else just "SCORE"
		cf := face(true, math.Max(8, math.Round(9*k)))
		free := lx - (x0 + trackedWidth(lf, head, 1.5) + int(8*k))
		cap := "SLEEP SCORE"
		if trackedWidth(cf, cap, 1) > free {
			cap = "SCORE"
		}
		drawTracked(img, cf, lx-trackedWidth(cf, cap, 1), hy, cap, 1, muted)
		if sc.Delta != "" {
			dl := strings.Replace(sc.Delta, " vs 7-day avg", " vs 7d", 1)
			drawText(img, small, lx, hy+int(14*k), dl, muted, alignRight)
		}
	}

	// the moon, as large as the rest allows, with room for its glow
	space := float64(hy-r.Min.Y) - 18*k
	R := math.Min(float64(r.Dx())/2-16*k, space/2-10*k)
	if R < 24*k {
		return
	}
	drawPhaseDial(img, cx, float64(r.Min.Y)+space/2+2*k, R, n, phaseDial{stages: true, ticks: true, hub: true, mark: true, glow: true})
}

// seriesMin is the lowest reading in s.
func seriesMin(s Series) (float64, bool) {
	lo, ok := math.Inf(1), false
	for _, v := range s.Values {
		if v != nil {
			lo, ok = math.Min(lo, *v), true
		}
	}
	return lo, ok
}

// seriesMean is the average reading in s.
func seriesMean(s Series) (float64, bool) {
	var sum float64
	var n int
	for _, v := range s.Values {
		if v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// stageChip draws a stage's swatch, w by h at x, y, on a chip of the moon's
// lit gray so white light sleep shows.
func stageChip(img *image.Gray, x, y, w, h int, st Stage) {
	fillRect(img, image.Rect(x, y, x+w, y+h), windowShade)
	for yy := y + 2; yy < y+h-2; yy++ {
		for xx := x + 2; xx < x+w-2; xx++ {
			if c, ok := moonShade(st, float64(yy-y-2)/float64(max(1, h-4))); ok {
				img.SetGray(xx, yy, c)
			}
		}
	}
}

// briefRings draws today's three rings with their scores beside them.
func briefRings(img *image.Gray, r image.Rectangle, d Daily, k float64) {
	R := math.Min(float64(r.Dy())/2-6*k, 50*k)
	cx, cy := float64(r.Min.X)+R+10*k, float64(r.Min.Y+r.Max.Y)/2
	drawScoreRings(img, cx, cy, R, d.Rings)

	x0, x1 := int(cx+R)+int(18*k), r.Max.X-int(4*k)
	y := r.Min.Y + int(18*k)
	small := face(false, math.Max(9, math.Round(10*k)))
	vf := face(true, math.Max(11, math.Round(14*k)))
	vals := []struct {
		v   float64
		has bool
	}{{d.Rings.Sleep, d.Rings.HasSleep}, {d.Rings.Readiness, d.Rings.HasReadiness}, {d.Rings.Activity, d.Rings.HasActivity}}
	for i, v := range vals {
		ringSwatch(img, image.Rect(x0, y-int(8*k), x0+int(14*k), y-int(2*k)), scoreRings[i].style)
		drawText(img, small, x0+int(19*k), y, scoreRings[i].label, muted, alignLeft)
		s := "—"
		if v.has {
			s = fmt.Sprintf("%.0f", v.v)
			if i == 2 && d.Rings.ActivityGoal {
				s += "%"
			}
		}
		drawText(img, vf, x1, y, s, ink, alignRight)
		y += int(21 * k)
	}
	// the activity goal's progress under its row, as much as fits
	for _, note := range []string{strings.Join(nonEmpty(d.CalNote, d.GoalNote), " · "), d.CalNote, d.GoalNote} {
		if note != "" && textWidth(small, note) <= x1-x0 {
			drawText(img, small, x1, y-int(6*k), note, muted, alignRight)
			break
		}
	}
	if d.TotalCal != "" {
		drawText(img, small, x0, r.Max.Y-int(2*k), "Total burn", muted, alignLeft)
		drawText(img, vf, x1, r.Max.Y-int(2*k), d.TotalCal, ink, alignRight)
	}
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// briefYesterday draws the finished figures, each a label and value over a
// pill strip of its month's range, and at the foot the key to the strips.
func briefYesterday(img *image.Gray, r image.Rectangle, d Daily, k float64) {
	ms := d.Metrics[1:]
	for _, m := range ms {
		if m.Range.Has {
			keyR := image.Rect(r.Min.X+int(4*k), r.Max.Y-int(34*k), r.Max.X-int(4*k), r.Max.Y)
			drawPillKey(img, keyR, k)
			r.Max.Y = keyR.Min.Y - int(4*k)
			break
		}
	}
	rowH := float64(r.Dy()) / float64(len(ms))
	lf := face(true, math.Max(9, math.Round(10*k)))
	vf := face(true, math.Max(12, math.Round(17*k)))
	small := face(false, math.Max(8, math.Round(9*k)))
	for i, m := range ms {
		y0 := r.Min.Y + int(float64(i)*rowH)
		x0, x1 := r.Min.X+int(4*k), r.Max.X-int(4*k)
		label := strings.ToUpper(m.Label)
		if m.Note != "" && m.Note != "yesterday" {
			label += " · " + strings.ToUpper(m.Note)
		}
		drawTracked(img, lf, x0, y0+int(12*k), label, 1, ink)
		drawText(img, vf, x1, y0+int(15*k), m.Value, ink, alignRight)
		if m.Delta != "" {
			drawText(img, small, x0, y0+int(26*k), m.Delta, muted, alignLeft)
		}
		// the pill low in its row, clear of the change above it, its
		// pointer included; the typical tick hangs under it
		py := y0 + max(int(39*k), min(int(rowH)-int(15*k), int(44*k)))
		pill := image.Rect(x0+int(30*k), py, x1-int(30*k), py+int(8*k))
		if m.Range.Has && int(float64(pill.Max.Y)+5*k) <= r.Max.Y {
			rangePill(img, pill, m.Range, small, k)
		}
	}
}

// drawPillKey draws the key to the range pills in r, two entries to a row,
// each mark drawn as on a pill, as the readiness gauge's key is.
func drawPillKey(img *image.Gray, r image.Rectangle, k float64) {
	sw := int(18 * k)
	entries := []keyEntry{
		{"yesterday", func(x, y int) {
			p := image.Rect(x, y-int(6*k), x+sw, y)
			roundRect(img, p, float64(p.Dy())/2, 1, false)
			mx := float64(x + sw*2/3)
			hatchPill(img, p, int(mx))
			ty := float64(p.Min.Y) - 1
			fillPolygon(img, [][2]float64{{mx, ty}, {mx - 4*k, ty - 6*k}, {mx + 4*k, ty - 6*k}}, ink)
		}},
		{"typical day", func(x, y int) {
			p := image.Rect(x, y-int(8*k), x+sw, y-int(3*k))
			roundRect(img, p, float64(p.Dy())/2, 1, false)
			fillRect(img, image.Rect(x+sw/2-1, p.Max.Y+1, x+sw/2+2, p.Max.Y+int(4*k)), ink)
		}},
		{"last 30 days, low to high", func(x, y int) { // last: the long label gets a row to itself
			p := image.Rect(x, y-int(6*k), x+sw, y)
			roundRect(img, p, float64(p.Dy())/2, 1, false)
		}},
	}
	drawKeyGrid(img, r, k, entries)
}

// rangePill draws a month's range as a hollow pill with its ends labeled,
// a heavy tick under it at typical, and a pointer above at the value.
func rangePill(img *image.Gray, p image.Rectangle, rg Range, f font.Face, k float64) {
	roundRect(img, p, float64(p.Dy())/2, 1, false)
	span := rg.High - rg.Low
	xOf := func(v float64) float64 {
		if span <= 0 {
			return float64(p.Min.X+p.Max.X) / 2
		}
		return float64(p.Min.X) + clamp01((v-rg.Low)/span)*float64(p.Dx())
	}
	cy := (p.Min.Y + p.Max.Y) / 2
	drawText(img, f, p.Min.X-int(5*k), cy+int(3*k), rg.LowLabel, muted, alignRight)
	drawText(img, f, p.Max.X+int(5*k), cy+int(3*k), rg.HighLabel, muted, alignLeft)
	tx := int(math.Round(xOf(rg.Typical)))
	fillRect(img, image.Rect(tx-1, p.Max.Y+1, tx+2, p.Max.Y+int(5*k)), ink)
	// the value: solid in the pill up to it, and a pointer above
	vx := xOf(rg.Now)
	hatchPill(img, p, int(vx))
	fillPolygon(img, [][2]float64{{vx, float64(p.Min.Y) - 1}, {vx - 5*k, float64(p.Min.Y) - 8*k}, {vx + 5*k, float64(p.Min.Y) - 8*k}}, ink)
}

// radialTick draws a short radial line at turn f from radius r0 to r1.
func radialTick(img *image.Gray, cx, cy, r0, r1, f, w float64, c color.Gray) {
	a := f * 2 * math.Pi
	thickLine(img, cx+r0*math.Sin(a), cy-r0*math.Cos(a), cx+r1*math.Sin(a), cy-r1*math.Cos(a), w, c)
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }
