package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// ErrNoSleep means there is no stored sleep period for the requested day.
var ErrNoSleep = errors.New("no sleep period stored for that day")

// Night builds the sleep-stage chart for the night that counts toward day
// (YYYY-MM-DD, Oura's wake-up day). An empty day means the latest stored.
//
// A day can hold several periods: the main sleep, naps, or the fragments of
// a broken night. The chart shows the main sleep (long_sleep) if there is
// one, otherwise the longest period, and the subtitle says when it is one of
// several.
func Night(ctx context.Context, db *store.DB, day string) (chart.Night, error) {
	var n chart.Night
	if day == "" {
		latest, err := db.LatestSleepDay(ctx)
		if err != nil {
			return n, err
		}
		if latest == "" {
			return n, ErrNoSleep
		}
		day = latest
	}
	periods, err := db.SleepPeriodsOnDay(ctx, day)
	if err != nil {
		return n, err
	}
	p, ok := mainPeriod(periods)
	if !ok {
		return n, fmt.Errorf("%w: %s", ErrNoSleep, day)
	}

	start, err := time.Parse(time.RFC3339, p.BedtimeStart)
	if err != nil {
		return n, fmt.Errorf("dashboard: bedtime_start %q: %w", p.BedtimeStart, err)
	}
	end, err := time.Parse(time.RFC3339, p.BedtimeEnd)
	if err != nil {
		return n, fmt.Errorf("dashboard: bedtime_end %q: %w", p.BedtimeEnd, err)
	}
	n.Start, n.End = start, end

	// The 30-second phases are ten times finer than the 5-minute ones and
	// show every brief waking; fall back to 5-minute if a period lacks them.
	switch {
	case p.Phases30Sec != nil && *p.Phases30Sec != "":
		n.Stages, n.StageEpoch = decodeStages(*p.Phases30Sec), 30*time.Second
	case p.Phases5Min != nil:
		n.Stages, n.StageEpoch = decodeStages(*p.Phases5Min), 5*time.Minute
	}
	if p.Movement30s != nil {
		n.Movement, n.MoveEpoch = decodeDigits(*p.Movement30s), 30*time.Second
	}
	if n.HeartRate, err = toSeries(p.HeartRate); err != nil {
		return n, err
	}
	if n.HRV, err = toSeries(p.HRV); err != nil {
		return n, err
	}

	// Times print in the ring's local offset, which the timestamps carry.
	// Title by the evening the night began, which is how people name a night.
	n.Title = "Night of " + start.Format("Mon, Jan 2")
	if p.Type != "long_sleep" {
		n.Title = strings.ToUpper(sleepTypeName(p.Type)[:1]) + sleepTypeName(p.Type)[1:] +
			", " + start.Format("Mon, Jan 2")
	}
	n.Subtitle = fmt.Sprintf("%s – %s · %s in bed",
		start.Format("3:04 PM"), end.Format("3:04 PM"), duration(float64(p.TimeInBed)))
	if len(periods) > 1 {
		n.Subtitle += fmt.Sprintf(" · 1 of %d periods", len(periods))
	}
	if p.TotalSleep != nil {
		n.Headline = duration(float64(*p.TotalSleep)) + " asleep"
	}
	var parts []string
	if p.Efficiency != nil {
		parts = append(parts, fmt.Sprintf("efficiency %d%%", *p.Efficiency))
	}
	if p.Latency != nil {
		parts = append(parts, "asleep in "+duration(float64(*p.Latency)))
	}
	n.Subline = strings.Join(parts, " · ")
	return n, nil
}

// mainPeriod picks the period to chart: the long_sleep, else the longest.
func mainPeriod(ps []store.SleepPeriod) (store.SleepPeriod, bool) {
	var best store.SleepPeriod
	found := false
	for _, p := range ps {
		switch {
		case !found:
			best, found = p, true
		case p.Type == "long_sleep" && best.Type != "long_sleep":
			best = p
		case (p.Type == "long_sleep") == (best.Type == "long_sleep") && p.TimeInBed > best.TimeInBed:
			best = p
		}
	}
	return best, found
}

func sleepTypeName(t string) string {
	switch t {
	case "late_nap":
		return "late nap"
	case "rest":
		return "rest"
	case "sleep":
		return "short sleep" // Oura's type for a period too short to be the main sleep
	}
	return t
}

// decodeStages turns Oura's phase string ('1' deep, '2' light, '3' REM,
// '4' awake) into stages. Any other character is unknown and draws nothing.
func decodeStages(s string) []chart.Stage {
	out := make([]chart.Stage, len(s))
	for i, c := range s {
		if c >= '1' && c <= '4' {
			out[i] = chart.Stage(c - '0')
		}
	}
	return out
}

// decodeDigits turns a string of '1'..'4' into numbers; others become 0.
func decodeDigits(s string) []uint8 {
	out := make([]uint8, len(s))
	for i, c := range s {
		if c >= '1' && c <= '4' {
			out[i] = uint8(c - '0')
		}
	}
	return out
}

func toSeries(s *store.Series) (chart.Series, error) {
	if s == nil {
		return chart.Series{}, nil
	}
	start, err := time.Parse(time.RFC3339, s.Start)
	if err != nil {
		return chart.Series{}, fmt.Errorf("dashboard: series timestamp %q: %w", s.Start, err)
	}
	return chart.Series{
		Start:    start,
		Interval: time.Duration(s.Interval * float64(time.Second)),
		Values:   s.Items,
	}, nil
}
