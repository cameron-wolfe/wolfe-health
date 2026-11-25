package dashboard

import (
	"context"
	"strings"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// alertDB stores 40 quiet nights (temperature ~0, HRV 50, lowest HR 50),
// then lets each test set the most recent nights.
func alertDB(t *testing.T, temps map[int]float64, hrv, hr map[int]int) *store.DB {
	t.Helper()
	var samples []store.Sample
	var periods []store.SleepPeriod
	for i := 0; i < 40; i++ {
		d := day(i).Format(dayLayout)
		temp := []float64{-0.1, 0, 0.1}[i%3]
		if v, ok := temps[i]; ok {
			temp = v
		}
		if v, ok := temps[i]; !ok || v < 99 { // 99 = no reading that night
			samples = append(samples, store.Sample{Metric: store.MetricTempDev, Day: d, Value: temp})
		}
		h, r := 50+i%3, 50+i%2
		if v, ok := hrv[i]; ok {
			h = v
		}
		if v, ok := hr[i]; ok {
			r = v
		}
		p := period("p"+d, "long_sleep", 25000)
		p.Day, p.AverageHRV, p.LowestHR = d, ptr(h), ptr(r)
		periods = append(periods, p)
	}
	db := openWith(t, samples)
	if err := db.SaveSleepPeriods(context.Background(), periods); err != nil {
		t.Fatal(err)
	}
	return db
}

func alertFor(t *testing.T, db *store.DB) string {
	t.Helper()
	a, err := Alert(context.Background(), db, today)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAlertHiddenWhenNormal(t *testing.T) {
	if a := alertFor(t, alertDB(t, nil, nil, nil)); a != "" {
		t.Errorf("quiet nights raised %q", a)
	}
}

// Above the range but under +0.3 °C: too small to be worth a warning.
func TestAlertNeedsAMeaningfulRise(t *testing.T) {
	if a := alertFor(t, alertDB(t, map[int]float64{0: 0.25}, nil, nil)); a != "" {
		t.Errorf("+0.25 °C raised %q", a)
	}
}

func TestAlertLastNight(t *testing.T) {
	a := alertFor(t, alertDB(t, map[int]float64{0: 0.6}, nil, nil))
	if a != "Temperature +0.6 °C, above your normal range last night" {
		t.Errorf("got %q", a)
	}
}

// Consecutive elevated nights are counted, and heart rate and HRV that
// agree that night are mentioned.
func TestAlertCountsNightsAndCorroborates(t *testing.T) {
	a := alertFor(t, alertDB(t, map[int]float64{0: 0.8, 1: 0.6}, map[int]int{0: 30}, map[int]int{0: 62}))
	want := "Temperature +0.8 °C, above your normal range for 2 nights · lowest heart rate also high · HRV below normal"
	if a != want {
		t.Errorf("got  %q\nwant %q", a, want)
	}
}

// A warning from a reading two days old would linger on the wall; hide it.
func TestAlertHiddenWhenStale(t *testing.T) {
	a := alertFor(t, alertDB(t, map[int]float64{0: 99, 1: 99, 2: 0.9}, nil, nil))
	if a != "" {
		t.Errorf("stale reading raised %q", a)
	}
	// yesterday's reading still counts when last night hasn't arrived yet
	if a := alertFor(t, alertDB(t, map[int]float64{0: 99, 1: 0.9}, nil, nil)); a == "" {
		t.Error("yesterday's elevated reading should still show")
	}
}

func TestAlertEmptyDatabase(t *testing.T) {
	if a := alertFor(t, openWith(t, nil)); a != "" {
		t.Errorf("empty database raised %q", a)
	}
}

// While hidden, the status says why — so the warning is visibly watching.
func TestAlertStatusExplainsWhyHidden(t *testing.T) {
	cases := []struct {
		db   *store.DB
		want string
	}{
		{alertDB(t, map[int]float64{0: 0.1}, nil, nil), "last night +0.10 °C, within your normal range (-"},
		{alertDB(t, map[int]float64{0: 0.25}, nil, nil), "last night +0.25 °C: above your normal range, but under the +0.30 °C threshold"},
		{alertDB(t, map[int]float64{0: 99, 1: 99}, nil, nil), "no temperature reading from the last two nights yet"},
		{openWith(t, []store.Sample{{Metric: store.MetricTempDev, Day: ago(0), Value: 0.1}}), "building your normal range"},
	}
	for i, c := range cases {
		text, why, err := AlertStatus(context.Background(), c.db, today)
		if err != nil {
			t.Fatal(err)
		}
		if text != "" || !strings.HasPrefix(why, c.want) {
			t.Errorf("case %d: text %q, why %q; want why starting %q", i, text, why, c.want)
		}
	}
}
