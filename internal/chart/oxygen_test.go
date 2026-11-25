package chart

import (
	"testing"
	"time"
)

func TestOxygenViewRenders(t *testing.T) {
	to := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -29)
	o := OxygenTide{Title: "Oxygen", From: from, To: to}
	for i := 0; i < 30; i++ {
		if i == 10 || i == 11 {
			continue // a gap the waterline bridges
		}
		d := from.AddDate(0, 0, i)
		o.SpO2 = append(o.SpO2, Point{Day: d, Value: 97 + float64(i%3)/2})
		o.Smooth = append(o.Smooth, Point{Day: d, Value: 97.5})
		o.Band = append(o.Band, BandPoint{Day: d, Lo: 96.8, Hi: 98.2})
		o.BDI = append(o.BDI, Point{Day: d, Value: float64(i % 6)})
	}
	o.Low, o.HasLow = o.SpO2[4], true
	o.Weeks = []WeekAvg{{Start: from.AddDate(0, 0, -int(from.Weekday())), Value: 97.4}, {Start: from.AddDate(0, 0, 7-int(from.Weekday())), Value: 97.6}}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		if _, err := OxygenView(o, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	// no readings yet still draws
	if _, err := OxygenView(OxygenTide{From: from, To: to}, 800, 480); err != nil {
		t.Fatal(err)
	}
	if _, err := OxygenView(o, 300, 200); err == nil {
		t.Error("expected an error for a tiny image")
	}
}
