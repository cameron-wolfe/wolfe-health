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

// Week radar.
const (
	weekRadarHistory = 8 // the weeks before that make "your normal"
	weekRadarMinDays = 3 // days with a value for a week's average to count
	weekRadarMinWeek = 4 // weeks of history before there's a normal
)

// weekMeasure is one spoke on the week radar.
type weekMeasure struct {
	label        string
	better       int // +1 higher is better, -1 lower is better: outward is always better
	completeOnly bool
	sdFloor      float64 // the normal range's narrowest spread, in the measure's units
	format       func(float64) string
}

var weekMeasures = []weekMeasure{
	{"Sleep", 1, false, 2, score},
	{"Readiness", 1, false, 2, score},
	{"Activity", 1, true, 2, score},
	{"Steps", 1, true, 400, compactThousands},
	{"Asleep", 1, false, 0.2, func(h float64) string { return duration(h * 3600) }},
	{"HRV", 1, false, 2, func(v float64) string { return fmt.Sprintf("%.0f ms", v) }},
	{"Resting HR", -1, false, 1, func(v float64) string { return fmt.Sprintf("%.0f bpm", v) }},
	{"High stress", -1, true, 600, duration}, // seconds of high stress a day
}

// WeekRadar builds the week radar for the Sunday-to-Saturday week starting
// at start: each measure's average for the week, placed against the user's
// normal (the weekly averages of the 8 weeks before: their median and a
// robust spread), with last week as a dashed outline. Every spoke is
// oriented so outward is better: for resting heart rate and high-stress
// time, lower is better, so they're turned round.
//
// For the current week, days after today are blank and today's activity,
// steps and stress, still accumulating, are left out, as on the week in
// review.
func WeekRadar(ctx context.Context, db *store.DB, start, today time.Time) (chart.DeviationRadar, error) {
	start, today = date(start), date(today)
	end := start.AddDate(0, 0, 6)
	early := start.AddDate(0, 0, -7*weekRadarHistory)
	d := chart.DeviationRadar{
		Title:      "WolfeSense · Week · " + weekSpan(start, end),
		Subtitle:   "This week's averages against your last 8 weeks: outward is better",
		ShapeLabel: "this week",
		AvgLabel:   "last week",
		Inverted:   "inverted: worse than usual",
	}
	if !end.Before(today) && !start.After(today) {
		d.Title += " · so far"
	}
	days := int(end.Sub(early).Hours()/24) + 1

	metric := func(name string) (map[time.Time]float64, error) {
		samples, err := db.Range(ctx, name, days+1, early.Format(dayLayout), end.Format(dayLayout))
		if err != nil {
			return nil, err
		}
		out := map[time.Time]float64{}
		for _, s := range samples {
			dd, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			out[dd] = s.Value
		}
		return out, nil
	}
	var series []map[time.Time]float64
	for _, name := range []string{store.MetricSleep, store.MetricReadiness, store.MetricActivity, store.MetricSteps} {
		m, err := metric(name)
		if err != nil {
			return d, err
		}
		series = append(series, m)
	}
	asleep, err := asleepHours(ctx, db, early, end)
	if err != nil {
		return d, err
	}
	hrv, hr, _, err := loadSignals(ctx, db, early, end, days+1)
	if err != nil {
		return d, err
	}
	stress, err := metric(store.MetricStress)
	if err != nil {
		return d, err
	}
	series = append(series, asleep, hrv, hr, stress)

	// a week's average of one measure, over its days up to today
	weekAvg := func(vals map[time.Time]float64, ws time.Time, completeOnly bool) (float64, bool) {
		var sum float64
		n := 0
		for i := 0; i < 7; i++ {
			day := ws.AddDate(0, 0, i)
			if day.After(today) || (completeOnly && !day.Before(today)) {
				continue
			}
			if v, ok := vals[day]; ok {
				sum += v
				n++
			}
		}
		if n < weekRadarMinDays {
			return 0, false
		}
		return sum / float64(n), true
	}

	var better, worse []string
	inNormal, read := 0, 0
	for i, m := range weekMeasures {
		vals := series[i]
		s := chart.DeviationSpoke{Label: m.label}
		var history []float64
		for k := 1; k <= weekRadarHistory; k++ {
			if v, ok := weekAvg(vals, start.AddDate(0, 0, -7*k), m.completeOnly); ok {
				history = append(history, v)
			}
		}
		cur, has := weekAvg(vals, start, m.completeOnly)
		if len(history) >= weekRadarMinWeek && has {
			sorted := append([]float64(nil), history...)
			sort.Float64s(sorted)
			mid := median(sorted)
			devs := make([]float64, len(sorted))
			for j, v := range sorted {
				devs[j] = math.Abs(v - mid)
			}
			sort.Float64s(devs)
			half := bandSD * math.Max(1.4826*median(devs), m.sdFloor)
			dev := func(v float64) float64 { return float64(m.better) * (v - mid) / half }
			s.D, s.Has, s.Text = dev(cur), true, m.format(cur)
			s.Alert = s.D < -1 // worse than usual
			if last, ok := weekAvg(vals, start.AddDate(0, 0, -7), m.completeOnly); ok {
				s.AvgD, s.HasAvg = dev(last), true
				s.Change = signedChange(cur-last, m.format)
			}
			read++
			switch {
			case s.D > 1:
				better = append(better, m.label)
			case s.D < -1:
				worse = append(worse, m.label)
			default:
				inNormal++
			}
		}
		d.Spokes = append(d.Spokes, s)
	}

	d.PanelTitle, d.PanelVs = "THIS WEEK", "vs last week"
	if len(worse) > 0 {
		d.TakeawayHead, d.Takeaway = "Worse than usual:", worse
	} else {
		d.TakeawayHead, d.Takeaway = "Better than usual:", better
	}
	d.TakeawayNone = "Every measure within your usual"
	if read == 0 {
		d.Headline = "not enough weeks yet"
		d.SummaryLabel, d.Summary = "Needs", fmt.Sprintf("%d weeks", weekRadarMinWeek)
		return d, nil
	}
	switch {
	case len(better) == 0 && len(worse) == 0:
		d.Headline = "a usual week"
	default:
		d.Headline = fmt.Sprintf("%d better than usual, %d worse", len(better), len(worse))
	}
	d.SummaryLabel, d.Summary = "Usual", fmt.Sprintf("%d/%d", inNormal, read)
	return d, nil
}

// signedChange words a change with a measure's own format: "+3", "−1.2k",
// "+25m", "same".
func signedChange(diff float64, format func(float64) string) string {
	size := format(math.Abs(diff))
	if size == format(0) {
		return "same"
	}
	if diff < 0 {
		return "−" + size
	}
	return "+" + size
}

// weekSpan words a week's dates: "Nov 8 – 14", or "Oct 25 – Nov 1".
func weekSpan(start, end time.Time) string {
	if start.Month() == end.Month() {
		return start.Format("Jan 2") + " – " + end.Format("2")
	}
	return start.Format("Jan 2") + " – " + end.Format("Jan 2")
}
