package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSaveAndRecent(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)

	err := db.Save(ctx, []Sample{
		{Metric: "sleep", Day: "2026-08-01", Value: 71},
		{Metric: "sleep", Day: "2026-08-02", Value: 84},
		{Metric: "steps", Day: "2026-08-02", Value: 9021},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	n, err := db.Count(ctx, "sleep")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Errorf("Count(sleep) = %d, want 2", n)
	}

	recent, err := db.Recent(ctx, "sleep", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("Recent(sleep) returned %d samples, want 2", len(recent))
	}
	// Most recent day first.
	if recent[0].Day != "2026-08-02" {
		t.Errorf("Recent[0].Day = %q, want 2026-08-02", recent[0].Day)
	}
}

// Oura revises recent days as the ring syncs, so re-saving a day must
// overwrite rather than duplicate or fail.
func TestSaveUpserts(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)

	if err := db.Save(ctx, []Sample{{Metric: "sleep", Day: "2026-08-01", Value: 71}}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := db.Save(ctx, []Sample{{Metric: "sleep", Day: "2026-08-01", Value: 88}}); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	n, err := db.Count(ctx, "sleep")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("Count after re-save = %d, want 1 (upsert, not duplicate)", n)
	}

	recent, err := db.Recent(ctx, "sleep", 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if recent[0].Value != 88 {
		t.Errorf("value after re-save = %v, want 88", recent[0].Value)
	}
}

// The whole point of the pointer fields is that a contributor Oura could not
// compute survives the round trip as nil, not as a zero that would read as the
// worst possible sub-score.
func TestSleepContributorsRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)

	deep, eff := 62, 91
	err := db.SaveSleepContributors(ctx, []SleepContributors{{
		Day:       "2026-08-01",
		DeepSleep: &deep,
		// every other contributor left nil - stored as NULL
	}, {
		Day:        "2026-08-02",
		Efficiency: &eff,
	}})
	if err != nil {
		t.Fatalf("SaveSleepContributors: %v", err)
	}

	got, err := db.ContributorsOnDay(ctx, "2026-08-01")
	if err != nil {
		t.Fatalf("ContributorsOnDay: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ContributorsOnDay returned %d rows, want 1", len(got))
	}
	if got[0].DeepSleep == nil || *got[0].DeepSleep != 62 {
		t.Errorf("DeepSleep = %v, want 62", got[0].DeepSleep)
	}
	if got[0].RemSleep != nil {
		t.Errorf("RemSleep = %v, want nil (stored NULL)", *got[0].RemSleep)
	}

	recent, err := db.RecentContributors(ctx, 10)
	if err != nil {
		t.Fatalf("RecentContributors: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("RecentContributors returned %d nights, want 2", len(recent))
	}
	// Most recent day first.
	if recent[0].Day != "2026-08-02" {
		t.Errorf("recent[0].Day = %q, want 2026-08-02", recent[0].Day)
	}
}

// Oura revises a night's contributors as the ring finishes syncing, so
// re-saving a day must overwrite rather than duplicate or fail.
func TestSleepContributorsUpsert(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)

	first, second := 40, 77
	if err := db.SaveSleepContributors(ctx, []SleepContributors{
		{Day: "2026-08-01", DeepSleep: &first},
	}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := db.SaveSleepContributors(ctx, []SleepContributors{
		{Day: "2026-08-01", DeepSleep: &second},
	}); err != nil {
		t.Fatalf("second save: %v", err)
	}

	got, err := db.RecentContributors(ctx, 10)
	if err != nil {
		t.Fatalf("RecentContributors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("after re-save got %d nights, want 1 (upsert, not duplicate)", len(got))
	}
	if got[0].DeepSleep == nil || *got[0].DeepSleep != 77 {
		t.Errorf("DeepSleep after re-save = %v, want 77", got[0].DeepSleep)
	}
}

func TestRecentEmpty(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)

	recent, err := db.Recent(ctx, "sleep", 10)
	if err != nil {
		t.Fatalf("Recent on empty db: %v", err)
	}
	if len(recent) != 0 {
		t.Errorf("Recent on empty db returned %d samples, want 0", len(recent))
	}
}

func TestSleepPeriodsRoundTrip(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	v := 60.0
	phases := "4221113"
	lowest := 47
	in := SleepPeriod{
		ID: "p1", Day: "2026-11-18", Type: "long_sleep",
		BedtimeStart: "2026-11-17T21:28:20-08:00", BedtimeEnd: "2026-11-18T05:03:50-08:00",
		TimeInBed: 27330, LowestHR: &lowest, Phases30Sec: &phases,
		HeartRate: &Series{Interval: 300, Items: []*float64{&v, nil}, Start: "2026-11-17T21:28:20-08:00"},
	}
	if err := db.SaveSleepPeriods(ctx, []SleepPeriod{in}); err != nil {
		t.Fatal(err)
	}
	got, err := db.SleepPeriodsOnDay(ctx, "2026-11-18")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d periods", len(got))
	}
	p := got[0]
	if p.LowestHR == nil || *p.LowestHR != 47 || p.TotalSleep != nil || *p.Phases30Sec != phases {
		t.Errorf("fields = %+v", p)
	}
	if p.HeartRate == nil || len(p.HeartRate.Items) != 2 || *p.HeartRate.Items[0] != 60 || p.HeartRate.Items[1] != nil {
		t.Errorf("heart rate = %+v", p.HeartRate)
	}
	if p.HRV != nil {
		t.Error("absent series should stay nil")
	}
	if day, _ := db.LatestSleepDay(ctx); day != "2026-11-18" {
		t.Errorf("latest day = %q", day)
	}

	// Oura marks a discarded period "deleted": it must disappear
	if err := db.SaveSleepPeriods(ctx, []SleepPeriod{{ID: "p1", Type: "deleted"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.SleepPeriodsOnDay(ctx, "2026-11-18"); len(got) != 0 {
		t.Errorf("deleted period still stored: %+v", got)
	}
}

// Databases made before sleep periods were stored hold an empty placeholder
// table; Open must replace it with the real one.
func TestOpenReplacesSleepPeriodStub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE sleep_periods (id TEXT PRIMARY KEY, day TEXT NOT NULL, fetched_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveSleepPeriods(context.Background(), []SleepPeriod{{
		ID: "p", Day: "2026-11-18", Type: "sleep", BedtimeStart: "x", BedtimeEnd: "y",
	}}); err != nil {
		t.Fatalf("save into migrated table: %v", err)
	}
}

func TestDevicesRoundTrip(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if _, ok, err := db.DeviceByMAC(ctx, "x"); ok || err != nil {
		t.Fatalf("missing device: ok=%v err=%v", ok, err)
	}
	v := 4.05
	dv := Device{MAC: "x", APIKey: "k", FriendlyID: "ABC123", Model: "og", Battery: &v,
		LastSeen: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)}
	if err := db.SaveDevice(ctx, dv); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.DeviceByMAC(ctx, "x")
	if err != nil || !ok || got.APIKey != "k" || *got.Battery != 4.05 || !got.LastSeen.Equal(dv.LastSeen) || got.CreatedAt.IsZero() {
		t.Errorf("got %+v ok=%v err=%v", got, ok, err)
	}
	all, err := db.Devices(ctx)
	if err != nil || len(all) != 1 {
		t.Errorf("devices = %v, %v", all, err)
	}
}
