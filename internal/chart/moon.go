package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"
)

// Moon dial geometry, as fractions of the dial's radius R: the center ring
// that frames the mark, then each stage's ring, deepest innermost, out to
// just inside the rim.
const (
	moonHub              = 0.30
	moonDeepIn, moonDeep = 0.37, 0.53
	moonLightIn, moonLig = 0.57, 0.72
	moonREMIn, moonREM   = 0.76, 0.88
	moonAwakeIn, moonAwk = 0.91, 0.96
)

// MoonView renders WolfePhase: one night on a 12-hour clock face, 12 at the
// top and running clockwise, so a night fills about half the dial, its
// span outlined as a half-moon in a thin dark line, its ends timed. Inside it
// the sleep stages wrap round as rings, a hypnogram bent into a circle:
// deep at the core, then light, REM, and awake at the rim, solid and
// thinner as sleep lightens — deep black, REM dark gray, light sleep white,
// awake a hairline. The center holds the Wolfe Health mark
// in a ring, as on WolfeSense. A right-hand column in the WolfeSense layout
// gives the night's figures and the key.
func MoonView(n Night, width, height int) (*image.Gray, error) {
	if width < 600 || height < 300 {
		return nil, fmt.Errorf("chart: WolfePhase needs at least 600x300, got %dx%d", width, height)
	}
	if !n.End.After(n.Start) {
		return nil, fmt.Errorf("chart: WolfePhase needs a night with a start and an end")
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad = 16
	drawText(img, face(true, 22), pad, 30, "WolfePhase · "+lowerFirstWord(n.Title), ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, n.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, n.Headline, ink, alignRight)

	panelW := 236
	plot := image.Rect(pad, 64, width-pad-panelW-12, height-8)
	const labelRoom = 30
	R := math.Min(float64(plot.Dx())/2-60, float64(plot.Dy())/2-labelRoom)
	cx := float64(plot.Min.X+plot.Max.X) / 2
	cy := float64(plot.Min.Y+plot.Max.Y) / 2

	drawPhaseDial(img, cx, cy, R, n, phaseFull)
	drawMoonPanel(img, image.Rect(width-pad-panelW, 64, width-pad, height-8), n)
	return img, nil
}

// phaseDial says which parts of the WolfePhase dial to draw: the full dial
// has them all; a small one, as in a calendar cell, leaves out what would
// only clutter it at that size.
type phaseDial struct {
	stages bool // the stage rings inside the night
	ticks  bool // hour ticks round the rim
	hub    bool // the clear center ring that frames the mark
	mark   bool // the wolf in the center
	glow   bool // the moonglow past the rim
	labels bool // the hours and the night's two times
}

var phaseFull = phaseDial{stages: true, ticks: true, hub: true, mark: true, glow: true, labels: true}

// drawPhaseDial draws one night as WolfePhase's moon, centered at cx, cy
// with radius R: a 12-hour face, the night its lit side and the rest its
// dark side, craters across both, and the parts o asks for.
func drawPhaseDial(img *image.Gray, cx, cy, R float64, n Night, o phaseDial) {
	hub := 0.0 // without the center ring, the face runs to the middle
	if o.hub {
		hub = moonHub
	}
	pt := func(f, r float64) (float64, float64) {
		a := f * 2 * math.Pi
		return cx + r*math.Sin(a), cy - r*math.Cos(a)
	}
	loc := n.Start.Location()
	turn := func(t time.Time) float64 { // a 12-hour face: 0 at 12 o'clock, clockwise
		t = t.In(loc)
		return (float64(t.Hour()%12) + float64(t.Minute())/60 + float64(t.Second())/3600) / 12
	}
	// a 12-hour face holds at most 12 hours: a longer night draws its first
	// twelve, so it can't wrap round over its own start
	drawEnd := n.End
	if limit := n.Start.Add(12*time.Hour - time.Minute); drawEnd.After(limit) {
		drawEnd = limit
	}
	a0, a1 := turn(n.Start), turn(drawEnd)
	inNight := func(f float64) bool { // on the night's arc, which may cross midnight
		if a1 >= a0 {
			return f >= a0 && f <= a1
		}
		return f >= a0 || f <= a1
	}
	timeAt := func(f float64) time.Time { // the moment within the night at turn f
		d := f - a0
		if d < 0 {
			d++
		}
		return n.Start.Add(time.Duration(d * 12 * float64(time.Hour)))
	}

	// The dial's geometry is drawn at moonSS times the size and averaged
	// down, so every curve gets soft edges: on a four-gray panel those
	// become smooth steps rather than a staircase of hard pixels. Labels
	// are drawn afterwards at full size, so text stays crisp.
	const ss = moonSS
	margin := 1.12 * R // the ticks and the glow, past the rim
	box := image.Rect(int(cx-margin), int(cy-margin), int(cx+margin)+1, int(cy+margin)+1)
	big := image.NewGray(image.Rect(0, 0, box.Dx()*ss, box.Dy()*ss))
	fillRect(big, big.Bounds(), surface)
	bcx, bcy, bR := (cx-float64(box.Min.X))*ss, (cy-float64(box.Min.Y))*ss, R*ss
	bpt := func(f, r float64) (float64, float64) {
		a := f * 2 * math.Pi
		return bcx + r*math.Sin(a), bcy - r*math.Cos(a)
	}

	// the moon, as a phase: the night is its lit face, light gray, where
	// the stages sit; the rest is its dark side, dark gray
	side := func(px, py float64) bool { // on the night's side of the dial
		f := math.Atan2(px-bcx, -(py-bcy)) / (2 * math.Pi)
		if f < 0 {
			f++
		}
		return inNight(f)
	}
	for y := 0; y < big.Bounds().Dy(); y++ {
		for x := 0; x < big.Bounds().Dx(); x++ {
			dx, dy := float64(x)+0.5-bcx, float64(y)+0.5-bcy
			r := math.Hypot(dx, dy)
			if r > bR || r < hub*bR {
				continue
			}
			f := math.Atan2(dx, -dy) / (2 * math.Pi)
			if f < 0 {
				f++
			}
			if inNight(f) {
				big.SetGray(x, y, windowShade)
			} else {
				big.SetGray(x, y, remShade)
			}
		}
	}
	for _, c := range moonCraters {
		x, y := bcx+c.x*bR, bcy+c.y*bR
		rr := c.size * bR
		// each a step darker than the surface under it: dark gray on the
		// lit face, black on the dark side
		craterShadow(big, x, y, rr, 1.1*ss, func(px, py float64) color.Gray {
			if side(px, py) {
				return remShade
			}
			return ink
		})
	}

	// the stages, wrapped: each pixel in a stage ring inks by the stage at
	// its moment
	type ringBand struct {
		in, out float64
		st      Stage
	}
	bands := []ringBand{{moonDeepIn, moonDeep, StageDeep}, {moonLightIn, moonLig, StageLight},
		{moonREMIn, moonREM, StageREM}, {moonAwakeIn, moonAwk, StageAwake}}
	// the stages smoothed for drawing: brief flips merged into the stretch
	// around them, so the rings are clean arcs, not slivers; the column's
	// figures still count the raw stages
	stages := smoothStages(n.Stages, moonSmoothWindow(n.StageEpoch))
	stageAt := func(t time.Time) Stage {
		if n.StageEpoch <= 0 || t.Before(n.Start) {
			return StageUnknown
		}
		i := int(t.Sub(n.Start) / n.StageEpoch)
		if i < 0 || i >= len(stages) {
			return StageUnknown
		}
		return stages[i]
	}
	for y := int(bcy - bR); y <= int(bcy+bR) && o.stages; y++ {
		for x := int(bcx - bR); x <= int(bcx+bR); x++ {
			dx, dy := float64(x)+0.5-bcx, float64(y)+0.5-bcy
			r := math.Hypot(dx, dy) / bR
			if r < moonDeepIn || r > moonAwk {
				continue
			}
			f := math.Atan2(dx, -dy) / (2 * math.Pi)
			if f < 0 {
				f++
			}
			if !inNight(f) {
				continue
			}
			st := stageAt(timeAt(f))
			for _, b := range bands {
				if b.st != st || r < b.in || r > b.out {
					continue
				}
				if c, ok := moonShade(st, (r-b.in)/(b.out-b.in)); ok {
					big.SetGray(x, y, c)
				}
			}
		}
	}

	// two faint guide rings only, the center's and the rim; the stages mark
	// their own rings
	if o.hub {
		arcBand(big, bcx, bcy, moonHub*bR-0.5*ss, moonHub*bR+0.5*ss, 0, 1, grid)
	}
	arcBand(big, bcx, bcy, bR-1*ss, bR+0.5*ss, 0, 1, rule)
	// the night's span: thin dark arcs along the rim and round the center
	// ring, which with the edges close the night into one half-moon shape
	w := moonEdgeW * ss
	nightArc := func(ri, ro float64) {
		if a1 >= a0 {
			arcBand(big, bcx, bcy, ri, ro, a0, a1, moonEdge)
		} else {
			arcBand(big, bcx, bcy, ri, ro, a0, 1, moonEdge)
			arcBand(big, bcx, bcy, ri, ro, 0, a1, moonEdge)
		}
	}
	nightArc(bR-w, bR+0.5*ss)
	if o.hub {
		nightArc(moonHub*bR-w/2, moonHub*bR+w/2)
	}
	// the night's edges at bedtime and waking, in the arcs' line so the four
	// sides close into one half-moon
	for _, f := range []float64{a0, a1} {
		x0, y0 := bpt(f, math.Max(0, hub*bR-w/2))
		x1, y1 := bpt(f, bR+0.5*ss)
		thickLine(big, x0, y0, x1, y1, w, moonEdge)
	}
	// hour ticks round the rim
	for h := 0; h < 12 && o.ticks; h++ {
		f := float64(h) / 12
		out := 1.04
		if h%3 == 0 {
			out = 1.08
		}
		x0, y0 := bpt(f, bR)
		x1, y1 := bpt(f, out*bR)
		thickLine(big, x0, y0, x1, y1, 1.3*ss, ink)
	}

	// the center: the mark, in the outline's dark gray so it anchors the
	// lighter dial
	if o.mark {
		DrawWolfOutline(big, bcx, bcy, 1.45*moonHub*bR, math.Max(1, math.Min(2, R/40))*ss, muted)
	}

	// down to full size: each pixel the average of its block. A block of
	// blank page is left alone, so the dial's square box never paints over
	// the title above it, which a large dial's box reaches.
	for y := 0; y < box.Dy(); y++ {
		for x := 0; x < box.Dx(); x++ {
			sum := 0
			for yy := 0; yy < ss; yy++ {
				for xx := 0; xx < ss; xx++ {
					sum += int(big.GrayAt(x*ss+xx, y*ss+yy).Y)
				}
			}
			if v := uint8((sum + ss*ss/2) / (ss * ss)); v != surface.Y {
				img.SetGray(box.Min.X+x, box.Min.Y+y, color.Gray{Y: v})
			}
		}
	}

	// the moonglow: rings of dots just past the rim, sparser outward, drawn
	// only on blank page so the ticks stay whole
	for y := box.Min.Y; y < box.Max.Y && o.glow; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			r := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) / R
			if r <= 1.015 || r > 1.11 || img.GrayAt(x, y).Y != surface.Y {
				continue
			}
			step := 4
			switch {
			case r > 1.075:
				step = 9
			case r > 1.045:
				step = 6
			}
			if x%step == 0 && y%step == 0 {
				img.SetGray(x, y, ink)
			}
		}
	}

	// labels, at full size over the dial: the hours every 3, leaving out
	// one that would sit under bedtime's or waking's, then those two times
	near := func(f, g float64) bool {
		d := math.Abs(f - g)
		return math.Min(d, 1-d) < 0.045
	}
	if !o.labels {
		return
	}
	for h := 0; h < 12; h += 3 {
		f := float64(h) / 12
		if near(f, a0) || near(f, a1) {
			continue
		}
		lx, ly := pt(f, 1.08*R+14)
		drawText(img, face(true, 11), int(lx), int(ly)+4, map[int]string{0: "12", 3: "3", 6: "6", 9: "9"}[h], muted, alignCenter)
	}
	for i, f := range []float64{a0, a1} {
		lx, ly := pt(f, R+20)
		t := n.Start
		if i == 1 {
			t = n.End // the real waking, even when the arc stops at 12 hours
		}
		al := alignLeft
		if math.Sin(f*2*math.Pi) < 0 {
			al = alignRight
		}
		label := t.In(loc).Format("3:04 PM")
		f12 := face(true, 12)
		tw := textWidth(f12, label)
		bx := int(lx)
		if al == alignRight {
			bx -= tw
		}
		fillRect(img, image.Rect(bx-3, int(ly)-9, bx+tw+3, int(ly)+5), surface)
		drawText(img, f12, int(lx), int(ly)+4, label, ink, al)
	}

}

// moonCraters are the moon's craters, fixed so the face is the same every
// night: their centers across the face (x right, y down) and radii, as
// fractions of R. The layout follows cartoon moons: a strong contrast of
// sizes — two big craters on opposite diagonals, an overlapping pair low
// on the left, medium ones between, and small ones toward the rim — all
// clear of the mark in the middle. They cover the whole face, the night's
// lit side included, under the stages.
var moonCraters = []struct{ x, y, size float64 }{
	{-0.45, -0.50, 0.19}, // big, upper left
	{0.38, 0.60, 0.18},   // big, lower right
	{-0.52, 0.30, 0.12},  // the pair, low on the left
	{-0.30, 0.66, 0.20},
	{0.72, 0.08, 0.13},  // medium, right
	{0.24, -0.72, 0.11}, // medium, top
	{0.52, -0.22, 0.07}, // small
	{0.66, -0.50, 0.06},
	{-0.84, 0.02, 0.06},
	{0.02, -0.86, 0.05},
	{-0.10, 0.90, 0.05},
}

// craterShadow draws a crater, a disc of radius r at (cx, cy): a thin rim
// of width rim, and a crescent of shadow along the rim facing away from
// the light, the floor less a copy of it nudged toward the light, so the
// shadow is thickest opposite the light and tapers to points either side,
// as a real crater's does. Every crater takes its light from the same
// side, the lower right. The floor is the surface itself, so a crater
// reads as a dent, not a hole; shade gives the ink at each point.
func craterShadow(img *image.Gray, cx, cy, r, rim float64, shade func(x, y float64) color.Gray) {
	lx, ly := cx+0.32*r*0.7071, cy+0.32*r*0.7071 // the floor, nudged toward the light
	for y := int(cy - r); y <= int(cy+r)+1; y++ {
		for x := int(cx - r); x <= int(cx+r)+1; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			d := math.Hypot(px-cx, py-cy)
			if d <= r && (d >= r-rim || math.Hypot(px-lx, py-ly) > r) {
				img.SetGray(x, y, shade(px, py))
			}
		}
	}
}

// moonSS is the dial's supersampling: drawn at this many times the size and
// averaged down, for soft edges on every curve.
const moonSS = 3

// The half-moon's outline: its color and width, shared by the rim arc,
// the inner arc and the bedtime and waking edges, so the four sides close
// as one shape.
var (
	moonEdge  = muted
	moonEdgeW = 2.5
)

// Stage grays on the dial: REM a dark gray, light sleep white on the lit
// face's light gray, so the four stages read by shade as well as by ring.
var remShade = color.Gray{Y: 100}

// moonShade is how a stage inks its ring, t being how far across the ring
// (0 to 1): solid, and thinner as sleep lightens, so thickness reads as
// depth: deep black and filling its ring, REM dark gray, light sleep
// white, awake a black hairline.

func moonShade(st Stage, t float64) (color.Gray, bool) {
	within := func(w float64) bool { return t >= 0.5-w/2 && t <= 0.5+w/2 }
	switch st {
	case StageDeep:
		return ink, true
	case StageREM:
		return remShade, within(0.5)
	case StageLight:
		return surface, within(0.75)
	case StageAwake:
		return ink, within(0.22)
	}
	return surface, false
}

// moonSmoothWindow is how many epochs either side a stage is smoothed
// over: about two and a half minutes, so a flip shorter than that merges
// into its surroundings. Five-minute epochs aren't smoothed.
func moonSmoothWindow(epoch time.Duration) int {
	if epoch <= 0 {
		return 0
	}
	return int((150 * time.Second) / epoch)
}

// smoothStages gives each epoch the most common stage in the window w
// epochs either side of it, unknowns not counted; a tie keeps the epoch's
// own stage. Real stretches keep their shape; brief flips disappear.
func smoothStages(stages []Stage, w int) []Stage {
	if w <= 0 {
		return stages
	}
	out := make([]Stage, len(stages))
	for i := range stages {
		var count [StageAwake + 1]int
		for k := max(0, i-w); k <= min(len(stages)-1, i+w); k++ {
			if s := stages[k]; s != StageUnknown && s <= StageAwake {
				count[s]++
			}
		}
		best := stages[i]
		for s := StageDeep; s <= StageAwake; s++ {
			if count[s] > count[best] || (best == StageUnknown && count[s] > 0) {
				best = s
			}
		}
		out[i] = best
	}
	return out
}

// lowerFirstWord turns "Night of Tue, Nov 18" into "night of Tue, Nov 18".
func lowerFirstWord(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// drawMoonPanel draws the right column in the WolfeSense layout: the night's
// time asleep, then each figure, then the key.
func drawMoonPanel(img *image.Gray, r image.Rectangle, n Night) {
	loc := n.Start.Location()
	var tot = map[Stage]time.Duration{}
	for _, s := range n.Stages {
		tot[s] += n.StageEpoch
	}
	asleep := tot[StageDeep] + tot[StageLight] + tot[StageREM]
	share := func(d time.Duration) string {
		if asleep <= 0 {
			return ""
		}
		return fmt.Sprintf("%.0f%%", float64(d)/float64(asleep)*100)
	}
	rows := []PanelRow{
		{Label: "Deep", Value: Duration(tot[StageDeep].Minutes()), Note: share(tot[StageDeep])},
		{Label: "REM", Value: Duration(tot[StageREM].Minutes()), Note: share(tot[StageREM])},
		{Label: "Light", Value: Duration(tot[StageLight].Minutes()), Note: share(tot[StageLight])},
		{Label: "Awake", Value: Duration(tot[StageAwake].Minutes())},
	}
	if lo, ok := seriesMin(n.HeartRate); ok {
		rows = append(rows, PanelRow{Label: "Lowest HR", Value: fmt.Sprintf("%.0f bpm", lo)})
	}
	if avg, ok := seriesMean(n.HRV); ok {
		rows = append(rows, PanelRow{Label: "HRV, avg", Value: fmt.Sprintf("%.0f ms", avg)})
	}
	// each stage's swatch on a chip of the lit face's gray, as the stage
	// sits on the dial, so white light sleep shows
	swatch := func(st Stage) func(x, y int) {
		return func(x, y int) { stageChip(img, x-2, y-10, 26, 13, st) }
	}
	drawColumn(img, r, column{
		head:      "NIGHT OF " + strings.ToUpper(n.Start.In(loc).Format("Jan 2")),
		headNote:  n.Start.In(loc).Format("3:04 PM") + " – " + n.End.In(loc).Format("3:04 PM"),
		leadLabel: "Asleep", lead: Duration(asleep.Minutes()),
		rows: rows,
		key: []keyEntry{
			{"deep, the inner ring", swatch(StageDeep)},
			{"light", swatch(StageLight)},
			{"REM", swatch(StageREM)},
			{"awake, the outer ring", swatch(StageAwake)},
		},
	})
}
