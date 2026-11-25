package habits

import (
	"testing"
	"time"
)

// The menu's names are found whatever their case or spacing; nothing else
// is.
func TestFind(t *testing.T) {
	for _, in := range []string{"Caffeine", "caffeine", " Cold plunge "} {
		if _, ok := Find(in); !ok {
			t.Errorf("%q not found", in)
		}
	}
	for _, bad := range []string{"", "coffee", "Late meal", "two beers"} {
		if h, ok := Find(bad); ok {
			t.Errorf("%q found %s", bad, h.Name)
		}
	}
}

// Every habit has a unique name and code.
func TestCatalog(t *testing.T) {
	names, codes := map[string]bool{}, map[string]bool{}
	for _, h := range Catalog {
		if names[h.Name] || codes[h.Code] || h.Code == "" || len(h.Code) > 4 {
			t.Errorf("%s: name or code %q repeated or bad", h.Name, h.Code)
		}
		names[h.Name], codes[h.Code] = true, true
	}
	if h, ok := ForOuraTag("tag_sleep_alcohol"); !ok || h.Name != "Alcohol" {
		t.Error("Oura's alcohol tag isn't Alcohol")
	}
	if b := ByName("House Reno"); b.Code != "HOUS" {
		t.Errorf("a custom tag's code = %q", b.Code)
	}
	if got := ByName("Travel").Amount(3); got != "travel day" {
		t.Errorf("an uncounted habit's amount = %q", got)
	}
}

// A timed habit is late close to bed, and its late form charts under its
// own name with the same badge.
func TestLate(t *testing.T) {
	bed := time.Date(2026, 11, 21, 22, 30, 0, 0, time.UTC)
	meal, wkt := ByName("Meal"), ByName("Workout")
	if !meal.IsLate(bed.Add(-2*time.Hour), bed) || meal.IsLate(bed.Add(-4*time.Hour), bed) {
		t.Error("a meal 2 hours before bed is late; 4 hours isn't")
	}
	if wkt.IsLate(bed.Add(-150*time.Minute), bed) || !wkt.IsLate(bed.Add(-time.Hour), bed) {
		t.Error("a workout is late within 2 hours")
	}
	if l := ByName("Late meal"); !l.Late || l.Code != "MEAL" || l.LateWithin != 0 {
		t.Errorf("late meal = %+v", l)
	}
	if ByName("Caffeine").IsLate(bed, bed) {
		t.Error("caffeine isn't timed")
	}
}

// A log belongs to the night that follows its day; small hours count with
// the evening before.
func TestNight(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	day := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, loc) }
	for _, c := range []struct {
		at   time.Time
		want int
	}{{day(21, 15), 22}, {day(21, 23), 22}, {day(22, 1), 22}, {day(22, 5), 23}} {
		if got := Night(c.at); got.Day() != c.want {
			t.Errorf("%s: night of the %d, want the %d", c.at.Format("Jan 2 15:04"), got.Day(), c.want)
		}
	}
}
