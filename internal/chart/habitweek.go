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

// HabitBadge is one habit logged on a day: its code, and for a counted
// habit how many and when the last was.
type HabitBadge struct {
	Code    string // "CAF"
	Name    string // "Caffeine", for the key
	Count   float64
	Counted bool
	Last    string // the last log's time, for a counted habit: "3 PM"
	Late    bool   // a meal, sweets or workout close to bed: the badge inverted
}

// HabitDay is one card of the habits week: what was logged that day, and
// the night that followed it.
type HabitDay struct {
	Day     time.Time
	Future  bool // a day still to come
	Badges  []HabitBadge
	Tonight bool // the night after it hasn't happened yet

	HasNight      bool
	Score         string // the next night's sleep score: "88"
	ScoreVal      float64
	Asleep, Deep  string // "9h 30m"
	HRV           string // "64 ms"
	HasScoreValue bool
}

// HabitWeek is the content of the habits week screen.
type HabitWeek struct {
	Title, Subtitle, Headline string
	Days                      [7]HabitDay
	Best, Worst               int // cards whose next night was the best and toughest; -1 for none
	Stats                     []StatLine3
	Key                       []HabitBadge // the codes on the cards, for the key
}

// HabitWeekView renders the habits week in the week review's frame: seven
// day cards, Sunday to Saturday, each listing the habits logged that day
// as coded badges (with how many, and when the last was), and under them
// the night that followed: its sleep score, time asleep, deep sleep and
// HRV. The card whose next night was best has an inverted header, the
// toughest a heavy border, and each says so. Below, the week's totals
// against the week before, and the key to the codes.
func HabitWeekView(w HabitWeek, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: habits week needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad, gap = 16, 6
	drawText(img, face(true, 22), pad, 30, w.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, w.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, w.Headline, ink, alignRight)

	// a short panel keeps the cards' badges by trimming the rest: the
	// night to its score, and the week's totals to one row
	compact := height < 420
	cardsT := 68
	statsH, statRows := 118, 2
	if compact {
		statsH, statRows = 84, 1
	}
	cardsB := height - statsH - 10
	cardW := float64(width-2*pad-6*gap) / 7
	for i, d := range w.Days {
		x0 := pad + int(float64(i)*(cardW+gap))
		drawHabitCard(img, image.Rect(x0, cardsT, x0+int(cardW), cardsB), d, i == w.Best, i == w.Worst, compact)
	}

	top := cardsB + 10
	hline(img, pad, width-pad-1, top, rule)
	drawText(img, face(true, 11), pad, top+16, "THIS WEEK", muted, alignLeft)
	drawText(img, face(false, 11), width-pad, top+16, "next night: the sleep after that day's habits", muted, alignRight)
	colW := (width - 2*pad) / 4
	if len(w.Stats) == 0 {
		drawText(img, face(false, 12), pad, top+44, "Nothing logged this week or last. Say \"Hey Siri, log a habit\".", muted, alignLeft)
	}
	for i, s := range w.Stats {
		if i >= 4*statRows {
			break
		}
		x := pad + (i%4)*colW
		y := top + 38 + (i/4)*34
		// the figure follows its own label: right-aligned at the column's
		// end it sat against the next column's label, so caffeine's 5
		// read as "5 Alcohol"
		lf := face(false, 12)
		drawText(img, lf, x, y, s.Label, muted, alignLeft)
		drawText(img, face(true, 14), x+textWidth(lf, s.Label)+6, y, s.Value, ink, alignLeft)
		drawText(img, face(false, 11), x, y+14, s.Change, muted, alignLeft)
	}
	drawHabitKey(img, pad, height-4, width-pad, w.Key)
	return img, nil
}

// habitPill draws a habit's code in a rounded outline at x, baseline y,
// solid with the code in white for a late one, and returns its right edge.
func habitPill(img *image.Gray, x, y int, code string, late bool) int {
	f := face(true, 9)
	w := max(textWidth(f, code)+10, 30)
	r := image.Rect(x, y-10, x+w, y+3)
	c := ink
	if late {
		roundRect(img, r, 6, 0, true)
		c = surface
	} else {
		roundRect(img, r, 6, 1, false)
	}
	drawText(img, f, x+w/2, y, code, c, alignCenter)
	return r.Max.X
}

// drawHabitKey draws, along baseline y from x0 to x1, each code on the
// cards with its habit, and how a badge reads.
func drawHabitKey(img *image.Gray, x0, y, x1 int, key []HabitBadge) {
	f := face(false, 10)
	hline(img, x0, x1-1, y-16, grid)
	x := habitPill(img, x0, y-2, "CAF", false) + 5
	x = drawTextW(img, f, x, y-2, "2 · 3 PM: two, the last at 3 PM", muted) + 14
	x = habitPill(img, x, y-2, "MEAL", true) + 5
	x = drawTextW(img, f, x, y-2, "solid: close to bed", muted) + 14
	shown := map[string]bool{}
	for _, k := range key {
		if shown[k.Code] {
			continue
		}
		shown[k.Code] = true
		label := strings.ToLower(strings.TrimPrefix(k.Name, "Late "))
		need := 30 + 5 + textWidth(f, label) + 14
		if x+need > x1 {
			break
		}
		x = habitPill(img, x, y-2, k.Code, false) + 5
		x = drawTextW(img, f, x, y-2, label, muted) + 14
	}
}

// drawTextW draws s left-aligned and returns where it ends.
func drawTextW(img *image.Gray, f font.Face, x, y int, s string, c color.Gray) int {
	drawText(img, f, x, y, s, c, alignLeft)
	return x + textWidth(f, s)
}

func drawHabitCard(img *image.Gray, r image.Rectangle, d HabitDay, best, worst, compact bool) {
	head, ok := dayCard(img, r, d.Day, d.Future, best, worst)
	if !ok {
		return
	}

	// the night that followed, at the foot
	nightH := 92
	if compact {
		nightH = 44
	}
	split := r.Max.Y - nightH
	x0, x1 := r.Min.X+6, r.Max.X-6
	small := face(false, 10)

	// the day's habits, as badges
	y := head.Max.Y + 15
	drawText(img, face(true, 9), x0, y, "HAD", muted, alignLeft)
	y += 17
	rowH := 17
	room := (split - 8 - (y - 11)) / rowH
	switch {
	case len(d.Badges) == 0:
		drawText(img, small, x0, y, "nothing logged", muted, alignLeft)
	default:
		for i, b := range d.Badges {
			if i == room-1 && len(d.Badges) > room {
				drawText(img, small, x0, y, fmt.Sprintf("+%d more", len(d.Badges)-i), muted, alignLeft)
				break
			}
			if i >= room {
				break
			}
			px := habitPill(img, x0, y, b.Code, b.Late) + 4
			if b.Counted {
				note := trimFloat(b.Count)
				if b.Last != "" && px+textWidth(small, note+" · "+b.Last) <= x1+2 {
					note += " · " + b.Last
				}
				drawText(img, small, px, y, note, ink, alignLeft)
			}
			y += rowH
		}
	}

	hline(img, x0, x1-1, split, grid)
	caption, cc := "NEXT NIGHT", muted
	switch {
	case best:
		caption, cc = "BEST NIGHT", ink
	case worst:
		caption, cc = "TOUGHEST NIGHT", ink
	}
	cf := face(true, 9)
	if textWidth(cf, caption) > x1-x0 {
		caption = strings.TrimSuffix(caption, " NIGHT")
	}
	drawText(img, cf, x0, split+14, caption, cc, alignLeft)
	if d.Tonight || !d.HasNight {
		note := "no night"
		if d.Tonight {
			note = "tonight"
		}
		drawText(img, small, x0, split+34, note, muted, alignLeft)
		return
	}
	drawText(img, small, x0, split+36, "Sleep", muted, alignLeft)
	drawText(img, face(true, 20), x1, split+38, or(d.Score, "—"), ink, alignRight)
	if compact {
		return
	}
	ry := split + 56
	for _, row := range [][2]string{{"Asleep", d.Asleep}, {"Deep", d.Deep}, {"HRV", d.HRV}} {
		drawText(img, small, x0, ry, row[0], muted, alignLeft)
		drawText(img, face(true, 11), x1, ry, or(row[1], "—"), ink, alignRight)
		ry += 14
	}
}

func trimFloat(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
