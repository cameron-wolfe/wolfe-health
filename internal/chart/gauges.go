package chart

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"golang.org/x/image/font"
)

// Gauge is one instrument on the vital-signs panel: a reading on a scale
// with zones, formatted by the caller. From the middle out: the normal
// zone [Lo, Hi], caution out to [CautionLo, CautionHi], and alert beyond,
// to the scale's ends [Min, Max].
type Gauge struct {
	Label, Unit          string // "HRV", "ms"
	Value                float64
	Has                  bool
	Lo, Hi               float64 // the normal zone
	CautionLo, CautionHi float64
	Min, Max             float64   // the scale
	HasBand              bool      // the zones are known; without them, a bare scale
	Decimal              int       // decimals in the readout
	Signed               bool      // a deviation: "+0.1"
	Round                bool      // a round readout in the center column, not a bar
	Better               int       // +1 higher is better, -1 lower is better, 0 neither: off-range the good way is not an alert
	Recent               []float64 // the last seven nights, oldest first, NaN where missing: the trace
	Short                string    // a code for the status lamps: "TMP"
}

// status is where the reading sits: "", "LOW" or "HIGH".
func (g Gauge) status() string {
	switch {
	case !g.Has || !g.HasBand:
		return ""
	case g.Value < g.Lo:
		return "LOW"
	case g.Value > g.Hi:
		return "HIGH"
	}
	return ""
}

// Alert reports a reading outside its normal range in a way that
// matters. A neutral signal alerts as soon as it leaves normal, either
// way. One with a better direction alerts at once the bad way; the good
// way, moderately beyond normal (the caution zone) is a good sign, not an
// alarm, and only far beyond it (the alert zone) is flagged: a spike in
// HRV can be an artifact or an irregular rhythm rather than fitness.
func (g Gauge) Alert() bool {
	st := g.status()
	if st == "" {
		return false
	}
	good := (st == "HIGH" && g.Better > 0) || (st == "LOW" && g.Better < 0)
	return !good || g.zone(g.Value) == 2
}

// alertText is how an alert reads: "LOW", "HIGH", or for a reading far
// beyond normal the good way, "VERY HIGH" or "VERY LOW".
func (g Gauge) alertText() string {
	st := g.status()
	if (st == "HIGH" && g.Better > 0) || (st == "LOW" && g.Better < 0) {
		return "VERY " + st
	}
	return st
}

// zone is the zone a value falls in: 0 normal, 1 caution, 2 alert.
func (g Gauge) zone(v float64) int {
	switch {
	case !g.HasBand:
		return 0
	case v >= g.Lo && v <= g.Hi:
		return 0
	case v >= g.CautionLo && v <= g.CautionHi:
		return 1
	}
	return 2
}

func (g Gauge) format(v float64) string {
	if math.Abs(v) < 0.5*math.Pow(10, -float64(g.Decimal)) {
		v = 0 // no "−0"
	}
	s := fmt.Sprintf("%.*f", g.Decimal, v)
	if g.Signed && v > 0 {
		s = "+" + s
	}
	return strings.Replace(s, "-", "−", 1)
}

// tick formats a ladder number with only the decimals its step needs:
// "96", not "96.0", on a scale stepping by whole numbers.
func (g Gauge) tick(v, step float64) string {
	t := g
	t.Decimal = 0
	if step < 1 {
		t.Decimal = int(math.Ceil(-math.Log10(step) - 1e-9))
	}
	return t.format(v)
}

// Instruments is the content of the vital-signs panel.
type Instruments struct {
	Title, Subtitle string
	Night           time.Time // the night shown, by its wake-up day
	Gauges          []Gauge   // bars in order, left then right; round ones go to the center
	Notes           []string  // standouts for the status box on a night with nothing out of range
}

// InstrumentsView renders the vital-signs panel as a console in the manner
// of a sick bay's bio-bed display. Three framed banks, each under a black
// header tab: body signals on the left and sleep on the right as tall
// pill-shaped bars, and the vitals in the center. Each bar has a numbered
// tick ladder and its zones — normal clear, caution hatched, alert solid —
// with a heavy line beside it marking the normal range; a triangle points
// at the reading with the value inside, white when normal and solid when
// out of range; under the label, a trace of the last seven nights. The
// center bank holds the round readouts and a status box. A strip of status
// lamps runs along the bottom, one per channel, solid when out of range.
func InstrumentsView(p Instruments, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: vital signs need at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	k := math.Min(float64(width)/800, float64(height)/480)

	const pad = 12
	// the title and the night, then the legend across the right
	tf := face(true, math.Round(18*k))
	tx := drawTracked(img, tf, pad+4, int(26*k), strings.ToUpper(p.Title), 2.5, ink)
	sub := face(false, math.Max(10, math.Round(12*k)))
	drawText(img, sub, tx+int(10*k), int(26*k), "· "+p.Subtitle, muted, alignLeft)
	drawLegend(img, width-pad-4, int(26*k), tx+int(18*k)+textWidth(sub, "· "+p.Subtitle), k)

	var bars, rounds []Gauge
	for _, g := range p.Gauges {
		if g.Round {
			rounds = append(rounds, g)
		} else {
			bars = append(bars, g)
		}
	}
	gap := int(8 * k)
	lampH := int(28 * k)
	top := int(38 * k)
	bottom := height - pad - lampH - gap
	centerW := int(math.Round(176 * k))
	sideW := (width - 2*pad - centerW - 2*gap) / 2
	left := (len(bars) + 1) / 2
	banks := []struct {
		title string
		r     image.Rectangle
		gs    []Gauge
	}{
		{"BIO-SIGNS", image.Rect(pad, top, pad+sideW, bottom), bars[:left]},
		{"SLEEP SYSTEMS", image.Rect(width-pad-sideW, top, width-pad, bottom), bars[left:]},
	}
	for _, bk := range banks {
		inner := drawBank(img, bk.r, bk.title, k)
		w := float64(inner.Dx()) / float64(max(1, len(bk.gs)))
		for i, g := range bk.gs {
			x0 := float64(inner.Min.X) + float64(i)*w
			drawBar(img, image.Rect(int(x0), inner.Min.Y, int(x0+w), inner.Max.Y), g, k)
		}
	}
	center := image.Rect(pad+sideW+gap, top, pad+sideW+gap+centerW, bottom)
	drawCenter(img, drawBank(img, center, "VITALS", k), rounds, p, k)
	drawLamps(img, image.Rect(pad, height-pad-lampH, width-pad, height-pad), p.Gauges, k)
	return img, nil
}

// drawLegend draws the key right-aligned at x on baseline y, each mark as
// it appears on the bars: the three zones as short pill segments, the
// normal-range line, and the pointer. It leaves out entries, right to left
// in priority, that won't fit after minX.
func drawLegend(img *image.Gray, x, y, minX int, k float64) {
	f := face(true, math.Max(9, math.Round(10*k)))
	type entry struct {
		label string
		w     int // the mark's width
		mark  func(x0 int)
	}
	sw, sh := int(18*k), int(10*k)
	seg := func(zone int) func(int) {
		return func(x0 int) {
			r := image.Rect(x0, y-sh, x0+sw, y)
			mask := image.NewGray(r)
			fillRect(mask, r, surface)
			roundRect(mask, r, float64(sh)/2, 0, true)
			for yy := r.Min.Y; yy < r.Max.Y; yy++ {
				for xx := r.Min.X; xx < r.Max.X; xx++ {
					if mask.GrayAt(xx, yy).Y == ink.Y && (zone == 2 || (zone == 1 && (xx+yy)%3 == 0)) {
						img.SetGray(xx, yy, ink)
					}
				}
			}
			roundRect(img, r, float64(sh)/2, 1, false)
		}
	}
	entries := []entry{
		{"NORMAL", sw, seg(0)},
		{"CAUTION", sw, seg(1)},
		{"ALERT", sw, seg(2)},
		{"YOUR RANGE", int(4 * k), func(x0 int) { fillRect(img, image.Rect(x0, y-sh-2, x0+int(3*k), y+1), ink) }},
		{"READING", int(16 * k), func(x0 int) {
			fy := float64(y - sh/2)
			fillPolygon(img, [][2]float64{{float64(x0), fy}, {float64(x0) + 16*k, fy - 6*k}, {float64(x0) + 16*k, fy + 6*k}}, ink)
			fillPolygon(img, [][2]float64{{float64(x0) + 4*k, fy}, {float64(x0) + 14.5*k, fy - 4*k}, {float64(x0) + 14.5*k, fy + 4*k}}, surface)
		}},
	}
	width := func(es []entry) int {
		w := 0
		for i, e := range es {
			if i > 0 {
				w += int(12 * k)
			}
			w += e.w + int(5*k) + trackedWidth(f, e.label, 1)
		}
		return w
	}
	for len(entries) > 0 && x-width(entries) < minX {
		entries = entries[:len(entries)-1]
	}
	cx := x - width(entries)
	for _, e := range entries {
		e.mark(cx)
		cx += e.w + int(5*k)
		cx = drawTracked(img, f, cx, y, e.label, 1, ink) + int(12*k)
	}
}

// drawBank frames a bank of instruments: a rounded outline with a black
// header tab carrying its name, and returns the space inside.
func drawBank(img *image.Gray, r image.Rectangle, title string, k float64) image.Rectangle {
	rad := 10 * k
	roundRect(img, r, rad, 2, false)
	f := face(true, math.Max(9, math.Round(11*k)))
	tabW := trackedWidth(f, title, 2) + int(22*k)
	tab := image.Rect(r.Min.X, r.Min.Y, r.Min.X+tabW, r.Min.Y+int(20*k))
	roundRect(img, tab, rad, 0, true)
	fillRect(img, image.Rect(tab.Min.X, tab.Max.Y-int(rad), tab.Min.X+int(rad), tab.Max.Y), ink) // square the tab's lower left into the frame
	drawTracked(img, f, tab.Min.X+int(11*k), tab.Max.Y-int(6*k), title, 2, surface)
	// a rule across from the tab, as on a console's panel header
	hline(img, tab.Max.X+int(6*k), r.Max.X-int(10*k), tab.Min.Y+tab.Dy()/2, ink)
	return image.Rect(r.Min.X+int(6*k), tab.Max.Y+int(8*k), r.Max.X-int(6*k), r.Max.Y-int(6*k))
}

// roundRect fills r with rounded corners of radius rad, or with t > 0 and
// solid false, draws its outline t pixels thick.
func roundRect(img *image.Gray, r image.Rectangle, rad float64, t int, solid bool) {
	in := func(x, y int, inset float64) bool {
		fx, fy := float64(x)+0.5, float64(y)+0.5
		x0, y0 := float64(r.Min.X)+inset, float64(r.Min.Y)+inset
		x1, y1 := float64(r.Max.X)-inset, float64(r.Max.Y)-inset
		rr := math.Max(0, rad-inset)
		if fx < x0 || fx > x1 || fy < y0 || fy > y1 {
			return false
		}
		cx := math.Max(x0+rr, math.Min(fx, x1-rr))
		cy := math.Max(y0+rr, math.Min(fy, y1-rr))
		return math.Hypot(fx-cx, fy-cy) <= rr
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if in(x, y, 0) && (solid || !in(x, y, float64(t))) {
				img.SetGray(x, y, ink)
			}
		}
	}
}

// drawBar draws one bar gauge in r: the bar, its pointer, then its label,
// unit and seven-night trace at the bottom.
func drawBar(img *image.Gray, r image.Rectangle, g Gauge, k float64) {
	footH := int(48 * k) // label, unit, trace
	numW := int(math.Round(28 * k))
	barW := int(math.Max(8, math.Round(12*k)))
	bx := r.Min.X + numW + int(6*k)
	bar := image.Rect(bx, r.Min.Y+int(6*k), bx+barW, r.Max.Y-footH-int(4*k))
	lo, hi := g.Min, g.Max
	if hi <= lo {
		lo, hi = 0, 1
	}
	yOf := func(v float64) float64 {
		t := (math.Max(lo, math.Min(hi, v)) - lo) / (hi - lo)
		return float64(bar.Max.Y) - t*float64(bar.Dy())
	}

	// the zones inside a pill: normal clear, caution hatched, alert solid
	rad := float64(barW) / 2
	pill := image.NewGray(bar)
	fillRect(pill, bar, surface)       // a new image is all black: clear it first
	roundRect(pill, bar, rad, 0, true) // then the pill's shape, as a mask
	for y := bar.Min.Y; y < bar.Max.Y; y++ {
		v := lo + (float64(bar.Max.Y)-float64(y)-0.5)/float64(bar.Dy())*(hi-lo)
		z := g.zone(v)
		for x := bar.Min.X; x < bar.Max.X; x++ {
			if pill.GrayAt(x, y).Y != ink.Y {
				continue
			}
			if z == 2 || (z == 1 && (x+y)%3 == 0) {
				img.SetGray(x, y, ink)
			}
		}
	}
	roundRect(img, bar, rad, 1, false)

	// a channel with nothing to show is offline: the empty bar, no ladder
	if offline := !g.Has && !g.HasBand; offline {
		of := face(true, math.Max(9, math.Round(10*k)))
		mid := (bar.Min.Y + bar.Max.Y) / 2
		for i, w := range []string{"NO", "SIGNAL"} {
			drawTracked(img, of, bar.Max.X+int(8*k), mid+i*int(13*k), w, 1.5, muted)
		}
	} else {
		// the tick ladder and its numbers, on a round step
		step := niceStep((hi - lo) / 6)
		f := face(false, math.Max(9, math.Round(10*k)))
		for v := math.Ceil(lo/step) * step; v <= hi+step/1000; v += step {
			y := int(math.Round(yOf(v)))
			hline(img, bar.Min.X-int(6*k), bar.Min.X-2, y, ink)
			drawText(img, f, bar.Min.X-int(8*k), y+4, g.tick(v, step), ink, alignRight)
			if mid := v + step/2; mid < hi {
				hline(img, bar.Min.X-int(3*k), bar.Min.X-2, int(math.Round(yOf(mid))), ink)
			}
		}

		// the normal range: a heavy line beside the bar
		lineX := bar.Max.X + int(math.Round(3*k))
		if g.HasBand {
			y0, y1 := int(math.Round(yOf(g.Hi))), int(math.Round(yOf(g.Lo)))
			fillRect(img, image.Rect(lineX, y0, lineX+int(math.Max(2, math.Round(3*k))), y1+1), ink)
		}

		// the pointer: a triangle aimed at the reading, its value inside;
		// hollow when normal, solid when out of range
		if g.Has {
			y := yOf(g.Value)
			tipX := float64(lineX) + 5*k
			w, h := math.Min(52*k, float64(r.Max.X)-tipX-2), 36*k
			alert := g.Alert()
			fillPolygon(img, [][2]float64{{tipX, y}, {tipX + w, y - h/2}, {tipX + w, y + h/2}}, ink)
			if !alert {
				in := 2.2 * k
				fillPolygon(img, [][2]float64{{tipX + in*2.6, y}, {tipX + w - in, y - h/2 + in*1.7}, {tipX + w - in, y + h/2 - in*1.7}}, surface)
			}
			c := ink
			if alert {
				c = surface
			}
			drawText(img, face(true, math.Max(10, math.Round(12*k))), int(tipX+w*0.64), int(y)+int(4*k), g.format(g.Value), c, alignCenter)
		}

	}

	// the foot: the label, then the unit with the trace of the last seven
	// nights beside it
	fx := r.Min.X + int(4*k)
	fy := r.Max.Y - footH
	lf := face(true, math.Max(10, math.Round(12*k)))
	drawTracked(img, lf, fx, fy+int(14*k), strings.ToUpper(g.Label), 1, ink)
	uf := face(false, math.Max(9, math.Round(10*k)))
	drawText(img, uf, fx, fy+int(30*k), g.Unit, muted, alignLeft)
	trace := image.Rect(fx+textWidth(uf, g.Unit)+int(8*k), fy+int(20*k), r.Max.X-int(4*k), r.Max.Y-int(2*k))
	drawTrace(img, trace, g.Recent, k)
}

// drawTrace draws the last nights as a small line in r, scaled to their own
// spread, broken where a night is missing, ending in a dot; with its
// baseline as a dotted rule, and "7 NIGHTS" under nothing.
func drawTrace(img *image.Gray, r image.Rectangle, vals []float64, k float64) {
	for x := r.Min.X; x < r.Max.X; x += 2 {
		img.SetGray(x, r.Max.Y-1, grid)
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		if !math.IsNaN(v) {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 0) || len(vals) < 2 {
		return
	}
	if hi-lo < 1e-9 {
		lo, hi = lo-1, hi+1
	}
	px := func(i int) float64 { return float64(r.Min.X) + 3 + float64(i)/float64(len(vals)-1)*float64(r.Dx()-6) }
	py := func(v float64) float64 { return float64(r.Max.Y) - 3 - (v-lo)/(hi-lo)*float64(r.Dy()-6) }
	last := -1
	for i, v := range vals {
		if math.IsNaN(v) {
			continue
		}
		if last >= 0 && i-last <= 2 { // across one missing night, too
			thickLine(img, px(last), py(vals[last]), px(i), py(v), 1.5, ink)
		}
		disc(img, px(i), py(v), 1.3, ink)
		last = i
	}
	if last >= 0 {
		disc(img, px(last), py(vals[last]), 2.8*k, ink)
	}
}

// drawCenter draws the center bank: each round readout as a ring with its
// value inside (a solid disc when out of range), then the status box.
func drawCenter(img *image.Gray, r image.Rectangle, rounds []Gauge, p Instruments, k float64) {
	cx := float64(r.Min.X+r.Max.X) / 2
	R := 27 * k
	y := float64(r.Min.Y) + R + 2
	small := face(false, math.Max(9, math.Round(10*k)))
	for _, g := range rounds {
		alert := g.Alert()
		if alert {
			disc(img, cx, y, R, ink)
		} else {
			arcBand(img, cx, y, R-3*k, R, 0, 1, ink)
			ring(img, cx, y, R-6*k, grid)
		}
		val := "—"
		if g.Has {
			val = g.format(g.Value)
		}
		c := ink
		if alert {
			c = surface
		}
		// the value, with its unit under it inside the ring
		drawText(img, face(true, math.Round(19*k)), int(cx), int(y+3*k), val, c, alignCenter)
		drawText(img, face(false, math.Max(8, math.Round(9*k))), int(cx), int(y+15*k), g.Unit, c, alignCenter)
		lf := face(true, math.Max(10, math.Round(11*k)))
		label := strings.ToUpper(g.Label)
		if g.Alert() {
			label += " " + g.alertText()
		}
		drawTracked(img, lf, int(cx)-trackedWidth(lf, label, 1)/2, int(y+R+14*k), label, 1, ink)
		if g.HasBand {
			drawText(img, small, int(cx), int(y+R+27*k), fmt.Sprintf("normal %s–%s", g.format(g.Lo), g.format(g.Hi)), muted, alignCenter)
		}
		y += 2*R + 44*k
	}

	// the status box: an inverted header, then the verdict
	box := image.Rect(r.Min.X+int(4*k), int(y-R+6*k), r.Max.X-int(4*k), r.Max.Y)
	if box.Dy() < int(40*k) {
		return
	}
	roundRect(img, box, 6*k, 1, false)
	head := image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+int(16*k))
	roundRect(img, head, 6*k, 0, true)
	fillRect(img, image.Rect(head.Min.X, head.Max.Y-int(6*k), head.Max.X, head.Max.Y), ink)
	hf := face(true, math.Max(9, math.Round(10*k)))
	drawTracked(img, hf, head.Min.X+int(8*k), head.Max.Y-int(4*k), "STATUS", 2, surface)
	if !p.Night.IsZero() {
		night := strings.ToUpper(p.Night.AddDate(0, 0, -1).Format("Jan 2"))
		drawTracked(img, hf, head.Max.X-int(8*k)-trackedWidth(hf, night, 1), head.Max.Y-int(4*k), night, 1, surface)
	}
	var out []string
	read := 0
	for _, g := range p.Gauges {
		if g.Has {
			read++
		}
		if g.Alert() {
			out = append(out, strings.ToUpper(g.Label)+" "+g.alertText())
		}
	}
	sf := face(true, math.Max(10, math.Round(12*k)))
	ty := float64(head.Max.Y) + 16*k
	line := func(s string, f font.Face, tracking float64) {
		if int(ty) > box.Max.Y-4 {
			return
		}
		drawTracked(img, f, int(cx)-trackedWidth(f, s, tracking)/2, int(ty), s, tracking, ink)
		ty += 14 * k
	}
	switch {
	case read == 0:
		line("NO SIGNAL", sf, 1.5)
	case len(out) == 0:
		line("ALL READINGS", sf, 1.5)
		line("NORMAL", sf, 1.5)
		ty += 4 * k
		for _, n := range p.Notes {
			line(n, small, 0.5)
		}
	default:
		line(fmt.Sprintf("%d OUT OF RANGE", len(out)), sf, 1.5)
		for _, o := range out {
			line(o, small, 0.5)
		}
	}
}

// drawLamps draws the status strip: one lamp per channel with its code,
// solid when out of range, hollow when normal, a dash with no reading; and
// the channel and alert counts at the right.
func drawLamps(img *image.Gray, r image.Rectangle, gs []Gauge, k float64) {
	roundRect(img, r, float64(r.Dy())/2, 2, false)
	f := face(true, math.Max(9, math.Round(10*k)))
	x := r.Min.X + int(16*k)
	cy := (r.Min.Y + r.Max.Y) / 2
	alerts := 0
	for _, g := range gs {
		lamp := image.Rect(x, cy-int(5*k), x+int(18*k), cy+int(5*k))
		switch {
		case !g.Has:
			hline(img, lamp.Min.X+2, lamp.Max.X-3, cy, muted)
		case g.Alert():
			roundRect(img, lamp, 5*k, 0, true)
			alerts++
		default:
			roundRect(img, lamp, 5*k, 1, false)
		}
		code := g.Short
		if code == "" {
			code = strings.ToUpper(g.Label)
		}
		drawTracked(img, f, lamp.Max.X+int(5*k), cy+int(4*k), code, 1, ink)
		x = lamp.Max.X + int(5*k) + trackedWidth(f, code, 1) + int(16*k)
	}
	summary := fmt.Sprintf("%d CHANNELS · %d %s", len(gs), alerts, map[bool]string{true: "ALERT", false: "ALERTS"}[alerts == 1])
	drawTracked(img, f, r.Max.X-int(16*k)-trackedWidth(f, summary, 1.5), cy+int(4*k), summary, 1.5, ink)
}

// niceStep rounds a raw step to 1, 2 or 5 times a power of ten.
func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if raw <= m*p {
			return m * p
		}
	}
	return 10 * p
}
