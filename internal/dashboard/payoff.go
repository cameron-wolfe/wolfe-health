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

// Bedtime payoff.
const (
	payoffSlot       = 30  // minutes per bedtime slot
	payoffMinSlot    = 5   // nights a slot needs before its average joins the line
	payoffMinInBed   = 180 // minutes: a shorter main period is a nap or a fragment, not a bedtime
	payoffMinNights  = 9   // nights before the earliest and latest thirds are compared
	payoffClear      = 2.0 // sleep-score points the thirds must differ by to call it
	payoffLatestBed  = 600 // night-axis minutes (4 AM): a main sleep starting later is a day's sleep, not a bedtime
	payoffBestWithin = 3.0 // sleep-score points from the best slot that still count as the best window
)

// PayoffWindowDays is the nights the best bedtime window is worked out
// from: the bedtime-payoff screen's default, and the window the sleep
// schedule shows, so the two always agree.
const PayoffWindowDays = 180

// BedtimePayoff builds the bedtime-payoff chart for the nights ending on
// the trailing days: each night's bedtime (its main sleep, at least three
// hours in bed, starting between 6 PM and 4 AM) against the sleep score
// that followed, averaged by half
// hour, and the earliest third of bedtimes against the latest third on
// sleep score, HRV and lowest heart rate.
//
// It shows how the user's own nights went, not that bedtime caused it:
// late nights differ in other ways too.
func BedtimePayoff(ctx context.Context, db *store.DB, days int, today time.Time) (chart.Payoff, error) {
	if days < 30 {
		return chart.Payoff{}, fmt.Errorf("dashboard: days must be at least 30, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	p := chart.Payoff{
		Title:         "Bedtime payoff · " + spanWords(days),
		Subtitle:      "Each night's bedtime against the sleep score that followed",
		SlotLen:       payoffSlot,
		MinSlotNights: payoffMinSlot,
	}

	periods, err := db.SleepPeriodsBetween(ctx, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return p, err
	}
	scores, err := db.Range(ctx, store.MetricSleep, days+1, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return p, err
	}
	scoreOn := map[string]float64{}
	for _, s := range scores {
		scoreOn[s.Day] = s.Value
	}
	byDay := map[string][]store.SleepPeriod{}
	for _, sp := range periods {
		byDay[sp.Day] = append(byDay[sp.Day], sp)
	}

	type night struct {
		bed, score float64
		hrv, hr    *int
		asleep     *int // seconds of sleep in the main period
	}
	var nights []night
	for day, ps := range byDay {
		m, ok := mainPeriod(ps)
		sc, scored := scoreOn[day]
		if !ok || !scored || m.Type == "rest" || m.TimeInBed < payoffMinInBed*60 {
			continue
		}
		bed, err := time.Parse(time.RFC3339, m.BedtimeStart)
		if err != nil {
			continue
		}
		pos := chart.NightClock(bed)
		if pos < 0 || pos > payoffLatestBed { // the afternoon, or after 4 AM: not a night's bedtime
			continue
		}
		nights = append(nights, night{pos, sc, m.AverageHRV, m.LowestHR, m.TotalSleep})
	}
	sort.Slice(nights, func(i, j int) bool { return nights[i].bed < nights[j].bed })
	if len(nights) == 0 {
		p.Stats = []chart.StatLine{{Label: "No scored nights yet", Value: "—"}}
		return p, nil
	}

	// the nights, and their half-hour slots
	slots := map[float64]*chart.PayoffSlot{}
	var beds []float64
	for _, n := range nights {
		p.Nights = append(p.Nights, chart.PayoffNight{Bed: n.bed, Score: n.score})
		beds = append(beds, n.bed)
		start := math.Floor(n.bed/payoffSlot) * payoffSlot
		s, ok := slots[start]
		if !ok {
			s = &chart.PayoffSlot{Start: start}
			slots[start] = s
		}
		s.Avg = (s.Avg*float64(s.N) + n.score) / float64(s.N+1)
		s.N++
	}
	for _, s := range slots {
		p.Slots = append(p.Slots, *s)
	}
	sort.Slice(p.Slots, func(i, j int) bool { return p.Slots[i].Start < p.Slots[j].Start })
	p.UsualBed, p.HasUsual = medianOf(beds), true

	// Oura's latest bedtime window, for reference
	windows, err := db.SleepTimesBetween(ctx, from.Format(dayLayout), to.AddDate(0, 0, 1).Format(dayLayout))
	if err != nil {
		return p, err
	}
	for i := len(windows) - 1; i >= 0; i-- {
		w := windows[i]
		if w.StartOffset == nil || w.EndOffset == nil || w.DayTZ == nil {
			continue
		}
		d, err := time.Parse(dayLayout, w.Day)
		if err != nil {
			continue
		}
		midnight := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.FixedZone("", *w.DayTZ))
		p.WinStart = chart.NightClock(midnight.Add(time.Duration(*w.StartOffset) * time.Second))
		p.WinEnd = p.WinStart + float64(*w.EndOffset-*w.StartOffset)/60
		p.HasWindow = true
		break
	}

	// The best window: around the best trusted slot, the run of trusted
	// slots whose average is within payoffBestWithin of it. A thin or
	// missing slot ends the run, so the window is only what the nights show.
	best := -1
	for i, s := range p.Slots {
		if s.N >= payoffMinSlot && (best < 0 || s.Avg > p.Slots[best].Avg) {
			best = i
		}
	}
	if best >= 0 {
		ok := func(i int) bool {
			s := p.Slots[i]
			return s.N >= payoffMinSlot && s.Avg >= p.Slots[best].Avg-payoffBestWithin
		}
		lo, hi := best, best
		for lo > 0 && p.Slots[lo-1].Start == p.Slots[lo].Start-payoffSlot && ok(lo-1) {
			lo--
		}
		for hi+1 < len(p.Slots) && p.Slots[hi+1].Start == p.Slots[hi].Start+payoffSlot && ok(hi+1) {
			hi++
		}
		p.BestStart, p.BestEnd, p.HasBest = p.Slots[lo].Start, p.Slots[hi].Start+payoffSlot, true
		window := clockRange(p.BestStart, p.BestEnd)
		p.Stats = append(p.Stats, chart.StatLine{Label: "Best window", Value: window})
		p.Stats = append(p.Stats, chart.StatLine{Label: "Usual bedtime", Value: chart.ClockLabel(p.UsualBed)})
		var where, short string
		switch u := p.UsualBed; {
		case u < p.BestStart:
			where = "no, " + duration((p.BestStart-u)*60) + " early"
			short = "usual " + chart.ClockLabel(u) + ", early"
		case u > p.BestEnd:
			where = "no, " + duration((u-p.BestEnd)*60) + " late"
			short = "usual " + chart.ClockLabel(u) + ", late"
		default:
			where = "yes, " + duration((p.BestEnd-u)*60) + " to spare"
			short = "usual " + chart.ClockLabel(u) + ", inside"
		}
		p.Stats = append(p.Stats, chart.StatLine{Label: "Inside?", Value: where})
		p.Headline = "best bedtime " + window + " · " + short
	}
	p.Subtitle += fmt.Sprintf(" (%d nights)", len(nights))
	if len(nights) < payoffMinNights {
		if p.Headline == "" {
			p.Headline = "too few nights to compare yet"
		}
		return p, nil
	}

	// the earliest third of bedtimes against the latest third
	third := len(nights) / 3
	early, late := nights[:third], nights[len(nights)-third:]
	type avgs struct {
		score, hrv, hr, asleep   float64
		hasHRV, hasHR, hasAsleep bool
	}
	average := func(ns []night) avgs {
		var a avgs
		var hrvN, hrN, asleepN int
		for _, n := range ns {
			a.score += n.score
			if n.asleep != nil {
				a.asleep += float64(*n.asleep)
				asleepN++
			}
			if n.hrv != nil {
				a.hrv += float64(*n.hrv)
				hrvN++
			}
			if n.hr != nil {
				a.hr += float64(*n.hr)
				hrN++
			}
		}
		a.score /= float64(len(ns))
		if hrvN > 0 {
			a.hrv, a.hasHRV = a.hrv/float64(hrvN), true
		}
		if hrN > 0 {
			a.hr, a.hasHR = a.hr/float64(hrN), true
		}
		if asleepN > 0 {
			a.asleep, a.hasAsleep = a.asleep/float64(asleepN), true
		}
		return a
	}
	e, l := average(early), average(late)
	// the earliest third against the latest, side by side
	p.EarlyBy, p.LateAfter = chart.ClockLabel(early[len(early)-1].bed), chart.ClockLabel(late[0].bed)
	p.Compare = append(p.Compare, chart.StatLine{Label: "Sleep score", Value: score(e.score) + " vs " + score(l.score)})
	// time asleep: for most people with a set wake time, the reason a later
	// bedtime scores lower
	if e.hasAsleep && l.hasAsleep {
		p.Compare = append(p.Compare, chart.StatLine{Label: "Asleep", Value: duration(e.asleep) + " vs " + duration(l.asleep)})
	}
	if e.hasHRV && l.hasHRV {
		p.Compare = append(p.Compare, chart.StatLine{Label: "HRV", Value: fmt.Sprintf("%.0f vs %.0f ms", e.hrv, l.hrv)})
	}
	if e.hasHR && l.hasHR {
		p.Compare = append(p.Compare, chart.StatLine{Label: "Lowest HR", Value: fmt.Sprintf("%.0f vs %.0f bpm", e.hr, l.hr)})
	}
	if p.Headline == "" { // no best window: say which way the thirds lean
		switch diff := e.score - l.score; {
		case diff >= payoffClear:
			p.Headline = fmt.Sprintf("earlier bedtimes: +%.0f sleep score", diff)
		case diff <= -payoffClear:
			p.Headline = fmt.Sprintf("later bedtimes: +%.0f sleep score", -diff)
		default:
			p.Headline = "no clear payoff either way"
		}
	}
	return p, nil
}

// clockRange words a night-axis span, sharing the AM/PM when both ends
// have it: "8:00–10:00 PM", "11:30 PM–12:30 AM".
func clockRange(from, to float64) string {
	a, b := chart.ClockLabel(from), chart.ClockLabel(to)
	if len(a) > 3 && len(b) > 3 && a[len(a)-2:] == b[len(b)-2:] {
		return a[:len(a)-3] + "–" + b
	}
	return a + "–" + b
}
