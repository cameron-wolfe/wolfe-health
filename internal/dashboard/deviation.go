package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// deviationOrder is the radar's spokes, clockwise from the top: the heart's
// signals, then breathing and blood oxygen, temperature, and sleep.
var deviationOrder = []string{"HRV", "Pulse", "Respiration", "SpO2", "Temp", "Asleep", "Sleep", "Readiness"}

// deviationUnits is how each reading's value is worded on the radar.
var deviationUnits = map[string]string{
	"HRV": " ms", "Pulse": " bpm", "Respiration": " br/min", "SpO2": "%",
	"Temp": " °C", "Asleep": " h", "Sleep": "", "Readiness": "",
}

// scoreSDFloor is the narrowest spread of a score's normal range, in points.
const scoreSDFloor = 3

// Deviation builds the deviation radar for the latest night with any
// reading: each vital-signs reading as its distance from the user's own
// normal (from the 30 nights before, the body-signals method — scores
// included, unlike the vital-signs panel's Oura bands), in half-ranges, with
// the six nights before averaged for comparison. A reading counts as off
// normal by the vital-signs panel's rules: either way for a neutral signal,
// the bad way for one with a better direction, and the good way only far
// beyond.
func Deviation(ctx context.Context, db *store.DB, today time.Time) (chart.DeviationRadar, error) {
	to := date(today)
	d := chart.DeviationRadar{
		Title:    "WolfeSense · Signals",
		Subtitle: "Each reading against your normal range: outward is above, inward is below",
	}
	series, night, err := vitalSeries(ctx, db, to)
	if err != nil {
		return d, err
	}
	index := map[string]int{}
	for i, sp := range vitalSpecs {
		index[sp.label] = i
	}
	if !night.IsZero() {
		d.Title += " · night of " + night.AddDate(0, 0, -1).Format("Jan 2")
	}

	var off []string
	inNormal, read := 0, 0
	for _, label := range deviationOrder {
		i := index[label]
		sp, vals := vitalSpecs[i], series[i]
		s := chart.DeviationSpoke{Label: sp.label}
		floor := sp.sdFloor
		if sp.score {
			floor = scoreSDFloor
		}
		b, banded := chart.BandPoint{}, false
		if !night.IsZero() {
			b, banded = baseline(vals, night, floor)
		}
		v, has := vals[night]
		if banded && has {
			mid, half := (b.Lo+b.Hi)/2, math.Max((b.Hi-b.Lo)/2, 1e-9)
			s.D, s.Has = (v-mid)/half, true
			g := chart.Gauge{Value: v, Has: true, Lo: b.Lo, Hi: b.Hi, HasBand: true,
				CautionLo: b.Lo - 2*half, CautionHi: b.Hi + 2*half, Better: sp.better}
			s.Alert = g.Alert()
			unit := deviationUnits[label]
			s.Text = fmt.Sprintf("%s%s", signedIf(sp.signed, v, sp.decimal), unit)
			read++
			if math.Abs(s.D) <= 1 {
				inNormal++
			} else {
				off = append(off, sp.label)
			}
			// the six nights before: their average shape, and the change
			var sum, raw float64
			n := 0
			for k := 1; k <= 6; k++ {
				if pv, ok := vals[night.AddDate(0, 0, -k)]; ok {
					sum += (pv - mid) / half
					raw += pv
					n++
				}
			}
			if n > 0 {
				s.AvgD, s.HasAvg = sum/float64(n), true
				s.Change = changeText(v-raw/float64(n), sp.decimal, "") // the value carries the unit
			}
		}
		d.Spokes = append(d.Spokes, s)
	}

	d.PanelTitle, d.PanelVs = "LAST NIGHT", "vs 6 nights before"
	d.TakeawayHead, d.TakeawayNone = "Off normal:", "Every reading within your normal"
	d.Takeaway = off
	switch {
	case read == 0:
		d.Headline = "no readings yet"
		return d, nil
	case len(off) == 0:
		d.Headline = "all readings normal"
	default:
		d.Headline = fmt.Sprintf("%d %s off normal", len(off), plural(len(off), "reading", "readings"))
	}
	d.SummaryLabel, d.Summary = "In your normal", fmt.Sprintf("%d/%d", inNormal, read)
	return d, nil
}

// signedIf formats v with decimals, with a "+" when signed and positive.
func signedIf(signed bool, v float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, v)
	if signed && v > 0 {
		s = "+" + s
	}
	return s
}

// changeText words a change in a reading's units: "+4 ms", "−0.3 °C",
// "same" when it rounds to nothing.
func changeText(diff float64, decimals int, unit string) string {
	size := fmt.Sprintf("%.*f", decimals, math.Abs(diff))
	if size == fmt.Sprintf("%.*f", decimals, 0.0) {
		return "same"
	}
	sign := "+"
	if diff < 0 {
		sign = "−"
	}
	return sign + size + unit
}
