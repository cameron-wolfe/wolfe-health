package chart

import "testing"

func TestPayoffViewRenders(t *testing.T) {
	p := Payoff{Title: "Bedtime payoff", SlotLen: 30, MinSlotNights: 5, UsualBed: 240, HasUsual: true,
		WinStart: 90, WinEnd: 180, HasWindow: true,
		Stats:   []StatLine{{Label: "Best window", Value: "8:00–10:00 PM"}},
		EarlyBy: "9:21 PM", LateAfter: "10:28 PM", Compare: []StatLine{{Label: "Sleep score", Value: "84 vs 75"}}}
	for i := 0; i < 40; i++ {
		p.Nights = append(p.Nights, PayoffNight{Bed: 150 + float64(i*5), Score: 90 - float64(i)/2})
	}
	for s := 150.0; s < 360; s += 30 {
		p.Slots = append(p.Slots, PayoffSlot{Start: s, Avg: 85 - (s-150)/20, N: 6})
	}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		if _, err := PayoffView(p, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	if _, err := PayoffView(Payoff{}, 800, 480); err != nil {
		t.Fatal(err)
	}
	if hourWords(0) != "6 PM" || hourWords(360) != "12 AM" || hourWords(1080) != "noon" || hourWords(420) != "1 AM" {
		t.Errorf("hour words %q %q %q %q", hourWords(0), hourWords(360), hourWords(1080), hourWords(420))
	}
}
