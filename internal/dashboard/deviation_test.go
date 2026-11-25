package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Each reading is placed as its distance from the user's normal, in
// half-ranges; a reading far off normal counts as off and is flagged; one
// without readings has no point.
func TestDeviation(t *testing.T) {
	var samples []store.Sample
	for i := 0; i < 40; i++ {
		day := today.AddDate(0, 0, -i).Format(dayLayout)
		temp := []float64{-0.1, 0, 0.1}[i%3]
		sleep := []float64{80, 82, 84}[i%3]
		if i == 0 {
			temp, sleep = 0.8, 82 // last night: temperature well above, sleep at its middle
		}
		samples = append(samples,
			store.Sample{Metric: store.MetricTempDev, Day: day, Value: temp},
			store.Sample{Metric: store.MetricSleep, Day: day, Value: sleep})
	}
	d, err := Deviation(context.Background(), openWith(t, samples), today)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Spokes) != 8 || d.Spokes[0].Label != "HRV" {
		t.Fatalf("spokes %+v", d.Spokes)
	}
	byLabel := map[string]int{}
	for i, s := range d.Spokes {
		byLabel[s.Label] = i
	}
	temp, sleep, hrv := d.Spokes[byLabel["Temp"]], d.Spokes[byLabel["Sleep"]], d.Spokes[byLabel["HRV"]]
	if !temp.Has || temp.D <= 2 || !temp.Alert || temp.Text != "+0.80 °C" || !temp.HasAvg {
		t.Errorf("temp spoke %+v", temp)
	}
	if !sleep.Has || sleep.D != 0 || sleep.Alert {
		t.Errorf("sleep spoke %+v", sleep)
	}
	if hrv.Has {
		t.Errorf("HRV with no readings: %+v", hrv)
	}
	if d.Headline != "1 reading off normal" {
		t.Errorf("headline %q", d.Headline)
	}
}
