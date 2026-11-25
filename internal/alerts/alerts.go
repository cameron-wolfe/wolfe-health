// Package alerts sends phone notifications when something in the data is
// worth knowing about without looking: signals drifting toward illness, or
// the data itself going stale (the Oura login broken, the ring not syncing).
//
// Rules read the database and the fetch status and say what's true now;
// the Engine sends each alert once (by its key, recorded in the database),
// holds them through quiet hours, and caps how many go out in a day.
// Delivery is Home Assistant's notify action (homeassistant.go), to the
// phone its companion app registered.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Alert is one notification.
type Alert struct {
	// Key names the occurrence, so it's sent once: "illness:2026-11-24"
	// goes out on the 24th, and again only for a new day.
	Key            string
	Title, Message string
	// Level is iOS's interruption level: "passive", "active" (the
	// default) or "time-sensitive", which gets through a Focus.
	Level string
	// Path is a page on this server a tap opens ("/emulator?show=…"),
	// joined to the engine's link base; empty for none.
	Path string
}

// Notifier delivers an alert.
type Notifier interface {
	Send(ctx context.Context, a Alert, link string) error
	Target() string // where alerts go, for the index page
}

// FetchState is what the rules need of the fetch loop's status.
type FetchState struct {
	LastSuccess time.Time
	LastError   string
}

// Engine checks the rules and sends what's new.
type Engine struct {
	DB       *store.DB
	Notify   Notifier
	LinkBase string // "http://100.x.y.z:8090": where a tap's page is
	// Quiet hours, local clock hours: nothing is sent from QuietFrom until
	// QuietTo; alerts that come up meanwhile go out after.
	QuietFrom, QuietTo int
	MaxPerDay          int
	Settings           Settings // the rules' adjustable parts
	Now                func() time.Time
}

// The defaults, which WH_ALERT_QUIET and WH_ALERT_MAX_PER_DAY change.
const (
	DefaultQuiet     = "22-7"
	DefaultMaxPerDay = 10
)

// NewEngine is an engine with the defaults: quiet 10 PM to 7 AM, at most
// ten alerts a day.
func NewEngine(db *store.DB, n Notifier, linkBase string) *Engine {
	return &Engine{DB: db, Notify: n, LinkBase: strings.TrimRight(linkBase, "/"),
		QuietFrom: 22, QuietTo: 7, MaxPerDay: DefaultMaxPerDay, Settings: DefaultSettings(), Now: time.Now}
}

// ParseQuiet reads quiet hours as a span of clock hours, "22-7" or
// "22:00-07:00" (local time, across midnight when the end is earlier), or
// "off" for none, which is returned as from == to.
func ParseQuiet(s string) (from, to int, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "off" || s == "none" {
		return 0, 0, nil
	}
	a, b, ok := strings.Cut(s, "-")
	hour := func(v string) (int, bool) {
		v = strings.TrimSuffix(strings.TrimSpace(v), ":00")
		h, err := strconv.Atoi(v)
		return h, err == nil && h >= 0 && h <= 23
	}
	from, okA := hour(a)
	to, okB := hour(b)
	if !ok || !okA || !okB || from == to {
		return 0, 0, fmt.Errorf("alerts: quiet hours %q: want a span of whole hours like 22-7 or 22:00-07:00, or off", s)
	}
	return from, to, nil
}

// Limits words the engine's quiet hours and cap for the index page:
// "never between 10 PM and 7 AM (held until then), at most 10 a day".
func (e *Engine) Limits() string {
	h := func(v int) string { return time.Date(2000, 1, 1, v, 0, 0, 0, time.UTC).Format("3 PM") }
	s := "at any hour"
	if e.QuietFrom != e.QuietTo {
		s = fmt.Sprintf("never between %s and %s (held until then)", h(e.QuietFrom), h(e.QuietTo))
	}
	return fmt.Sprintf("%s, at most %d a day", s, e.MaxPerDay)
}

// quiet says whether t falls in quiet hours.
func (e *Engine) quiet(t time.Time) bool {
	h := t.Hour()
	if e.QuietFrom > e.QuietTo { // across midnight
		return h >= e.QuietFrom || h < e.QuietTo
	}
	return h >= e.QuietFrom && h < e.QuietTo
}

// Check runs the rules and sends each alert not yet sent, unless it's
// quiet hours or the day's cap is reached; what isn't sent now goes out at
// a later check while it's still true. It returns what it sent.
func (e *Engine) Check(ctx context.Context, fs FetchState) ([]Alert, error) {
	now := e.Now()
	due, err := Rules(ctx, e.DB, fs, now, e.Settings)
	if err != nil {
		return nil, err
	}
	if len(due) == 0 || e.quiet(now) {
		return nil, nil
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sentToday, err := e.DB.AlertsSince(ctx, midnight, onDemand...) // the buttons' sends don't count
	if err != nil {
		return nil, err
	}
	var sent []Alert
	var errs []error
	for _, a := range due {
		done, err := e.DB.AlertSent(ctx, a.Key)
		if err != nil {
			return sent, err
		}
		if done {
			continue
		}
		if sentToday >= e.MaxPerDay {
			log.Printf("alerts: today's %d sent; holding %s", e.MaxPerDay, a.Key)
			break
		}
		if err := e.send(ctx, a, now); err != nil {
			errs = append(errs, err)
			continue
		}
		sent = append(sent, a)
		sentToday++
	}
	return sent, errors.Join(errs...)
}

// send delivers a and records it.
func (e *Engine) send(ctx context.Context, a Alert, now time.Time) error {
	link := ""
	if a.Path != "" && e.LinkBase != "" {
		link = e.LinkBase + a.Path
	}
	if err := e.Notify.Send(ctx, a, link); err != nil {
		return fmt.Errorf("alerts: send %s: %w", a.Key, err)
	}
	log.Printf("alerts: sent %s: %s", a.Key, a.Title)
	return e.DB.RecordAlert(ctx, store.SentAlert{Key: a.Key, Title: a.Title, Message: a.Message, SentAt: now})
}

// onDemand are the key prefixes of alerts sent from the index page's
// buttons (Test, SendSummary): recorded and listed, but not counted toward
// the day's cap, which is for alerts the rules raise.
var onDemand = []string{"test:", "summary-now:"}

// Test sends a test alert at once, whatever the hour, and records it.
func (e *Engine) Test(ctx context.Context) error {
	now := e.Now()
	return e.send(ctx, Alert{
		Key:     onDemand[0] + now.Format(time.RFC3339Nano),
		Title:   "Wolfe Health test",
		Message: "Alerts reach this phone. Tap to open the display.",
		Path:    "/emulator",
	}, now)
}

// SendSummary sends the morning summary at once, as it stands now, whatever
// the hour: on demand, from the index page. It's recorded under its own key,
// so the day's 8 AM summary still goes out.
func (e *Engine) SendSummary(ctx context.Context) error {
	now := e.Now()
	title, msg, _, err := dashboard.MorningNote(ctx, e.DB, now)
	if err != nil {
		return err
	}
	return e.send(ctx, Alert{
		Key:     onDemand[1] + now.Format(time.RFC3339Nano),
		Title:   title,
		Message: msg,
		Path:    "/emulator?show=daily",
	}, now)
}

// Rules is every alert that's on and true now.
func Rules(ctx context.Context, db *store.DB, fs FetchState, now time.Time, set Settings) ([]Alert, error) {
	var out []Alert
	if set.on("illness") {
		ill, err := illness(ctx, db, now, set)
		if err != nil {
			return nil, err
		}
		out = append(out, ill...)
	}
	if set.on("summary") {
		morning, err := morningSummary(ctx, db, now, set)
		if err != nil {
			return nil, err
		}
		out = append(out, morning...)
	}
	if set.on("fetch") {
		out = append(out, fetching(fs, now, set)...)
	}
	if set.on("sync") {
		sync, err := ringSync(ctx, db, fs, now, set)
		if err != nil {
			return nil, err
		}
		out = append(out, sync...)
	}
	return out, nil
}

// illness fires when set.IllnessSignals or more of the early warning's
// signals are up together for set.IllnessNights nights running, ending last
// night: the pattern that often comes before feeling ill. It names the
// signals and their readings, and what was logged before last night that
// could explain it.
func illness(ctx context.Context, db *store.DB, now time.Time, set Settings) ([]Alert, error) {
	ew, err := dashboard.EarlyWarning(ctx, db, max(7, set.IllnessNights+1), now, now.Location())
	if err != nil {
		return nil, err
	}
	if len(ew.Days) == 0 {
		return nil, nil
	}
	last := ew.Days[len(ew.Days)-1]
	streak := 0
	for i := len(ew.Days) - 1; i >= 0 && ew.Days[i].Up >= set.IllnessSignals; i-- {
		streak++
	}
	if streak < set.IllnessNights {
		return nil, nil
	}
	var up []string
	for _, l := range ew.Lanes {
		if n := len(l.Nights); n > 0 && l.Nights[n-1].Up && l.Nights[n-1].Day.Equal(last.Day) {
			up = append(up, fmt.Sprintf("%s %s", lowerFirst(l.Title), l.Value))
		}
	}
	msg := fmt.Sprintf("%s beyond your normal, %d nights running. Several moving together can come a day or two before feeling ill.",
		capitalize(listWords(up)), streak)
	if len(last.Habits) > 0 {
		names := map[string]string{}
		for _, k := range ew.HabitKey { // "ALC alcohol"
			code, name, _ := strings.Cut(k, " ")
			names[code] = name
		}
		var logged []string
		for _, c := range last.Habits {
			logged = append(logged, names[c])
		}
		msg += " Logged before last night: " + listWords(logged) + "."
	}
	return []Alert{{
		Key:     "illness:" + last.Day.Format("2006-01-02"),
		Title:   fmt.Sprintf("Early warning: %d signals up", last.Up),
		Message: msg,
		Level:   "time-sensitive",
		Path:    "/emulator?show=early-warning",
	}}, nil
}

// morningSummary is the day's figures, from set.SummaryHour: the two
// scores as the title, then last night, sleep, readiness and yesterday, as
// on the daily brief. It waits for last night to sync, for three hours.
func morningSummary(ctx context.Context, db *store.DB, now time.Time, set Settings) ([]Alert, error) {
	if now.Hour() < set.SummaryHour {
		return nil, nil
	}
	title, msg, ready, err := dashboard.MorningNote(ctx, db, now)
	if err != nil {
		return nil, err
	}
	if !ready && now.Hour() < set.summaryLatest() {
		return nil, nil // wait for the night to sync
	}
	return []Alert{{
		Key:     "summary:" + now.Format("2006-01-02"),
		Title:   title,
		Message: msg,
		Path:    "/emulator?show=daily",
	}}, nil
}

// fetching fires when no fetch from Oura has worked for set.FetchStale: most
// often the login, which needs `wolfe-health auth` again.
func fetching(fs FetchState, now time.Time, set Settings) []Alert {
	if fs.LastSuccess.IsZero() || now.Sub(fs.LastSuccess) < set.FetchStale || fs.LastError == "" {
		return nil
	}
	msg := fmt.Sprintf("No data from Oura since %s. Last error: %s", clock(fs.LastSuccess, now), shorten(fs.LastError, 160))
	if loginError(fs.LastError) {
		msg = fmt.Sprintf("The Oura login has stopped working (no data since %s). Run `make auth` on the server to log in again.",
			clock(fs.LastSuccess, now))
	}
	return []Alert{{
		Key:     "fetch:" + now.Format("2006-01-02"),
		Title:   "Wolfe Health isn't getting data",
		Message: msg,
		Level:   "time-sensitive",
		Path:    "/",
	}}
}

// loginError says whether a fetch error is the Oura login's.
func loginError(e string) bool {
	for _, s := range []string{"auth again", "no Oura token", "got 401", "refresh token"} {
		if strings.Contains(e, s) {
			return true
		}
	}
	return false
}

// ringSync fires when fetches work but the ring's newest reading is older
// than set.SyncStale: the ring isn't reaching Oura (the phone app hasn't
// synced, or the ring is off or flat).
func ringSync(ctx context.Context, db *store.DB, fs FetchState, now time.Time, set Settings) ([]Alert, error) {
	if fs.LastSuccess.IsZero() || now.Sub(fs.LastSuccess) >= set.FetchStale {
		return nil, nil // nothing new to judge by, or fetching says it already
	}
	synced, ok, err := db.LatestHeartRateTime(ctx)
	if err != nil {
		return nil, err
	}
	b, hasBattery, err := db.LatestBattery(ctx)
	if err != nil {
		return nil, err
	}
	if hasBattery && b.Time.After(synced) {
		synced, ok = b.Time, true
	}
	if !ok || now.Sub(synced) < set.SyncStale {
		return nil, nil
	}
	msg := fmt.Sprintf("The ring's newest reading is from %s. Open the Oura app to sync it", clock(synced, now))
	if hasBattery {
		msg += fmt.Sprintf("; its battery was at %d%%", b.Level)
	}
	return []Alert{{
		Key:     "sync:" + now.Format("2006-01-02"),
		Title:   "The ring hasn't synced",
		Message: msg + ".",
		Path:    "/",
	}}, nil
}

// clock is t as a short time: "3:04 PM" today, else "Mon 3:04 PM".
func clock(t, now time.Time) string {
	t = t.In(now.Location())
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("3:04 PM")
	}
	return t.Format("Mon 3:04 PM")
}

// listWords joins words as a sentence does: "a", "a and b", "a, b and c".
func listWords(ws []string) string {
	switch len(ws) {
	case 0:
		return ""
	case 1:
		return ws[0]
	}
	return strings.Join(ws[:len(ws)-1], ", ") + " and " + ws[len(ws)-1]
}

// lowerFirst lowercases a title's first letter for use mid-sentence,
// unless it starts an abbreviation: "Lowest HR" → "lowest HR", "HRV" stays.
func lowerFirst(s string) string {
	if s == "" || (len(s) > 1 && strings.ToUpper(s[1:2]) == s[1:2] && s[1] != ' ') {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
