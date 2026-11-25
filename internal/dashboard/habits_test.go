package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// A day's logs, and Oura's tags, land on the night after it; small hours
// count with the evening before; the week marks its best and toughest
// nights and totals against the week before.
func TestHabitWeek(t *testing.T) {
	ctx := context.Background()
	loc := time.FixedZone("PST", -8*3600)
	at := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, loc) }
	var samples []store.Sample
	for d, v := range map[int]float64{16: 80, 17: 70, 18: 90, 19: 85} { // wake-up days
		samples = append(samples, store.Sample{Metric: store.MetricSleep, Day: "2026-11-" + itoa(d), Value: v})
	}
	db := openWith(t, samples)
	for _, l := range []store.HabitLog{
		{At: at(15, 9), Habit: "Caffeine", Count: 1}, {At: at(15, 15), Habit: "Caffeine", Count: 1},
		{At: at(16, 20), Habit: "Alcohol", Count: 2}, {At: at(17, 1), Habit: "Alcohol", Count: 1}, // 1 AM: still the 16th
		{At: at(17, 18), Habit: "Cold plunge", Count: 1}, {At: at(17, 19), Habit: "Cold plunge", Count: 1},
		{At: at(17, 12), Habit: "Meal", Count: 1},    // noon: not late, and an ordinary meal isn't shown
		{At: at(17, 21), Habit: "Meal", Count: 1},    // 9 PM against the usual 10:30 PM: late
		{At: at(17, 21), Habit: "Workout", Count: 1}, // 9 PM: within 2 hours, late too
		{At: at(9, 9), Habit: "Caffeine", Count: 3},  // the week before
	} {
		if _, err := db.AddHabitLog(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	code := "tag_sleep_alcohol"
	if err := db.SaveTags(ctx, []store.Tag{{ID: "t1", TypeCode: &code, StartDay: "2026-11-18", StartTime: "2026-11-18T21:00:00-08:00"}}); err != nil {
		t.Fatal(err)
	}
	w, err := HabitWeek(ctx, db, time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), today, loc) // today is the 19th
	if err != nil {
		t.Fatal(err)
	}
	sun, mon, tue, wed := w.Days[0], w.Days[1], w.Days[2], w.Days[3]
	if len(sun.Badges) != 1 || sun.Badges[0].Code != "CAF" || sun.Badges[0].Count != 2 || sun.Badges[0].Last != "3 PM" {
		t.Errorf("Sunday: %+v", sun.Badges)
	}
	if len(mon.Badges) != 1 || mon.Badges[0].Count != 3 || mon.Score != "70" {
		t.Errorf("Monday, with the 1 AM drink, then Tuesday's night: %+v, score %s", mon.Badges, mon.Score)
	}
	if len(tue.Badges) != 3 || tue.Badges[0].Name != "Late meal" || !tue.Badges[0].Late || tue.Badges[1].Name != "Late workout" ||
		tue.Badges[2].Code != "CLD" || tue.Badges[2].Count != 1 || tue.Badges[2].Counted {
		t.Errorf("Tuesday: a late meal and workout, and two cold plunges as one: %+v", tue.Badges)
	}
	if len(wed.Badges) != 1 || wed.Badges[0].Code != "ALC" || wed.Score != "85" {
		t.Errorf("Wednesday, Oura's alcohol tag, and the night woken from today: %+v, score %s", wed.Badges, wed.Score)
	}
	if thu, fri := w.Days[4], w.Days[5]; !thu.Tonight || thu.Future || !fri.Future || w.Best != 2 || w.Worst != 1 {
		t.Errorf("Thursday tonight %v, Friday future %v, best %d, worst %d", thu.Tonight, fri.Future, w.Best, w.Worst)
	}
	if len(w.Stats) == 0 || w.Stats[0].Label != "Alcohol" || w.Stats[0].Value != "4" {
		t.Errorf("stats: %+v", w.Stats)
	}
	for _, s := range w.Stats {
		if s.Label == "Caffeine" && s.Change != "−1 vs last week" {
			t.Errorf("caffeine change: %q", s.Change)
		}
	}
	if _, err := chart.HabitWeekView(w, 800, 432); err != nil {
		t.Fatal(err)
	}
	empty, _ := HabitWeek(ctx, openWith(t, nil), time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), today, loc)
	if _, err := chart.HabitWeekView(empty, 600, 360); err != nil {
		t.Fatal(err)
	}
}

// A habit's effect shows once it has enough nights with and without it,
// as the average after it less the average without.
func TestHabitEffects(t *testing.T) {
	ctx := context.Background()
	loc := time.FixedZone("PST", -8*3600)
	var samples []store.Sample
	for i := 0; i < 12; i++ {
		v := 85.0
		if i%2 == 0 {
			v = 75 // nights after a drink
		}
		samples = append(samples, store.Sample{Metric: store.MetricSleep, Day: ago(i), Value: v})
	}
	db := openWith(t, samples)
	for i := 0; i < 12; i += 2 {
		evening := today.AddDate(0, 0, -i-1)
		at := time.Date(evening.Year(), evening.Month(), evening.Day(), 20, 0, 0, 0, loc)
		db.AddHabitLog(ctx, store.HabitLog{At: at, Habit: "Alcohol", Count: 2})
	}
	db.AddHabitLog(ctx, store.HabitLog{At: time.Date(2026, 11, 18, 18, 0, 0, 0, loc), Habit: "Cold plunge", Count: 1})
	e, err := HabitEffects(ctx, db, 30, today, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Rows) != 2 || e.Rows[0].Name != "Alcohol" || e.Rows[0].Nights != 6 || !e.Rows[0].Enough {
		t.Fatalf("rows: %+v", e.Rows)
	}
	if d := e.Rows[0].Diff[0]; d != -10 {
		t.Errorf("alcohol's sleep score difference = %v, want −10", d)
	}
	if e.Rows[1].Enough {
		t.Error("one cold plunge night shouldn't be enough")
	}
	if e.Headline != "alcohol: sleep score −10" {
		t.Errorf("headline %q", e.Headline)
	}
	if _, err := chart.HabitEffectsView(e, 800, 432); err != nil {
		t.Fatal(err)
	}
	empty, _ := HabitEffects(ctx, openWith(t, nil), 90, today, loc)
	if _, err := chart.HabitEffectsView(empty, 600, 300); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string { return time.Date(2000, 1, n, 0, 0, 0, 0, time.UTC).Format("02") }
