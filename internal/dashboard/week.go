package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// WeekStart is the Sunday of the week containing t: weeks run Sunday to
// Saturday, as on the calendars.
func WeekStart(t time.Time) time.Time {
	d := date(t)
	return d.AddDate(0, 0, -int(d.Weekday()))
}

// WeekReview builds the week-in-review screen for the Sunday-to-Saturday
// week starting at start: each day's sleep, readiness and activity scores, steps
// and high-stress time, and the week's averages against the week before.
//
// For the current week, days after today are blank, and today's activity,
// steps and stress — still accumulating — are left out, as elsewhere.
func WeekReview(ctx context.Context, db *store.DB, start, today time.Time) (chart.WeekReview, error) {
	start, today = date(start), date(today)
	end := start.AddDate(0, 0, 6)
	prevStart := start.AddDate(0, 0, -7)
	w := chart.WeekReview{Title: "Week in review", Best: -1, Worst: -1}
	w.Subtitle = start.Format("Jan 2") + " – " + end.Format("Jan 2")
	if start.Month() == end.Month() {
		w.Subtitle = start.Format("Jan 2") + " – " + end.Format("2")
	}
	if !end.Before(today) && !start.After(today) {
		w.Subtitle += " · so far"
	}

	load := func(metric string, completeOnly bool) (cur, prev monthAvg, err error) {
		samples, err := db.Range(ctx, metric, 15, prevStart.Format(dayLayout), end.Format(dayLayout))
		if err != nil {
			return nil, nil, err
		}
		cur, prev = monthAvg{}, monthAvg{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			if completeOnly && !d.Before(today) {
				continue
			}
			if d.Before(start) {
				prev[d] = s.Value
			} else {
				cur[d] = s.Value
			}
		}
		return cur, prev, nil
	}
	type metric struct {
		label        string
		name         string
		completeOnly bool
		format       func(float64) string
	}
	metrics := []metric{
		{"Sleep", store.MetricSleep, false, score},
		{"Readiness", store.MetricReadiness, false, score},
		{"Activity", store.MetricActivity, true, score},
		{"Steps", store.MetricSteps, true, thousands},
		{"High stress", store.MetricStress, true, duration},
		{"Active cal", store.MetricActiveCal, true, thousands},
		{"Total cal", store.MetricTotalCal, true, thousands},
	}
	cur := map[string]monthAvg{}
	for _, m := range metrics {
		c, p, err := load(m.name, m.completeOnly)
		if err != nil {
			return w, err
		}
		cur[m.name] = c
		w.Stats = append(w.Stats, weekStat(m.label, c, p, m.format))
	}
	asleepCur, err := asleepByNight(ctx, db, start, end)
	if err != nil {
		return w, err
	}
	asleepPrev, err := asleepByNight(ctx, db, prevStart, start.AddDate(0, 0, -1))
	if err != nil {
		return w, err
	}
	w.Stats = append(w.Stats, weekStat("Time asleep", asleepCur, asleepPrev, duration))
	// four across: sleep, readiness and activity, then movement, burn, stress
	order := []string{"Sleep", "Time asleep", "Readiness", "Activity", "Steps", "Active cal", "Total cal", "High stress"}
	byLabel := map[string]chart.StatLine3{}
	for _, st := range w.Stats {
		byLabel[st.Label] = st
	}
	w.Stats = w.Stats[:0]
	for _, l := range order {
		w.Stats = append(w.Stats, byLabel[l])
	}

	for i := 0; i < 7; i++ {
		d := start.AddDate(0, 0, i)
		day := chart.WeekDay{Day: d, Future: d.After(today)}
		if v, ok := cur[store.MetricSleep][d]; ok {
			day.Sleep, day.HasSleep = v, true
		}
		if v, ok := cur[store.MetricReadiness][d]; ok {
			day.Readiness, day.HasReadiness = v, true
		}
		if v, ok := cur[store.MetricActivity][d]; ok {
			day.Activity, day.HasActivity = v, true
		}
		day.Steps, day.Stress, day.Calories = "—", "—", "—"
		if v, ok := cur[store.MetricActiveCal][d]; ok {
			day.Calories = thousands(v)
		}
		if v, ok := cur[store.MetricSteps][d]; ok {
			day.Steps = compactThousands(v)
		}
		if v, ok := cur[store.MetricStress][d]; ok {
			day.Stress = duration(v)
		}
		w.Days[i] = day
	}

	// best and toughest night by sleep score; ties go to the earlier day
	sleeps := 0
	for i, d := range w.Days {
		if !d.HasSleep {
			continue
		}
		sleeps++
		if w.Best < 0 || d.Sleep > w.Days[w.Best].Sleep {
			w.Best = i
		}
		if w.Worst < 0 || d.Sleep < w.Days[w.Worst].Sleep {
			w.Worst = i
		}
	}
	if sleeps < 2 {
		w.Worst = -1 // one night is not both best and toughest
	}
	if s, ok := cur[store.MetricSleep].mean(); ok {
		w.Headline = "sleep " + score(s)
		if r, ok := cur[store.MetricReadiness].mean(); ok {
			w.Headline += " · readiness " + score(r)
		}
	}
	optimal := 0
	for _, d := range w.Days {
		if d.AllOptimal() {
			optimal++
		}
	}
	if optimal > 0 && w.Headline != "" {
		w.Headline += fmt.Sprintf(" · %d %s", optimal, plural(optimal, "optimal day", "optimal days"))
	}
	return w, nil
}

// weekStat words one metric's week: its average and the change from the
// week before.
func weekStat(label string, cur, prev monthAvg, format func(float64) string) chart.StatLine3 {
	st := chart.StatLine3{Label: label, Value: "—"}
	c, ok := cur.mean()
	if !ok {
		return st
	}
	st.Value = format(c)
	p, ok := prev.mean()
	if !ok {
		return st
	}
	diff := c - p
	size := format(math.Abs(diff))
	switch {
	case size == format(0):
		st.Change = "same as last week"
	case diff < 0:
		st.Change = "−" + size + " vs last week"
	default:
		st.Change = "+" + size + " vs last week"
	}
	return st
}

// compactThousands is a count in a narrow column: 6,240 -> "6.2k".
func compactThousands(v float64) string {
	if v < 1000 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1fk", v/1000)
}
