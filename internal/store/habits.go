package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// habitSchema holds the habit log: what you had or did and when, logged by
// hand (a Siri shortcut, the index page), not fetched from Oura, whose API
// can't take tags. Applied with schema on every Open.
const habitSchema = `
/* one row per logged habit: ts is Unix seconds, habit a name from the
   server's catalog (caffeine, alcohol, ...), count how many of its unit
   (one double shot, one drink), source what logged it */
CREATE TABLE IF NOT EXISTS habit_logs (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	ts     INTEGER NOT NULL,
	habit  TEXT NOT NULL,
	count  REAL NOT NULL DEFAULT 1,
	source TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS habit_logs_ts ON habit_logs (ts);
`

// HabitLog is one logged habit.
type HabitLog struct {
	ID     int64
	At     time.Time
	Habit  string
	Count  float64
	Source string
}

// AddHabitLog stores one log and returns it with its ID.
func (d *DB) AddHabitLog(ctx context.Context, l HabitLog) (HabitLog, error) {
	res, err := d.sql.ExecContext(ctx, `INSERT INTO habit_logs (ts, habit, count, source) VALUES (?, ?, ?, ?)`,
		l.At.Unix(), l.Habit, l.Count, l.Source)
	if err != nil {
		return l, fmt.Errorf("store: add habit log: %w", err)
	}
	l.ID, err = res.LastInsertId()
	return l, err
}

// HabitLogsBetween returns the logs from from up to, not including, to,
// earliest first, with times in loc.
func (d *DB) HabitLogsBetween(ctx context.Context, from, to time.Time, loc *time.Location) ([]HabitLog, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, ts, habit, count, source FROM habit_logs
		WHERE ts >= ? AND ts < ? ORDER BY ts, id`, from.Unix(), to.Unix())
	if err != nil {
		return nil, fmt.Errorf("store: query habit logs: %w", err)
	}
	defer rows.Close()
	var out []HabitLog
	for rows.Next() {
		var l HabitLog
		var ts int64
		if err := rows.Scan(&l.ID, &ts, &l.Habit, &l.Count, &l.Source); err != nil {
			return nil, fmt.Errorf("store: scan habit log: %w", err)
		}
		l.At = time.Unix(ts, 0).In(loc)
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteHabitLog removes one log by ID, reporting whether it existed.
func (d *DB) DeleteHabitLog(ctx context.Context, id int64) (bool, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM habit_logs WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("store: delete habit log: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// LastHabitLog returns the most recent log, if any.
func (d *DB) LastHabitLog(ctx context.Context, loc *time.Location) (HabitLog, bool, error) {
	var l HabitLog
	var ts int64
	err := d.sql.QueryRowContext(ctx, `SELECT id, ts, habit, count, source FROM habit_logs
		ORDER BY ts DESC, id DESC LIMIT 1`).Scan(&l.ID, &ts, &l.Habit, &l.Count, &l.Source)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return l, false, nil
		}
		return l, false, fmt.Errorf("store: last habit log: %w", err)
	}
	l.At = time.Unix(ts, 0).In(loc)
	return l, true, nil
}

// TagsBetween returns the Oura tags starting on days from to to, inclusive,
// earliest first.
func (d *DB) TagsBetween(ctx context.Context, from, to string) ([]Tag, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, type_code, custom_name, comment, start_day, end_day,
		start_time, end_time FROM tags WHERE start_day >= ? AND start_day <= ? ORDER BY start_time`, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: query tags: %w", err)
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.TypeCode, &t.CustomName, &t.Comment, &t.StartDay, &t.EndDay,
			&t.StartTime, &t.EndTime); err != nil {
			return nil, fmt.Errorf("store: scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateHabitLog changes one log's time and count, reporting whether it
// existed.
func (d *DB) UpdateHabitLog(ctx context.Context, id int64, at time.Time, count float64) (bool, error) {
	res, err := d.sql.ExecContext(ctx, `UPDATE habit_logs SET ts = ?, count = ? WHERE id = ?`, at.Unix(), count, id)
	if err != nil {
		return false, fmt.Errorf("store: update habit log: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// HabitLogByID returns one log.
func (d *DB) HabitLogByID(ctx context.Context, id int64, loc *time.Location) (HabitLog, bool, error) {
	var l HabitLog
	var ts int64
	err := d.sql.QueryRowContext(ctx, `SELECT id, ts, habit, count, source FROM habit_logs WHERE id = ?`, id).
		Scan(&l.ID, &ts, &l.Habit, &l.Count, &l.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	if err != nil {
		return l, false, fmt.Errorf("store: habit log %d: %w", id, err)
	}
	l.At = time.Unix(ts, 0).In(loc)
	return l, true, nil
}
