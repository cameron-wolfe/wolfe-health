package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"
)

// PhaseMonth is the content of WolfePhase · Month: a month of nights as
// small moons.
type PhaseMonth struct {
	Title, Subtitle, Headline string
	Month                     time.Time       // any day in the month
	Nights                    map[int]Night   // by the day of the month each night began
	Asleep                    map[int]float64 // minutes asleep, by the same day
	Today                     int             // day of the month after which nights are to come; 0 for a past month
	Best, Worst               int             // days to mark; 0 for none

	PanelHead  string // "NOVEMBER"
	PanelNote  string // "21 nights"
	LeadLabel  string // "Asleep, avg"
	LeadValue  string // "8h 2m"
	Rows       []PanelRow
	Conclusion string // a line under the rows: "Bedtime held within 40 minutes"
}

// PhaseMonthView renders WolfePhase · Month: a calendar, Sunday to
// Saturday, each night a small WolfePhase moon, lit from bedtime to waking
// on its 12-hour face, with the time asleep beneath. At this size the
// stages are left out: the shape of the lit side is the point. A steady routine shows as the lit side holding still from night
// to night; a late night turns it clockwise. The longest night's date is
// inverted and the shortest's boxed, as on the other month screens. On the
// right, the WolfeSense column: the month's averages and the key.
func PhaseMonthView(m PhaseMonth, width, height int) (*image.Gray, error) {
	if width < 600 || height < 300 {
		return nil, fmt.Errorf("chart: WolfePhase month needs at least 600x300, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	const pad = 16
	drawText(img, face(true, 22), pad, 30, m.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, m.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, m.Headline, ink, alignRight)

	panelW := 236
	if width < 760 {
		panelW = 200
	}
	cal := image.Rect(pad, 64, width-pad-panelW-12, height-6)

	first := time.Date(m.Month.Year(), m.Month.Month(), 1, 0, 0, 0, 0, time.UTC)
	days := first.AddDate(0, 1, -1).Day()
	lead := int(first.Weekday())
	rows := (lead + days + 6) / 7

	headH := 16
	hf := face(true, 10)
	colW := float64(cal.Dx()) / 7
	for i, n := range []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"} {
		x := float64(cal.Min.X) + (float64(i)+0.5)*colW
		drawTracked(img, hf, int(x)-trackedWidth(hf, n, 1)/2, cal.Min.Y+10, n, 1, muted)
	}
	rowH := float64(cal.Dy()-headH) / float64(rows)
	labelH := 13.0
	R := math.Min(colW/2-7, (rowH-labelH-6)/2)
	df := face(true, 10)
	vf := face(false, 10)
	for day := 1; day <= days; day++ {
		cell := lead + day - 1
		col, row := cell%7, cell/7
		x0 := float64(cal.Min.X) + float64(col)*colW
		y0 := float64(cal.Min.Y+headH) + float64(row)*rowH
		cx := x0 + colW/2
		cy := y0 + 3 + R
		// each night slept a zone: the daily brief of the morning after,
		// which leads with that night
		if m.Today == 0 || day < m.Today {
			wake := time.Date(m.Month.Year(), m.Month.Month(), day+1, 0, 0, 0, 0, time.UTC)
			addZone("day:"+wake.Format("2006-01-02"),
				image.Rect(int(x0), int(y0), int(x0+colW), int(y0+rowH)))
		}

		// the date at the cell's top left: inverted for the best night,
		// boxed for the toughest
		num := fmt.Sprint(day)
		nx, ny := int(x0)+3, int(y0)+11
		nw := textWidth(df, num)
		switch day {
		case m.Best:
			fillRect(img, image.Rect(nx-2, ny-10, nx+nw+2, ny+3), ink)
			drawText(img, df, nx, ny, num, surface, alignLeft)
		case m.Worst:
			outline(img, image.Rect(nx-3, ny-11, nx+nw+3, ny+4), 2, ink)
			drawText(img, df, nx, ny, num, ink, alignLeft)
		default:
			drawText(img, df, nx, ny, num, muted, alignLeft)
		}

		n, ok := m.Nights[day]
		switch {
		case m.Today > 0 && day > m.Today:
			dottedCircle(img, cx, cy, R, grid)
			continue
		case !ok || !n.End.After(n.Start):
			dottedCircle(img, cx, cy, R, muted)
			drawText(img, vf, int(cx), int(cy+R+labelH), "—", muted, alignCenter)
			continue
		}
		drawPhaseDial(img, cx, cy, R, n, phaseDial{})
		thickLine(img, cx, cy-R-1, cx, cy-R-5, 1.5, ink) // 12 o'clock, so the face reads as a clock
		if a, ok := m.Asleep[day]; ok {
			drawText(img, vf, int(cx), int(cy+R+labelH), Duration(a), ink, alignCenter)
		}
	}

	drawPhaseMonthPanel(img, image.Rect(width-pad-panelW, 64, width-pad, height-8), m)
	return img, nil
}

// dottedCircle draws a circle of dots, for a moon with no night to show.
func dottedCircle(img *image.Gray, cx, cy, r float64, c color.Gray) {
	n := int(2 * math.Pi * r / 4)
	for i := 0; i < n; i++ {
		a := float64(i) / float64(n) * 2 * math.Pi
		img.SetGray(int(cx+r*math.Sin(a)), int(cy-r*math.Cos(a)), c)
	}
}

// drawPhaseMonthPanel draws the right-hand column: the month and its
// average, the rows, a conclusion, and the key at the foot.
func drawPhaseMonthPanel(img *image.Gray, r image.Rectangle, m PhaseMonth) {
	chip := func(c color.Gray) func(x, y int) {
		return func(x, y int) { fillRect(img, image.Rect(x, y-9, x+23, y+2), c) }
	}
	drawColumn(img, r, column{
		head: strings.ToUpper(m.PanelHead), headNote: m.PanelNote,
		leadLabel: m.LeadLabel, lead: m.LeadValue,
		rows: m.Rows, takeaway: m.Conclusion,
		key: []keyEntry{
			{"lit: bedtime to waking", chip(windowShade)},
			{"dark: the rest of 12 hours", chip(remShade)},
			{"longest night · shortest night", func(x, y int) {
				fillRect(img, image.Rect(x, y-10, x+12, y+3), ink)
				outline(img, image.Rect(x+13, y-11, x+25, y+4), 2, ink)
			}},
			{"tick: 12 o'clock", func(x, y int) {
				thickLine(img, float64(x+11), float64(y-10), float64(x+11), float64(y+2), 1.5, ink)
			}},
		},
	})
}
