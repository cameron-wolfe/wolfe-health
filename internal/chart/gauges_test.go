package chart

import (
	"testing"
	"time"
)

func TestGaugeZones(t *testing.T) {
	g := Gauge{Lo: 40, Hi: 60, CautionLo: 30, CautionHi: 70, HasBand: true, Has: true}
	for v, want := range map[float64]string{50: "", 39: "LOW", 61: "HIGH"} {
		g.Value = v
		if got := g.status(); got != want {
			t.Errorf("status(%v) = %q, want %q", v, got, want)
		}
	}
	for v, want := range map[float64]int{50: 0, 35: 1, 65: 1, 25: 2, 75: 2} {
		if got := g.zone(v); got != want {
			t.Errorf("zone(%v) = %d, want %d", v, got, want)
		}
	}
	if got := (Gauge{Signed: true, Decimal: 1}).format(-0.2); got != "−0.2" {
		t.Errorf("format = %q", got)
	}
	if niceStep(7) != 10 || niceStep(0.13) != 0.2 || niceStep(3) != 5 {
		t.Errorf("niceStep: %v %v %v", niceStep(7), niceStep(0.13), niceStep(3))
	}
}

func TestInstrumentsViewRenders(t *testing.T) {
	p := Instruments{Title: "Vital signs", Night: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC),
		Gauges: []Gauge{
			{Label: "Temp", Unit: "°C", Value: 0.6, Has: true, Lo: -0.2, Hi: 0.2, CautionLo: -0.6, CautionHi: 0.6, Min: -1, Max: 1, HasBand: true, Decimal: 1, Signed: true},
			{Label: "HRV", Unit: "ms"},
			{Label: "Sleep", Unit: "score", Value: 78, Has: true, Lo: 85, Hi: 100, CautionLo: 70, CautionHi: 100, Min: 50, Max: 100, HasBand: true},
			{Label: "Pulse", Unit: "bpm", Value: 50, Has: true, Lo: 47, Hi: 53, HasBand: true, Round: true},
		}}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		if _, err := InstrumentsView(p, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	if _, err := InstrumentsView(Instruments{}, 800, 480); err != nil {
		t.Fatal(err)
	}
}

// Ladder numbers carry only the decimals their step needs.
func TestGaugeTick(t *testing.T) {
	g := Gauge{Decimal: 1}
	for _, c := range []struct {
		v, step float64
		want    string
	}{{96, 2, "96"}, {96.5, 0.5, "96.5"}, {0.25, 0.05, "0.25"}} {
		if got := g.tick(c.v, c.step); got != c.want {
			t.Errorf("tick(%v, %v) = %q, want %q", c.v, c.step, got, c.want)
		}
	}
	if got := (Gauge{Signed: true}).tick(0.5, 0.5); got != "+0.5" {
		t.Errorf("signed tick = %q", got)
	}
}

// Off-range the good way isn't an alert: high HRV, a low pulse. The bad
// way is, and a neutral signal alerts either way.
func TestGaugeAlert(t *testing.T) {
	g := Gauge{Lo: 40, Hi: 60, CautionLo: 20, CautionHi: 80, HasBand: true, Has: true}
	for _, c := range []struct {
		better int
		v      float64
		want   bool
	}{
		{1, 70, false}, {1, 30, true}, {-1, 30, false}, {-1, 70, true},
		{0, 70, true}, {0, 30, true}, {1, 50, false},
		{1, 90, true}, {-1, 10, true}, // far beyond the good way: flagged
	} {
		g.Better, g.Value = c.better, c.v
		if got := g.Alert(); got != c.want {
			t.Errorf("better %d, value %v: alert %v, want %v", c.better, c.v, got, c.want)
		}
	}
}

func TestGaugeAlertText(t *testing.T) {
	g := Gauge{Lo: 40, Hi: 60, CautionLo: 20, CautionHi: 80, HasBand: true, Has: true, Better: 1}
	g.Value = 90
	if got := g.alertText(); got != "VERY HIGH" {
		t.Errorf("far above, higher better: %q", got)
	}
	g.Value = 30
	if got := g.alertText(); got != "LOW" {
		t.Errorf("below, higher better: %q", got)
	}
}
