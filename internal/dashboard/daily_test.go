package dashboard

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

var today = time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)

func ago(n int) string { return today.AddDate(0, 0, -n).Format(dayLayout) }

func openWith(t *testing.T, samples []store.Sample) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Save(context.Background(), samples); err != nil {
		t.Fatal(err)
	}
	return db
}

// The hero compares today with the 7 days before it, places it in its
// 30-day range, and words where it ranks.
func TestHeroDeltaRangeAndVerdict(t *testing.T) {
	var s []store.Sample
	for i := 1; i <= 7; i++ {
		s = append(s, store.Sample{Metric: store.MetricReadiness, Day: ago(i), Value: 80})
	}
	s = append(s, store.Sample{Metric: store.MetricReadiness, Day: ago(0), Value: 68})
	d, err := Daily(context.Background(), openWith(t, s), today)
	if err != nil {
		t.Fatal(err)
	}
	h := d.Hero
	if h.Value != "68" || h.Delta != "−12 vs 7-day avg" || h.Note != "today" {
		t.Errorf("hero = %+v", h)
	}
	if r := h.Range; !r.Has || r.Low != 68 || r.High != 80 || r.Typical != 80 || r.Now != 68 {
		t.Errorf("range = %+v", r)
	}
	if d.Verdict != "Well below your usual" || d.VerdictDetail != "12 under your 7-day average" {
		t.Errorf("verdict = %q / %q", d.Verdict, d.VerdictDetail)
	}
}

func TestVerdictBands(t *testing.T) {
	month := []float64{60, 65, 70, 75, 80, 85, 90, 95, 100}
	for v, want := range map[float64]string{
		60: "Well below your usual", 70: "Below your usual", 80: "About your usual",
		90: "Above your usual", 100: "Well above your usual",
	} {
		head, _ := verdict(built{day: "x", value: v, month: month})
		if head != want {
			t.Errorf("verdict(%v) = %q, want %q", v, head, want)
		}
	}
	if head, detail := verdict(built{}); head != "No readiness yet" || detail != "" {
		t.Errorf("no data: %q %q", head, detail)
	}
}

// Stress accumulates through the day, so today's partial number is skipped
// in favor of yesterday's complete one.
func TestStressUsesLastCompleteDay(t *testing.T) {
	s := []store.Sample{
		{Metric: store.MetricStress, Day: ago(1), Value: 5400, Qualifier: "normal"},
		{Metric: store.MetricStress, Day: ago(0), Value: 0},
	}
	d, err := Daily(context.Background(), openWith(t, s), today)
	if err != nil {
		t.Fatal(err)
	}
	if m := d.Metrics[3]; m.Value != "1h 30m" || m.Note != "yesterday" {
		t.Errorf("stress = %+v", m)
	}
}

// Too little history: the value shows, but no delta and no range strip.
func TestNoDeltaOrRangeWithoutHistory(t *testing.T) {
	s := []store.Sample{
		{Metric: store.MetricSleep, Day: ago(1), Value: 80},
		{Metric: store.MetricSleep, Day: ago(0), Value: 70},
	}
	d, err := Daily(context.Background(), openWith(t, s), today)
	if err != nil {
		t.Fatal(err)
	}
	if m := d.Metrics[0]; m.Value != "70" || m.Delta != "" || m.Range.Has {
		t.Errorf("sleep = %+v", m)
	}
}

func TestEmptyDatabase(t *testing.T) {
	d, err := Daily(context.Background(), openWith(t, nil), today)
	if err != nil {
		t.Fatal(err)
	}
	if d.Hero.Value != "—" || d.Verdict != "No readiness yet" || d.Stages.InBed() != 0 {
		t.Errorf("empty: %+v", d)
	}
	if _, err := chart.DailyView(d, 800, 480); err != nil {
		t.Fatal(err)
	}
}

// The night band follows the night the sleep score is from: its stage bar,
// its times, and only the parts of sleep under 70, lowest first.
func TestNightFollowsSleepDay(t *testing.T) {
	db := openWith(t, []store.Sample{{Metric: store.MetricSleep, Day: ago(2), Value: 75}})
	ctx := context.Background()
	p := period("p", "long_sleep", 22110)
	p.Day = ago(2)
	p.Phases30Sec = ptr("11112222")
	if err := db.SaveSleepPeriods(ctx, []store.SleepPeriod{p}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSleepContributors(ctx, []store.SleepContributors{{
		Day: ago(2), DeepSleep: ptr(57), RemSleep: ptr(49), TotalSleep: ptr(88), Timing: ptr(100),
	}}); err != nil {
		t.Fatal(err)
	}
	d, err := Daily(ctx, db, today)
	if err != nil {
		t.Fatal(err)
	}
	if d.NightTitle != "Night of Nov 16" {
		t.Errorf("title = %q", d.NightTitle)
	}
	if d.Stages.Deep != 2*time.Minute || d.Stages.Light != 2*time.Minute {
		t.Errorf("stages = %+v", d.Stages)
	}
	if d.NightWhen != "11:02 PM → 5:10 AM · 4m asleep" {
		t.Errorf("when = %q", d.NightWhen)
	}
	if d.HeldBack != "Held back by: REM 49 · Deep sleep 57" {
		t.Errorf("held back = %q", d.HeldBack)
	}
	if !d.HasPhase || d.Phase.Start.IsZero() {
		t.Error("the brief's moon has no night")
	}
	if _, err := chart.DailyView(d, 800, 480); err != nil {
		t.Fatal(err)
	}
	if _, err := chart.BriefView(d, 800, 432); err != nil {
		t.Fatal(err)
	}
}

func TestFormatters(t *testing.T) {
	for in, want := range map[float64]string{0: "0", 999: "999", 6765: "6,765", 1234567: "1,234,567"} {
		if got := thousands(in); got != want {
			t.Errorf("thousands(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0: "0m", 900: "15m", 3600: "1h", 5400: "1h 30m"} {
		if got := duration(in); got != want {
			t.Errorf("duration(%v) = %q, want %q", in, got, want)
		}
	}
	if got := deltaText(0.3, score); got != "level with 7-day avg" {
		t.Errorf("tiny delta = %q", got)
	}
}

// Today's rings: sleep and readiness as scores, the inner ring as progress
// toward Oura's activity goal, worded as the distance left; nothing from
// yesterday stands in for today.
func TestDailyRings(t *testing.T) {
	s := func(metric, day string, v float64) store.Sample {
		return store.Sample{Metric: metric, Day: day, Value: v}
	}
	db := openWith(t, []store.Sample{
		s(store.MetricSleep, "2026-11-19", 88), s(store.MetricReadiness, "2026-11-19", 91),
		s(store.MetricActiveCal, "2026-11-19", 310), s(store.MetricGoalCal, "2026-11-19", 500),
		s(store.MetricGoalLeft, "2026-11-19", 3400), s(store.MetricTotalCal, "2026-11-19", 2610),
		s(store.MetricActiveCal, "2026-11-18", 900), s(store.MetricGoalCal, "2026-11-18", 500), // yesterday: ignored
	})
	d, err := Daily(context.Background(), db, today)
	if err != nil {
		t.Fatal(err)
	}
	r := d.Rings
	if !d.HasRings || !r.ActivityGoal || r.Activity != 62 || d.GoalNote != "3.4 km to goal" {
		t.Errorf("rings %+v, note %q", r, d.GoalNote)
	}
	if d.CalNote != "310 of 500 cal" || d.TotalCal != "2,610" {
		t.Errorf("calories: %q, total %q", d.CalNote, d.TotalCal)
	}
	if r.Optimal() != 2 || r.AllOptimal() {
		t.Errorf("sleep and readiness optimal, goal at 62%%: optimal %d", r.Optimal())
	}

	db = openWith(t, []store.Sample{
		s(store.MetricSleep, "2026-11-19", 88),
		s(store.MetricActiveCal, "2026-11-19", 520), s(store.MetricGoalCal, "2026-11-19", 500),
		s(store.MetricGoalLeft, "2026-11-19", 0),
	})
	if d, err = Daily(context.Background(), db, today); err != nil {
		t.Fatal(err)
	}
	if d.Rings.Activity != 104 || d.GoalNote != "goal met" || d.Rings.HasReadiness {
		t.Errorf("goal met: %+v, note %q", d.Rings, d.GoalNote)
	}

	db = openWith(t, []store.Sample{s(store.MetricSleep, "2026-11-18", 88)}) // nothing today
	if d, err = Daily(context.Background(), db, today); err != nil {
		t.Fatal(err)
	}
	if d.HasRings {
		t.Errorf("no scores today should draw no rings: %+v", d.Rings)
	}
	if _, err := chart.DailyView(d, 800, 450); err != nil {
		t.Fatal(err)
	}
}
