package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The usual band comes from the four weeks before, leaving out workouts;
// today's readings stop at now; sleep and the day's workouts become spans;
// the figures compare awake readings with the usual at the same hours.
func TestHeartToday(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, loc)
	now := start.Add(10 * time.Hour)
	db := openWith(t, nil)
	ctx := context.Background()

	var hr []store.HeartRate
	// the past four weeks: 60 bpm at 9 AM, every 5 minutes for half an
	// hour, and a workout at 120 that shouldn't count
	for d := 1; d <= 28; d++ {
		base := start.AddDate(0, 0, -d).Add(9 * time.Hour)
		for m := 0; m < 30; m += 5 {
			hr = append(hr, store.HeartRate{Time: base.Add(time.Duration(m) * time.Minute), BPM: 60, Source: "awake"})
		}
		hr = append(hr, store.HeartRate{Time: base.Add(2 * time.Minute), BPM: 120, Source: "workout"})
	}
	// today: asleep at 50 until 6, 9 AM readings above usual, and one
	// after now that must not appear
	hr = append(hr,
		store.HeartRate{Time: start.Add(3 * time.Hour), BPM: 50, Source: "sleep"},
		// in the night's sleep period but not labeled sleep: still asleep
		store.HeartRate{Time: start.Add(4 * time.Hour), BPM: 47, Source: "rest"},
		store.HeartRate{Time: start.Add(9 * time.Hour), BPM: 70, Source: "awake"},
		store.HeartRate{Time: start.Add(9*time.Hour + 5*time.Minute), BPM: 80, Source: "awake"},
		store.HeartRate{Time: start.Add(11 * time.Hour), BPM: 99, Source: "awake"},
	)
	if err := db.SaveHeartRates(ctx, hr); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSleepPeriods(ctx, []store.SleepPeriod{{ID: "n", Day: "2026-11-20", Type: "long_sleep",
		BedtimeStart: "2026-11-19T23:00:00-08:00", BedtimeEnd: "2026-11-20T06:00:00-08:00", TimeInBed: 25200}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveWorkouts(ctx, []store.Workout{{ID: "w", Day: "2026-11-20", Activity: "strength_training",
		Intensity: "hard", Source: "manual", Start: "2026-11-20T09:00:00-08:00", End: "2026-11-20T09:30:00-08:00"}}); err != nil {
		t.Fatal(err)
	}

	h, err := HeartToday(ctx, db, day, now)
	if err != nil {
		t.Fatal(err)
	}
	if h.Title != "Heart rate today" || len(h.Readings) != 4 {
		t.Fatalf("title %q, %d readings (the one after now excluded)", h.Title, len(h.Readings))
	}
	if len(h.Usual) != 1 || h.Usual[0].Lo != 60 || h.Usual[0].Hi != 60 || h.Usual[0].Start.Hour() != 9 {
		t.Errorf("usual %+v; workouts left out", h.Usual)
	}
	if len(h.Asleep) != 1 || h.Asleep[0].End.Hour() != 6 {
		t.Errorf("asleep %+v", h.Asleep)
	}
	if len(h.Activities) != 1 || h.Activities[0].Label != "strength training" {
		t.Errorf("activities %+v", h.Activities)
	}
	if h.Headline != "80 bpm at 9:05 AM" || !h.HasPeak || h.Peak.BPM != 80 {
		t.Errorf("headline %q, peak %+v", h.Headline, h.Peak)
	}
	got := map[string]string{}
	for _, s := range h.Stats {
		got[s.Label] = s.Value
	}
	for k, v := range map[string]string{
		"Latest": "80 · 9:05 AM", "Awake average": "75 bpm", "Usual, same hours": "60 bpm",
		"Lowest asleep": "47 bpm", // the "rest" reading inside the sleep period "Above usual": "10m", "Workouts, sessions": "1 · 30m",
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	// yesterday: no "latest", the whole day, a dated title
	past, err := HeartToday(ctx, db, day.AddDate(0, 0, -1), now)
	if err != nil {
		t.Fatal(err)
	}
	if past.Title != "Heart rate yesterday · Thursday, Nov 19" || !past.Now.Equal(start) {
		t.Errorf("past day: %q, now %v", past.Title, past.Now)
	}
	older, err := HeartToday(ctx, db, day.AddDate(0, 0, -3), now)
	if err != nil {
		t.Fatal(err)
	}
	if older.Title != "Heart rate · Tuesday, Nov 17" {
		t.Errorf("older day: %q", older.Title)
	}
}
