package dashboard

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// vitalTrace is the nights in each bar's trace, the latest included.
const vitalTrace = 7

// A vital-signs reading: where it comes from and how its zones are set.
type vitalSpec struct {
	label, unit string
	short       string  // the status lamp's code
	sdFloor     float64 // the normal range's narrowest spread (personal zones)
	decimal     int
	signed      bool
	round       bool // a round readout in the center column
	better      int  // +1 higher is better, -1 lower is better, 0 neither
	score       bool // a 0-100 score, zoned by Oura's bands rather than the user's range
}

// The panel, in drawing order: three bars left, three right, and the two
// round readouts — respiration and pulse — in the center.
var vitalSpecs = []vitalSpec{
	{label: "Temp", unit: "°C", short: "TMP", sdFloor: 0.1, decimal: 2, signed: true}, // hundredths: a 0.3 °C threshold is near-missed by 0.01
	{label: "HRV", unit: "ms", short: "HRV", better: 1, sdFloor: 3},
	{label: "SpO2", unit: "%", short: "SPO2", better: 1, sdFloor: 0.5, decimal: 1},
	{label: "Sleep", unit: "score", short: "SLP", better: 1, score: true},
	{label: "Readiness", unit: "score", short: "RDY", better: 1, score: true},
	{label: "Asleep", unit: "hours", short: "TST", sdFloor: 0.25, decimal: 1},
	{label: "Respiration", unit: "br/min", short: "RSP", sdFloor: 0.3, decimal: 1, round: true},
	{label: "Pulse", unit: "bpm", short: "PLS", better: -1, sdFloor: 1.5, round: true},
}

// Instruments builds the vital-signs panel for the latest night with any
// reading: temperature deviation, HRV, blood oxygen, time asleep,
// breathing rate and resting (lowest) heart rate against the user's normal
// range from the 30 nights before (the body-signals method), with caution
// one range-width either side; and the sleep and readiness scores against
// Oura's bands (85 and up optimal, 70 to 84 good).
func Instruments(ctx context.Context, db *store.DB, today time.Time) (chart.Instruments, error) {
	to := date(today)
	p := chart.Instruments{Title: "Vital signs", Subtitle: "no readings yet"}
	series, night, err := vitalSeries(ctx, db, to)
	if err != nil {
		return p, err
	}
	p.Night = night
	if !p.Night.IsZero() {
		p.Subtitle = "night of " + p.Night.AddDate(0, 0, -1).Format("Mon Jan 2")
	}

	for i, sp := range vitalSpecs {
		g := chart.Gauge{Label: sp.label, Unit: sp.unit, Short: sp.short, Decimal: sp.decimal, Signed: sp.signed, Round: sp.round, Better: sp.better}
		vals := series[i]
		if !p.Night.IsZero() {
			for k := vitalTrace - 1; k >= 0; k-- {
				v, ok := vals[p.Night.AddDate(0, 0, -k)]
				if !ok {
					v = math.NaN()
				}
				g.Recent = append(g.Recent, v)
			}
			if v, ok := vals[p.Night]; ok {
				g.Value, g.Has = v, true
			}
			if sp.score {
				g.Lo, g.Hi, g.CautionLo, g.CautionHi, g.HasBand = 85, 100, 70, 100, true
				g.Min, g.Max = 50, 100
				if g.Has && g.Value < 50 {
					g.Min = math.Floor(g.Value/10) * 10
				}
			} else if b, ok := baseline(vals, p.Night, sp.sdFloor); ok {
				w := b.Hi - b.Lo
				g.Lo, g.Hi, g.HasBand = b.Lo, b.Hi, true
				g.CautionLo, g.CautionHi = b.Lo-w, b.Hi+w
				g.Min, g.Max = b.Lo-2*w, b.Hi+2*w
			}
		}
		if !sp.score {
			if !g.HasBand && g.Has {
				g.Min, g.Max = g.Value-1, g.Value+1
			}
			if g.Has {
				g.Min, g.Max = math.Min(g.Min, g.Value), math.Max(g.Max, g.Value)
			}
			// a quantity can't go below zero, nor a percentage above 100
			if !sp.signed {
				g.Min, g.CautionLo = math.Max(0, g.Min), math.Max(0, g.CautionLo)
			}
			if sp.unit == "%" {
				g.Max, g.CautionHi = math.Min(100, g.Max), math.Min(100, g.CautionHi)
			}
			// nobody sleeps 20 hours: past 14, a varied record only
			// squeezes the useful part of the bar
			if sp.unit == "hours" {
				top := 14.0
				if g.Has && g.Value > 13 {
					top = math.Ceil(g.Value + 1)
				}
				g.Max, g.CautionHi = math.Min(top, g.Max), math.Min(top, g.CautionHi)
			}
		}
		p.Gauges = append(p.Gauges, g)
	}
	p.Notes = vitalNotes(p.Gauges)
	return p, nil
}

// vitalNotes picks the night's standouts for the status box, for a night
// with no alerts: first readings beyond their range the good way (HRV
// above it, pulse below), then scores in Oura's optimal band, a score
// that's the best of the week, and HRV or pulse in the better half of its
// range. At most four.
func vitalNotes(gs []chart.Gauge) []string {
	var notes []string
	add := func(s string) {
		if len(notes) < 4 {
			notes = append(notes, s)
		}
	}
	// first, readings beyond their range the good way
	beyond := map[string]bool{}
	for _, g := range gs {
		if !g.Has || !g.HasBand || g.Better == 0 || g.Label == "Sleep" || g.Label == "Readiness" {
			continue
		}
		switch {
		case g.Alert(): // far beyond the good way: an alert, not a standout
		case g.Better > 0 && g.Value > g.Hi:
			add(fmt.Sprintf("%s %.*f · ABOVE RANGE", strings.ToUpper(g.Label), g.Decimal, g.Value))
			beyond[g.Label] = true
		case g.Better < 0 && g.Value < g.Lo:
			add(fmt.Sprintf("%s %.*f · BELOW RANGE", strings.ToUpper(g.Label), g.Decimal, g.Value))
			beyond[g.Label] = true
		}
	}
	upper := func(g chart.Gauge) bool { return g.Value >= (g.Lo+g.Hi)/2 }
	for _, g := range gs {
		if !g.Has || !g.HasBand || beyond[g.Label] {
			continue
		}
		switch g.Label {
		case "Sleep", "Readiness":
			if g.Value >= 85 {
				add(fmt.Sprintf("%s %.0f · OPTIMAL", strings.ToUpper(g.Label), g.Value))
			}
			best := true
			for _, v := range g.Recent[:len(g.Recent)-1] {
				if !math.IsNaN(v) && v >= g.Value {
					best = false
				}
			}
			if best {
				add("BEST " + strings.ToUpper(g.Label) + " THIS WEEK")
			}
		case "HRV":
			if upper(g) {
				add(fmt.Sprintf("HRV %.0f · UPPER HALF", g.Value))
			}
		case "Pulse":
			if !upper(g) {
				add(fmt.Sprintf("PULSE %.0f · LOWER HALF", g.Value))
			}
		}
	}
	return notes
}

// asleepHours is each night's time asleep, in hours, from its main sleep.
func asleepHours(ctx context.Context, db *store.DB, from, to time.Time) (map[time.Time]float64, error) {
	periods, err := db.SleepPeriodsBetween(ctx, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, err
	}
	byDay := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	out := map[time.Time]float64{}
	for day, ps := range byDay {
		m, ok := mainPeriod(ps)
		if !ok || m.TotalSleep == nil {
			continue
		}
		d, err := time.Parse(dayLayout, day)
		if err != nil {
			return nil, fmt.Errorf("dashboard: bad day %q: %w", day, err)
		}
		out[d] = float64(*m.TotalSleep) / 3600
	}
	return out, nil
}

// vitalSeries loads each vital-signs reading's nightly values, in
// vitalSpecs order, for the nights before to that a normal range needs, and
// finds the latest night with any reading within the past week.
func vitalSeries(ctx context.Context, db *store.DB, to time.Time) ([]map[time.Time]float64, time.Time, error) {
	var night time.Time
	early := to.AddDate(0, 0, -(baselineDays + 9))
	limit := baselineDays + 10

	hrv, hr, temp, err := loadSignals(ctx, db, early, to, limit)
	if err != nil {
		return nil, time.Time{}, err
	}
	metric := func(name string) (map[time.Time]float64, error) {
		samples, err := db.Range(ctx, name, limit, early.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return nil, err
		}
		out := map[time.Time]float64{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			out[d] = s.Value
		}
		return out, nil
	}
	spo2, err := metric(store.MetricSpO2)
	if err != nil {
		return nil, time.Time{}, err
	}
	sleep, err := metric(store.MetricSleep)
	if err != nil {
		return nil, time.Time{}, err
	}
	readiness, err := metric(store.MetricReadiness)
	if err != nil {
		return nil, time.Time{}, err
	}
	breath, err := breathByNight(ctx, db, early, to)
	if err != nil {
		return nil, time.Time{}, err
	}
	asleep, err := asleepHours(ctx, db, early, to)
	if err != nil {
		return nil, time.Time{}, err
	}
	series := []map[time.Time]float64{temp, hrv, spo2, sleep, readiness, asleep, breath, hr}

	// the latest night with any reading, within the past week
	for d := to; !d.Before(to.AddDate(0, 0, -7)) && night.IsZero(); d = d.AddDate(0, 0, -1) {
		for _, s := range series {
			if _, ok := s[d]; ok {
				night = d
				break
			}
		}
	}
	return series, night, nil
}
