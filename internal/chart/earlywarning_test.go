package chart

import (
	"testing"
	"time"
)

func TestEarlyWarningViewRenders(t *testing.T) {
	from := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	e := EarlyWarning{Title: "Early warning", Headline: "Watch: 2 signals up", From: from, To: from.AddDate(0, 0, 20),
		Watch: 2, HabitKey: []string{"ALC alcohol"}}
	for i := 0; i < 21; i++ {
		d := from.AddDate(0, 0, i)
		e.Days = append(e.Days, WarnDay{Day: d, Up: i % 3, Of: 4, Habits: []string{"ALC", "OUT", "WKT"}[:i%4]})
	}
	for _, title := range []string{"Temperature", "Lowest HR", "HRV", "Breathing"} {
		l := WarnLane{Title: title, Hint: "up: warmer", Value: "+0.29 °C", Status: "high for 2 nights"}
		for i := 0; i < 21; i++ {
			s := float64(i%9)/2 - 2 // runs past both ends of the scale
			l.Nights = append(l.Nights, WarnNight{Day: from.AddDate(0, 0, i), S: s, Up: s > 1})
			l.Avg = append(l.Avg, Point{Day: from.AddDate(0, 0, i), Value: s / 2})
		}
		e.Lanes = append(e.Lanes, l)
	}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		if _, err := EarlyWarningView(e, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	if _, err := EarlyWarningView(EarlyWarning{}, 800, 480); err != nil {
		t.Fatal(err)
	}
}
