package chart

import "testing"

func TestDeviationViewRenders(t *testing.T) {
	d := DeviationRadar{Title: "WolfeSense · Signals", Headline: "1 reading off normal",
		Spokes: []DeviationSpoke{
			{Label: "HRV", Text: "57 ms", D: 0.4, Has: true, AvgD: 0.1, HasAvg: true},
			{Label: "Pulse", Text: "47 bpm", D: -0.8, Has: true},
			{Label: "Temp", Text: "+0.8 °C", D: 3.5, Has: true, Alert: true}, // past the rim: clamped
			{Label: "SpO2"}, // no reading: left out of the shape
			{Label: "Sleep", Text: "66", D: -2.1, Has: true, Alert: true, AvgD: -0.5, HasAvg: true},
		},
		SummaryLabel: "In your normal", Summary: "2/4", TakeawayHead: "Off normal:", Takeaway: []string{"Temp", "Sleep"}}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}, {800, 400}} {
		if _, err := DeviationView(d, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	if _, err := DeviationView(DeviationRadar{Spokes: d.Spokes[:2]}, 800, 480); err == nil {
		t.Error("expected an error for too few spokes")
	}
}
