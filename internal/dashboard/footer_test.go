package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The footer gives the battery, the ring's last sync (its newest reading)
// and the last update, as clock times; a low battery warns, charging
// doesn't.
func TestFooter(t *testing.T) {
	ctx := context.Background()
	loc := time.FixedZone("PST", -8*3600)
	now := time.Date(2026, 11, 20, 15, 0, 0, 0, loc)

	db := openWith(t, nil)
	if f, err := Footer(ctx, db, now); err != nil || f.Left != "" || f.Right != "" {
		t.Errorf("empty database: %+v, %v", f, err)
	}

	db = openWith(t, []store.Sample{{Metric: store.MetricSleep, Day: "2026-11-20", Value: 80}})
	sync := time.Date(2026, 11, 20, 14, 28, 0, 0, loc)
	if err := db.SaveHeartRates(ctx, []store.HeartRate{{Time: sync, BPM: 60, Source: "awake"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBatteryLevels(ctx, []store.BatteryLevel{{Time: sync.Add(-time.Hour), Level: 64}}); err != nil {
		t.Fatal(err)
	}
	f, err := Footer(ctx, db, now)
	if err != nil {
		t.Fatal(err)
	}
	if f.Left != "ring 64% · synced 2:28 PM" || f.Warn || len(f.Right) < len("updated ") || f.Right[:8] != "updated " {
		t.Errorf("footer %+v", f)
	}

	no, yes := false, true
	if err := db.SaveBatteryLevels(ctx, []store.BatteryLevel{{Time: sync.Add(time.Minute), Level: 12, Charging: &no}}); err != nil {
		t.Fatal(err)
	}
	if f, _ := Footer(ctx, db, now); f.Left != "ring 12%: charge soon · synced 2:29 PM" || !f.Warn {
		t.Errorf("low battery: %+v", f)
	}
	if err := db.SaveBatteryLevels(ctx, []store.BatteryLevel{{Time: sync.Add(2 * time.Minute), Level: 12, Charging: &yes}}); err != nil {
		t.Fatal(err)
	}
	if f, _ := Footer(ctx, db, now); f.Left != "ring 12%, charging · synced 2:30 PM" || f.Warn {
		t.Errorf("charging: %+v", f)
	}
	// a sync on an earlier day carries its date
	if f, _ := Footer(ctx, db, now.AddDate(0, 0, 1)); f.Left != "ring 12%, charging · synced Nov 20, 2:30 PM" {
		t.Errorf("next day: %+v", f)
	}
}
