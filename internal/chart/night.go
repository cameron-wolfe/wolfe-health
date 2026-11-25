package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"time"
)

// Stage is a sleep stage, numbered as Oura encodes it. Zero is unknown.
type Stage uint8

const (
	StageUnknown Stage = iota
	StageDeep
	StageLight
	StageREM
	StageAwake
)

// Series is a regularly spaced run of readings: Values[i] was taken at
// Start + i*Interval. A nil value is a missing reading.
type Series struct {
	Start    time.Time
	Interval time.Duration
	Values   []*float64
}

// Night is the content of the sleep-stage chart for one sleep period.
//
// Header text arrives formatted. Everything the chart derives from the data
// itself — stage totals, heart rate average and low — it computes from what it
// draws, so the numbers and the picture can never disagree.
type Night struct {
	Title    string // "Night of Tue, Nov 18"
	Subtitle string // "11:02 PM – 5:10 AM · 6h 8m in bed"
	Headline string // "5h 19m asleep"
	Subline  string // "efficiency 87% · asleep in 12m"

	Start, End time.Time // the period: bedtime start and end

	StageEpoch time.Duration // length of one Stages entry: 30s or 5m
	Stages     []Stage

	MoveEpoch time.Duration // length of one Movement entry
	Movement  []uint8       // 1 still, 2 restless, 3 tossing, 4 active; 0 unknown

	HeartRate Series // beats per minute
	HRV       Series // milliseconds
}

// Grays specific to this chart.
var (
	connector = color.Gray{Y: 175} // stage-transition verticals: the trace, recessive
	laneGrid  = color.Gray{Y: 232} // hour grid inside the lanes, fainter than connectors
	laneShade = color.Gray{Y: 246} // alternate lane background
)

// Lanes, top to bottom, in the conventional hypnogram order: lighter sleep
// higher, so the trace sinks as sleep deepens.
var laneOrder = []Stage{StageAwake, StageREM, StageLight, StageDeep}

var stageNames = map[Stage]string{
	StageAwake: "Awake", StageREM: "REM", StageLight: "Light", StageDeep: "Deep",
}

// stageStyle is how a stage's blocks are inked. Ink density rises with depth:
// a dotted hairline for awake, a dashed bar for REM, a hatched bar for light,
// a solid bar for deep. Each is pure black on white, so the stages stay
// distinct after the panel reduces everything to two levels — position (the
// lane) and texture both carry the stage, never gray alone.
type stageStyle struct {
	height int // bar thickness in pixels
	// ink reports whether pixel (x, dy) of the bar is black, dy being the row
	// within the bar from 0 at its top. x is absolute, so a pattern stays in
	// phase across neighboring blocks instead of restarting at each one.
	ink func(x, dy, h int) bool
}

var stageStyles = map[Stage]stageStyle{
	StageDeep: {height: 26, ink: func(x, dy, h int) bool { return true }},
	StageLight: {height: 16, ink: func(x, dy, h int) bool {
		// outline top and bottom, 45° hatch between
		return dy == 0 || dy == h-1 || (x+dy)%4 == 0
	}},
	StageREM: {height: 8, ink: func(x, dy, h int) bool {
		return x%9 < 6 // 6 on, 3 off
	}},
	StageAwake: {height: 4, ink: func(x, dy, h int) bool {
		return x%4 < 2 // square dots
	}},
}

// Layout of the night view, in pixels.
const (
	nightPad    = 16
	nightLabelW = 72 // left column: lane names, panel tick values
	nightTotalW = 92 // right column: totals and summaries
	nightHeadH  = 62 // title block
	axisH       = 24 // hour labels at the bottom
	topAxisH    = 16 // the same labels again, above the stage lanes
	rowGap      = 9  // space above each lower row's divider
	rowTitleH   = 18 // divider to plot: the row's title line
	moveH       = 18 // movement plot
)

// A lower row is a divider rule across the full width, a bold title with a
// muted hint, then the plot. The rule, the title, the break in the hour grid
// and the alternating shade together keep the rows from running into each
// other — position alone was not enough.
var rule = color.Gray{Y: 185} // darker than grid: a separator, not a reference line

// NightView renders one sleep period as a detailed sleep-stage chart: four
// stage lanes drawn with distinct thickness and texture, then movement, heart
// rate and HRV rows sharing the time axis.
func NightView(n Night, width, height int) (*image.Gray, error) {
	if width < 640 || height < 400 {
		return nil, fmt.Errorf("chart: night view needs at least 640x400, got %dx%d", width, height)
	}
	if !n.End.After(n.Start) {
		return nil, fmt.Errorf("chart: night ends (%v) before it starts (%v)", n.End, n.Start)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	// header: what and when on the left, the outcome on the right
	drawText(img, face(true, 22), nightPad, 30, n.Title, ink, alignLeft)
	drawText(img, face(false, 14), nightPad, 52, n.Subtitle, muted, alignLeft)
	drawText(img, face(true, 22), width-nightPad, 30, n.Headline, ink, alignRight)
	drawText(img, face(false, 14), width-nightPad, 52, n.Subline, muted, alignRight)

	lanes, hrH, hrvH := nightLayout(width, height)
	plotL, plotR := lanes.Min.X, lanes.Max.X

	tx := timeAxis{start: n.Start, end: n.End, left: float64(plotL), right: float64(plotR)}
	// Hour labels at the bottom for the panels, and again directly above the
	// stage lanes: reading a block's time off an axis four panels away is
	// hard, and the lanes are where the time of night matters most.
	for _, t := range tx.ticks() {
		x := int(math.Round(tx.x(t)))
		drawText(img, labelFace(), x, height-8, hourLabel(t), muted, alignCenter)
		drawText(img, labelFace(), x, lanes.Min.Y-5, hourLabel(t), muted, alignCenter)
	}

	drawLanes(img, lanes, n, tx, width)

	// Lower rows continue the lanes' white / shaded alternation: the lanes end
	// on shaded Deep, so movement is white, heart rate shaded, HRV white.
	y := lanes.Max.Y
	row := func(h int, title, hint string, shaded bool) (plot image.Rectangle, titleY int) {
		y += rowGap
		hline(img, nightPad, width-nightPad-1, y, rule)
		titleY = y + 14
		f := face(true, 12)
		drawText(img, f, plotL, titleY, title, ink, alignLeft)
		drawText(img, face(false, 11), plotL+textWidth(f, title)+6, titleY, hint, muted, alignLeft)
		plot = image.Rect(plotL, y+rowTitleH, plotR, y+rowTitleH+h)
		y = plot.Max.Y
		if shaded {
			fillRect(img, plot, laneShade)
		}
		hourGrid(img, plot, tx, grid)
		return plot, titleY
	}

	move, moveY := row(moveH, "Movement", "taller tick = more movement", false)
	drawMovement(img, move, n, tx, moveY, width)
	hr, hrY := row(hrH, "Heart rate", "bpm, every 5 min", true)
	drawHeartRate(img, hr, n.HeartRate, tx, hrY, width)
	hrv, hrvY := row(hrvH, "HRV", "ms, per 5-min window", false)
	drawHRV(img, hrv, n.HRV, tx, hrvY, width)
	return img, nil
}

// nightLayout splits the height: the heart rate and HRV plots take fixed
// shares, movement a fixed height, and the stage lanes everything left over.
// It returns the lanes' rectangle and the two plot heights.
func nightLayout(width, height int) (lanes image.Rectangle, hrH, hrvH int) {
	top := nightHeadH + 8 + topAxisH
	bottom := height - axisH
	hrH = (bottom - top) * 16 / 100
	hrvH = (bottom - top) * 14 / 100
	rowsH := 3*(rowGap+rowTitleH) + moveH + hrH + hrvH
	lanes = image.Rect(nightPad+nightLabelW, top, width-nightPad-nightTotalW, bottom-rowsH)
	return lanes, hrH, hrvH
}

// hourGrid draws the hour lines inside one panel only, so the grid breaks
// between panels instead of stitching them together.
func hourGrid(img *image.Gray, r image.Rectangle, tx timeAxis, c color.Gray) {
	for _, t := range tx.ticks() {
		vline(img, int(math.Round(tx.x(t))), r.Min.Y, r.Max.Y-1, c)
	}
}

// timeAxis maps clock time across the plot.
type timeAxis struct {
	start, end  time.Time
	left, right float64
}

func (a timeAxis) x(t time.Time) float64 {
	f := float64(t.Sub(a.start)) / float64(a.end.Sub(a.start))
	return a.left + f*(a.right-a.left)
}

// ticks are the whole hours inside the period, thinned to every second or
// third hour when a long period would crowd the labels.
func (a timeAxis) ticks() []time.Time {
	span := a.end.Sub(a.start)
	step := time.Hour
	if px := (a.right - a.left) / span.Hours(); px < 46 {
		step = time.Duration(math.Ceil(46/px)) * time.Hour
	}
	first := a.start.Truncate(time.Hour)
	if first.Before(a.start) {
		first = first.Add(time.Hour)
	}
	var out []time.Time
	for t := first; !t.After(a.end); t = t.Add(time.Hour) {
		if t.Hour()%int(step.Hours()) == 0 {
			out = append(out, t)
		}
	}
	return out
}

// hourLabel is "11 PM", "12 AM", "1 AM": the clock face people read.
func hourLabel(t time.Time) string { return t.Format("3 PM") }

// run is a stretch of consecutive epochs in one stage.
type run struct {
	stage      Stage
	start, end int // epoch indexes, end exclusive
}

// runs collapses per-epoch stages into runs. Unknown epochs end a run and
// start none, so a gap in Oura's data stays a gap in the chart.
func runs(stages []Stage) []run {
	var out []run
	for i := 0; i < len(stages); {
		s := stages[i]
		j := i + 1
		for j < len(stages) && stages[j] == s {
			j++
		}
		if s != StageUnknown {
			out = append(out, run{s, i, j})
		}
		i = j
	}
	return out
}

// stageTotals is the time spent in each stage.
func stageTotals(stages []Stage, epoch time.Duration) map[Stage]time.Duration {
	t := map[Stage]time.Duration{}
	for _, s := range stages {
		if s != StageUnknown {
			t[s] += epoch
		}
	}
	return t
}

// drawLanes draws the four stage lanes: labels at the left, totals at the
// right, transition connectors, then the stage blocks over them.
func drawLanes(img *image.Gray, r image.Rectangle, n Night, tx timeAxis, width int) {
	laneH := float64(r.Dy()) / float64(len(laneOrder))
	mid := map[Stage]int{}
	for i, s := range laneOrder {
		y0 := r.Min.Y + int(laneH*float64(i))
		y1 := r.Min.Y + int(laneH*float64(i+1))
		mid[s] = (y0 + y1) / 2
		if i%2 == 1 {
			fillRect(img, image.Rect(r.Min.X, y0, r.Max.X, y1), laneShade)
		}
	}
	// fainter here than in the panels, so it never reads as a stage transition
	hourGrid(img, r, tx, laneGrid)

	rs := runs(n.Stages)
	x := func(epoch int) float64 {
		return tx.x(n.Start.Add(time.Duration(epoch) * n.StageEpoch))
	}

	// transitions: a hairline from one lane's center to the next at each
	// change, giving the classic stepped trace without competing with blocks
	for i := 1; i < len(rs); i++ {
		a, b := rs[i-1], rs[i]
		if a.end != b.start {
			continue // a data gap between them: no transition to draw
		}
		bx := int(math.Round(x(b.start)))
		y0, y1 := mid[a.stage], mid[b.stage]
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		vline(img, bx, y0, y1, connector)
	}

	for _, rn := range rs {
		drawBlock(img, rn.stage, x(rn.start), x(rn.end), mid[rn.stage])
	}
	labelBlocks(img, rs, x, mid, n.StageEpoch, float64(r.Min.X), float64(r.Max.X))

	// lane labels, and totals with their share of time in bed
	totals := stageTotals(n.Stages, n.StageEpoch)
	var all time.Duration
	for _, d := range totals {
		all += d
	}
	for _, s := range laneOrder {
		y := mid[s]
		drawText(img, face(false, 15), nightPad, y+5, stageNames[s], ink, alignLeft)
		d := totals[s]
		drawText(img, face(true, 15), width-nightPad, y, hm(d), ink, alignRight)
		share := "—"
		if all > 0 {
			share = fmt.Sprintf("%.0f%%", 100*float64(d)/float64(all))
		}
		drawText(img, labelFace(), width-nightPad, y+16, share, muted, alignRight)
	}
}

// Block labels. The rule, in full:
//
//   - Labels go on episodes, not raw runs. An episode is continuous time in
//     one stage where interruptions of up to episodeGap do not break it. The
//     30-second data splits a visibly solid REM stretch into several runs
//     around brief blips; labeling runs would leave such a block bare. The
//     blocks themselves are still drawn run by run, blips and all.
//   - Every deep, REM and awake episode of at least minLabeled gets a label:
//     a threshold in time, so it does not shift with image size or the length
//     of the night. Light sleep is excluded; it fills the gaps between the
//     others, and its total is in the right column.
//   - The label is the time actually spent in the stage, not counting the
//     interruptions, centered just above the episode in one style for all.
//   - Longest first: a label that would overlap one already placed in its
//     lane, or would not fit the plot, is dropped.
const (
	episodeGap = time.Minute
	minLabeled = 10 * time.Minute
	labelPad   = 4 // minimum pixels between labels in a lane
)

// episode is a stretch of one stage that tolerates brief interruptions.
type episode struct {
	stage      Stage
	start, end int // epoch indexes spanned, end exclusive
	inStage    int // epochs actually in the stage
}

// episodes groups each stage's runs into episodes, joining runs whose gap is
// at most maxGap epochs. Episodes come out per stage, in time order.
func episodes(rs []run, maxGap int) []episode {
	var out []episode
	open := map[Stage]int{} // stage -> index in out of its current episode
	for _, r := range rs {
		if i, ok := open[r.stage]; ok && r.start-out[i].end <= maxGap {
			out[i].end = r.end
			out[i].inStage += r.end - r.start
			continue
		}
		open[r.stage] = len(out)
		out = append(out, episode{r.stage, r.start, r.end, r.end - r.start})
	}
	return out
}

// blockLabel is a label placed by chooseLabels.
type blockLabel struct {
	stage  Stage
	text   string
	center float64 // x
}

// chooseLabels applies the rule above. x maps an epoch index to a pixel
// column; width measures a label; left and right bound the plot.
func chooseLabels(rs []run, epoch time.Duration, x func(int) float64,
	width func(string) int, left, right float64) []blockLabel {

	if len(rs) == 0 || epoch <= 0 {
		return nil // no stage data, nothing to label
	}
	maxGap := int(episodeGap / epoch) // 0 for 5-minute data: no joining
	var cands []episode
	for _, e := range episodes(rs, maxGap) {
		if e.stage != StageLight && time.Duration(e.inStage)*epoch >= minLabeled {
			cands = append(cands, e)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].inStage > cands[j].inStage })

	type span struct{ l, r float64 }
	placed := map[Stage][]span{}
	var out []blockLabel
next:
	for _, e := range cands {
		text := hm(time.Duration(e.inStage) * epoch)
		w := float64(width(text))
		c := (x(e.start) + x(e.end)) / 2
		// keep it inside the plot by sliding, never by clipping
		c = math.Max(left+w/2, math.Min(right-w/2, c))
		sp := span{c - w/2, c + w/2}
		for _, o := range placed[e.stage] {
			if sp.l < o.r+labelPad && o.l < sp.r+labelPad {
				continue next
			}
		}
		placed[e.stage] = append(placed[e.stage], sp)
		out = append(out, blockLabel{e.stage, text, c})
	}
	return out
}

// labelBlocks draws chooseLabels' labels just above each stage's bar.
func labelBlocks(img *image.Gray, rs []run, x func(int) float64, mid map[Stage]int,
	epoch time.Duration, left, right float64) {
	f := face(true, 11)
	for _, l := range chooseLabels(rs, epoch, x, func(s string) int { return textWidth(f, s) }, left, right) {
		top := mid[l.stage] - stageStyles[l.stage].height/2
		drawText(img, f, int(math.Round(l.center)), top-3, l.text, muted, alignCenter)
	}
}

// drawBlock inks one run in its stage's style, centered on the lane.
//
// A run always covers at least one pixel column, and its first and last
// columns are inked at full bar height whatever the pattern: a 30-second
// awakening is narrower than a pixel at this scale, and without the caps a
// dotted or dashed style could land entirely in a gap and vanish.
func drawBlock(img *image.Gray, s Stage, x0, x1 float64, mid int) {
	st := stageStyles[s]
	ix0 := int(math.Floor(x0))
	ix1 := int(math.Round(x1))
	if ix1 <= ix0 {
		ix1 = ix0 + 1
	}
	top := mid - st.height/2
	for x := ix0; x < ix1; x++ {
		cap := x == ix0 || x == ix1-1
		for dy := 0; dy < st.height; dy++ {
			if cap || st.ink(x, dy, st.height) {
				img.SetGray(x, top+dy, ink)
			}
		}
	}
}

// drawMovement draws a tick for each restless epoch, taller for more
// movement, rising from a baseline; still epochs draw nothing. The right
// column totals the restless time.
func drawMovement(img *image.Gray, r image.Rectangle, n Night, tx timeAxis, titleY, width int) {
	hline(img, r.Min.X, r.Max.X-1, r.Max.Y-1, rule)
	heights := map[uint8]int{2: r.Dy() * 4 / 10, 3: r.Dy() * 7 / 10, 4: r.Dy()}
	restless := 0
	for i, m := range n.Movement {
		h, ok := heights[m]
		if !ok {
			continue
		}
		restless++
		x := int(tx.x(n.Start.Add(time.Duration(i) * n.MoveEpoch)))
		vline(img, x, r.Max.Y-h, r.Max.Y-1, ink)
	}
	if len(n.Movement) == 0 {
		return
	}
	d := time.Duration(restless) * n.MoveEpoch
	drawText(img, face(true, 15), width-nightPad, titleY+2, hm(d), ink, alignRight)
	drawText(img, labelFace(), width-nightPad, titleY+18,
		fmt.Sprintf("restless, %.0f%%", 100*float64(restless)/float64(len(n.Movement))), muted, alignRight)
}

// seriesPoint is one non-missing reading, positioned on the time axis.
type seriesPoint struct {
	x0, x1 float64 // the reading's window: its start and the next reading's
	v      float64
	i      int // index in the series, to tell consecutive readings from gaps
}

// readings positions the series' non-missing values, and returns their mean.
func readings(s Series, tx timeAxis) ([]seriesPoint, float64) {
	var pts []seriesPoint
	var sum float64
	for i, v := range s.Values {
		if v == nil {
			continue
		}
		t := s.Start.Add(time.Duration(i) * s.Interval)
		x1 := math.Min(tx.x(t.Add(s.Interval)), tx.right)
		pts = append(pts, seriesPoint{tx.x(t), x1, *v, i})
		sum += *v
	}
	if len(pts) == 0 {
		return nil, 0
	}
	return pts, sum / float64(len(pts))
}

// panelScale draws a panel's round ticks as faint lines, labeling the top and
// bottom in the left column, and returns the value-to-y mapping.
//
// n is roughly how many intervals to aim for: a zero-based scale needs more
// than a fitted one, or its top tick overshoots the data by up to double.
func panelScale(img *image.Gray, r image.Rectangle, lo, hi float64, n int) func(float64) float64 {
	ymin, ymax, step := niceScale(lo, hi, n)
	top, bot := float64(r.Min.Y+3), float64(r.Max.Y-1)
	y := func(v float64) float64 { return bot - (v-ymin)/(ymax-ymin)*(bot-top) }
	for v := ymin; v <= ymax+step/2; v += step {
		ty := int(math.Round(y(v)))
		if v > ymin {
			hline(img, r.Min.X, r.Max.X-1, ty, grid)
		}
		if v == ymin || v+step > ymax+step/2 {
			drawText(img, face(false, 11), r.Min.X-6, ty+4, formatValue(v), muted, alignRight)
		}
	}
	return y
}

// markExtreme rings the reading that matters and prints its value inside
// the panel: below a low point or above a high one when there is room, else
// beside the dot; flipped to the left near the right edge so it never runs
// into the totals column.
func markExtreme(img *image.Gray, r image.Rectangle, x, y, v float64, high bool) {
	disc(img, x, y, dotRadius+dotRing, surface)
	disc(img, x, y, dotRadius, ink)

	const textH = 12 // cap height of valueFace, near enough
	f := valueFace()
	text := formatValue(v)
	w := textWidth(f, text)

	// default: centered above (high) or below (low) the dot
	lx, ly, a := int(x), int(y)+dotRadius+dotRing+textH+2, alignCenter
	if high {
		ly = int(y) - dotRadius - dotRing - 2
	}
	fits := ly-textH >= r.Min.Y && ly <= r.Max.Y-1
	if !fits {
		// beside the dot, vertically centered on it
		ly = int(y) + textH/2
		a, lx = alignLeft, int(x)+dotRadius+dotRing+3
	}
	if lx+w/2 > r.Max.X || (a == alignLeft && lx+w > r.Max.X) {
		a, lx = alignRight, int(x)-dotRadius-dotRing-3
	}
	drawText(img, f, lx, clampInt(ly, r.Min.Y+textH, r.Max.Y-1), text, ink, a)
}

func summary(img *image.Gray, titleY, width int, avg float64, detail string) {
	drawText(img, face(true, 15), width-nightPad, titleY+2, fmt.Sprintf("avg %.0f", avg), ink, alignRight)
	drawText(img, labelFace(), width-nightPad, titleY+18, detail, muted, alignRight)
}

// drawHeartRate draws heart rate as a line, broken where readings are
// missing, with the lowest point marked: the night's deepest recovery.
func drawHeartRate(img *image.Gray, r image.Rectangle, s Series, tx timeAxis, titleY, width int) {
	pts, avg := readings(s, tx)
	if len(pts) == 0 {
		drawText(img, labelFace(), r.Min.X+8, r.Min.Y+r.Dy()/2+5, "no readings", muted, alignLeft)
		return
	}
	lo, hi, ext := pts[0].v, pts[0].v, pts[0]
	for _, p := range pts {
		lo, hi = math.Min(lo, p.v), math.Max(hi, p.v)
		if p.v < ext.v {
			ext = p
		}
	}
	y := panelScale(img, r, lo, hi, 2)

	for k := 1; k < len(pts); k++ {
		a, b := pts[k-1], pts[k]
		if b.i-a.i == 1 {
			thickLine(img, a.x0, y(a.v), b.x0, y(b.v), lineWidth, ink)
		}
	}
	for k, p := range pts {
		prev := k > 0 && pts[k-1].i == p.i-1
		next := k < len(pts)-1 && pts[k+1].i == p.i+1
		if !prev && !next {
			disc(img, p.x0, y(p.v), lineWidth, ink)
		}
	}
	markExtreme(img, r, ext.x0, y(ext.v), ext.v, false)
	summary(img, titleY, width, avg, fmt.Sprintf("low %s bpm", formatValue(ext.v)))
}

// drawHRV draws HRV as one bar per reading window. HRV is a figure per
// 5-minute window rather than a continuous signal, which bars say directly —
// and bars set it apart at a glance from the heart-rate line above. Bars
// encode value as length, so the scale starts at zero; a missing reading is
// simply a missing bar. The highest reading is marked.
func drawHRV(img *image.Gray, r image.Rectangle, s Series, tx timeAxis, titleY, width int) {
	pts, avg := readings(s, tx)
	if len(pts) == 0 {
		drawText(img, labelFace(), r.Min.X+8, r.Min.Y+r.Dy()/2+5, "no readings", muted, alignLeft)
		return
	}
	hi, ext := pts[0].v, pts[0]
	for _, p := range pts {
		if p.v > hi {
			hi, ext = p.v, p
		}
	}
	y := panelScale(img, r, 0, hi, 4)
	base := r.Max.Y - 1
	for _, p := range pts {
		// a bar fills ~60% of its window, leaving a gap to its neighbors
		w := p.x1 - p.x0
		inset := math.Max(1, w*0.2)
		x0, x1 := int(math.Round(p.x0+inset)), int(math.Round(p.x1-inset))
		if x1 <= x0 {
			x1 = x0 + 1
		}
		fillRect(img, image.Rect(x0, int(math.Round(y(p.v))), x1, base), ink)
	}
	markExtreme(img, r, (ext.x0+ext.x1)/2, y(ext.v), ext.v, true)
	summary(img, titleY, width, avg, fmt.Sprintf("high %s ms", formatValue(ext.v)))
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
