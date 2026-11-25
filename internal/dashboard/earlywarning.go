package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/habits"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// WarnDays is the early-warning screen's default window: three weeks, wide
// enough to see a few nights' drift against what came before.
const WarnDays = 21

// warnWatch is how many signals must be up together before a night is
// shaded: one alone is everyday noise; two or more moving the same way is
// the pattern that often comes a day or two before feeling ill.
const warnWatch = 2

// breathSignal is breathing rate as a body signal, for the early warning.
var breathSignal = signalSpec{"Breathing", "br/min", 0.3, func(v float64) string { return fmt.Sprintf("%.1f br/min", v) }, func(v float64) string { return fmt.Sprintf("%.1f", v) }}

// warnHabits are the habits that can push these signals the wrong way on
// their own, so a night after one has an explanation besides illness.
// Sick is here too: a logged sick day marks what the chart is looking for.
var warnHabits = []string{"Alcohol", "Late night out", "Workout", "Stressful day", "Travel", "Cannabis", "Sick"}

// EarlyWarning builds the early-warning chart for the trailing days:
// temperature, lowest heart rate, HRV and breathing rate, each night
// placed against the user's normal range from the 30 nights before it (as
// on body signals and the warning banner), all turned so that up is the
// direction that comes before illness. It counts the signals up each
// night, and marks what was logged before a night that could explain it.
// It describes; it doesn't diagnose.
func EarlyWarning(ctx context.Context, db *store.DB, days int, today time.Time, loc *time.Location) (chart.EarlyWarning, error) {
	if days < 7 {
		return chart.EarlyWarning{}, fmt.Errorf("dashboard: days must be at least 7, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	early := from.AddDate(0, 0, -baselineDays)
	e := chart.EarlyWarning{
		Title:    "Early warning · last " + fmt.Sprint(days) + " nights",
		Subtitle: "Each night against your normal; up is toward illness: warmer, faster heart, lower HRV, faster breathing",
		From:     from, To: to,
		Watch: warnWatch,
	}

	hrv, hr, temp, err := loadSignals(ctx, db, early, to, days+baselineDays+1)
	if err != nil {
		return e, err
	}
	breath, err := breathByNight(ctx, db, early, to)
	if err != nil {
		return e, err
	}

	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		e.Days = append(e.Days, chart.WarnDay{Day: d})
	}
	lanes := []struct {
		spec signalSpec
		vals map[time.Time]float64
		dir  float64 // +1: higher is toward illness; −1: lower is
		hint string
	}{
		{tempSignal, temp, 1, "up: warmer"},
		{hrSignal, hr, 1, "up: faster heart"},
		{hrvSignal, hrv, -1, "up: lower HRV"},
		{breathSignal, breath, 1, "up: faster breathing"},
	}
	var upLast []string // signals up last night
	for _, l := range lanes {
		lane := chart.WarnLane{Title: l.spec.title, Hint: l.hint, Value: "—", Status: "no reading yet"}
		s := map[time.Time]float64{}
		var latest time.Time
		for i, d := range e.Days {
			v, has := l.vals[d.Day]
			if !has {
				continue
			}
			latest = d.Day
			band, ok := baseline(l.vals, d.Day, l.spec.sdFloor)
			if !ok {
				continue
			}
			mid, half := (band.Lo+band.Hi)/2, (band.Hi-band.Lo)/2
			st := l.dir * (v - mid) / half
			s[d.Day] = st
			nt := chart.WarnNight{Day: d.Day, S: st, Up: st > 1}
			lane.Nights = append(lane.Nights, nt)
			e.Days[i].Of++
			if nt.Up {
				e.Days[i].Up++
			}
		}
		// the 3-night average, where at least two of the three nights read
		for _, d := range e.Days {
			sum, k := 0.0, 0
			for j := 0; j < 3; j++ {
				if v, ok := s[d.Day.AddDate(0, 0, -j)]; ok {
					sum += v
					k++
				}
			}
			if k >= 2 {
				lane.Avg = append(lane.Avg, chart.Point{Day: d.Day, Value: sum / float64(k)})
			}
		}
		// the status words describe the reading as measured: HRV toward
		// illness is low
		toward, better := "high", "low"
		if l.dir < 0 {
			toward, better = better, toward
		}
		if !latest.IsZero() {
			lane.Value = l.spec.format(l.vals[latest])
			st, ok := s[latest]
			switch {
			case !ok:
				lane.Status = "building your range"
			case st > 1:
				streak := 0
				for d := latest; s[d] > 1; d = d.AddDate(0, 0, -1) {
					streak++
				}
				lane.Status = toward + ", toward illness"
				if streak > 1 {
					lane.Status = fmt.Sprintf("%s for %d nights", toward, streak)
				}
				if latest.Equal(to) {
					upLast = append(upLast, strings.ToLower(l.spec.title))
				}
			case st < -1:
				lane.Status = better + ", better than normal"
			default:
				lane.Status = "normal"
			}
			if !latest.Equal(to) {
				lane.Status = "night of " + chart.NightOf(latest).Format("Jan 2") + ": " + lane.Status
			}
		}
		e.Lanes = append(e.Lanes, lane)
	}

	// the habits logged before each night that could explain it
	nh, err := habitNights(ctx, db, from, to, loc)
	if err != nil {
		return e, err
	}
	seen := map[string]bool{}
	for i, d := range e.Days {
		got := nh[d.Day.Format(dayLayout)]
		if got == nil {
			continue
		}
		for _, name := range warnHabits {
			if _, ok := got.count[name]; ok {
				h := habits.ByName(name)
				e.Days[i].Habits = append(e.Days[i].Habits, h.Code)
				seen[name] = true
			}
		}
	}
	for _, name := range warnHabits {
		if seen[name] {
			e.HabitKey = append(e.HabitKey, habits.ByName(name).Code+" "+strings.ToLower(name))
		}
	}

	// the headline: last night, and how long two or more have been up
	last := e.Days[len(e.Days)-1]
	streak := 0
	for i := len(e.Days) - 1; i >= 0 && e.Days[i].Up >= warnWatch; i-- {
		streak++
	}
	switch {
	case last.Of == 0:
		e.Headline = "no readings for last night yet"
	case streak > 1:
		e.Headline = fmt.Sprintf("Watch: %d signals up, %d nights running", last.Up, streak)
	case streak == 1:
		e.Headline = fmt.Sprintf("Watch: %d signals up last night", last.Up)
	case last.Up == 1:
		e.Headline = "1 signal up last night: " + upLast[0]
	default:
		e.Headline = "all signals normal last night"
	}
	return e, nil
}
