package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

// StressDay is one day's cell on the stress calendar, formatted by the caller.
type StressDay struct {
	Day         time.Time
	Minutes     float64 // high-stress time: the dial's outer, solid arc
	Recovery    float64 // high-recovery time, in minutes: the inner, outlined arc
	HasRecovery bool    // false where Oura had no recovery figure
	Text        string  // the high-stress time as text: "1h 30m"
	Summary     string  // Oura's day summary; "stressful" inverts the date strip
	Partial     bool    // the day is still in progress: shown, but no arcs
}

// StatLine is a label and value for a side panel.
type StatLine struct{ Label, Value string }

// StressMonth is the content of the monthly stress calendar.
type StressMonth struct {
	Title    string // "Stress · November 2026"
	Subtitle string
	Month    time.Time   // any day in the month
	Today    time.Time   // outlined if it falls in the month; later days are left blank
	Days     []StressDay // days with data; others draw as "no data"
	Stats    []StatLine
}

// Each day is a dial: a ring of time drawn clockwise from 12 o'clock, high
// stress as a solid outer arc and recovery as an outlined inner arc. One
// fixed scale for every day and month — a full circle is dialFullMinutes —
// so dials compare across the calendar; a day past it gets a full ring and
// a tick at the top. Faint track circles show the full scale, so a short
// arc reads as "a little" rather than floating alone.
const dialFullMinutes = 360

// dialFrac is how much of the circle minutes fills, and whether it ran past.
func dialFrac(minutes float64) (float64, bool) {
	if minutes <= 0 {
		return 0, false
	}
	return math.Min(minutes/dialFullMinutes, 1), minutes > dialFullMinutes
}

// arcBand fills the ring between radii ri and ro from angle a0 to a1, in
// turns clockwise from 12 o'clock (0 to 1), sampling pixel centers.
func arcBand(img *image.Gray, cx, cy, ri, ro, a0, a1 float64, c color.Gray) {
	if a1 <= a0 {
		return
	}
	for y := int(math.Floor(cy - ro)); y <= int(math.Ceil(cy+ro)); y++ {
		for x := int(math.Floor(cx - ro)); x <= int(math.Ceil(cx+ro)); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			r := math.Hypot(dx, dy)
			if r < ri || r > ro {
				continue
			}
			a := math.Atan2(dx, -dy) / (2 * math.Pi) // 0 at 12 o'clock, clockwise
			if a < 0 {
				a++
			}
			if a >= a0 && a <= a1 {
				img.SetGray(x, y, c)
			}
		}
	}
}

// ring draws a 1px circle of radius r: the dial's track.
func ring(img *image.Gray, cx, cy, r float64, c color.Gray) {
	arcBand(img, cx, cy, r-0.5, r+0.5, 0, 1, c)
}

// drawDial draws one day's dial with outer radius R: tracks, then the
// recovery arc (outlined: a band of ink hollowed to its edges), then the
// stress arc (solid), then the past-the-scale ticks.
func drawDial(img *image.Gray, cx, cy, R float64, d StressDay) {
	th := math.Max(3, math.Round(R*0.26)) // band thickness
	so, si := R, R-th                     // stress ring
	ro, ri := si-1.5, si-1.5-th           // recovery ring, just inside
	ring(img, cx, cy, (so+si)/2, grid)
	if ri > 2 {
		ring(img, cx, cy, (ro+ri)/2, grid)
	}
	if d.Partial {
		return
	}
	if d.HasRecovery && ri > 2 {
		if f, over := dialFrac(d.Recovery); f > 0 {
			arcBand(img, cx, cy, ri, ro, 0, f, ink)
			if th >= 4 {
				inset := 1.2 / (2 * math.Pi * ro) // keep a 1px edge at the arc's ends
				arcBand(img, cx, cy, ri+1, ro-1, inset, f-inset, surface)
			}
			if over {
				// the recovery ring sits inside the stress ring; mark its
				// overflow on the inside edge
				fillRect(img, image.Rect(int(cx)-1, int(cy-ri), int(cx)+1, int(cy-ri+4)), ink)
			}
		}
	}
	if f, over := dialFrac(d.Minutes); f > 0 {
		arcBand(img, cx, cy, si, so, 0, f, ink)
		if over { // a short mark past the top of the full ring
			fillRect(img, image.Rect(int(cx)-1, int(cy-so-4), int(cx)+1, int(cy-so)), ink)
		}
	}
}

// outline draws a rectangle's border, t pixels thick, inside r.
func outline(img *image.Gray, r image.Rectangle, t int, c color.Gray) {
	for i := 0; i < t; i++ {
		hline(img, r.Min.X+i, r.Max.X-1-i, r.Min.Y+i, c)
		hline(img, r.Min.X+i, r.Max.X-1-i, r.Max.Y-1-i, c)
		vline(img, r.Min.X+i, r.Min.Y+i, r.Max.Y-1-i, c)
		vline(img, r.Max.X-1-i, r.Min.Y+i, r.Max.Y-1-i, c)
	}
}

// dottedOutline is a 1px border of every other pixel: "there is no data
// here", lighter than a real cell's solid edge.
func dottedOutline(img *image.Gray, r image.Rectangle, c color.Gray) {
	for x := r.Min.X; x < r.Max.X; x += 2 {
		img.SetGray(x, r.Min.Y, c)
		img.SetGray(x, r.Max.Y-1, c)
	}
	for y := r.Min.Y; y < r.Max.Y; y += 2 {
		img.SetGray(r.Min.X, y, c)
		img.SetGray(r.Max.X-1, y, c)
	}
}

// Calendar layout, in pixels.
const (
	calPad    = 16
	calPanelW = 176 // side panel: key and month figures
	calWeekW  = 80  // week totals column
	calGap    = 4   // between cells
	calStripH = 18  // white strip at the top of a cell for its date and time
)

// StressCalendar renders a month as a calendar grid, weeks starting Sunday.
// Each day is a dial of its high-stress and recovery time, under a strip
// with its date and stress time; days Oura summarized as stressful invert
// that strip. A week column totals each week's complete days.
func StressCalendar(m StressMonth, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: stress calendar needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	drawText(img, face(true, 22), calPad, 30, m.Title, ink, alignLeft)
	drawText(img, face(false, 14), calPad, 52, m.Subtitle, muted, alignLeft)

	first := time.Date(m.Month.Year(), m.Month.Month(), 1, 0, 0, 0, 0, time.UTC)
	daysIn := first.AddDate(0, 1, -1).Day()
	offset := int(first.Weekday()) // Sunday = 0
	rows := (offset + daysIn + 6) / 7

	gridL := calPad
	weekR := width - calPad - calPanelW - 18
	gridR := weekR - calWeekW
	headY := 82
	gridT, gridB := headY+8, height-calPad
	cellW := float64(gridR-gridL-6*calGap) / 7
	cellH := float64(gridB-gridT-(rows-1)*calGap) / float64(rows)

	cellRect := func(col, row int) image.Rectangle {
		x0 := gridL + int(float64(col)*(cellW+calGap))
		y0 := gridT + int(float64(row)*(cellH+calGap))
		return image.Rect(x0, y0, x0+int(cellW), y0+int(cellH))
	}
	for col, name := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
		r := cellRect(col, 0)
		drawText(img, face(false, 12), (r.Min.X+r.Max.X)/2, headY, name, muted, alignCenter)
	}
	drawText(img, face(true, 11), gridR+10, headY, "WEEK", muted, alignLeft)

	byDay := map[int]StressDay{}
	for _, d := range m.Days {
		if d.Day.Year() == first.Year() && d.Day.Month() == first.Month() {
			byDay[d.Day.Day()] = d
		}
	}
	today := date(m.Today)

	type weekSum struct {
		stress, recovery float64
		days, recDays    int
	}
	weeks := make([]weekSum, rows)

	for dom := 1; dom <= daysIn; dom++ {
		i := offset + dom - 1
		r := cellRect(i%7, i/7)
		day := first.AddDate(0, 0, dom-1)
		num := fmt.Sprint(dom)
		d, ok := byDay[dom]
		if !day.After(today) { // each day that has come a zone: its daily brief
			addZone("day:"+day.Format("2006-01-02"), r)
		}

		switch {
		case day.After(today):
			dottedOutline(img, r, grid)
			drawText(img, labelFace(), r.Min.X+5, r.Min.Y+14, num, muted, alignLeft) // text: muted stays legible in 1-bit
			continue
		case !ok:
			dottedOutline(img, r, muted)
			drawText(img, labelFace(), r.Min.X+5, r.Min.Y+14, num, muted, alignLeft)
			drawText(img, labelFace(), r.Max.X-5, r.Min.Y+14, "—", muted, alignRight)
			continue
		}

		outline(img, r, 1, ink)
		strip := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+calStripH)
		fg := ink
		if d.Summary == "stressful" {
			fillRect(img, strip, ink) // the day's strip inverted: the strongest mark
			fg = surface
		}
		drawText(img, labelFace(), r.Min.X+5, r.Min.Y+14, num, fg, alignLeft)
		drawText(img, face(true, 11), r.Max.X-4, r.Min.Y+14, d.Text, fg, alignRight)
		if day.Equal(today) {
			outline(img, r, 3, ink)
		}

		body := image.Rect(r.Min.X, strip.Max.Y, r.Max.X, r.Max.Y)
		cx, cy := float64(body.Min.X+body.Max.X)/2, float64(body.Min.Y+body.Max.Y)/2
		R := math.Min(float64(body.Dx()), float64(body.Dy()))/2 - 4
		if R >= 6 {
			drawDial(img, cx, cy, R, d)
		}
		if d.Partial {
			drawText(img, face(false, 10), int(cx), int(cy)+4, "so far", muted, alignCenter)
			continue
		}
		w := &weeks[i/7]
		w.stress += d.Minutes
		w.days++
		if d.HasRecovery {
			w.recovery += d.Recovery
			w.recDays++
		}
	}

	// Week totals: a framed cell beside each week's row, its border heavier
	// than a day's so it reads as a summary. The totals carry the dial's
	// encoding instead of words — a solid swatch for stress, an outlined one
	// for recovery — so the key covers them too.
	for row, w := range weeks {
		r := cellRect(0, row)
		if weekStart := first.AddDate(0, 0, row*7-offset); weekStart.After(today) {
			continue // a week that hasn't started: blank, like its days
		}
		cell := image.Rect(gridR+8, r.Min.Y, weekR, r.Max.Y)
		outline(img, cell, 2, ink)
		hline(img, cell.Min.X, cell.Max.X-1, cell.Min.Y+calStripH-1, ink)
		drawText(img, face(false, 11), cell.Min.X+6, cell.Min.Y+13, "week", muted, alignLeft)
		x := cell.Min.X + 6
		if w.days == 0 {
			drawText(img, labelFace(), x, cell.Min.Y+calStripH+18, "—", muted, alignLeft)
			continue
		}
		line := func(y int, solid bool, minutes float64) {
			sw := image.Rect(x, y-9, x+9, y)
			if solid {
				fillRect(img, sw, ink)
			} else {
				outline(img, sw, 1, ink)
			}
			// sized so the widest week ("16h 30m") clears the border
			drawText(img, face(true, 11), sw.Max.X+4, y, Duration(minutes), ink, alignLeft)
		}
		body := cell.Min.Y + calStripH
		gap := (cell.Max.Y - body) / 3
		if w.recDays > 0 {
			line(body+gap+4, true, w.stress)
			line(body+2*gap+4, false, w.recovery)
		} else {
			line(body+(cell.Max.Y-body)/2+5, true, w.stress)
		}
	}

	drawCalendarPanel(img, image.Rect(width-calPad-calPanelW, gridT, width-calPad, gridB), m.Stats)
	return img, nil
}

// drawCalendarPanel draws the key — a sample dial with its parts named —
// and then the month's figures.
func drawCalendarPanel(img *image.Gray, r image.Rectangle, stats []StatLine) {
	y := r.Min.Y + 12
	drawText(img, face(true, 13), r.Min.X, y, "Each day", ink, alignLeft)

	// a sample dial, with its arcs labeled to the right
	const R = 22.0
	cx, cy := float64(r.Min.X)+R+2, float64(y)+14+R
	drawDial(img, cx, cy, R, StressDay{Minutes: 150, Recovery: 90, HasRecovery: true})
	lx := int(cx + R + 12)
	drawText(img, labelFace(), lx, int(cy)-4, "outer: high stress", ink, alignLeft)
	drawText(img, labelFace(), lx, int(cy)+13, "inner: recovery", ink, alignLeft)
	y = int(cy+R) + 18
	drawText(img, labelFace(), r.Min.X, y, "full circle = 6 hours", muted, alignLeft)
	y += 16
	drawText(img, labelFace(), r.Min.X, y, "week: totals, same marks", muted, alignLeft)

	y += 14
	sw := image.Rect(r.Min.X, y, r.Min.X+30, y+14)
	fillRect(img, sw, ink)
	drawText(img, labelFace(), sw.Max.X+8, y+12, "stressful day", ink, alignLeft)

	y += 38
	drawText(img, face(true, 13), r.Min.X, y, "This month", ink, alignLeft)
	y += 4
	for _, s := range stats {
		y += 18
		drawText(img, labelFace(), r.Min.X, y, s.Label, muted, alignLeft)
		drawText(img, face(true, 13), r.Max.X, y, s.Value, ink, alignRight)
	}
}

// date strips t to midnight UTC on its calendar date.
func date(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
