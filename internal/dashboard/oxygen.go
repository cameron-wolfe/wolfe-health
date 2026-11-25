package dashboard

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Oxygen smoothing and trend.
const (
	tideWindow    = 3   // nights each side of a night in its average: 7 in all
	tideMinNights = 3   // fewer readings in a window: no average there
	bdiSDFloor    = 2   // the dips' narrowest spread: zero-dip runs don't make 1 or 2 dips unusual
	spo2SDFloor   = 0.5 // the normal range's narrowest spread, in points
	trendDays     = 14  // the headline compares the average with this long ago
	steadyWithin  = 0.3 // a change smaller than this, in points, is steady
	minWeekNights = 3   // a week needs this many readings for its average
)

// OxygenTide builds the oxygen tide chart for the trailing days, from
// Oura's daily SpO2 record (each night's average blood oxygen and breathing
// disturbance index), with breathing rate from the main sleep period in the
// side panel.
//
// A single night's SpO2 moves a point or two for no reason that matters, so
// the chart leads with the 7-night average and its direction, and judges
// single nights against the user's own normal range — the body-signals
// method, from the 30 nights before each — rather than a fixed line. It
// reports readings; it does not diagnose.
func OxygenTide(ctx context.Context, db *store.DB, days int, today time.Time) (chart.OxygenTide, error) {
	if days < 7 {
		return chart.OxygenTide{}, fmt.Errorf("dashboard: days must be at least 7, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	early := from.AddDate(0, 0, -(baselineDays + trendDays))
	o := chart.OxygenTide{
		Title:    fmt.Sprintf("Oxygen tide · last %d nights", days),
		Subtitle: "Blood oxygen: the 7-night trend against your normal range",
		From:     from, To: to,
	}
	load := func(metric string, since time.Time) (monthAvg, error) {
		n := int(to.Sub(since).Hours()/24) + 1
		samples, err := db.Range(ctx, metric, n, since.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return nil, err
		}
		out := monthAvg{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			out[d] = s.Value
		}
		return out, nil
	}
	all, err := load(store.MetricSpO2, early) // with the history the ranges need
	if err != nil {
		return o, err
	}
	bdi, err := load(store.MetricBreathingDI, early) // with history, as for SpO2
	if err != nil {
		return o, err
	}
	breath, err := breathByNight(ctx, db, from, to)
	if err != nil {
		return o, err
	}

	smooth := smoothed(all, early, to)
	spo2 := monthAvg{}
	var low *chart.Point
	var lowDays []time.Time
	var latestBand *chart.BandPoint
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if v, ok := smooth[d]; ok {
			o.Smooth = append(o.Smooth, chart.Point{Day: d, Value: v})
		}
		b, banded := baseline(all, d, spo2SDFloor)
		if banded {
			o.Band = append(o.Band, b)
			bb := b
			latestBand = &bb
		}
		v, ok := all[d]
		if !ok {
			continue
		}
		spo2[d] = v
		o.SpO2 = append(o.SpO2, chart.Point{Day: d, Value: v})
		if banded && v < b.Lo {
			lowDays = append(lowDays, d)
			if low == nil || v < low.Value {
				low = &chart.Point{Day: d, Value: v}
			}
		}
	}
	if low != nil {
		o.Low, o.HasLow = *low, true
	}
	// Breathing dips: Oura's disturbance index counts dips in blood oxygen,
	// and a few are usual, so a night is marked only when it had more than
	// is usual for the user — above the top of the range from the 30 nights
	// before it, the method the SpO2 band uses.
	extra, dipNights, dipSum := map[time.Time]bool{}, 0, 0.0
	var dipLine *chart.BandPoint // the latest night's range: where marking starts
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		v, ok := bdi[d]
		if !ok {
			continue
		}
		dipNights++
		dipSum += v
		b, banded := baseline(bdi, d, bdiSDFloor)
		if banded {
			bb := b
			dipLine = &bb
		}
		if banded && v > b.Hi {
			o.BDI = append(o.BDI, chart.Point{Day: d, Value: v})
			extra[d] = true
		}
	}
	o.Weeks = weeklyAverages(spo2, from, to)

	pct := func(v float64) string { return fmt.Sprintf("%.1f%%", v) }
	if len(o.SpO2) == 0 {
		o.Stats = append(o.Stats, chart.StatLine{Label: "No oxygen readings", Value: "—"})
	} else {
		last := o.SpO2[len(o.SpO2)-1]
		label := "Last night"
		if !last.Day.Equal(to) {
			label = "Night of " + chart.NightOf(last.Day).Format("Jan 2")
		}
		o.Stats = append(o.Stats, chart.StatLine{Label: label, Value: pct(last.Value)})
	}
	if len(o.Smooth) > 0 {
		cur := o.Smooth[len(o.Smooth)-1]
		o.Headline = pct(cur.Value) + " · " + trendWords(smooth, cur)
		o.Stats = append(o.Stats, chart.StatLine{Label: "7-night average", Value: pct(cur.Value)})
	}
	if latestBand != nil {
		o.Stats = append(o.Stats, chart.StatLine{Label: "Your normal",
			Value: fmt.Sprintf("%.1f–%.1f%%", latestBand.Lo, latestBand.Hi)})
	}
	if len(o.SpO2) > 0 {
		n := len(lowDays)
		o.Stats = append(o.Stats, chart.StatLine{Label: "Low for you",
			Value: fmt.Sprintf("%d %s", n, plural(n, "night", "nights"))})
	}
	// were the low nights the ones with extra dips? Counted among low
	// nights with a dip reading.
	lowExtra, lowRead := 0, 0
	for _, d := range lowDays {
		if _, ok := bdi[d]; ok {
			lowRead++
			if extra[d] {
				lowExtra++
			}
		}
	}
	if dipNights > 0 {
		// the baseline: the average night's dips, and the count that gets
		// a crest — the first whole number above the latest range's top
		o.Stats = append(o.Stats, chart.StatLine{Label: "Dips a night, avg",
			Value: fmt.Sprintf("%.1f", dipSum/float64(dipNights))})
		if dipLine != nil {
			o.Stats = append(o.Stats, chart.StatLine{Label: "Crest at",
				Value: fmt.Sprintf("%d+ dips", int(math.Floor(dipLine.Hi))+1)})
		}
		o.Stats = append(o.Stats, chart.StatLine{Label: "Extra-dip nights",
			Value: fmt.Sprintf("%d of %d", len(o.BDI), dipNights)})
	}
	if lowRead > 0 {
		o.Stats = append(o.Stats, chart.StatLine{Label: "Dips on low nights",
			Value: fmt.Sprintf("%d of %d", lowExtra, lowRead)})
	}
	if len(breath) > 0 {
		var lastDay time.Time
		for d := range breath {
			if d.After(lastDay) {
				lastDay = d
			}
		}
		avg, _ := breath.mean()
		o.Stats = append(o.Stats, chart.StatLine{Label: "Breaths/min",
			Value: fmt.Sprintf("%.1f · avg %.1f", breath[lastDay], avg)})
	}
	return o, nil
}

// smoothed is each day's average of the readings within tideWindow nights
// either side, where there are at least tideMinNights of them. At the end
// of the record the window holds only earlier nights, so the latest value
// is a trailing average.
func smoothed(vals monthAvg, from, to time.Time) monthAvg {
	out := monthAvg{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		var sum float64
		n := 0
		for k := -tideWindow; k <= tideWindow; k++ {
			if v, ok := vals[d.AddDate(0, 0, k)]; ok {
				sum += v
				n++
			}
		}
		// a day past the last reading isn't averaged into existence
		if _, ok := vals[d]; !ok && !hasAfter(vals, d, to) {
			continue
		}
		if n >= tideMinNights {
			out[d] = sum / float64(n)
		}
	}
	return out
}

// hasAfter reports whether vals has a reading after d, up to to.
func hasAfter(vals monthAvg, d, to time.Time) bool {
	for e := d.AddDate(0, 0, 1); !e.After(to); e = e.AddDate(0, 0, 1) {
		if _, ok := vals[e]; ok {
			return true
		}
	}
	return false
}

// trendWords says where the average has gone over the last trendDays:
// "steady over 2 weeks", "down 0.8 in 2 weeks", "up 0.5 in 2 weeks".
func trendWords(smooth monthAvg, cur chart.Point) string {
	// the average nearest to trendDays before, looking back a few days more
	for k := 0; k <= tideWindow; k++ {
		then, ok := smooth[cur.Day.AddDate(0, 0, -trendDays-k)]
		if !ok {
			continue
		}
		diff := cur.Value - then
		switch {
		case math.Abs(diff) < steadyWithin:
			return "steady over 2 weeks"
		case diff < 0:
			return fmt.Sprintf("down %.1f in 2 weeks", -diff)
		}
		return fmt.Sprintf("up %.1f in 2 weeks", diff)
	}
	return "7-night average"
}

// weeklyAverages averages the readings in each Sunday-to-Saturday week that
// overlaps from..to, for weeks with at least minWeekNights of them.
func weeklyAverages(vals monthAvg, from, to time.Time) []chart.WeekAvg {
	sums := map[time.Time][2]float64{}
	for d, v := range vals {
		if d.Before(from) || d.After(to) {
			continue
		}
		w := WeekStart(d)
		s := sums[w]
		sums[w] = [2]float64{s[0] + v, s[1] + 1}
	}
	var out []chart.WeekAvg
	for w, s := range sums {
		if s[1] >= minWeekNights {
			out = append(out, chart.WeekAvg{Start: w, Value: s[0] / s[1]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// breathByNight is each night's average breathing rate, from its main
// sleep period.
func breathByNight(ctx context.Context, db *store.DB, from, to time.Time) (monthAvg, error) {
	periods, err := db.SleepPeriodsBetween(ctx, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, err
	}
	byNight := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byNight[p.Day] = append(byNight[p.Day], p)
	}
	out := monthAvg{}
	for day, ps := range byNight {
		p, ok := mainPeriod(ps)
		if !ok || p.AverageBreath == nil || *p.AverageBreath <= 0 {
			continue
		}
		d, err := time.Parse(dayLayout, day)
		if err != nil {
			return nil, fmt.Errorf("dashboard: bad day %q: %w", day, err)
		}
		out[d] = *p.AverageBreath
	}
	return out, nil
}
