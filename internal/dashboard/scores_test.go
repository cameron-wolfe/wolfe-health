package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Days with any score become cells; today's activity is left out; averages
// compare with the month before; optimal days and the best day are counted.
func TestScoreMonth(t *testing.T) {
	s := func(metric, day string, v float64) store.Sample {
		return store.Sample{Metric: metric, Day: day, Value: v}
	}
	db := openWith(t, []store.Sample{
		s(store.MetricSleep, "2026-10-10", 80), // October sleep: 80
		s(store.MetricSleep, "2026-11-02", 88), s(store.MetricReadiness, "2026-11-02", 90), s(store.MetricActivity, "2026-11-02", 86),
		s(store.MetricSleep, "2026-11-05", 90), s(store.MetricReadiness, "2026-11-05", 91), s(store.MetricActivity, "2026-11-05", 95),
		s(store.MetricSleep, "2026-11-10", 70), s(store.MetricReadiness, "2026-11-10", 72),
		s(store.MetricSleep, "2026-11-19", 86), s(store.MetricReadiness, "2026-11-19", 87),
		s(store.MetricActivity, "2026-11-19", 99), // today: not settled, left out
	})
	m, err := ScoreMonth(context.Background(), db, today, today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Scores · November 2026" || len(m.Days) != 4 {
		t.Fatalf("title %q, %d days", m.Title, len(m.Days))
	}
	last := m.Days[3]
	if last.Day.Day() != 19 || last.HasActivity || last.AllOptimal() {
		t.Errorf("today = %+v; activity should be left out", last)
	}
	if m.StatsNote != "averages, change vs October" {
		t.Errorf("note = %q", m.StatsNote)
	}
	want := map[string]string{
		"Sleep":        "84 · +4", // (88+90+70+86)/4 = 83.5
		"Readiness":    "85",      // nothing in October to compare
		"Activity":     "90",      // (86+95)/2, rounded as score() rounds
		"Optimal days": "2",
		"Best day":     "Nov 5",
		"Days scored":  "4",
	}
	got := map[string]string{}
	for _, st := range m.Stats {
		got[st.Label] = st.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestScoreMonthEmpty(t *testing.T) {
	m, err := ScoreMonth(context.Background(), openWith(t, nil), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), today)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Days) != 0 || len(m.Stats) != 1 || m.StatsNote != "" {
		t.Errorf("empty month: %d days, note %q, stats %v", len(m.Days), m.StatsNote, m.Stats)
	}
}

func TestSignedScore(t *testing.T) {
	for in, want := range map[float64]string{0.4: "same", -0.4: "same", 3.6: "+4", -2.2: "−2"} {
		if got := signedScore(in); got != want {
			t.Errorf("signedScore(%v) = %q, want %q", in, got, want)
		}
	}
}
