package chart

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

// Night clock positions are minutes after 6 PM on the evening the night
// begins, so a night reads left to right without wrapping at midnight: 0 is
// 6 PM, 360 midnight, 1080 noon the next day.
const (
	nightAxisMin = 1080.0 // the axis runs 6 PM to noon
	nightStartHr = 18
)

// NightClock is a time's place on the night axis: minutes after 6 PM,
// counting a morning time as the same night's. A time between noon and 6 PM
// counts as an early evening (negative), so a nap then sits off the left.
func NightClock(t time.Time) float64 {
	m := float64(t.Hour()*60+t.Minute()) + float64(t.Second())/60
	pos := math.Mod(m-nightStartHr*60+1440, 1440)
	if pos > nightAxisMin {
		pos -= 1440
	}
	return pos
}

// ClockLabel words a night-axis position as a time of day: "10:12 PM".
func ClockLabel(pos float64) string {
	m := int(math.Round(pos)) + nightStartHr*60
	m = ((m % 1440) + 1440) % 1440
	return time.Date(2000, 1, 1, m/60, m%60, 0, 0, time.UTC).Format("3:04 PM")
}

// SleepNight is one night's row: positions on the night axis.
type SleepNight struct {
	Day       time.Time // the wake-up day
	Has       bool      // a main sleep was recorded
	Bed, Wake float64   // the main sleep, in bed to out
	Extra     [][2]float64
	Weekend   bool // a Friday or Saturday night
	InWindow  bool // the bedtime fell in the best window
}

// SleepSchedule is the content of the sleep-schedule chart.
type SleepSchedule struct {
	Title, Subtitle string
	Headline        string
	Nights          []SleepNight // oldest first
	UsualBed        float64
	UsualWake       float64
	HasUsual        bool
	// BestStart and BestEnd are the user's best bedtime window, from the
	// bedtime-payoff screen: drawn as a light gray column behind the nights.
	BestStart, BestEnd float64
	HasBest            bool
	Stats              []StatLine
}

// Schedule layout, in pixels.
const (
	schedPanelW = 176
	schedLabelW = 46
)

// ScheduleView renders the sleep schedule: one row per night, oldest at the
// top, on an axis from 6 PM to noon. Each night's main sleep is a solid bar
// from bedtime to getting up, other sleep that night (a nap, a broken
// stretch) an outlined one, and the user's best bedtime window (worked out
// from their own nights, as on the bedtime-payoff screen) a light gray
// column behind them all. Dashed lines mark the usual bedtime and
// wake time, so a late night stands out as a bar starting right of the
// line. Weekend nights' dates are bold. A side panel holds the key and the
// figures.
func ScheduleView(s SleepSchedule, width, height int) (*image.Gray, error) {
	if width < 600 || height < 360 {
		return nil, fmt.Errorf("chart: sleep schedule needs at least 600x360, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)

	const pad, headH, footH = 16, 22, 26
	drawText(img, face(true, 22), pad, 30, s.Title, ink, alignLeft)
	drawText(img, face(false, 14), pad, 52, s.Subtitle, muted, alignLeft)
	drawText(img, face(true, 16), width-pad, 30, s.Headline, ink, alignRight)

	plotL, plotR := pad+schedLabelW, width-pad-schedPanelW-18
	top := 64 + headH
	plotB := height - footH
	x := func(pos float64) float64 {
		pos = math.Max(0, math.Min(nightAxisMin, pos))
		return float64(plotL) + pos/nightAxisMin*float64(plotR-plotL)
	}

	// the best window, behind everything: the hour lines stay on top
	if s.HasBest {
		fillRect(img, image.Rect(int(math.Round(x(s.BestStart))), top-4, int(math.Round(x(s.BestEnd)))+1, plotB), windowShade)
	}
	// hours: a hairline every 3, midnight darker, labels above and below
	hourLabel := map[int]string{0: "6 PM", 3: "9 PM", 6: "12 AM", 9: "3 AM", 12: "6 AM", 15: "9 AM", 18: "noon"}
	for h := 0; h <= 18; h += 3 {
		cx := int(math.Round(x(float64(h * 60))))
		c := grid
		if h == 6 {
			c = muted
		}
		vline(img, cx, top-4, plotB, c)
		a := alignCenter
		if h == 0 {
			a = alignLeft
		} else if h == 18 {
			a = alignRight
		}
		drawText(img, face(false, 11), cx, top-8, hourLabel[h], muted, a)
		drawText(img, face(false, 11), cx, plotB+15, hourLabel[h], muted, a)
	}

	n := len(s.Nights)
	if n == 0 {
		drawText(img, labelFace(), plotL+8, (top+plotB)/2, "no nights recorded yet", muted, alignLeft)
		drawSchedulePanel(img, image.Rect(width-pad-schedPanelW, top-14, width-pad, height-pad), s.Stats, s.HasBest)
		return img, nil
	}
	rowH := float64(plotB-top) / float64(n)
	barH := math.Max(3, math.Min(rowH-4, 12))
	labelF := face(false, math.Min(11, math.Max(8, rowH-1)))
	boldF := face(true, math.Min(11, math.Max(8, rowH-1)))
	everyOther := rowH < 9 // too tight for every date

	for i, nt := range s.Nights {
		cy := float64(top) + (float64(i)+0.5)*rowH
		y0 := int(math.Round(cy - barH/2))
		y1 := int(math.Round(cy + barH/2))

		if !everyOther || (n-1-i)%2 == 0 {
			f := labelF
			if nt.Weekend {
				f = boldF
			}
			drawText(img, f, plotL-6, int(cy)+4, NightOf(nt.Day).Format("Mon 2"), ink, alignRight) // the evening it began
		}

		for _, e := range nt.Extra {
			r := image.Rect(int(math.Round(x(e[0]))), y0+1, int(math.Round(x(e[1])))+1, y1)
			if r.Dx() < 2 {
				r.Max.X = r.Min.X + 2
			}
			fillRect(img, r, surface)
			outline(img, r, 1, ink)
		}
		if !nt.Has {
			drawText(img, labelF, plotL+4, int(cy)+4, "—", muted, alignLeft)
			continue
		}
		r := image.Rect(int(math.Round(x(nt.Bed))), y0, int(math.Round(x(nt.Wake)))+1, y1+1)
		fillRect(img, r, ink)
	}

	// the usual bedtime and wake: dashed, over the bars
	if s.HasUsual {
		for _, pos := range []float64{s.UsualBed, s.UsualWake} {
			ux := int(math.Round(x(pos)))
			for y := top; y < plotB; y++ {
				switch {
				case (y/4)%2 == 0:
					img.SetGray(ux, y, ink)
					img.SetGray(ux+1, y, ink)
				default:
					img.SetGray(ux, y, surface)
					img.SetGray(ux+1, y, surface)
				}
			}
		}
	}

	drawSchedulePanel(img, image.Rect(width-pad-schedPanelW, top-14, width-pad, height-pad), s.Stats, s.HasBest)
	return img, nil
}

// drawSchedulePanel draws the key, each mark as on the chart, then the
// figures.
func drawSchedulePanel(img *image.Gray, r image.Rectangle, stats []StatLine, best bool) {
	key := []keyEntry{
		{"in bed, main sleep", centered(func(cx, cy int) { fillRect(img, image.Rect(cx-10, cy-5, cx+11, cy+5), ink) })},
		{"a nap or extra sleep", centered(func(cx, cy int) { outline(img, image.Rect(cx-6, cy-4, cx+7, cy+4), 1, ink) })},
	}
	if best {
		key = append(key, keyEntry{"your best window", centered(func(cx, cy int) {
			fillRect(img, image.Rect(cx-10, cy-6, cx+11, cy+6), windowShade)
		})})
	}
	key = append(key,
		keyEntry{"your usual bed, wake", centered(func(cx, cy int) {
			for yy := cy - 7; yy <= cy+5; yy++ {
				if (yy/4)%2 == 0 {
					img.SetGray(cx, yy, ink)
					img.SetGray(cx+1, yy, ink)
				}
			}
		})},
		keyEntry{"weekend night", centered(func(cx, cy int) { drawText(img, face(true, 11), cx-10, cy+4, "Sat", ink, alignLeft) })},
	)
	drawKeyAndFigures(img, r, key, stats)
}

// windowShade fills a bedtime window: a light gray that 2-bit output
// keeps as its light gray and 1-bit output draws as a sparse dot pattern.
var windowShade = color.Gray{Y: 196}
