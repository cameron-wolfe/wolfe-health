package server

import (
	"context"
	"encoding/json"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
)

// Detail views: each screen's closer looks, so a display can show one main
// screen and let you step into what's behind it, instead of the schedule
// rotating through everything. A tablet taps its way in (the emulator's
// detail strip); a TRMNL steps through them with its button.

// screenDetails are each screen's detail views, most useful first: where a
// look at this screen most often leads. Every screen the schedule offers
// has some, and each names only screens the schedule offers.
var screenDetails = map[string][]string{
	// the morning: the brief opens onto last night and the body
	// (heart rate today is the brief's Today zone, not a button)
	scrDaily:  {scrNight, scrSleepBreakdown, scrReadinessParts, scrVitals, scrEarlyWarning, scrThisWeek},
	scrDigest: {scrNight, scrSleepBreakdown, scrReadinessParts, scrVitals, scrEarlyWarning, scrThisWeek},
	scrNight:  {scrMoon, scrSleepBreakdown, scrSleepStages, scrHeartYesterday},
	scrMoon:   {scrNight, scrPhaseMonth, scrSleepSchedule},

	// the body against its normal
	scrVitals:         {scrEarlyWarning, scrDeviation, scrBodySignals, scrOxygen},
	scrDeviation:      {scrVitals, scrEarlyWarning, scrBodySignals},
	scrEarlyWarning:   {scrBodySignals, scrVitals, scrOxygen, scrHabitWeek},
	scrBodySignals:    {scrEarlyWarning, scrOxygen, scrFitness},
	scrOxygen:         {scrBodySignals, scrEarlyWarning},
	scrReadinessParts: {scrVitals, scrBodySignals, scrTrends},
	scrSleepBreakdown: {scrNight, scrSleepStages, scrTrends},
	scrHeartToday:     {scrHeartYesterday, scrVitals},
	scrHeartYesterday: {scrHeartToday, scrNight},

	// weeks and the longer run
	// the weeks zoom out to the month: its report leads to its calendars
	scrWeekReview:   {scrWeekRadar, scrHabitWeek, scrWeeklyRhythm, scrTrends, scrMonthReport},
	scrThisWeek:     {scrHabitWeek, scrWeekRadar, scrTrends, scrMonthReport},
	scrWeekRadar:    {scrWeekReview, scrWeeklyRhythm},
	scrWeeklyRhythm: {scrWeekRadar, scrTrends},
	scrTrends:       {scrFitness, scrWeeklyRhythm, scrSleepStages},
	scrFitness:      {scrTrends, scrBodySignals},

	// toward a good night
	scrSleepSchedule: {scrPayoff, scrSleepStages, scrPhaseMonth},
	scrSleepStages:   {scrSleepSchedule, scrNight},
	scrPayoff:        {scrSleepSchedule, scrHabitEffects},
	scrHabitWeek:     {scrHabitEffects, scrEarlyWarning},
	scrHabitEffects:  {scrHabitWeek, scrPayoff},

	// months: this one's views lead to each other, as do last month's
	scrMonthReport: {scrScoreMonth, scrStressMonth, scrPhaseMonth},
	scrScoreMonth:  {scrMonthReport, scrStressMonth},
	scrStressMonth: {scrMonthReport, scrScoreMonth},
	scrPhaseMonth:  {scrSleepSchedule, scrMonthReport},
	scrLastMonth:   {scrLastScores, scrLastStress, scrLastPhase},
	scrLastScores:  {scrLastMonth, scrLastStress},
	scrLastStress:  {scrLastMonth, scrLastScores},
	scrLastPhase:   {scrLastMonth, scrLastScores},
}

// screenZones are the parts of a screen a tap opens directly, by the
// zone names its chart gives them (chart.Zone), each to a detail view.
// A tap anywhere else opens the screen's detail strip as usual. A zone
// named "<kind>:<date>" (a week's day cards, "day:2026-11-21") opens its
// kind's view as of that date.
var screenZones = map[string]map[string]string{
	scrDaily:      {"readiness": scrReadinessParts, "night": scrMoon, "today": scrHeartToday},
	scrWeekReview: {"day": scrDaily},
	scrThisWeek:   {"day": scrDaily},
	// the month views: each day opens its brief (a WolfePhase night, the
	// brief of the morning after it)
	scrMonthReport: {"day": scrDaily},
	scrScoreMonth:  {"day": scrDaily},
	scrStressMonth: {"day": scrDaily},
	scrPhaseMonth:  {"day": scrDaily},
	scrLastMonth:   {"day": scrDaily},
	scrLastScores:  {"day": scrDaily},
	scrLastStress:  {"day": scrDaily},
	scrLastPhase:   {"day": scrDaily},
}

// zonesKey keys the context value that collects a drawn screen's zones.
type zonesKey struct{}

// withZones is ctx set to collect the zones of the next screen drawn.
func withZones(ctx context.Context) (context.Context, *[]chart.Zone) {
	var zs []chart.Zone
	return context.WithValue(ctx, zonesKey{}, &zs), &zs
}

// zonesHeader words a drawn screen's tappable zones for the X-Zones
// header: JSON, each with its detail view and its box as fractions of the
// image, so a page can hit-test a tap at whatever size it shows the image.
func zonesHeader(screen string, zones []chart.Zone, bounds image.Rectangle) string {
	targets := screenZones[screen]
	out := []map[string]any{}
	frac := func(v, of int) float64 { return math.Round(float64(v)/float64(of)*1e4) / 1e4 }
	for _, z := range zones {
		kind, date, _ := strings.Cut(z.Name, ":")
		to, ok := targets[kind]
		if !ok || z.Rect.Empty() {
			continue
		}
		zone := map[string]any{
			"screen": to,
			"x":      frac(z.Rect.Min.X-bounds.Min.X, bounds.Dx()),
			"y":      frac(z.Rect.Min.Y-bounds.Min.Y, bounds.Dy()),
			"w":      frac(z.Rect.Dx(), bounds.Dx()),
			"h":      frac(z.Rect.Dy(), bounds.Dy()),
		}
		if date != "" {
			zone["date"] = date
		}
		out = append(out, zone)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// homeScreen is home: the daily brief, a step away from every screen
// whatever the schedule shows.
const homeScreen = scrDaily

// detailsOf is screen's detail views, or none for a screen without any.
func detailsOf(screen string) []string { return screenDetails[screen] }

// buttonSteps are the screens a TRMNL's button steps through from main:
// its detail views, then home if they don't already include it.
func buttonSteps(main string) []string {
	ds := detailsOf(main)
	if main == homeScreen || slices.Contains(ds, homeScreen) {
		return ds
	}
	return append(slices.Clone(ds), homeScreen)
}

// knownScreen says whether name is a screen the schedule offers: the only
// screens a display may be asked to show.
func knownScreen(name string) bool {
	for _, o := range scheduleScreens {
		if o.Name == name {
			return true
		}
	}
	return false
}

// detailStep is the screen a button-stepped display shows: the main screen
// at step 0, then each of its detail views in turn, then home, then the
// main screen again, round and round.
func detailStep(main string, step int) string {
	ds := buttonSteps(main)
	if step <= 0 || len(ds) == 0 {
		return main
	}
	i := (step - 1) % (len(ds) + 1)
	if i == len(ds) {
		return main
	}
	return ds[i]
}
