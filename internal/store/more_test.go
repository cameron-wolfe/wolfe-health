package store

import (
	"context"
	"testing"
	"time"
)

func TestMoreTablesRoundTrip(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)

	// re-saving the same instant replaces it
	if err := db.SaveHeartRates(ctx, []HeartRate{{t0, 60, "awake"}, {t0.Add(5 * time.Minute), 64, "awake"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHeartRates(ctx, []HeartRate{{t0, 62, "rest"}}); err != nil {
		t.Fatal(err)
	}
	hr, err := db.HeartRatesBetween(ctx, t0, t0.Add(time.Hour))
	if err != nil || len(hr) != 2 || hr[0].BPM != 62 || hr[0].Source != "rest" || !hr[0].Time.Equal(t0) {
		t.Errorf("heart rate %+v, %v", hr, err)
	}

	cal := 310.5
	if err := db.SaveWorkouts(ctx, []Workout{
		{ID: "w2", Day: "2026-11-19", Activity: "cycling", Intensity: "hard", Source: "manual",
			Start: "2026-11-19T17:00:00-08:00", End: "2026-11-19T18:00:00-08:00", Calories: &cal},
		{ID: "w1", Day: "2026-11-19", Activity: "walking", Intensity: "easy", Source: "autodetected",
			Start: "2026-11-19T07:00:00-08:00", End: "2026-11-19T07:30:00-08:00"},
		{ID: "w0", Day: "2026-11-10", Activity: "walking", Intensity: "easy", Source: "autodetected",
			Start: "2026-11-10T07:00:00-08:00", End: "2026-11-10T07:30:00-08:00"},
	}); err != nil {
		t.Fatal(err)
	}
	ws, err := db.WorkoutsBetween(ctx, "2026-11-19", "2026-11-19")
	if err != nil || len(ws) != 2 || ws[0].ID != "w1" || *ws[1].Calories != 310.5 || ws[0].Calories != nil {
		t.Errorf("workouts %+v, %v", ws, err)
	}

	if lt, ok, err := db.LatestHeartRateTime(ctx); !ok || err != nil || !lt.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("latest heart rate %v ok=%v err=%v", lt, ok, err)
	}
	if _, ok, err := db.LatestBattery(ctx); ok || err != nil {
		t.Errorf("empty battery: ok=%v err=%v", ok, err)
	}
	yes := true
	if err := db.SaveBatteryLevels(ctx, []BatteryLevel{{Time: t0, Level: 80}, {Time: t0.Add(time.Hour), Level: 76, Charging: &yes}}); err != nil {
		t.Fatal(err)
	}
	if b, ok, err := db.LatestBattery(ctx); !ok || err != nil || b.Level != 76 || !*b.Charging {
		t.Errorf("latest battery %+v ok=%v err=%v", b, ok, err)
	}

	if err := db.SaveSessions(ctx, []Session{{ID: "s", Day: "2026-11-19", Type: "meditation",
		Start: "x", End: "y", HeartRate: &Series{Interval: 5, Start: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if ss, err := db.SessionsBetween(ctx, "2026-11-19", "2026-11-19"); err != nil || len(ss) != 1 || ss[0].Type != "meditation" {
		t.Errorf("sessions %+v, %v", ss, err)
	}
	if ss, err := db.SessionsBetween(ctx, "2026-11-20", "2026-11-21"); err != nil || len(ss) != 0 {
		t.Errorf("sessions on other days %+v, %v", ss, err)
	}
	so, eo, tz := -3600, 0, -28800
	rec := "follow_optimal_bedtime"
	if err := db.SaveSleepTimes(ctx, []SleepTime{{Day: "2026-11-19", StartOffset: &so, EndOffset: &eo, DayTZ: &tz, Recommendation: &rec},
		{Day: "2026-11-20"}}); err != nil {
		t.Fatal(err)
	}
	if st, err := db.SleepTimesBetween(ctx, "2026-11-19", "2026-11-20"); err != nil || len(st) != 2 ||
		*st[0].StartOffset != -3600 || *st[0].DayTZ != -28800 || st[1].StartOffset != nil {
		t.Errorf("sleep times %+v, %v", st, err)
	}
	if err := db.SavePersonalInfo(ctx, PersonalInfo{ID: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePersonalInfo(ctx, PersonalInfo{ID: "p"}); err != nil { // a second fetch replaces
		t.Fatal(err)
	}
	for table, want := range map[string]int{"heart_rate": 2, "workouts": 3, "ring_battery": 2, "sessions": 1, "personal_info": 1, "tags": 0, "sleep_times": 2} {
		if n, err := db.TableCount(ctx, table); err != nil || n != want {
			t.Errorf("%s: %d rows, %v; want %d", table, n, err, want)
		}
	}
	if _, err := db.TableCount(ctx, "samples; DROP TABLE x"); err == nil {
		t.Error("TableCount should refuse unknown tables")
	}
}
