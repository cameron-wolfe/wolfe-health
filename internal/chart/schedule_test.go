package chart

import (
	"testing"
	"time"
)

func TestNightClock(t *testing.T) {
	loc := time.FixedZone("", -8*3600)
	for in, want := range map[time.Time]float64{
		time.Date(2026, 11, 18, 18, 0, 0, 0, loc):  0,
		time.Date(2026, 11, 18, 22, 30, 0, 0, loc): 270,
		time.Date(2026, 11, 19, 0, 0, 0, 0, loc):   360,
		time.Date(2026, 11, 19, 12, 0, 0, 0, loc):  1080,
		time.Date(2026, 11, 19, 14, 0, 0, 0, loc):  -240, // an afternoon: before the night
	} {
		if got := NightClock(in); got != want {
			t.Errorf("NightClock(%v) = %v, want %v", in, got, want)
		}
	}
	if ClockLabel(270) != "10:30 PM" || ClockLabel(750) != "6:30 AM" || ClockLabel(-240) != "2:00 PM" {
		t.Errorf("labels %q %q %q", ClockLabel(270), ClockLabel(750), ClockLabel(-240))
	}
}

func TestScheduleViewRenders(t *testing.T) {
	d := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	s := SleepSchedule{Title: "Sleep schedule", UsualBed: 270, UsualWake: 750, HasUsual: true,
		BestStart: 120, BestEnd: 240, HasBest: true}
	for i := 0; i < 60; i++ {
		s.Nights = append(s.Nights, SleepNight{Day: d.AddDate(0, 0, i), Has: i%9 != 0, Bed: 250 + float64(i%5)*20,
			Wake: 740, Extra: [][2]float64{{100, 120}}})
	}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		for _, n := range []int{7, 30, 60} {
			v := s
			v.Nights = s.Nights[:n]
			if _, err := ScheduleView(v, size[0], size[1]); err != nil {
				t.Fatalf("%v, %d nights: %v", size, n, err)
			}
		}
	}
	if _, err := ScheduleView(SleepSchedule{}, 800, 480); err != nil {
		t.Fatal(err)
	}
	if _, err := ScheduleView(s, 300, 200); err == nil {
		t.Error("expected an error for a tiny image")
	}
}
