package chart

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"golang.org/x/image/font"
)

// Range places one value within its recent spread: the low and high of the
// last month, a typical (median) value, and the value itself. It is drawn as
// a range strip — a calmer and more useful answer to "is today normal for
// me?" than a jittery sparkline of daily scores.
type Range struct {
	Has                     bool
	Low, High, Typical, Now float64
	LowLabel, HighLabel     string // the ends, formatted: "58", "1h 5m"
}

// Metric is one figure on the daily screen, formatted by the caller.
type Metric struct {
	Label string // "Sleep"
	Note  string // which day it is from, when not today: "yesterday"
	Value string // "66", or "—" with no data
	Delta string // "−20 vs 7-day avg"; empty to omit
	Range Range
}

// Daily is the content of the daily screen.
type Daily struct {
	Date time.Time

	Hero          Metric // readiness: the one large figure
	Verdict       string // "Below your usual"
	VerdictDetail string // "22 under your 7-day average"

	Metrics [4]Metric // sleep, activity, steps, stress

	NightTitle string     // "Last night"
	NightWhen  string     // "11:02 PM → 5:10 AM · 5h 19m asleep"
	Stages     StageNight // zero when there is no night to show
	HeldBack   string     // "Held back by: Total sleep 47 · REM 49"

	// Phase is that night in full, for the brief's WolfePhase moon; drawn
	// when HasPhase.
	Phase    Night
	HasPhase bool

	// Parts is the readiness score's parts against their average, for the
	// brief's readiness radar; with HasParts false the brief draws its
	// gauge instead.
	Parts    SleepRadar
	HasParts bool

	// Rings are today's three rings, as on the week in review, with the
	// inner ring as progress toward Oura's activity goal (ActivityGoal).
	// GoalNote words that progress: "1.4 km to go", "goal met". Drawn when
	// HasRings.
	Rings    WeekDay
	HasRings bool
	GoalNote string
	CalNote  string // active calories against the goal: "310 of 500 cal"
	TotalCal string // today's total burn so far, resting included: "2,610"
}

// dl is the daily layout at one scale. Every size below is written for an
// 800x480 panel and multiplied by k, so a 640x384 panel gets the same design
// at 80% rather than overlapping bands, and a larger panel scales up.
type dl struct{ k float64 }

func (l dl) px(v float64) int { return int(math.Round(v * l.k)) }

func (l dl) face(bold bool, size float64) font.Face {
	return face(bold, math.Round(size*l.k*2)/2) // half-point steps keep the face cache small
}

// DailyView renders the daily digest, the first daily brief's layout, in three bands
// separated by thin rules rather than boxed into a grid: readiness as the
// hero with a plain-language verdict and its range strip; four metrics, each
// a value, its change, and a range strip; and last night as one stage bar
// with only the parts of sleep that held it back named. Hierarchy comes from
// size and whitespace — one large figure, everything else quieter.
func DailyView(d Daily, width, height int) (*image.Gray, error) {
	if width < 480 || height < 288 {
		return nil, fmt.Errorf("chart: daily view needs at least 480x288, got %dx%d", width, height)
	}
	l := dl{k: math.Min(float64(width)/800, float64(height)/480)}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	pad := l.px(24)
	right := width - pad

	headY := l.px(38)
	drawText(img, l.face(true, 22), pad, headY, d.Date.Format("Monday, January 2"), ink, alignLeft)

	heroTop := headY + l.px(18)
	heroBot := heroTop + l.px(150)
	metTop := heroBot + l.px(14)
	metBot := metTop + l.px(125)
	nightTop := metBot + l.px(14)

	heroRight := right
	if d.HasRings {
		// today's rings take the top right, from the header down through
		// the hero band; the verdict column gives way
		block := l.px(320)
		heroRight = right - block - l.px(24)
		l.rings(img, image.Rect(right-block, l.px(12), right, heroBot), d)
	}
	l.hero(img, image.Rect(pad, heroTop, heroRight, heroBot), d)
	hline(img, pad, right-1, heroBot+l.px(6), rule)
	l.metrics(img, image.Rect(pad, metTop, right, metBot), d.Metrics)
	hline(img, pad, right-1, metBot+l.px(6), rule)
	l.night(img, image.Rect(pad, nightTop, right, height), d)
	return img, nil
}

// caps is the small uppercase label style used to name each section.
func (l dl) caps(img *image.Gray, x, y int, s string) {
	drawText(img, l.face(true, 12), x, y, strings.ToUpper(s), muted, alignLeft)
}

func (l dl) hero(img *image.Gray, r image.Rectangle, d Daily) {
	l.caps(img, r.Min.X, r.Min.Y+l.px(14), d.Hero.Label+noteSuffix(d.Hero.Note))
	numFace := l.face(true, 92)
	drawText(img, numFace, r.Min.X-l.px(4), r.Min.Y+l.px(102), d.Hero.Value, ink, alignLeft)

	// the verdict column starts after the number, whatever its width
	x := r.Min.X + max(textWidth(numFace, d.Hero.Value), textWidth(numFace, "88")) + l.px(28)
	drawText(img, l.face(true, 24), x, r.Min.Y+l.px(50), d.Verdict, ink, alignLeft)
	drawText(img, l.face(false, 15), x, r.Min.Y+l.px(74), d.VerdictDetail, muted, alignLeft)
	strip := image.Rect(x, r.Min.Y+l.px(92), r.Max.X, r.Min.Y+l.px(104))
	l.rangeStrip(img, strip, d.Hero.Range, "30-day range")
}

// rings draws today's score rings at the left of r, with rows naming them
// to the right: the week in review's card, turned sideways.
func (l dl) rings(img *image.Gray, r image.Rectangle, d Daily) {
	day := d.Rings
	R := math.Min(float64(r.Dy())/2-float64(l.px(10)), float64(l.px(86)))
	cx, cy := float64(r.Min.X)+R+float64(l.px(10)), float64(r.Min.Y+r.Max.Y)/2
	drawScoreRings(img, cx, cy, R, day)

	x0 := int(cx+R) + l.px(22)
	x1 := r.Max.X
	y := int(cy) - l.px(62)
	l.caps(img, x0, y, "Today")
	y += l.px(26)
	scores := []struct {
		v   float64
		has bool
	}{{day.Sleep, day.HasSleep}, {day.Readiness, day.HasReadiness}, {day.Activity, day.HasActivity}}
	for i, sc := range scores {
		ringSwatch(img, image.Rect(x0, y-l.px(9), x0+l.px(14), y-l.px(3)), scoreRings[i].style)
		label, v := scoreRings[i].label, "—"
		if sc.has {
			v = fmt.Sprintf("%.0f", sc.v)
		}
		if i == 2 && day.ActivityGoal {
			if sc.has { // percent of the goal; the note says so
				v += "%"
			}
		}
		drawText(img, l.face(false, 12), x0+l.px(19), y, label, muted, alignLeft)
		drawText(img, l.face(true, 15), x1, y, v, ink, alignRight)
		y += l.px(24)
	}
	// under the activity row: the burn behind the ring, then what's left
	for _, note := range []string{d.CalNote, d.GoalNote} {
		if note != "" {
			drawText(img, l.face(false, 11), x0+l.px(19), y-l.px(10), note, muted, alignLeft)
			y += l.px(13)
		}
	}
	y += l.px(10)
	drawText(img, l.face(false, 12), x0, y, "Total burn", muted, alignLeft)
	drawText(img, l.face(true, 15), x1, y, d.TotalCal, ink, alignRight)
	key := "tick: 85, optimal"
	if day.ActivityGoal {
		key = "tick: 85 · inner full: goal"
	}
	drawText(img, l.face(false, 11), x0, y+l.px(18), key, muted, alignLeft)
}

func (l dl) metrics(img *image.Gray, r image.Rectangle, ms [4]Metric) {
	colW := float64(r.Dx()) / 4
	gutter := l.px(28)
	for i, m := range ms {
		x0 := r.Min.X + int(float64(i)*colW)
		x1 := r.Min.X + int(float64(i+1)*colW) - gutter
		if i == 3 {
			x1 = r.Max.X
		}
		l.caps(img, x0, r.Min.Y+l.px(14), m.Label+noteSuffix(m.Note))
		drawText(img, l.face(true, 36), x0-l.px(2), r.Min.Y+l.px(56), m.Value, ink, alignLeft)
		drawText(img, l.face(false, 14), x0, r.Min.Y+l.px(78), m.Delta, ink, alignLeft)
		l.rangeStrip(img, image.Rect(x0, r.Min.Y+l.px(92), x1, r.Min.Y+l.px(104)), m.Range, "")
	}
}

func noteSuffix(note string) string {
	if note == "" || note == "today" {
		return ""
	}
	return " · " + note
}

// rangeStrip draws a range strip in r: a track from the month's low to its
// high, a tick at the typical value, and a ringed dot for now, with the ends
// labeled underneath. label, if set, is centered between the end labels.
// Line weights stay whole pixels at any scale; only lengths and the dot grow.
func (l dl) rangeStrip(img *image.Gray, r image.Rectangle, rg Range, label string) {
	if !rg.Has {
		drawText(img, l.face(false, 13), r.Min.X, r.Max.Y, "no recent data", muted, alignLeft)
		return
	}
	dot := 5 * l.k
	left, right := float64(r.Min.X)+dot+2, float64(r.Max.X)-dot-2
	mid := float64(r.Min.Y+r.Max.Y) / 2
	x := func(v float64) float64 {
		if rg.High <= rg.Low {
			return (left + right) / 2
		}
		return left + (v-rg.Low)/(rg.High-rg.Low)*(right-left)
	}
	cap, tick := l.px(4), l.px(7)
	// track: a 2px line with end caps, ink so it holds on a 1-bit panel
	fillRect(img, image.Rect(int(left), int(mid)-1, int(right)+1, int(mid)+1), ink)
	vline(img, int(left), int(mid)-cap, int(mid)+cap-1, ink)
	vline(img, int(right), int(mid)-cap, int(mid)+cap-1, ink)
	// typical: a taller tick
	tx := int(math.Round(x(rg.Typical)))
	fillRect(img, image.Rect(tx-1, int(mid)-tick, tx+1, int(mid)+tick), ink)
	// now: a ringed dot, over the track and tick
	nx := x(rg.Now)
	disc(img, nx, mid, dot+2, surface)
	disc(img, nx, mid, dot, ink)

	ly := r.Max.Y + l.px(16)
	f := l.face(false, 11)
	drawText(img, f, int(left), ly, rg.LowLabel, muted, alignCenter)
	drawText(img, f, int(right), ly, rg.HighLabel, muted, alignCenter)
	if label != "" {
		drawText(img, f, (int(left)+int(right))/2, ly, label, muted, alignCenter)
	}
}

func (l dl) night(img *image.Gray, r image.Rectangle, d Daily) {
	l.caps(img, r.Min.X, r.Min.Y+l.px(14), d.NightTitle)
	drawText(img, l.face(false, 15), r.Min.X+textWidth(l.face(true, 12), strings.ToUpper(d.NightTitle))+l.px(14),
		r.Min.Y+l.px(14), d.NightWhen, ink, alignLeft)

	n := d.Stages
	if n.InBed() <= 0 {
		drawText(img, l.face(false, 13), r.Min.X, r.Min.Y+l.px(44), "No sleep recorded", muted, alignLeft)
		return
	}
	// one bar, proportional, in the stage chart's textures and stack order
	bar := image.Rect(r.Min.X, r.Min.Y+l.px(28), r.Max.X, r.Min.Y+l.px(52))
	total := float64(n.InBed())
	x := float64(bar.Min.X)
	lf := l.face(false, 12)
	for _, s := range stackOrder {
		w := float64(n.of(s)) / total * float64(bar.Dx())
		if w <= 0 {
			continue
		}
		seg := image.Rect(int(math.Round(x)), bar.Min.Y, int(math.Round(x+w))-2, bar.Max.Y)
		fillStage(img, seg, s)
		if s == StageDeep {
			outline(img, seg, 1, ink)
		}
		// name and length under the segment, when there is room
		text := stageNames[s] + " " + hm(n.of(s))
		if textWidth(lf, text)+6 <= seg.Dx() {
			drawText(img, lf, seg.Min.X, bar.Max.Y+l.px(15), text, ink, alignLeft)
		} else if textWidth(lf, hm(n.of(s)))+4 <= seg.Dx() {
			drawText(img, lf, seg.Min.X, bar.Max.Y+l.px(15), hm(n.of(s)), ink, alignLeft)
		}
		x += w
	}
	drawText(img, l.face(false, 15), r.Min.X, bar.Max.Y+l.px(42), d.HeldBack, ink, alignLeft)
}
