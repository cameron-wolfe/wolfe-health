package store

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Most of the database is a cache of Oura that a fetch can rebuild, but not
// all of it: habit_logs, settings (the display schedule, the habit log's
// token, the webhook token) and trmnl_devices (display pairings) were made
// here and exist nowhere else. Hence the daily snapshots below, and why the
// fix for a changed table is never to delete the file.

// Snapshot writes a consistent copy of the whole database to path, safe
// while other connections write. path must not exist yet.
func (d *DB) Snapshot(ctx context.Context, path string) error {
	if _, err := d.sql.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("store: snapshot to %s: %w", path, err)
	}
	return nil
}

const backupPrefix, backupSuffix = "wolfe-health-", ".db"

// Backup keeps one snapshot per day in dir, named for day
// (wolfe-health-2026-11-21.db), and removes all but the newest keep. It
// does nothing when day's snapshot already exists, so calling it often is
// cheap. It reports the snapshot's path and whether it made one.
func (d *DB) Backup(ctx context.Context, dir string, day time.Time, keep int) (string, bool, error) {
	if keep < 1 {
		return "", false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, fmt.Errorf("store: backup folder: %w", err)
	}
	path := filepath.Join(dir, backupPrefix+day.Format("2006-01-02")+backupSuffix)
	made := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// written beside it and renamed, so a snapshot cut short never
		// passes for a whole one
		tmp := path + ".partial"
		os.Remove(tmp)
		if err := d.Snapshot(ctx, tmp); err != nil {
			os.Remove(tmp)
			return "", false, err
		}
		if err := os.Chmod(tmp, 0o600); err != nil { // it holds tokens and pairing keys
			os.Remove(tmp)
			return "", false, fmt.Errorf("store: backup: %w", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			return "", false, fmt.Errorf("store: backup: %w", err)
		}
		made = true
	}
	return path, made, pruneBackups(dir, keep)
}

// pruneBackups removes all but the newest keep dated snapshots in dir;
// other files are left alone.
func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("store: backup folder: %w", err)
	}
	var dated []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, backupPrefix) && strings.HasSuffix(n, backupSuffix) {
			if _, err := time.Parse("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(n, backupPrefix), backupSuffix)); err == nil {
				dated = append(dated, n)
			}
		}
	}
	sort.Strings(dated) // dates in the name sort oldest first
	for len(dated) > keep {
		if err := os.Remove(filepath.Join(dir, dated[0])); err != nil {
			return fmt.Errorf("store: removing an old backup: %w", err)
		}
		dated = dated[1:]
	}
	return nil
}

// ImportHabitLogs adds logs that aren't stored yet and reports how many it
// added. Logs alike in time, habit, count and source are counted, not
// collapsed: two coffees backdated to the same minute are two logs, so a
// file holding two where the database holds one adds one.
func (d *DB) ImportHabitLogs(ctx context.Context, logs []HabitLog) (int, error) {
	type key struct {
		ts            int64
		habit, source string
		count         float64
	}
	want := map[key]int{}
	var order []key
	for _, l := range logs {
		k := key{l.At.Unix(), l.Habit, l.Source, l.Count}
		if want[k] == 0 {
			order = append(order, k)
		}
		want[k]++
	}
	added := 0
	for _, k := range order {
		var have int
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM habit_logs WHERE ts = ? AND habit = ? AND count = ? AND source = ?`,
			k.ts, k.habit, k.count, k.source).Scan(&have); err != nil {
			return added, fmt.Errorf("store: import habit logs: %w", err)
		}
		for ; have < want[k]; have++ {
			if _, err := d.AddHabitLog(ctx, HabitLog{At: time.Unix(k.ts, 0), Habit: k.habit, Count: k.count, Source: k.source}); err != nil {
				return added, err
			}
			added++
		}
	}
	return added, nil
}

// habitCSVHeader is the habit log export's header row.
var habitCSVHeader = []string{"time", "habit", "count", "source"}

// WriteHabitCSV writes logs as CSV: a header, then one row per log with its
// time in RFC 3339 (local offset included), so the file reads in any
// spreadsheet and imports back exactly.
func WriteHabitCSV(w io.Writer, logs []HabitLog) error {
	cw := csv.NewWriter(w)
	cw.Write(habitCSVHeader)
	for _, l := range logs {
		cw.Write([]string{l.At.Format(time.RFC3339), l.Habit, strconv.FormatFloat(l.Count, 'f', -1, 64), l.Source})
	}
	cw.Flush()
	return cw.Error()
}

// ReadHabitCSV reads what WriteHabitCSV wrote.
func ReadHabitCSV(r io.Reader) ([]HabitLog, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("store: habit CSV: %w", err)
	}
	if len(rows) == 0 || strings.Join(rows[0], ",") != strings.Join(habitCSVHeader, ",") {
		return nil, fmt.Errorf("store: habit CSV: the first row should be %s", strings.Join(habitCSVHeader, ","))
	}
	var logs []HabitLog
	for i, row := range rows[1:] {
		if len(row) != len(habitCSVHeader) {
			return nil, fmt.Errorf("store: habit CSV row %d: %d fields, want %d", i+2, len(row), len(habitCSVHeader))
		}
		at, err := time.Parse(time.RFC3339, row[0])
		if err != nil {
			return nil, fmt.Errorf("store: habit CSV row %d: time %q: %w", i+2, row[0], err)
		}
		n, err := strconv.ParseFloat(row[2], 64)
		if err != nil || row[1] == "" {
			return nil, fmt.Errorf("store: habit CSV row %d: bad habit or count", i+2)
		}
		logs = append(logs, HabitLog{At: at, Habit: row[1], Count: n, Source: row[3]})
	}
	return logs, nil
}

// CacheTables are the tables a fetch fills from Oura, and so can rebuild.
// Every other table holds something made here.
var CacheTables = []string{
	"samples", "sleep_periods", "sleep_contributors", "workouts", "sessions", "tags",
	"rest_mode_periods", "heart_rate", "ring_battery", "ring_configurations", "personal_info", "sleep_times",
}

// DropCacheTable drops one of CacheTables, for a table whose columns
// changed: the next Open creates it afresh and the next long fetch refills
// it. It refuses any other table, so the habit log, settings and display
// pairings can't go this way.
func (d *DB) DropCacheTable(ctx context.Context, name string) error {
	for _, t := range CacheTables {
		if t == name {
			if _, err := d.sql.ExecContext(ctx, "DROP TABLE IF EXISTS "+t); err != nil {
				return fmt.Errorf("store: drop %s: %w", t, err)
			}
			return nil
		}
	}
	return fmt.Errorf("store: %q isn't a table Oura can refill (%s); its data was made here, so it's never dropped",
		name, strings.Join(CacheTables, ", "))
}
