package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// monthAvg is one metric's month: its daily values by date.
type monthAvg map[time.Time]float64

func (m monthAvg) mean() (float64, bool) {
	if len(m) == 0 {
		return 0, false
	}
	var s float64
	for _, v := range m {
		s += v
	}
	return s / float64(len(m)), true
}

// extreme is the date with the highest (or lowest) value; ties go to the
// earliest date, so the answer is stable.
func (m monthAvg) extreme(highest bool) (time.Time, float64, bool) {
	var best time.Time
	var bv float64
	found := false
	for d, v := range m {
		better := !found || (highest && v > bv) || (!highest && v < bv) || (v == bv && d.Before(best))
		if better {
			best, bv, found = d, v, true
		}
	}
	return best, bv, found
}

// MonthReport builds the month-in-review screen for the month containing
// month: each metric's average against the previous month, a calendar of
// nightly sleep scores, and the month's standout days.
//
// The current month covers the days so far. Activity, steps and stress
// leave out today's unfinished day, as the other screens do; time asleep
// comes from each night's main sleep, counted as the night view counts it.
func MonthReport(ctx context.Context, db *store.DB, month, today time.Time) (chart.MonthReport, error) {
	today = date(today)
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	prevFirst := first.AddDate(0, -1, 0)
	prevLast := first.AddDate(0, 0, -1)
	r := chart.MonthReport{Month: first, Today: today, Title: first.Format("January 2006")}

	if first.After(today) {
		r.Subtitle = "Month in review · hasn't started yet"
		return r, nil
	}
	end := last
	if !last.Before(today) {
		end = today
		days := today.Day()
		r.Subtitle = fmt.Sprintf("Month in review · %d %s so far", days, plural(days, "day", "days"))
	} else {
		r.Subtitle = fmt.Sprintf("Month in review · all %d days", last.Day())
	}

	// daily metrics: this month and last
	load := func(metric string, from, to time.Time, completeOnly bool) (monthAvg, error) {
		samples, err := db.Range(ctx, metric, 32, from.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return nil, err
		}
		out := monthAvg{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			if completeOnly && !d.Before(today) {
				continue
			}
			out[d] = s.Value
		}
		return out, nil
	}
	type metric struct {
		label        string
		name         string
		completeOnly bool
		format       func(float64) string
	}
	metrics := []metric{
		{"Sleep score", store.MetricSleep, false, score},
		{"Readiness", store.MetricReadiness, false, score},
		{"Activity", store.MetricActivity, true, score},
		{"Steps", store.MetricSteps, true, thousands},
		{"High stress", store.MetricStress, true, duration},
	}
	cur := map[string]monthAvg{}
	prevLabel := "vs " + prevFirst.Format("Jan")
	for _, m := range metrics {
		c, err := load(m.name, first, end, m.completeOnly)
		if err != nil {
			return r, err
		}
		p, err := load(m.name, prevFirst, prevLast, false)
		if err != nil {
			return r, err
		}
		cur[m.name] = c
		r.Rows = append(r.Rows, reportRow(m.label, c, p, m.format, prevLabel))
	}

	// time asleep, from each night's main period
	asleepCur, err := asleepByNight(ctx, db, first, end)
	if err != nil {
		return r, err
	}
	asleepPrev, err := asleepByNight(ctx, db, prevFirst, prevLast)
	if err != nil {
		return r, err
	}
	r.Rows = append(r.Rows, reportRow("Time asleep", asleepCur, asleepPrev, duration, prevLabel))

	// the calendar, and the best and toughest night by sleep score
	r.Scores = map[int]int{}
	for d, v := range cur[store.MetricSleep] {
		r.Scores[d.Day()] = int(math.Round(v))
	}
	night := func(d time.Time, v float64) string {
		s := fmt.Sprintf("%s · score %.0f", chart.NightOf(d).Format("Jan 2"), v) // the evening the night began
		if a, ok := asleepCur[d]; ok {
			s += " · " + duration(a) + " asleep"
		}
		return s
	}
	if d, v, ok := cur[store.MetricSleep].extreme(true); ok {
		r.Best = d.Day()
		r.Highlights = append(r.Highlights, chart.StatLine{Label: "Best night", Value: night(d, v)})
	}
	if d, v, ok := cur[store.MetricSleep].extreme(false); ok && len(cur[store.MetricSleep]) > 1 {
		r.Worst = d.Day()
		r.Highlights = append(r.Highlights, chart.StatLine{Label: "Toughest night", Value: night(d, v)})
	}
	if d, v, ok := cur[store.MetricSteps].extreme(true); ok {
		r.Highlights = append(r.Highlights, chart.StatLine{Label: "Most active",
			Value: fmt.Sprintf("%s · %s steps", d.Format("Jan 2"), thousands(v))})
	}
	if d, v, ok := cur[store.MetricStress].extreme(true); ok && v > 0 {
		r.Highlights = append(r.Highlights, chart.StatLine{Label: "Most stressful",
			Value: fmt.Sprintf("%s · %s in high stress", d.Format("Jan 2"), duration(v))})
	}
	return r, nil
}

// reportRow words one metric's month against the month before.
func reportRow(label string, cur, prev monthAvg, format func(float64) string, prevLabel string) chart.ReportRow {
	row := chart.ReportRow{Label: label, Value: "—"}
	c, ok := cur.mean()
	if !ok {
		return row
	}
	row.Value = format(c)
	p, ok := prev.mean()
	if !ok {
		return row
	}
	diff := c - p
	size := format(math.Abs(diff))
	switch {
	case size == format(0):
		row.Delta = "same as " + prevLabel[3:]
	case diff < 0:
		row.Delta = "−" + size + " " + prevLabel
	default:
		row.Delta = "+" + size + " " + prevLabel
	}
	return row
}

// asleepByNight is each night's time asleep, in seconds, from its main
// sleep period, for the nights between from and to.
func asleepByNight(ctx context.Context, db *store.DB, from, to time.Time) (monthAvg, error) {
	periods, err := db.SleepPeriodsBetween(ctx, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, err
	}
	byDay := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	out := monthAvg{}
	for day, ps := range byDay {
		p, _ := mainPeriod(ps)
		n, ok := stageTimes(p)
		if !ok || n.Asleep() <= 0 {
			continue
		}
		d, err := time.Parse(dayLayout, day)
		if err != nil {
			return nil, fmt.Errorf("dashboard: bad day %q: %w", day, err)
		}
		out[d] = n.Asleep().Seconds()
	}
	return out, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
