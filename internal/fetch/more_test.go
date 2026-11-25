package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/oura"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// A full run against a fake Oura: every further collection lands in its
// table or metric, one the token can't read is skipped with a warning while
// the rest go ahead, and heart rate over 10 days is fetched in two requests.
func TestRunFetchesEverything(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	today := time.Now().Format("2006-01-02")
	body := map[string]string{
		"daily_spo2":               `{"id":"a","day":"%s","spo2_percentage":{"average":96.5},"breathing_disturbance_index":2}`,
		"daily_cardiovascular_age": `{"id":"a","day":"%s","vascular_age":38,"pulse_wave_velocity":7.1}`,
		"daily_resilience":         `{"id":"a","day":"%s","level":"solid","contributors":{"sleep_recovery":70,"daytime_recovery":40,"stress":55}}`,
		"vO2_max":                  `{"id":"a","day":"%s","timestamp":"x","vo2_max":44}`,
		"workout":                  `{"id":"w","day":"%s","activity":"walking","intensity":"easy","source":"autodetected","start_datetime":"x","end_datetime":"y","calories":120}`,
		"session":                  `{"id":"s","day":"%s","type":"breathing","start_datetime":"x","end_datetime":"y","heart_rate":{"interval":5,"items":[60,null],"timestamp":"x"}}`,
		"enhanced_tag":             `{"id":"t","start_day":"%s","start_time":"x","tag_type_code":"tag_generic_caffeine"}`,
		"rest_mode_period":         `{"id":"r","start_day":"%s","episodes":[]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		hits[name]++
		n := hits[name]
		mu.Unlock()
		switch name {
		case "sleep_time":
			w.WriteHeader(http.StatusUnauthorized) // as Oura answers a scope the token lacks
			fmt.Fprint(w, `{"detail":"Token is not authorized access sleep_time scope."}`)
		case "heartrate":
			fmt.Fprintf(w, `{"data":[{"timestamp":"2026-11-19T08:0%d:00+00:00","bpm":%d,"source":"awake"}],"next_token":null}`, n, 60+n)
		case "ring_battery_level":
			fmt.Fprint(w, `{"data":[{"timestamp":"2026-11-19T09:00:00+00:00","level":71}],"next_token":null}`)
		case "ring_configuration":
			fmt.Fprint(w, `{"data":[{"id":"ring","hardware_type":"gen3","size":10}],"next_token":null}`)
		case "personal_info":
			fmt.Fprint(w, `{"id":"me","age":40}`)
		default:
			if b, ok := body[name]; ok {
				fmt.Fprintf(w, `{"data":[`+b+`],"next_token":null}`, today)
				return
			}
			fmt.Fprint(w, `{"data":[],"next_token":null}`) // the core collections: empty
		}
	}))
	defer srv.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := oura.New(oura.StaticToken("t")).WithBaseURL(srv.URL + "/")
	ctx := context.Background()

	r, err := Run(ctx, c, db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 || !strings.HasPrefix(r.Warnings[0], "sleep time:") {
		t.Errorf("warnings %q, want only sleep time", r.Warnings)
	}
	if hits["heartrate"] != 2 || r.More["heart rate"] != 2 {
		t.Errorf("heart rate: %d requests, %d rows; want 2 and 2 (10 days in 7-day chunks)", hits["heartrate"], r.More["heart rate"])
	}
	if !strings.Contains(r.String(), "skipped sleep time:") || !strings.Contains(r.String(), "workouts 1") {
		t.Errorf("summary %q", r.String())
	}

	for metric, want := range map[string]float64{
		store.MetricSpO2: 96.5, store.MetricBreathingDI: 2, store.MetricCardioAge: 38, store.MetricPulseWave: 7.1,
		store.MetricVO2Max: 44, store.MetricResilience: 3, store.MetricResSleep: 70, store.MetricResStress: 55,
	} {
		s, err := db.MetricOnDay(ctx, metric, today)
		if err != nil || len(s) != 1 || s[0].Value != want {
			t.Errorf("%s: %+v, %v; want %v", metric, s, err, want)
		}
	}
	if s, _ := db.MetricOnDay(ctx, store.MetricResilience, today); len(s) == 1 && s[0].Qualifier != "solid" {
		t.Errorf("resilience qualifier %q", s[0].Qualifier)
	}
	for table, want := range map[string]int{"workouts": 1, "sessions": 1, "tags": 1, "rest_mode_periods": 1,
		"heart_rate": 2, "ring_battery": 1, "ring_configurations": 1, "personal_info": 1, "sleep_times": 0} {
		if n, err := db.TableCount(ctx, table); err != nil || n != want {
			t.Errorf("%s: %d rows, %v; want %d", table, n, err, want)
		}
	}
}

func TestChunks(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got := chunks(start, start.Add(10*24*time.Hour), timeSeriesChunk)
	if len(got) != 2 || !got[0][1].Equal(start.Add(timeSeriesChunk)) || !got[1][1].Equal(start.Add(10*24*time.Hour)) {
		t.Errorf("chunks %v", got)
	}
	if len(chunks(start, start, timeSeriesChunk)) != 0 {
		t.Error("an empty range should have no chunks")
	}
}
