package dashboard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The summary reads like speech: figures spelled out, changes in words,
// what held sleep and readiness back named, and an older night called what
// it is rather than "last night".
func TestSpokenSummary(t *testing.T) {
	d := chart.Daily{
		NightTitle: "Last night",
		Hero:       chart.Metric{Value: "90"},
		Verdict:    "Above your usual",
		HeldBack:   "Held back by: Timing 54 · REM 49",
		Stages:     chart.StageNight{Deep: 2 * time.Hour, Light: 5 * time.Hour, REM: 2*time.Hour + 30*time.Minute},
		HasParts:   true,
		Parts: chart.SleepRadar{Parts: []chart.RadarPart{
			{Label: "HRV balance", Value: 62, Has: true}, {Label: "Sleep regularity", Value: 58, Has: true}, {Label: "Body temp", Value: 95, Has: true},
		}},
	}
	d.Metrics[0] = chart.Metric{Value: "88", Delta: "+5 vs 7-day avg"}
	want := "Sleep score 88, 5 above your weekly average. You slept 9 hours 30 minutes. Readiness 90, above your usual. " +
		"Sleep was held back by timing at 54 and REM at 49. Readiness was held back by HRV balance at 62 and sleep regularity at 58."
	if got := spokenSummary(d); got != want {
		t.Errorf("summary:\n got %q\nwant %q", got, want)
	}

	d.NightTitle = "Night of Nov 16" // as the brief heads an older night
	d.Metrics[0].Delta = "−20 vs 7-day avg"
	d.HeldBack, d.HasParts = "Every part of sleep scored 70 or better", false
	want = "No score for last night yet. Your latest, the night of Nov 16, was 88, 20 below your weekly average. You slept 9 hours 30 minutes. Readiness 90, above your usual."
	if got := spokenSummary(d); got != want {
		t.Errorf("an older night:\n got %q\nwant %q", got, want)
	}
}

func TestSpokenPieces(t *testing.T) {
	for in, want := range map[time.Duration]string{
		45 * time.Minute: "45 minutes", time.Hour: "1 hour", 61 * time.Minute: "1 hour 1 minute", 8 * time.Hour: "8 hours",
	} {
		if got := spokenDuration(in); got != want {
			t.Errorf("%v = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"+5 vs 7-day avg": "5 above your weekly average", "0 vs 7-day avg": "the same as your weekly average", "": ""} {
		if got := spokenChange(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

// With nothing stored, it says so rather than reading out dashes.
func TestSummaryEmpty(t *testing.T) {
	got, err := Summary(context.Background(), openWith(t, []store.Sample{}), today)
	if err != nil || got != "No sleep score yet for last night." {
		t.Errorf("empty: %q, %v", got, err)
	}
}

// The morning note fits a lock-screen preview: the scores and their
// change in the title, then three short lines: the night, the weakest
// parts behind the scores, and yesterday. Until last night syncs, it says
// so and isn't ready.
func TestMorningNote(t *testing.T) {
	d := chart.Daily{
		NightTitle: "Last night",
		NightWhen:  "10:26 PM → 8:10 AM · 9h 10m asleep",
		Hero:       chart.Metric{Value: "86", Delta: "−4 vs 7-day avg"},
		Verdict:    "Below your usual",
		HeldBack:   "Held back by: Timing 54 · REM 69",
		HasParts:   true,
		Parts: chart.SleepRadar{Parts: []chart.RadarPart{
			{Label: "Recovery index", Value: 61, Has: true}, {Label: "HRV balance", Value: 66, Has: true}, {Label: "Body temp", Value: 95, Has: true},
		}},
	}
	d.Metrics[0] = chart.Metric{Value: "92", Delta: "+7 vs 7-day avg"}
	d.Metrics[1] = chart.Metric{Value: "63"}
	d.Metrics[2] = chart.Metric{Value: "3,974"}
	d.Metrics[3] = chart.Metric{Value: "4h 45m"}
	title, msg, ready := morningNote(d)
	want := "9h 10m asleep · 10:26 PM–8:10 AM\n" +
		"Low: timing 54, recovery index 61\n" +
		"Yesterday: 3,974 steps · activity 63"
	if title != "Sleep 92 ↑7 · Readiness 86 ↓4" || msg != want || !ready {
		t.Errorf("note %q, ready %v:\n%s\nwant\n%s", title, ready, msg, want)
	}
	for _, line := range strings.Split(msg, "\n") {
		if len([]rune(line)) > 40 {
			t.Errorf("a line too long for a preview: %q", line)
		}
	}

	d.NightTitle = "Night of Nov 22" // last night hasn't synced
	title, msg, ready = morningNote(d)
	if ready || title != "Readiness 86 ↓4" || !strings.HasPrefix(msg, "Last night hasn't synced yet") ||
		strings.Contains(msg, "timing") {
		t.Errorf("unsynced: %q %q %v", title, msg, ready)
	}
}
