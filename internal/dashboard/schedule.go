package dashboard

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// SleepSchedule builds the sleep-schedule chart for the nights ending on
// the trailing days: each night's main sleep, any other sleep that night,
// and the user's best bedtime window (from the bedtime-payoff screen, over
// PayoffWindowDays), with the usual bedtime and wake time (medians, so one
// late night doesn't move them) and how much they vary.
func SleepSchedule(ctx context.Context, db *store.DB, days int, today time.Time) (chart.SleepSchedule, error) {
	if days < 7 {
		return chart.SleepSchedule{}, fmt.Errorf("dashboard: days must be at least 7, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	s := chart.SleepSchedule{
		Title:    fmt.Sprintf("Sleep schedule · last %d nights", days),
		Subtitle: "Each night from bedtime to getting up, against your best bedtime window",
	}

	// periods a day either side, so sleep that starts the evening before
	// the first night, or is filed under a neighboring day, still lands
	periods, err := db.SleepPeriodsBetween(ctx, from.AddDate(0, 0, -1).Format(dayLayout), to.AddDate(0, 0, 1).Format(dayLayout))
	if err != nil {
		return s, err
	}
	// the best window, as the bedtime-payoff screen works it out
	po, err := BedtimePayoff(ctx, db, PayoffWindowDays, today)
	if err != nil {
		return s, err
	}
	s.BestStart, s.BestEnd, s.HasBest = po.BestStart, po.BestEnd, po.HasBest

	rows := map[time.Time]*chart.SleepNight{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		wd := d.Weekday()
		s.Nights = append(s.Nights, chart.SleepNight{Day: d, Weekend: wd == time.Saturday || wd == time.Sunday})
	}
	for i := range s.Nights {
		rows[s.Nights[i].Day] = &s.Nights[i]
	}

	// the main sleep: Oura's own pick of the day's periods
	byDay := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	mainIDs := map[string]bool{}
	for day, ps := range byDay {
		d, err := time.Parse(dayLayout, day)
		if err != nil {
			return s, fmt.Errorf("dashboard: bad day %q: %w", day, err)
		}
		nt, ok := rows[d]
		if !ok {
			continue
		}
		p, ok := mainPeriod(ps)
		if !ok || p.Type == "rest" {
			continue
		}
		bed, err1 := time.Parse(time.RFC3339, p.BedtimeStart)
		wake, err2 := time.Parse(time.RFC3339, p.BedtimeEnd)
		if err1 != nil || err2 != nil {
			continue
		}
		mainIDs[p.BedtimeStart] = true
		nt.Has, nt.Bed, nt.Wake = true, chart.NightClock(bed), chart.NightClock(wake)
		if nt.Wake < nt.Bed { // up after noon: the bar runs to the axis's end
			nt.Wake = 1080
		}
	}
	// other sleep: in the row of the night its start falls in, 6 PM to noon
	for _, p := range periods {
		if mainIDs[p.BedtimeStart] || p.Type == "rest" {
			continue
		}
		st, err1 := time.Parse(time.RFC3339, p.BedtimeStart)
		en, err2 := time.Parse(time.RFC3339, p.BedtimeEnd)
		if err1 != nil || err2 != nil {
			continue
		}
		nt, ok := rows[nightOf(st)]
		pos := chart.NightClock(st)
		if !ok || pos < 0 {
			continue // an afternoon nap: not part of a night
		}
		end := pos + en.Sub(st).Minutes()
		nt.Extra = append(nt.Extra, [2]float64{pos, math.Min(end, 1080)})
	}
	// the figures
	var beds, wakes, weekday, weekend []float64
	var inBed float64
	inWindow := 0
	for i := range s.Nights {
		nt := &s.Nights[i]
		if !nt.Has {
			continue
		}
		beds = append(beds, nt.Bed)
		wakes = append(wakes, nt.Wake)
		inBed += nt.Wake - nt.Bed
		if nt.Weekend {
			weekend = append(weekend, nt.Bed)
		} else {
			weekday = append(weekday, nt.Bed)
		}
		if s.HasBest && nt.Bed >= s.BestStart && nt.Bed <= s.BestEnd {
			nt.InWindow = true
			inWindow++
		}
	}
	if len(beds) == 0 {
		s.Stats = []chart.StatLine{{Label: "No nights recorded", Value: "—"}}
		return s, nil
	}
	s.UsualBed, s.UsualWake, s.HasUsual = medianOf(beds), medianOf(wakes), true
	bedSpread := spreadOf(beds, s.UsualBed)
	s.Headline = fmt.Sprintf("bedtime %s ± %s", chart.ClockLabel(s.UsualBed), duration(bedSpread*60))
	s.Stats = append(s.Stats,
		chart.StatLine{Label: "Usual bedtime", Value: chart.ClockLabel(s.UsualBed)},
		chart.StatLine{Label: "Bedtime varies", Value: "± " + duration(bedSpread*60)},
		chart.StatLine{Label: "Usual wake", Value: chart.ClockLabel(s.UsualWake)},
		chart.StatLine{Label: "Wake varies", Value: "± " + duration(spreadOf(wakes, s.UsualWake)*60)},
		chart.StatLine{Label: "Time in bed, avg", Value: duration(inBed / float64(len(beds)) * 60)},
	)
	if len(weekday) >= 2 && len(weekend) >= 2 {
		s.Stats = append(s.Stats, chart.StatLine{Label: "Weekend bedtime",
			Value: signedMinutes(medianOf(weekend) - medianOf(weekday))})
	}
	if s.HasBest {
		s.Stats = append(s.Stats,
			chart.StatLine{Label: "Best window", Value: clockRange(s.BestStart, s.BestEnd)},
			chart.StatLine{Label: "Nights inside it", Value: fmt.Sprintf("%d of %d", inWindow, len(beds))})
	}
	return s, nil
}

// nightOf is the wake-up day of the night a time belongs to: a time before
// noon is that morning's night; noon or later, the night ahead.
func nightOf(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if t.Hour() >= 12 {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// medianOf is the median of xs, which it leaves unsorted.
func medianOf(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	return median(c)
}

// spreadOf is the median absolute deviation of xs from mid: how far a
// typical night strays, unswayed by one very late one.
func spreadOf(xs []float64, mid float64) float64 {
	devs := make([]float64, len(xs))
	for i, x := range xs {
		devs[i] = math.Abs(x - mid)
	}
	return medianOf(devs)
}

// signedMinutes words a shift in minutes: "45m later", "20m earlier",
// "same".
func signedMinutes(m float64) string {
	r := math.Round(m)
	switch {
	case r == 0:
		return "same"
	case r > 0:
		return duration(r*60) + " later"
	}
	return duration(-r*60) + " earlier"
}
