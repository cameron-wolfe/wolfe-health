package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// StressMonth builds the stress calendar for the month containing month.
//
// Every stored day in the month becomes a cell. Today's is marked partial —
// stress accumulates through the day — and left out of the month's figures,
// as the daily view leaves it out too. The side panel compares the month's
// daily average with the previous month's and names the most stressful day.
func StressMonth(ctx context.Context, db *store.DB, month, today time.Time) (chart.StressMonth, error) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	m := chart.StressMonth{
		Title:    "Stress · " + first.Format("January 2006"),
		Subtitle: "High-stress and recovery time each day, from the ring",
		Month:    first,
		Today:    date(today),
	}

	cur, err := monthSamples(ctx, db, store.MetricStress, first)
	if err != nil {
		return m, err
	}
	recSamples, err := monthSamples(ctx, db, store.MetricRecovery, first)
	if err != nil {
		return m, err
	}
	recovery := map[string]float64{} // by day, in seconds
	for _, s := range recSamples {
		recovery[s.Day] = s.Value
	}
	prev, err := monthSamples(ctx, db, store.MetricStress, first.AddDate(0, -1, 0))
	if err != nil {
		return m, err
	}

	var total, recTotal float64
	var recDays int
	var most store.Sample
	var complete []store.Sample
	counts := map[string]int{}
	for _, s := range cur {
		day, err := time.Parse(dayLayout, s.Day)
		if err != nil {
			return m, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
		}
		d := chart.StressDay{Day: day, Minutes: s.Value / 60, Text: duration(s.Value)}
		if r, ok := recovery[s.Day]; ok {
			d.Recovery, d.HasRecovery = r/60, true
		}
		if !day.Before(m.Today) {
			d.Partial = true
			m.Days = append(m.Days, d)
			continue
		}
		complete = append(complete, s)
		d.Summary = s.Qualifier // the chart marks the exceptions; normal is unmarked
		counts[s.Qualifier]++
		m.Days = append(m.Days, d)
		total += s.Value
		if r, ok := recovery[s.Day]; ok {
			recTotal += r
			recDays++
		}
		if s.Value > most.Value {
			most = s
		}
	}
	if len(complete) == 0 {
		m.Stats = []chart.StatLine{{Label: "No complete days yet", Value: "—"}}
		return m, nil
	}

	avg := total / float64(len(complete))
	m.Stats = append(m.Stats, chart.StatLine{Label: "Daily average", Value: duration(avg)})
	if len(prev) > 0 {
		var pt float64
		for _, s := range prev {
			pt += s.Value
		}
		diff := avg - pt/float64(len(prev))
		label := "vs " + first.AddDate(0, -1, 0).Format("January")
		m.Stats = append(m.Stats, chart.StatLine{Label: label, Value: signedDuration(diff)})
	}
	if recDays > 0 {
		m.Stats = append(m.Stats, chart.StatLine{Label: "Recovery, daily", Value: duration(recTotal / float64(recDays))})
	}
	if most.Value > 0 {
		d, _ := time.Parse(dayLayout, most.Day)
		m.Stats = append(m.Stats, chart.StatLine{
			Label: "Peak day", Value: d.Format("Jan 2") + " · " + duration(most.Value)})
	}
	m.Stats = append(m.Stats,
		chart.StatLine{Label: "Stressful days", Value: fmt.Sprint(counts["stressful"])},
		chart.StatLine{Label: "Restored days", Value: fmt.Sprint(counts["restored"])},
		chart.StatLine{Label: "Days counted", Value: fmt.Sprint(len(complete))},
	)
	return m, nil
}

// monthSamples returns a metric's stored samples in first's month.
func monthSamples(ctx context.Context, db *store.DB, metric string, first time.Time) ([]store.Sample, error) {
	last := first.AddDate(0, 1, -1)
	return db.Range(ctx, metric, 31, first.Format(dayLayout), last.Format(dayLayout))
}

// signedDuration is duration with a sign: "+8m", "−1h 5m", or "same".
func signedDuration(seconds float64) string {
	d := duration(abs(seconds))
	if d == "0m" {
		return "same"
	}
	if seconds < 0 {
		return "−" + d
	}
	return "+" + strings.TrimSpace(d)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
