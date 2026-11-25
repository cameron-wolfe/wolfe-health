package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// ScoreMonth builds the score calendar for the month containing month:
// every day's sleep, readiness and activity rings, as in the week in
// review, and the month's averages against the month before.
//
// Today's activity score is left out — it isn't settled until the day is
// over — as the week in review leaves it out.
func ScoreMonth(ctx context.Context, db *store.DB, month, today time.Time) (chart.ScoreMonth, error) {
	today = date(today)
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevFirst := first.AddDate(0, -1, 0)
	m := chart.ScoreMonth{
		Title:    "Scores · " + first.Format("January 2006"),
		Subtitle: "Sleep, readiness and activity each day, from the ring",
		Month:    first,
		Today:    today,
	}

	metrics := []struct {
		label, name  string
		completeOnly bool
	}{
		{"Sleep", store.MetricSleep, false},
		{"Readiness", store.MetricReadiness, false},
		{"Activity", store.MetricActivity, true},
	}
	cur := make([]monthAvg, len(metrics))
	prev := make([]monthAvg, len(metrics))
	load := func(metric string, from time.Time, completeOnly bool) (monthAvg, error) {
		samples, err := monthSamples(ctx, db, metric, from)
		if err != nil {
			return nil, err
		}
		out := monthAvg{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			if d.After(today) || (completeOnly && !d.Before(today)) {
				continue
			}
			out[d] = s.Value
		}
		return out, nil
	}
	for i, mt := range metrics {
		var err error
		if cur[i], err = load(mt.name, first, mt.completeOnly); err != nil {
			return m, err
		}
		if prev[i], err = load(mt.name, prevFirst, mt.completeOnly); err != nil {
			return m, err
		}
	}

	// best day: the highest total of the three scores, among days with all
	// three; ties go to the earlier day
	var best time.Time
	bestSum, optimal := -1.0, 0
	for d := first; d.Month() == first.Month() && !d.After(today); d = d.AddDate(0, 0, 1) {
		day := chart.WeekDay{Day: d}
		day.Sleep, day.HasSleep = lookup(cur[0], d)
		day.Readiness, day.HasReadiness = lookup(cur[1], d)
		day.Activity, day.HasActivity = lookup(cur[2], d)
		if !day.HasSleep && !day.HasReadiness && !day.HasActivity {
			continue
		}
		m.Days = append(m.Days, day)
		if day.AllOptimal() {
			optimal++
		}
		if day.HasSleep && day.HasReadiness && day.HasActivity {
			if s := day.Sleep + day.Readiness + day.Activity; s > bestSum {
				best, bestSum = d, s
			}
		}
	}
	if len(m.Days) == 0 {
		m.Stats = []chart.StatLine{{Label: "No scores yet", Value: "—"}}
		return m, nil
	}

	m.StatsNote = "averages"
	hasPrev := false
	for _, p := range prev {
		if len(p) > 0 {
			hasPrev = true
		}
	}
	if hasPrev {
		m.StatsNote += ", change vs " + prevFirst.Format("January")
	}
	for i, mt := range metrics {
		v := "—"
		if c, ok := cur[i].mean(); ok {
			v = score(c)
			if p, ok := prev[i].mean(); ok {
				v += " · " + signedScore(c-p)
			}
		}
		m.Stats = append(m.Stats, chart.StatLine{Label: mt.label, Value: v})
	}
	m.Stats = append(m.Stats, chart.StatLine{Label: "Optimal days", Value: fmt.Sprint(optimal)})
	if bestSum >= 0 {
		m.Stats = append(m.Stats, chart.StatLine{Label: "Best day", Value: best.Format("Jan 2")})
	}
	m.Stats = append(m.Stats, chart.StatLine{Label: "Days scored", Value: fmt.Sprint(len(m.Days))})
	return m, nil
}

func lookup(m monthAvg, d time.Time) (float64, bool) {
	v, ok := m[d]
	return v, ok
}

// signedScore is a change in score points: "+3", "−2", or "same".
func signedScore(diff float64) string {
	r := math.Round(diff)
	switch {
	case r == 0:
		return "same"
	case r < 0:
		return fmt.Sprintf("−%.0f", -r)
	}
	return fmt.Sprintf("+%.0f", r)
}
