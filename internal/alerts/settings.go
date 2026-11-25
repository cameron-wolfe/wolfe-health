package alerts

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Settings are the rules' adjustable parts, from the environment
// (LoadSettings): when the morning summary goes, which alerts are off, how
// many signals for how many nights make an early warning, and how stale
// the data may get before saying so.
type Settings struct {
	// SummaryHour is the local hour the morning summary goes out from,
	// once last night has synced; three hours later it goes regardless.
	SummaryHour int
	// Off are the alerts turned off, by name (RuleNames).
	Off map[string]bool
	// IllnessSignals of the early warning's signals up together, for
	// IllnessNights nights running ending last night, is an early warning.
	IllnessSignals, IllnessNights int
	// FetchStale is how long without a fetch from Oura working before
	// saying so (the daemon tries every few hours, retrying within
	// minutes); SyncStale how old the ring's newest reading may get while
	// fetches work (a night's sleep syncs in the morning, so less than a
	// night and a morning would fire every day).
	FetchStale, SyncStale time.Duration
}

// RuleNames are the alerts, as WH_ALERTS_OFF names them; each alert's key
// starts with its name.
var RuleNames = []string{"summary", "illness", "fetch", "sync"}

// DefaultSettings are the settings with nothing set.
func DefaultSettings() Settings {
	return Settings{SummaryHour: 8, Off: map[string]bool{}, IllnessSignals: 2, IllnessNights: 2,
		FetchStale: 13 * time.Hour, SyncStale: 18 * time.Hour}
}

// on says whether the alert named name is on.
func (s Settings) on(name string) bool { return !s.Off[name] }

// summaryLatest is the hour the summary goes out whether or not last
// night has synced.
func (s Settings) summaryLatest() int { return min(s.SummaryHour+3, 23) }

// LoadSettings reads the settings from the environment (getenv), starting
// from the defaults:
//
//	WH_ALERT_SUMMARY_AT   the morning summary's hour: 8, 08:00, or off
//	WH_ALERTS_OFF         alerts to turn off: summary, illness, fetch, sync
//	WH_ALERT_ILLNESS      signals/nights for an early warning: 2/2
//	WH_ALERT_STALE        no fetch / no ring sync for: 13h/18h
//
// A value it can't read is an error naming the setting.
func LoadSettings(getenv func(string) string) (Settings, error) {
	s := DefaultSettings()
	if v := getenv("WH_ALERTS_OFF"); v != "" {
		off, err := ParseOff(v)
		if err != nil {
			return s, err
		}
		s.Off = off
	}
	if v := getenv("WH_ALERT_SUMMARY_AT"); v != "" {
		h, off, err := ParseSummaryAt(v)
		if err != nil {
			return s, err
		}
		if off {
			s.Off["summary"] = true
		} else {
			s.SummaryHour = h
		}
	}
	if v := getenv("WH_ALERT_ILLNESS"); v != "" {
		var err error
		if s.IllnessSignals, s.IllnessNights, err = ParseIllness(v); err != nil {
			return s, err
		}
	}
	if v := getenv("WH_ALERT_STALE"); v != "" {
		var err error
		if s.FetchStale, s.SyncStale, err = ParseStale(v); err != nil {
			return s, err
		}
	}
	return s, nil
}

// ParseOff reads a list of alerts to turn off: "sync,fetch".
func ParseOff(v string) (map[string]bool, error) {
	off := map[string]bool{}
	for _, name := range strings.FieldsFunc(strings.ToLower(v), func(r rune) bool { return r == ',' || r == ' ' }) {
		if !slices.Contains(RuleNames, name) {
			return nil, fmt.Errorf("WH_ALERTS_OFF: no alert %q; the alerts are %s", name, strings.Join(RuleNames, ", "))
		}
		off[name] = true
	}
	return off, nil
}

// ParseSummaryAt reads the morning summary's hour, "8" or "08:00", or
// "off".
func ParseSummaryAt(v string) (hour int, off bool, err error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "off" || v == "none" {
		return 0, true, nil
	}
	h, err := strconv.Atoi(strings.TrimSuffix(v, ":00"))
	if err != nil || h < 0 || h > 23 {
		return 0, false, fmt.Errorf("WH_ALERT_SUMMARY_AT: want a whole hour like 8 or 08:00, or off; got %q", v)
	}
	return h, false, nil
}

// ParseIllness reads an early warning's threshold, "signals/nights":
// "2/2" is two or more signals up, two nights running. There are four
// signals, and a run is counted over two weeks at most.
func ParseIllness(v string) (signals, nights int, err error) {
	a, b, ok := strings.Cut(strings.TrimSpace(v), "/")
	signals, errA := strconv.Atoi(strings.TrimSpace(a))
	nights, errB := strconv.Atoi(strings.TrimSpace(b))
	if !ok || errA != nil || errB != nil || signals < 1 || signals > 4 || nights < 1 || nights > 14 {
		return 0, 0, fmt.Errorf("WH_ALERT_ILLNESS: want signals/nights like 2/2 (signals 1 to 4, nights 1 to 14); got %q", v)
	}
	return signals, nights, nil
}

// ParseStale reads how long the data may go stale, "fetch/sync": "13h/18h"
// is no fetch from Oura working for 13 hours, or the ring's newest reading
// 18 hours old while fetches work. Each is a Go duration of an hour or
// more.
func ParseStale(v string) (fetch, sync time.Duration, err error) {
	a, b, ok := strings.Cut(strings.TrimSpace(v), "/")
	fetch, errA := time.ParseDuration(strings.TrimSpace(a))
	sync, errB := time.ParseDuration(strings.TrimSpace(b))
	if !ok || errA != nil || errB != nil || fetch < time.Hour || sync < time.Hour {
		return 0, 0, fmt.Errorf("WH_ALERT_STALE: want fetch/sync durations like 13h/18h, each an hour or more; got %q", v)
	}
	return fetch, sync, nil
}

// Describe words each alert that's on, for the index page.
func (s Settings) Describe() []string {
	hour := func(h int) string { return time.Date(2000, 1, 1, h, 0, 0, 0, time.UTC).Format("3 PM") }
	var out []string
	if s.on("summary") {
		out = append(out, fmt.Sprintf("the morning summary from %s, once last night has synced", hour(s.SummaryHour)))
	}
	if s.on("illness") {
		out = append(out, fmt.Sprintf("an early warning when %d or more signals are up %s", s.IllnessSignals, nightsWord(s.IllnessNights)))
	}
	if s.on("fetch") {
		out = append(out, fmt.Sprintf("when no data has come from Oura for %s (often the login: run make auth)", hoursWord(s.FetchStale)))
	}
	if s.on("sync") {
		out = append(out, fmt.Sprintf("when the ring hasn't synced for %s", hoursWord(s.SyncStale)))
	}
	return out
}

func nightsWord(n int) string {
	if n == 1 {
		return "for a night"
	}
	return fmt.Sprintf("%d nights running", n)
}

func hoursWord(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return d.String()
}
