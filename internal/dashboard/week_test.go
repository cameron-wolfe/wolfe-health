package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

func TestWeekStart(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-15": "2026-11-15", // Sunday
		"2026-11-19": "2026-11-15", // Thursday
		"2026-11-21": "2026-11-15", // Saturday
		"2026-11-22": "2026-11-22",
	} {
		d, _ := time.Parse(dayLayout, in)
		if got := WeekStart(d).Format(dayLayout); got != want {
			t.Errorf("WeekStart(%s) = %s, want %s", in, got, want)
		}
	}
}

// Days run Sunday to Saturday; averages compare with the week before; the
// best and toughest nights are marked; in the current week, future days are
// blank and today's steps and stress are left out.
func TestWeekReview(t *testing.T) {
	s := func(metric, day string, v float64) store.Sample {
		return store.Sample{Metric: metric, Day: day, Value: v}
	}
	db := openWith(t, []store.Sample{
		// last week (Nov 8-14): sleep averages 80
		s(store.MetricSleep, "2026-11-09", 80), s(store.MetricSleep, "2026-11-10", 80),
		// this week (Nov 15-21), today Thu 19
		s(store.MetricSleep, "2026-11-16", 84), s(store.MetricSleep, "2026-11-18", 90),
		s(store.MetricSleep, "2026-11-19", 66),
		s(store.MetricReadiness, "2026-11-16", 82),
		s(store.MetricSteps, "2026-11-16", 6240), s(store.MetricSteps, "2026-11-19", 9000), // today: partial
		s(store.MetricStress, "2026-11-16", 900),
		s(store.MetricActiveCal, "2026-11-16", 420), s(store.MetricActiveCal, "2026-11-19", 999), // today: partial
		s(store.MetricTotalCal, "2026-11-16", 2600),
		s(store.MetricActiveCal, "2026-11-10", 400), // last week
	})
	w, err := WeekReview(context.Background(), db, WeekStart(today), today)
	if err != nil {
		t.Fatal(err)
	}
	if w.Subtitle != "Nov 15 – 21 · so far" || w.Headline != "sleep 80 · readiness 82" {
		t.Errorf("subtitle %q, headline %q", w.Subtitle, w.Headline)
	}
	sun, mon, tue, thu, fri := w.Days[0], w.Days[1], w.Days[2], w.Days[4], w.Days[5]
	if !mon.HasSleep || mon.Sleep != 84 || mon.Steps != "6.2k" || mon.Stress != "15m" || !mon.HasReadiness {
		t.Errorf("monday %+v", mon)
	}
	if sun.HasSleep || sun.Day.Weekday() != time.Sunday {
		t.Errorf("sunday (no data) %+v", sun)
	}
	if tue.HasSleep || tue.Steps != "—" {
		t.Errorf("tuesday (no data) %+v", tue)
	}
	if thu.Steps != "—" || thu.Future || !fri.Future {
		t.Errorf("today's steps should be left out and Friday blank: thu %+v fri future=%v", thu, fri.Future)
	}
	if w.Best != 3 || w.Worst != 4 {
		t.Errorf("best %d worst %d, want Wed (3) and Thu (4)", w.Best, w.Worst)
	}
	stats := map[string]chart.StatLine3{}
	for _, st := range w.Stats {
		stats[st.Label] = st
	}
	if st := stats["Sleep"]; st.Value != "80" || st.Change != "same as last week" {
		t.Errorf("sleep stat %+v", st)
	}
	if mon.Calories != "420" || thu.Calories != "—" {
		t.Errorf("calories: mon %q, thu (today, partial) %q", mon.Calories, thu.Calories)
	}
	if st := stats["Active cal"]; st.Value != "420" || st.Change != "+20 vs last week" {
		t.Errorf("active cal stat %+v", st)
	}
	if st := stats["Total cal"]; st.Value != "2,600" {
		t.Errorf("total cal stat %+v", st)
	}
	if len(w.Stats) != 8 || w.Stats[1].Label != "Time asleep" || w.Stats[7].Label != "High stress" {
		t.Errorf("stat order %+v", w.Stats)
	}
	if st := stats["Steps"]; st.Value != "6,240" || st.Change != "" {
		t.Errorf("steps stat %+v (no steps last week, today left out)", st)
	}
	if _, err := chart.WeekView(w, 800, 450); err != nil {
		t.Fatal(err)
	}
}

func TestWeekReviewPastWeek(t *testing.T) {
	db := openWith(t, []store.Sample{{Metric: store.MetricSleep, Day: "2026-11-11", Value: 70}})
	w, err := WeekReview(context.Background(), db, WeekStart(today).AddDate(0, 0, -7), today)
	if err != nil {
		t.Fatal(err)
	}
	if w.Subtitle != "Nov 8 – 14" || w.Best != 3 || w.Worst != -1 {
		t.Errorf("past week: %q best=%d worst=%d (one night: no toughest)", w.Subtitle, w.Best, w.Worst)
	}
	for i, d := range w.Days {
		if d.Future {
			t.Errorf("day %d of a past week marked future", i)
		}
	}
}

// A day with all three scores optimal is an optimal day, tallied in the headline.
func TestWeekReviewAllOptimals(t *testing.T) {
	var samples []store.Sample
	for _, m := range []string{store.MetricSleep, store.MetricReadiness, store.MetricActivity} {
		samples = append(samples,
			store.Sample{Metric: m, Day: "2026-11-10", Value: 90},
			store.Sample{Metric: m, Day: "2026-11-11", Value: 80})
	}
	w, err := WeekReview(context.Background(), openWith(t, samples), WeekStart(today).AddDate(0, 0, -7), today)
	if err != nil {
		t.Fatal(err)
	}
	if !w.Days[2].AllOptimal() || w.Days[3].AllOptimal() || w.Headline != "sleep 85 · readiness 85 · 1 optimal day" {
		t.Errorf("headline %q, Tue optimal %v, Wed optimal %v", w.Headline, w.Days[2].AllOptimal(), w.Days[3].AllOptimal())
	}
}
