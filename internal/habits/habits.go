// Package habits is the habit log's catalog: what can be logged, how one log
// is measured, the words that name each habit, and which night a log
// belongs to. The server takes logs with it and the dashboard charts them,
// so both agree on names.
package habits

import (
	"strconv"
	"strings"
	"time"
)

// Habit is one thing the log can record.
type Habit struct {
	Name     string   // stored, and what the menu shows: "Caffeine"
	Code     string   // a short badge for the charts: "CAF"
	Counted  bool     // logged by how many (drinks); otherwise once a day is all that matters
	Unit     string   // one log: "double shot of espresso"
	Units    string   // several: "double shots of espresso"
	Note     string   // what one log amounts to, for the reply: "about 130 mg of caffeine"
	OuraTags []string // Oura tag type codes that mean the same thing

	// LateWithin, for a habit logged whenever it happens (a meal), is how
	// close to bed makes it late; the charts count a late one as LateName.
	// Zero for a habit whose time doesn't matter.
	LateWithin time.Duration
	LateName   string // "Late meal"
	Late       bool   // this is the late form of a timed habit
	Quiet      bool   // its ordinary, not-late form isn't worth charting (a meal)
}

// Catalog is every habit, in menu order: what tends to hurt sleep first,
// then what's meant to help, then circumstances.
var Catalog = []Habit{
	{Name: "Caffeine", Code: "CAF", Counted: true, Unit: "double shot of espresso", Units: "double shots of espresso",
		Note:     "about 130 mg of caffeine",
		OuraTags: []string{"tag_generic_caffeine", "tag_sleep_caffeine", "tag_generic_coffee"}},
	{Name: "Alcohol", Code: "ALC", Counted: true, Unit: "drink", Units: "drinks", Note: "about 14 g of alcohol",
		OuraTags: []string{"tag_generic_alcohol", "tag_sleep_alcohol"}},
	{Name: "Meal", Code: "MEAL", Unit: "meal", Units: "meals", LateWithin: 3 * time.Hour, LateName: "Late meal", Quiet: true,
		OuraTags: []string{"tag_generic_late_meal", "tag_sleep_late_meal", "tag_generic_heavy_meal"}},
	{Name: "Sweets", Code: "SWT", Unit: "dessert", Units: "desserts", LateWithin: 3 * time.Hour, LateName: "Late sweets", Quiet: true,
		OuraTags: []string{"tag_generic_sugar"}},
	{Name: "Workout", Code: "WKT", Unit: "workout", Units: "workouts", LateWithin: 2 * time.Hour, LateName: "Late workout",
		OuraTags: []string{"tag_generic_late_workout"}},
	{Name: "Screens in bed", Code: "SCR", Unit: "night of screens in bed", Units: "nights of screens in bed",
		OuraTags: []string{"tag_generic_screen_time", "tag_sleep_screen_time"}},
	{Name: "Cannabis", Code: "CAN", Counted: true, Unit: "dose of cannabis", Units: "doses of cannabis",
		OuraTags: []string{"tag_generic_cannabis", "tag_generic_cbd"}},
	{Name: "Late night out", Code: "OUT", Unit: "late night out", Units: "late nights out", OuraTags: []string{"tag_generic_party", "tag_generic_social"}},
	{Name: "Melatonin", Code: "MEL", Counted: true, Unit: "dose of melatonin", Units: "doses of melatonin", OuraTags: []string{"tag_generic_melatonin"}},
	{Name: "Cold plunge", Code: "CLD", Unit: "cold plunge", Units: "cold plunges", OuraTags: []string{"tag_generic_cold_shower", "tag_generic_ice_bath"}},
	{Name: "Stressful day", Code: "STR", Unit: "stressful day", Units: "stressful days", OuraTags: []string{"tag_generic_stress", "tag_generic_work_stress"}},
	{Name: "Sick", Code: "SICK", Unit: "sick day", Units: "sick days", OuraTags: []string{"tag_generic_sick", "tag_generic_illness"}},
	{Name: "Travel", Code: "TRV", Unit: "travel day", Units: "travel days",
		OuraTags: []string{"tag_generic_airplane", "tag_generic_jetlag", "tag_generic_travel"}},
}

// LateForm is the late form of a timed habit: its LateName, the same
// badge, marked Late.
func (h Habit) LateForm() Habit {
	l := h
	l.Name, l.Late, l.LateWithin, l.Quiet = h.LateName, true, 0, false
	return l
}

// IsLate says whether a log of the habit at t was late for a bedtime.
func (h Habit) IsLate(t, bedtime time.Time) bool {
	return h.LateWithin > 0 && !bedtime.IsZero() && bedtime.Sub(t) <= h.LateWithin
}

// ByName is the catalog entry for a stored name, a timed habit's late
// form by its LateName, or a bare one, coded from the name, for one the
// catalog no longer has (or a custom Oura tag).
func ByName(name string) Habit {
	for _, h := range Catalog {
		if strings.EqualFold(h.Name, name) {
			return h
		}
		if h.LateName != "" && strings.EqualFold(h.LateName, name) {
			return h.LateForm()
		}
	}
	code := strings.ToUpper(strings.Join(strings.Fields(name), ""))
	if len(code) > 4 {
		code = code[:4]
	}
	return Habit{Name: name, Code: code, Unit: strings.ToLower(name), Units: strings.ToLower(name)}
}

// ForOuraTag is the catalog habit an Oura tag type code means, if any.
func ForOuraTag(code string) (Habit, bool) {
	for _, h := range Catalog {
		for _, t := range h.OuraTags {
			if t == code {
				return h, true
			}
		}
	}
	return Habit{}, false
}

// Amount words a count of the habit: "1 double shot of espresso", "2 drinks",
// "sauna session" for a habit that isn't counted.
func (h Habit) Amount(n float64) string {
	if !h.Counted {
		return h.Unit
	}
	unit := h.Units
	if n == 1 {
		unit = h.Unit
	}
	return strconv.FormatFloat(n, 'f', -1, 64) + " " + unit
}

// Find is the habit named name, ignoring case and surrounding space: what
// the shortcut's menu sends.
func Find(name string) (Habit, bool) {
	name = strings.TrimSpace(name)
	for _, h := range Catalog {
		if strings.EqualFold(h.Name, name) {
			return h, true
		}
	}
	return Habit{}, false
}

// dayTurns is when a day's logs stop counting toward that day's night: a
// drink at 1 AM belongs to the evening before.
const dayTurns = 4 * time.Hour

// Day is the day a log at t belongs to, as a date: its calendar day, or the
// day before for a log in the small hours.
func Day(t time.Time) time.Time {
	t = t.Add(-dayTurns)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Night is the wake-up day of the night a log at t comes before: the day
// after Day(t). Oura names nights by the morning they end.
func Night(t time.Time) time.Time { return Day(t).AddDate(0, 0, 1) }
