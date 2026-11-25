package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A snapshot is a whole, openable copy; one is kept per day, a second call
// that day is a no-op, and only the newest keep survive.
func TestBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 11, 21, 8, 30, 0, 0, time.UTC)
	if _, err := db.AddHabitLog(ctx, HabitLog{At: at, Habit: "Caffeine", Count: 1, Source: "shortcut"}); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	day := func(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }
	path, made, err := db.Backup(ctx, backups, day(21), 3)
	if err != nil || !made || filepath.Base(path) != "wolfe-health-2026-11-21.db" {
		t.Fatalf("first backup: %s %v %v", path, made, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("snapshot permissions: %v, %v; it holds tokens", fi.Mode().Perm(), err)
	}
	if _, made, _ := db.Backup(ctx, backups, day(21), 3); made {
		t.Error("a second backup the same day")
	}
	snap, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := snap.HabitLogsBetween(ctx, at.Add(-time.Hour), at.Add(time.Hour), time.UTC)
	snap.Close()
	if err != nil || len(logs) != 1 || logs[0].Habit != "Caffeine" {
		t.Fatalf("the snapshot's habit logs: %+v, %v", logs, err)
	}

	os.WriteFile(filepath.Join(backups, "notes.txt"), []byte("mine"), 0o600) // not a snapshot: kept
	for _, d := range []int{22, 23, 24, 25} {
		if _, _, err := db.Backup(ctx, backups, day(d), 3); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(backups)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, " "); got != "notes.txt wolfe-health-2026-11-23.db wolfe-health-2026-11-24.db wolfe-health-2026-11-25.db" {
		t.Errorf("after pruning: %s", got)
	}
	if _, made, _ := db.Backup(ctx, backups, day(26), 0); made {
		t.Error("keep 0 should turn backups off")
	}
}

// The CSV export reads back exactly, and importing it again adds nothing.
func TestHabitCSVRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pst := time.FixedZone("PST", -8*3600)
	in := []HabitLog{
		{At: time.Date(2026, 11, 21, 8, 30, 0, 0, pst), Habit: "Caffeine", Count: 1, Source: "shortcut"},
		{At: time.Date(2026, 11, 20, 21, 0, 0, 0, pst), Habit: "Alcohol", Count: 2, Source: "shortcut"},
		{At: time.Date(2026, 11, 20, 21, 0, 0, 0, pst), Habit: "Late night out, with friends", Count: 1}, // a comma
		{At: time.Date(2026, 11, 21, 8, 30, 0, 0, pst), Habit: "Caffeine", Count: 1, Source: "shortcut"}, // a second coffee, the same minute
	}
	var buf bytes.Buffer
	if err := WriteHabitCSV(&buf, in); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "time,habit,count,source\n2026-11-21T08:30:00-08:00,Caffeine,1,shortcut\n") {
		t.Errorf("CSV = %q", buf.String())
	}
	out, err := ReadHabitCSV(bytes.NewReader(buf.Bytes()))
	if err != nil || len(out) != 4 || !out[0].At.Equal(in[0].At) || out[1].Count != 2 || out[2].Habit != in[2].Habit {
		t.Fatalf("read back: %+v, %v", out, err)
	}
	if n, err := db.ImportHabitLogs(ctx, out[:3]); err != nil || n != 3 {
		t.Fatalf("import: %d, %v", n, err)
	}
	if n, _ := db.ImportHabitLogs(ctx, out); n != 1 {
		t.Errorf("the second same-minute coffee: added %d, want 1", n)
	}
	if n, _ := db.ImportHabitLogs(ctx, out); n != 0 {
		t.Errorf("a second import added %d", n)
	}
	if _, err := ReadHabitCSV(strings.NewReader("when,what\nx,y\n")); err == nil {
		t.Error("a file without the header should be refused")
	}
}

// Only Oura's tables can be dropped; the ones holding data made here can't,
// and a dropped table comes back empty on the next open.
func TestDropCacheTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "w.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, mine := range []string{"habit_logs", "settings", "trmnl_devices", "sqlite_master", "samples; DROP TABLE habit_logs"} {
		if err := db.DropCacheTable(ctx, mine); err == nil {
			t.Errorf("%q was dropped", mine)
		}
	}
	db.Save(ctx, []Sample{{Metric: MetricSleep, Day: "2026-11-21", Value: 88}})
	if err := db.DropCacheTable(ctx, "samples"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err = Open(path); err != nil { // recreates it
		t.Fatal(err)
	}
	defer db.Close()
	if got, err := db.Range(ctx, MetricSleep, 10, "2026-11-01", "2026-11-30"); err != nil || len(got) != 0 {
		t.Errorf("samples after a drop: %v, %v", got, err)
	}
}
