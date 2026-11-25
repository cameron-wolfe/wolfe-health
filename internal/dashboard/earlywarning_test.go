package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Two nights of a warmer body, a faster heart and lower HRV: each lane
// turns them upward, the nights count three signals up and are watched,
// and a drink logged the evening before shows under last night.
func TestEarlyWarningSpell(t *testing.T) {
	db := alertDB(t, map[int]float64{0: 0.6, 1: 0.5}, map[int]int{0: 30, 1: 32}, map[int]int{0: 60, 1: 58})
	ctx := context.Background()
	if _, err := db.AddHabitLog(ctx, store.HabitLog{At: today.Add(-4 * time.Hour), Habit: "Alcohol", Count: 2, Source: "shortcut"}); err != nil {
		t.Fatal(err)
	}
	e, err := EarlyWarning(ctx, db, WarnDays, today, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Days) != WarnDays || len(e.Lanes) != 4 {
		t.Fatalf("%d days, %d lanes", len(e.Days), len(e.Lanes))
	}
	last, before := e.Days[len(e.Days)-1], e.Days[len(e.Days)-2]
	// no breathing in this data, so three signals are read each night
	if last.Up != 3 || last.Of != 3 || before.Up != 3 || e.Days[0].Up != 0 {
		t.Errorf("counts: last %+v, before %+v, first %+v", last, before, e.Days[0])
	}
	if e.Headline != "Watch: 3 signals up, 2 nights running" {
		t.Errorf("headline %q", e.Headline)
	}
	temp, hrv, breath := e.Lanes[0], e.Lanes[2], e.Lanes[3]
	if n := temp.Nights[len(temp.Nights)-1]; !n.Up || n.S <= 1 {
		t.Errorf("temperature last night %+v", n)
	}
	if n := hrv.Nights[len(hrv.Nights)-1]; !n.Up || n.S <= 1 { // low HRV points up
		t.Errorf("HRV last night %+v", n)
	}
	if temp.Value != "+0.60 °C" || temp.Status != "high for 2 nights" || hrv.Status != "low for 2 nights" {
		t.Errorf("statuses %q %q / %q", temp.Value, temp.Status, hrv.Status)
	}
	if breath.Value != "—" || len(breath.Nights) != 0 {
		t.Errorf("breathing with no data: %+v", breath)
	}
	if len(last.Habits) != 1 || last.Habits[0] != "ALC" || len(e.HabitKey) != 1 || e.HabitKey[0] != "ALC alcohol" {
		t.Errorf("habits %v, key %v", last.Habits, e.HabitKey)
	}
}

// Ordinary nights: nothing up, nothing shaded.
func TestEarlyWarningQuiet(t *testing.T) {
	e, err := EarlyWarning(context.Background(), alertDB(t, nil, nil, nil), 14, today, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range e.Days {
		if d.Up != 0 {
			t.Errorf("%s: %d up", d.Day.Format(dayLayout), d.Up)
		}
	}
	if e.Headline != "all signals normal last night" || e.Lanes[0].Status != "normal" {
		t.Errorf("headline %q, temperature %q", e.Headline, e.Lanes[0].Status)
	}
	if _, err := EarlyWarning(context.Background(), alertDB(t, nil, nil, nil), 3, today, time.UTC); err == nil {
		t.Error("a 3-night window was allowed")
	}
}
