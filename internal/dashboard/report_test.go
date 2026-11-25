package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

func rowsByLabel(r chart.MonthReport) map[string]chart.ReportRow {
	m := map[string]chart.ReportRow{}
	for _, row := range r.Rows {
		m[row.Label] = row
	}
	return m
}

func highlights(r chart.MonthReport) map[string]string {
	m := map[string]string{}
	for _, h := range r.Highlights {
		m[h.Label] = h.Value
	}
	return m
}

// The current month covers the days so far; steps skip today's partial
// day; averages compare with last month; best and worst nights are marked
// (ties to the earliest), and time asleep comes from the main period.
func TestMonthReport(t *testing.T) {
	s := func(metric, day string, v float64) store.Sample {
		return store.Sample{Metric: metric, Day: day, Value: v}
	}
	db := openWith(t, []store.Sample{
		s(store.MetricSleep, "2026-10-10", 80), s(store.MetricSleep, "2026-10-11", 80),
		s(store.MetricSleep, "2026-11-01", 94), s(store.MetricSleep, "2026-11-13", 94),
		s(store.MetricSleep, "2026-11-17", 60), s(store.MetricSleep, "2026-11-19", 76),
		s(store.MetricSteps, "2026-11-02", 8000), s(store.MetricSteps, "2026-11-19", 20000), // today: partial, skipped
		s(store.MetricStress, "2026-11-09", 17100),
	})
	p := period("p", "long_sleep", 30000)
	p.Day, p.Phases30Sec = "2026-11-01", ptr("1111222233") // 2m deep + 2m light + 1m REM = 5m asleep
	if err := db.SaveSleepPeriods(context.Background(), []store.SleepPeriod{p}); err != nil {
		t.Fatal(err)
	}
	r, err := MonthReport(context.Background(), db, today, today)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "November 2026" || r.Subtitle != "Month in review · 19 days so far" {
		t.Errorf("title %q / %q", r.Title, r.Subtitle)
	}
	rows := rowsByLabel(r)
	if got := rows["Sleep score"]; got.Value != "81" || got.Delta != "+1 vs Oct" {
		t.Errorf("sleep row %+v", got) // (94+94+60+76)/4 = 81 vs 80
	}
	if got := rows["Steps"]; got.Value != "8,000" || got.Delta != "" {
		t.Errorf("steps row %+v (today skipped; no October steps)", got)
	}
	if got := rows["Time asleep"]; got.Value != "5m" {
		t.Errorf("time asleep %+v", got)
	}
	if r.Best != 1 || r.Worst != 17 || r.Scores[13] != 94 {
		t.Errorf("best %d worst %d scores %v", r.Best, r.Worst, r.Scores)
	}
	h := highlights(r)
	if h["Best night"] != "Oct 31 · score 94 · 5m asleep" || h["Most active"] != "Nov 2 · 8,000 steps" ||
		h["Most stressful"] != "Nov 9 · 4h 45m in high stress" {
		t.Errorf("highlights %v", h)
	}
	if _, err := chart.MonthReportView(r, 800, 450); err != nil {
		t.Fatal(err)
	}
}

func TestMonthReportPastAndFuture(t *testing.T) {
	db := openWith(t, []store.Sample{{Metric: store.MetricSleep, Day: "2026-10-05", Value: 70}})
	past, err := MonthReport(context.Background(), db, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), today)
	if err != nil {
		t.Fatal(err)
	}
	if past.Subtitle != "Month in review · all 31 days" || past.Worst != 0 {
		t.Errorf("past: %q worst=%d (one night: no toughest)", past.Subtitle, past.Worst)
	}
	future, err := MonthReport(context.Background(), db, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), today)
	if err != nil {
		t.Fatal(err)
	}
	if future.Subtitle != "Month in review · hasn't started yet" {
		t.Errorf("future: %q", future.Subtitle)
	}
	if _, err := chart.MonthReportView(future, 800, 450); err != nil {
		t.Fatal(err)
	}
}
