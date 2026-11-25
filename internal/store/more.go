package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// moreSchema holds the tables for the collections beyond the daily scores:
// events (workouts, sessions, tags, rest mode), all-day time series (heart
// rate, ring battery), and account details. Applied with schema on every
// Open; every statement is idempotent. Times are kept as Oura sends them —
// RFC 3339, local offset for events, UTC for time series — except the time
// series keys, which are Unix seconds for range queries.
const moreSchema = `
CREATE TABLE IF NOT EXISTS workouts (
	id         TEXT PRIMARY KEY,
	day        TEXT NOT NULL,
	activity   TEXT NOT NULL,
	intensity  TEXT NOT NULL,
	source     TEXT NOT NULL,
	label      TEXT,
	start_time TEXT NOT NULL,
	end_time   TEXT NOT NULL,
	calories   REAL,
	distance   REAL, -- meters
	fetched_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS workouts_day ON workouts (day);

/* heart_rate and hrv are JSON {interval, items, timestamp}, as in sleep_periods */
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	day        TEXT NOT NULL,
	type       TEXT NOT NULL,
	mood       TEXT,
	start_time TEXT NOT NULL,
	end_time   TEXT NOT NULL,
	heart_rate TEXT,
	hrv        TEXT,
	fetched_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_day ON sessions (day);

CREATE TABLE IF NOT EXISTS tags (
	id          TEXT PRIMARY KEY,
	type_code   TEXT,
	custom_name TEXT,
	comment     TEXT,
	start_day   TEXT NOT NULL,
	end_day     TEXT,
	start_time  TEXT NOT NULL,
	end_time    TEXT,
	fetched_at  TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS tags_day ON tags (start_day);

/* episodes is Oura's JSON array, kept as sent */
CREATE TABLE IF NOT EXISTS rest_mode_periods (
	id         TEXT PRIMARY KEY,
	start_day  TEXT NOT NULL,
	end_day    TEXT,
	start_time TEXT,
	end_time   TEXT,
	episodes   TEXT,
	fetched_at TIMESTAMP NOT NULL
);

/* every heart-rate reading, day and night; ts is Unix seconds */
CREATE TABLE IF NOT EXISTS heart_rate (
	ts     INTEGER PRIMARY KEY,
	bpm    INTEGER NOT NULL,
	source TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ring_battery (
	ts         INTEGER PRIMARY KEY,
	level      INTEGER NOT NULL,
	charging   INTEGER,
	in_charger INTEGER
);

CREATE TABLE IF NOT EXISTS ring_configurations (
	id            TEXT PRIMARY KEY,
	color         TEXT,
	design        TEXT,
	firmware      TEXT,
	hardware_type TEXT,
	set_up_at     TEXT,
	size          INTEGER,
	fetched_at    TIMESTAMP NOT NULL
);

/* the account profile: one row */
CREATE TABLE IF NOT EXISTS personal_info (
	id             TEXT PRIMARY KEY,
	age            INTEGER,
	weight         REAL, -- kg
	height         REAL, -- m
	biological_sex TEXT,
	email          TEXT,
	fetched_at     TIMESTAMP NOT NULL
);

/* Oura's bedtime guidance: the window is seconds from midnight at day_tz */
CREATE TABLE IF NOT EXISTS sleep_times (
	day            TEXT PRIMARY KEY,
	start_offset   INTEGER,
	end_offset     INTEGER,
	day_tz         INTEGER,
	recommendation TEXT,
	status         TEXT,
	fetched_at     TIMESTAMP NOT NULL
);
`

// Workout is one workout.
type Workout struct {
	ID, Day, Activity, Intensity, Source string
	Label                                *string
	Start, End                           string // RFC 3339, local offset
	Calories, Distance                   *float64
}

// Session is one session: breathing, meditation, a nap, rest.
type Session struct {
	ID, Day, Type string
	Mood          *string
	Start, End    string
	HeartRate     *Series
	HRV           *Series
}

// Tag is one tag the user added.
type Tag struct {
	ID                            string
	TypeCode, CustomName, Comment *string
	StartDay                      string
	EndDay                        *string
	StartTime                     string
	EndTime                       *string
}

// RestModePeriod is one stretch of rest mode.
type RestModePeriod struct {
	ID                         string
	StartDay                   string
	EndDay, StartTime, EndTime *string
	Episodes                   *string // JSON
}

// HeartRate is one reading.
type HeartRate struct {
	Time   time.Time
	BPM    int
	Source string
}

// BatteryLevel is one ring battery reading.
type BatteryLevel struct {
	Time                time.Time
	Level               int
	Charging, InCharger *bool
}

// RingConfiguration is one ring on the account.
type RingConfiguration struct {
	ID                                             string
	Color, Design, Firmware, HardwareType, SetUpAt *string
	Size                                           *int
}

// PersonalInfo is the account profile.
type PersonalInfo struct {
	ID                   string
	Age                  *int
	Weight, Height       *float64
	BiologicalSex, Email *string
}

// SleepTime is one day's bedtime guidance.
type SleepTime struct {
	Day                           string
	StartOffset, EndOffset, DayTZ *int
	Recommendation, Status        *string
}

// upsert runs one statement per row in a single transaction. Like Save, it
// overwrites rows Oura has revised.
func (d *DB) upsert(ctx context.Context, what, query string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin %s: %w", what, err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("store: prepare %s: %w", what, err)
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r...); err != nil {
			return fmt.Errorf("store: write %s: %w", what, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit %s: %w", what, err)
	}
	return nil
}

// SaveWorkouts upserts workouts by id.
func (d *DB) SaveWorkouts(ctx context.Context, ws []Workout) error {
	now := time.Now().UTC()
	rows := make([][]any, len(ws))
	for i, w := range ws {
		rows[i] = []any{w.ID, w.Day, w.Activity, w.Intensity, w.Source, w.Label, w.Start, w.End, w.Calories, w.Distance, now}
	}
	return d.upsert(ctx, "workouts", `INSERT OR REPLACE INTO workouts
		(id, day, activity, intensity, source, label, start_time, end_time, calories, distance, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, rows)
}

// SaveSessions upserts sessions by id.
func (d *DB) SaveSessions(ctx context.Context, ss []Session) error {
	now := time.Now().UTC()
	rows := make([][]any, len(ss))
	for i, s := range ss {
		hr, err := seriesJSON(s.HeartRate)
		if err != nil {
			return err
		}
		hrv, err := seriesJSON(s.HRV)
		if err != nil {
			return err
		}
		rows[i] = []any{s.ID, s.Day, s.Type, s.Mood, s.Start, s.End, hr, hrv, now}
	}
	return d.upsert(ctx, "sessions", `INSERT OR REPLACE INTO sessions
		(id, day, type, mood, start_time, end_time, heart_rate, hrv, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, rows)
}

// SaveTags upserts tags by id.
func (d *DB) SaveTags(ctx context.Context, ts []Tag) error {
	now := time.Now().UTC()
	rows := make([][]any, len(ts))
	for i, t := range ts {
		rows[i] = []any{t.ID, t.TypeCode, t.CustomName, t.Comment, t.StartDay, t.EndDay, t.StartTime, t.EndTime, now}
	}
	return d.upsert(ctx, "tags", `INSERT OR REPLACE INTO tags
		(id, type_code, custom_name, comment, start_day, end_day, start_time, end_time, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, rows)
}

// SaveRestModePeriods upserts rest-mode periods by id.
func (d *DB) SaveRestModePeriods(ctx context.Context, ps []RestModePeriod) error {
	now := time.Now().UTC()
	rows := make([][]any, len(ps))
	for i, p := range ps {
		rows[i] = []any{p.ID, p.StartDay, p.EndDay, p.StartTime, p.EndTime, p.Episodes, now}
	}
	return d.upsert(ctx, "rest mode periods", `INSERT OR REPLACE INTO rest_mode_periods
		(id, start_day, end_day, start_time, end_time, episodes, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, rows)
}

// SaveHeartRates upserts readings by time.
func (d *DB) SaveHeartRates(ctx context.Context, hs []HeartRate) error {
	rows := make([][]any, len(hs))
	for i, h := range hs {
		rows[i] = []any{h.Time.Unix(), h.BPM, h.Source}
	}
	return d.upsert(ctx, "heart rate", `INSERT OR REPLACE INTO heart_rate (ts, bpm, source) VALUES (?, ?, ?)`, rows)
}

// SaveBatteryLevels upserts readings by time.
func (d *DB) SaveBatteryLevels(ctx context.Context, bs []BatteryLevel) error {
	rows := make([][]any, len(bs))
	for i, b := range bs {
		rows[i] = []any{b.Time.Unix(), b.Level, b.Charging, b.InCharger}
	}
	return d.upsert(ctx, "ring battery", `INSERT OR REPLACE INTO ring_battery
		(ts, level, charging, in_charger) VALUES (?, ?, ?, ?)`, rows)
}

// SaveRingConfigurations upserts rings by id.
func (d *DB) SaveRingConfigurations(ctx context.Context, rs []RingConfiguration) error {
	now := time.Now().UTC()
	rows := make([][]any, len(rs))
	for i, r := range rs {
		rows[i] = []any{r.ID, r.Color, r.Design, r.Firmware, r.HardwareType, r.SetUpAt, r.Size, now}
	}
	return d.upsert(ctx, "ring configurations", `INSERT OR REPLACE INTO ring_configurations
		(id, color, design, firmware, hardware_type, set_up_at, size, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, rows)
}

// SavePersonalInfo replaces the account profile.
func (d *DB) SavePersonalInfo(ctx context.Context, p PersonalInfo) error {
	return d.upsert(ctx, "personal info", `INSERT OR REPLACE INTO personal_info
		(id, age, weight, height, biological_sex, email, fetched_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		[][]any{{p.ID, p.Age, p.Weight, p.Height, p.BiologicalSex, p.Email, time.Now().UTC()}})
}

// SaveSleepTimes upserts bedtime guidance by day.
func (d *DB) SaveSleepTimes(ctx context.Context, ss []SleepTime) error {
	now := time.Now().UTC()
	rows := make([][]any, len(ss))
	for i, s := range ss {
		rows[i] = []any{s.Day, s.StartOffset, s.EndOffset, s.DayTZ, s.Recommendation, s.Status, now}
	}
	return d.upsert(ctx, "sleep times", `INSERT OR REPLACE INTO sleep_times
		(day, start_offset, end_offset, day_tz, recommendation, status, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, rows)
}

// HeartRatesBetween returns readings from from (inclusive) to to
// (exclusive), oldest first.
func (d *DB) HeartRatesBetween(ctx context.Context, from, to time.Time) ([]HeartRate, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT ts, bpm, source FROM heart_rate
		WHERE ts >= ? AND ts < ? ORDER BY ts`, from.Unix(), to.Unix())
	if err != nil {
		return nil, fmt.Errorf("store: query heart rate: %w", err)
	}
	defer rows.Close()
	var out []HeartRate
	for rows.Next() {
		var h HeartRate
		var ts int64
		if err := rows.Scan(&ts, &h.BPM, &h.Source); err != nil {
			return nil, fmt.Errorf("store: scan heart rate: %w", err)
		}
		h.Time = time.Unix(ts, 0).UTC()
		out = append(out, h)
	}
	return out, rows.Err()
}

// WorkoutsBetween returns the workouts on days from to to, inclusive,
// earliest first.
func (d *DB) WorkoutsBetween(ctx context.Context, from, to string) ([]Workout, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, day, activity, intensity, source, label,
		start_time, end_time, calories, distance FROM workouts
		WHERE day >= ? AND day <= ? ORDER BY start_time`, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: query workouts: %w", err)
	}
	defer rows.Close()
	var out []Workout
	for rows.Next() {
		var w Workout
		if err := rows.Scan(&w.ID, &w.Day, &w.Activity, &w.Intensity, &w.Source, &w.Label,
			&w.Start, &w.End, &w.Calories, &w.Distance); err != nil {
			return nil, fmt.Errorf("store: scan workout: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SessionsBetween returns the sessions on days from to to, inclusive,
// earliest first, without their heart rate and HRV series.
func (d *DB) SessionsBetween(ctx context.Context, from, to string) ([]Session, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, day, type, mood, start_time, end_time
		FROM sessions WHERE day >= ? AND day <= ? ORDER BY start_time`, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: query sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Day, &s.Type, &s.Mood, &s.Start, &s.End); err != nil {
			return nil, fmt.Errorf("store: scan session: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SleepTimesBetween returns the bedtime guidance for days from to to,
// inclusive, earliest first.
func (d *DB) SleepTimesBetween(ctx context.Context, from, to string) ([]SleepTime, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT day, start_offset, end_offset, day_tz,
		recommendation, status FROM sleep_times WHERE day >= ? AND day <= ? ORDER BY day`, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: query sleep times: %w", err)
	}
	defer rows.Close()
	var out []SleepTime
	for rows.Next() {
		var s SleepTime
		if err := rows.Scan(&s.Day, &s.StartOffset, &s.EndOffset, &s.DayTZ, &s.Recommendation, &s.Status); err != nil {
			return nil, fmt.Errorf("store: scan sleep time: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// LatestHeartRateTime returns the time of the most recent heart-rate
// reading, if any: with the latest battery reading, when the ring last
// synced.
func (d *DB) LatestHeartRateTime(ctx context.Context) (time.Time, bool, error) {
	var ts sql.NullInt64
	if err := d.sql.QueryRowContext(ctx, `SELECT MAX(ts) FROM heart_rate`).Scan(&ts); err != nil {
		return time.Time{}, false, fmt.Errorf("store: latest heart rate: %w", err)
	}
	if !ts.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(ts.Int64, 0).UTC(), true, nil
}

// LatestBattery returns the most recent ring battery reading, if any.
func (d *DB) LatestBattery(ctx context.Context) (BatteryLevel, bool, error) {
	var b BatteryLevel
	var ts int64
	err := d.sql.QueryRowContext(ctx, `SELECT ts, level, charging, in_charger FROM ring_battery
		ORDER BY ts DESC LIMIT 1`).Scan(&ts, &b.Level, &b.Charging, &b.InCharger)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return b, false, nil
		}
		return b, false, fmt.Errorf("store: query battery: %w", err)
	}
	b.Time = time.Unix(ts, 0).UTC()
	return b, true, nil
}

// TableCount is the number of rows in one of the tables above, for the
// fetch summary and tests.
func (d *DB) TableCount(ctx context.Context, table string) (int, error) {
	switch table {
	case "workouts", "sessions", "tags", "rest_mode_periods", "heart_rate", "ring_battery",
		"ring_configurations", "personal_info", "sleep_times":
	default:
		return 0, fmt.Errorf("store: unknown table %q", table)
	}
	var n int
	err := d.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n)
	return n, err
}
