package dashboard

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Summary words the morning's figures to be read aloud, as the "How did I
// sleep?" shortcut does: the sleep score and its change, time asleep,
// readiness and how it sits against your usual, and what held either back.
// It's built from the daily brief, so the two always agree.
func Summary(ctx context.Context, db *store.DB, today time.Time) (string, error) {
	d, err := Daily(ctx, db, today)
	if err != nil {
		return "", err
	}
	return spokenSummary(d), nil
}

// spokenSummary is Summary's wording, for any brief. Every figure is spelled
// out for speech: "9 hours 30 minutes", not "9h 30m", and "5 above your
// weekly average", not "+5 vs 7-day avg".
func spokenSummary(d chart.Daily) string {
	var out []string
	sleep := d.Metrics[0]
	switch {
	case sleep.Value == "—":
		out = append(out, "No sleep score yet for last night.")
	default:
		s := "Sleep score " + sleep.Value
		if d.NightTitle != "" && d.NightTitle != "Last night" {
			// the brief fell back to an older night: say so, not "last night"
			s = "No score for last night yet. Your latest, " + latestNight(d.NightTitle) + ", was " + sleep.Value
		}
		if c := spokenChange(sleep.Delta); c != "" {
			s += ", " + c
		}
		out = append(out, s+".")
	}
	if a := d.Stages.Asleep(); a > 0 {
		out = append(out, "You slept "+spokenDuration(a)+".")
	}
	if d.Hero.Value != "—" {
		s := "Readiness " + d.Hero.Value
		if d.Verdict != "" && !strings.HasPrefix(d.Verdict, "No ") {
			s += ", " + strings.ToLower(d.Verdict[:1]) + d.Verdict[1:]
		}
		out = append(out, s+".")
	}
	if parts, ok := strings.CutPrefix(d.HeldBack, "Held back by: "); ok {
		out = append(out, "Sleep was held back by "+spokenParts(strings.Split(parts, " · "))+".")
	}
	if d.HasParts {
		var low []string
		for _, p := range d.Parts.Parts {
			if p.Has && p.Value < 70 {
				low = append(low, fmt.Sprintf("%s %.0f", p.Label, p.Value))
			}
		}
		if len(low) > 0 {
			out = append(out, "Readiness was held back by "+spokenParts(low)+".")
		}
	}
	return strings.Join(out, " ")
}

var changeRE = regexp.MustCompile(`^([+−-]?)(\d+(?:\.\d+)?)`)

// spokenChange words a brief's change line for speech: "+5 vs 7-day avg" →
// "5 above your weekly average"; "" when there's none or it isn't a number.
func spokenChange(delta string) string {
	m := changeRE.FindStringSubmatch(strings.TrimSpace(delta))
	if m == nil {
		return ""
	}
	n, _ := strconv.ParseFloat(m[2], 64)
	switch {
	case n == 0:
		return "the same as your weekly average"
	case m[1] == "−" || m[1] == "-":
		return m[2] + " below your weekly average"
	}
	return m[2] + " above your weekly average"
}

// spokenParts joins "Timing 54", "REM 49" as "timing at 54 and REM at 49".
func spokenParts(parts []string) string {
	var words []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if i := strings.LastIndex(p, " "); i > 0 {
			if _, err := strconv.Atoi(p[i+1:]); err == nil {
				p = lowerWords(p[:i]) + " at " + p[i+1:]
			}
		}
		words = append(words, p)
	}
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// spokenDuration words a span for speech: "9 hours 30 minutes", "1 hour",
// "45 minutes".
func spokenDuration(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	h, m := m/60, m%60
	unit := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch {
	case h == 0:
		return unit(m, "minute")
	case m == 0:
		return unit(h, "hour")
	}
	return unit(h, "hour") + " " + unit(m, "minute")
}

// lowerWords lowercases each word but acronyms, which Siri should spell:
// "HRV balance" -> "HRV balance", "Sleep regularity" -> "sleep regularity".
func lowerWords(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) < 2 || w != strings.ToUpper(w) {
			words[i] = strings.ToLower(w)
		}
	}
	return strings.Join(words, " ")
}

// latestNight words the brief's heading for an older night mid-sentence:
// "Night of Nov 16" → "the night of Nov 16".
func latestNight(title string) string {
	if rest, ok := strings.CutPrefix(title, "Night of "); ok {
		return "the night of " + rest
	}
	return strings.TrimPrefix(title, "Sleep · ")
}

// MorningNote words the morning's figures for a phone notification, from
// the daily brief like Summary, but to read at a glance rather than aloud:
// a title of the two scores, then a line each for the night, sleep,
// readiness and yesterday. ready is false while last night's sleep hasn't
// synced (the brief would show an older night, or none).
func MorningNote(ctx context.Context, db *store.DB, today time.Time) (title, message string, ready bool, err error) {
	d, err := Daily(ctx, db, today)
	if err != nil {
		return "", "", false, err
	}
	title, message, ready = morningNote(d)
	return title, message, ready, nil
}

func morningNote(d chart.Daily) (title, message string, ready bool) {
	sleep := d.Metrics[0]
	ready = sleep.Value != "—" && (d.NightTitle == "" || d.NightTitle == "Last night")
	// the title carries the scores and their change against the 7-day
	// average, so the preview says most of it in one line
	var scores, lines []string
	if ready {
		scores = append(scores, "Sleep "+sleep.Value+arrow(sleep.Delta))
	}
	if d.Hero.Value != "—" {
		scores = append(scores, "Readiness "+d.Hero.Value+arrow(d.Hero.Delta))
	}
	title = strings.Join(scores, " · ")
	if title == "" {
		title = "Morning summary"
	}

	// then three short lines, to fit a lock-screen preview: the night, the
	// weakest parts behind the scores, and yesterday
	if !ready {
		lines = append(lines, "Last night hasn't synced yet: open the Oura app.")
	} else if when, asleep, ok := strings.Cut(d.NightWhen, " · "); ok {
		lines = append(lines, asleep+" · "+strings.ReplaceAll(when, " → ", "–"))
	}
	type part struct {
		name string
		v    float64
	}
	var low []part
	if parts, ok := strings.CutPrefix(d.HeldBack, "Held back by: "); ok && ready {
		for _, p := range strings.Split(parts, " · ") {
			if i := strings.LastIndex(p, " "); i > 0 {
				if v, err := strconv.ParseFloat(p[i+1:], 64); err == nil {
					low = append(low, part{lowerWords(p[:i]), v})
				}
			}
		}
	}
	if d.HasParts {
		for _, p := range d.Parts.Parts {
			if p.Has && p.Value < 70 {
				low = append(low, part{lowerWords(p.Label), p.Value})
			}
		}
	}
	sort.SliceStable(low, func(i, j int) bool { return low[i].v < low[j].v })
	if len(low) > 2 {
		low = low[:2]
	}
	if len(low) > 0 {
		var ws []string
		for _, p := range low {
			ws = append(ws, fmt.Sprintf("%s %.0f", p.name, p.v))
		}
		lines = append(lines, "Low: "+strings.Join(ws, ", "))
	}
	var y []string
	if m := d.Metrics[2]; m.Value != "—" && m.Value != "" {
		y = append(y, m.Value+" steps")
	}
	if m := d.Metrics[1]; m.Value != "—" && m.Value != "" {
		y = append(y, "activity "+m.Value)
	}
	if len(y) > 0 {
		lines = append(lines, "Yesterday: "+strings.Join(y, " · "))
	}
	return title, strings.Join(lines, "\n"), ready
}

// arrow words a change against the 7-day average compactly: "+7 vs 7-day
// avg" → " ↑7", "−4 …" → " ↓4"; "" for none or no change.
func arrow(delta string) string {
	m := changeRE.FindStringSubmatch(delta)
	if m == nil || m[2] == "0" {
		return ""
	}
	if m[1] == "−" || m[1] == "-" {
		return " ↓" + m[2]
	}
	return " ↑" + m[2]
}
