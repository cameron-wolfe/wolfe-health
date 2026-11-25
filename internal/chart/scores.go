package chart

import (
	"fmt"
	"image"
	"math"
	"time"
)

// ScoreMonth is the content of the monthly score calendar: the week in
// review's rings for every day of a month.
type ScoreMonth struct {
	Title     string // "Scores · November 2026"
	Subtitle  string
	Month     time.Time // any day in the month
	Today     time.Time // outlined if it falls in the month; later days are left blank
	Days      []WeekDay // days with any score; others draw as "no data"
	StatsNote string    // under the panel's heading: what the figures are
	Stats     []StatLine
}

// ScoreCalendar renders a month as a calendar grid, weeks starting Sunday
// as on the stress calendar. Each day holds its three score rings — sleep,
// readiness, activity, with the wolf in the center of an optimal day — and
// its date in the corner. A week column gives each week's average scores
// and its count of optimal days.
func ScoreCalendar(m ScoreMonth, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: score calendar needs at least 600x360, got %dx%d", width, height)
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

	byDay := map[int]WeekDay{}
	for _, d := range m.Days {
		if d.Day.Year() == first.Year() && d.Day.Month() == first.Month() {
			byDay[d.Day.Day()] = d
		}
	}
	today := date(m.Today)

	type weekSum struct {
		sums    [3]float64
		counts  [3]int
		optimal int
	}
	weeks := make([]weekSum, rows)

	numFace := face(true, 11)
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
			drawText(img, numFace, r.Min.X+4, r.Min.Y+13, num, muted, alignLeft)
			continue
		case !ok:
			dottedOutline(img, r, muted)
			if day.Equal(today) {
				outline(img, r, 3, ink) // today, not scored yet
			}
			drawText(img, numFace, r.Min.X+4, r.Min.Y+13, num, muted, alignLeft)
			drawText(img, labelFace(), (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2+5, "—", muted, alignCenter)
			continue
		}

		if day.Equal(today) {
			outline(img, r, 3, ink)
		} else {
			outline(img, r, 1, ink)
		}
		drawText(img, numFace, r.Min.X+4, r.Min.Y+13, num, ink, alignLeft)

		cx, cy, R := ringSpot(r, textWidth(numFace, num))
		if R >= 12 {
			drawCompactRings(img, cx, cy, R, d)
		}

		w := &weeks[i/7]
		for k, sc := range []struct {
			v   float64
			has bool
		}{{d.Sleep, d.HasSleep}, {d.Readiness, d.HasReadiness}, {d.Activity, d.HasActivity}} {
			if sc.has {
				w.sums[k] += sc.v
				w.counts[k]++
			}
		}
		if d.AllOptimal() {
			w.optimal++
		}
	}

	// Week cells: each row's average scores beside the ring textures, and
	// the week's optimal days in the header beside the wolf.
	for row, w := range weeks {
		r := cellRect(0, row)
		if weekStart := first.AddDate(0, 0, row*7-offset); weekStart.After(today) {
			continue
		}
		cell := image.Rect(gridR+8, r.Min.Y, weekR, r.Max.Y)
		outline(img, cell, 2, ink)
		hline(img, cell.Min.X, cell.Max.X-1, cell.Min.Y+calStripH-1, ink)
		drawText(img, face(false, 11), cell.Min.X+6, cell.Min.Y+13, "week", muted, alignLeft)
		if w.optimal > 0 {
			const sz = 12.0
			drawText(img, face(true, 11), cell.Max.X-6, cell.Min.Y+13, fmt.Sprint(w.optimal), ink, alignRight)
			wx := float64(cell.Max.X-6-textWidth(face(true, 11), fmt.Sprint(w.optimal))) - sz - 2
			DrawWolfMark(img, wx, float64(cell.Min.Y)+3, sz, ink, surface)
		}
		x := cell.Min.X + 6
		body := cell.Min.Y + calStripH
		if w.counts == [3]int{} {
			drawText(img, labelFace(), x, body+(cell.Max.Y-body)/2+5, "—", muted, alignLeft)
			continue
		}
		step := float64(cell.Max.Y-body-2) / 3
		for k := range scoreRings {
			y := body + int(step*float64(k)+step/2) + 5
			ringSwatch(img, image.Rect(x, y-8, x+14, y-2), scoreRings[k].style)
			v := "—"
			if w.counts[k] > 0 {
				v = fmt.Sprintf("%.0f", w.sums[k]/float64(w.counts[k]))
			}
			drawText(img, face(true, 11), cell.Max.X-7, y, v, ink, alignRight)
		}
	}

	drawScorePanel(img, image.Rect(width-calPad-calPanelW, gridT, width-calPad, gridB), m)
	return img, nil
}

// ringSpot places a day's rings in cell r: as large as the cell allows,
// nudged low and right of center, and shrunk until they clear the date of
// width numW in the top-left corner by 2px.
func ringSpot(r image.Rectangle, numW int) (cx, cy, R float64) {
	w, h := float64(r.Dx()), float64(r.Dy())
	corner := [2]float64{float64(r.Min.X + 4 + numW + 1), float64(r.Min.Y + 15)}
	for R = math.Min(w, h)/2 - 3; R > 0; R-- {
		cx = float64(r.Min.X) + w/2 + math.Min(4, math.Max(0, w/2-R-3))
		cy = float64(r.Min.Y) + h/2 + math.Min(3, math.Max(0, h/2-R-3))
		if math.Hypot(cx-corner[0], cy-corner[1]) >= R+2 {
			break
		}
	}
	return cx, cy, R
}

// drawScorePanel draws the key — a sample day's rings with each one named
// — and then the month's figures.
func drawScorePanel(img *image.Gray, r image.Rectangle, m ScoreMonth) {
	y := r.Min.Y + 12
	drawText(img, face(true, 13), r.Min.X, y, "Each day", ink, alignLeft)

	const R = 26.0
	cx, cy := float64(r.Min.X)+R+2, float64(y)+12+R
	drawCompactRings(img, cx, cy, R, WeekDay{
		Sleep: 88, Readiness: 76, Activity: 91, HasSleep: true, HasReadiness: true, HasActivity: true})
	lx := int(cx + R + 12)
	for i, rg := range scoreRings {
		ly := int(cy) - 14 + i*16
		ringSwatch(img, image.Rect(lx, ly-7, lx+14, ly-1), rg.style)
		drawText(img, labelFace(), lx+19, ly, rg.label, ink, alignLeft)
	}
	f := labelFace()
	y = int(cy+R) + 18
	drawText(img, f, r.Min.X, y, "outside in · fuller is better", muted, alignLeft)
	y += 16
	drawText(img, f, r.Min.X, y, "tick = 85, optimal", muted, alignLeft)
	y += 18
	DrawWolfMark(img, float64(r.Min.X), float64(y-12), 15, ink, surface)
	drawText(img, f, r.Min.X+21, y, "all three optimal", muted, alignLeft)
	y += 16
	drawText(img, f, r.Min.X, y, "else how many: 2/3", muted, alignLeft)

	y += 30
	drawText(img, face(true, 13), r.Min.X, y, "This month", ink, alignLeft)
	if m.StatsNote != "" {
		y += 15
		drawText(img, face(false, 11), r.Min.X, y, m.StatsNote, muted, alignLeft)
	}
	y += 2
	for _, s := range m.Stats {
		y += 18
		if y > r.Max.Y {
			break
		}
		drawText(img, labelFace(), r.Min.X, y, s.Label, muted, alignLeft)
		drawText(img, face(true, 13), r.Max.X, y, s.Value, ink, alignRight)
	}
}
