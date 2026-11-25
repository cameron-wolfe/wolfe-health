package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

func stress(day string, minutes float64, q string) store.Sample {
	return store.Sample{Metric: store.MetricStress, Day: day, Value: minutes * 60, Qualifier: q}
}

// Today is shown but marked partial and kept out of the figures; only the
// exceptional summaries become badges; the previous month is compared.
func TestStressMonth(t *testing.T) {
	db := openWith(t, []store.Sample{
		stress("2026-10-01", 60, "normal"), stress("2026-10-02", 120, "normal"), // Oct avg 1h 30m
		stress("2026-11-01", 45, "normal"),
		stress("2026-11-09", 285, "stressful"),
		stress("2026-11-15", 45, "restored"),
		stress("2026-11-19", 0, "normal"), // today, partial
	})
	m, err := StressMonth(context.Background(), db, today, today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Stress · November 2026" || len(m.Days) != 4 {
		t.Fatalf("title %q, %d days", m.Title, len(m.Days))
	}
	summaries := map[string]string{}
	for _, d := range m.Days {
		summaries[d.Day.Format("02")] = d.Summary
		if d.Day.Day() == 19 && !d.Partial {
			t.Error("today should be partial")
		}
	}
	if summaries["01"] != "normal" || summaries["09"] != "stressful" || summaries["15"] != "restored" {
		t.Errorf("summaries = %v", summaries)
	}
	want := map[string]string{
		"Daily average":  "2h 5m", // (45+285+45)/3, today excluded
		"vs October":     "+35m",
		"Peak day":       "Nov 9 · 4h 45m",
		"Stressful days": "1",
		"Restored days":  "1",
		"Days counted":   "3",
	}
	got := map[string]string{}
	for _, s := range m.Stats {
		got[s.Label] = s.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestStressMonthEmpty(t *testing.T) {
	m, err := StressMonth(context.Background(), openWith(t, nil), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), today)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Days) != 0 || len(m.Stats) != 1 {
		t.Errorf("empty month: %d days, stats %v", len(m.Days), m.Stats)
	}
}
