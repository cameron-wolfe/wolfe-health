package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// StageNights builds the stacked stage bars for the trailing nights, ending
// today. Each night is the day's main sleep period — the same choice the night
// view makes — and its stage times are counted from the same phase data, so a
// bar here always agrees with that night's detailed chart.
func StageNights(ctx context.Context, db *store.DB, days int, today time.Time) (chart.StageNights, error) {
	if days < 2 {
		return chart.StageNights{}, fmt.Errorf("dashboard: days must be at least 2, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	sn := chart.StageNights{
		Title:    fmt.Sprintf("Sleep stages · last %d nights", days),
		Subtitle: "Time in each stage, main sleep of each night",
		From:     from, To: to,
	}
	periods, err := db.SleepPeriodsBetween(ctx, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return sn, err
	}
	byDay := map[string][]store.SleepPeriod{}
	var order []string
	for _, p := range periods {
		if _, seen := byDay[p.Day]; !seen {
			order = append(order, p.Day)
		}
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	for _, day := range order {
		p, _ := mainPeriod(byDay[day])
		d, err := time.Parse(dayLayout, day)
		if err != nil {
			return sn, fmt.Errorf("dashboard: bad day %q: %w", day, err)
		}
		n, ok := stageTimes(p)
		if !ok {
			continue
		}
		n.Day = d
		sn.Nights = append(sn.Nights, n)
	}
	return sn, nil
}

// stageTimes counts a period's time per stage from its phase string, the
// 30-second one when present. A period without phases falls back to Oura's
// own duration fields; one with neither is skipped.
func stageTimes(p store.SleepPeriod) (chart.StageNight, bool) {
	var phases string
	var epoch time.Duration
	switch {
	case p.Phases30Sec != nil && *p.Phases30Sec != "":
		phases, epoch = *p.Phases30Sec, 30*time.Second
	case p.Phases5Min != nil && *p.Phases5Min != "":
		phases, epoch = *p.Phases5Min, 5*time.Minute
	}
	var n chart.StageNight
	if phases != "" {
		for _, s := range decodeStages(phases) {
			switch s {
			case chart.StageDeep:
				n.Deep += epoch
			case chart.StageLight:
				n.Light += epoch
			case chart.StageREM:
				n.REM += epoch
			case chart.StageAwake:
				n.Awake += epoch
			}
		}
		return n, true
	}
	if p.DeepSleep == nil && p.LightSleep == nil && p.RemSleep == nil {
		return n, false
	}
	sec := func(v *int) time.Duration {
		if v == nil {
			return 0
		}
		return time.Duration(*v) * time.Second
	}
	n.Deep, n.Light, n.REM, n.Awake = sec(p.DeepSleep), sec(p.LightSleep), sec(p.RemSleep), sec(p.AwakeTime)
	return n, true
}
