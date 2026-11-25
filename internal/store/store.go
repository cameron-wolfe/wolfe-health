// Package store persists Oura samples in a local SQLite database.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	// Pure-Go SQLite driver. Deliberately not mattn/go-sqlite3, which needs
	// cgo and would mean setting up a cross-compiler to build for the Pi.
	_ "modernc.org/sqlite"
)

// Metric names as stored in the samples table's metric column. They live here,
// beside the table, because the database is the interface between fetching
// and charting: both sides need the same names.
const (
	MetricSleep     = "sleep"
	MetricReadiness = "readiness"
	MetricStress    = "stress" // value: seconds in high stress; qualifier: day summary
	MetricActivity  = "activity"
	MetricSteps     = "steps"
	MetricTempDev   = "temp_deviation"   // °C from the user's baseline, from readiness
	MetricRecovery  = "recovery"         // value: seconds in high recovery, from daily_stress
	MetricActiveCal = "active_calories"  // the day's active calories so far, from daily_activity
	MetricTotalCal  = "total_calories"   // active plus resting burn, so far
	MetricGoalCal   = "target_calories"  // Oura's active-calorie goal for the day
	MetricGoalLeft  = "meters_to_target" // walking distance still to that goal, in meters

	MetricSpO2        = "spo2"                  // nightly average blood oxygen, %
	MetricBreathingDI = "breathing_disturbance" // breathing disturbance index, nightly
	MetricCardioAge   = "cardio_age"            // vascular age estimate, years
	MetricPulseWave   = "pulse_wave_velocity"   // m/s, behind cardio_age
	MetricVO2Max      = "vo2_max"               // ml/kg/min
	MetricResilience  = "resilience"            // level 1 (limited) to 5 (exceptional); qualifier: the level's name
	MetricResSleep    = "resilience_sleep"      // resilience contributors, 0-100
	MetricResDaytime  = "resilience_daytime"
	MetricResStress   = "resilience_stress"

	// MetricReadinessPart prefixes each part of the readiness score, 0-100,
	// by Oura's contributor name: "readiness_part_hrv_balance".
	MetricReadinessPart = "readiness_part_"
)

// ReadinessParts are Oura's readiness contributors, as stored after
// MetricReadinessPart.
var ReadinessParts = []string{
	"activity_balance", "body_temperature", "hrv_balance", "previous_day_activity",
	"previous_night", "recovery_index", "resting_heart_rate", "sleep_balance", "sleep_regularity",
}

const dailyColumns = `metric, day, value, qualifier`
const contributorColumns = `day, deep_sleep, efficiency, latency,
	rem_sleep, restfulness, timing, total_sleep`

// Sample is one metric's value on one day.
//
// Qualifier is an optional categorical label the API reports alongside the
// number — daily_stress returns "restored", "normal" or "stressful" in
// day_summary, for example. Metrics without one store the empty string rather
// than NULL, so readers never need to handle a null scan.
type Sample struct {
	Metric    string
	Day       string // YYYY-MM-DD
	Value     float64
	Qualifier string
}

// SleepPeriod is one stored sleep period: a night, a nap, or a fragment.
//
// It mirrors Oura's record closely because the chart needs nearly all of it.
// Nullable numbers are pointers, as elsewhere: nil is "Oura had no number",
// never zero. The phase and movement strings stay in Oura's one-character-
// per-epoch form; decoding them is the reader's job.
type SleepPeriod struct {
	ID           string
	Day          string // YYYY-MM-DD, the wake-up day
	Type         string // long_sleep, sleep, late_nap, rest
	BedtimeStart string // RFC 3339 with local offset
	BedtimeEnd   string

	TimeInBed     int // seconds
	TotalSleep    *int
	DeepSleep     *int
	LightSleep    *int
	RemSleep      *int
	AwakeTime     *int
	Efficiency    *int
	Latency       *int
	LowestHR      *int
	AverageHRV    *int
	RestlessCount *int
	AverageHR     *float64
	AverageBreath *float64

	Phases5Min  *string
	Phases30Sec *string
	Movement30s *string

	HeartRate *Series // nil when Oura sent none
	HRV       *Series
}

// Series is a regularly spaced run of readings: Items[i] was taken
// Interval*i seconds after Start. A nil item is a missing reading.
// Stored as JSON in a TEXT column.
type Series struct {
	Interval float64    `json:"interval"`
	Items    []*float64 `json:"items"`
	Start    string     `json:"timestamp"` // RFC 3339
}

// SleepContributors is one night's breakdown of the daily sleep score.
//
// This gets its own table rather than seven rows in samples because the seven
// numbers are only meaningful together — they are the decomposition of one
// score, always written and read as a set. A row per night keeps that intact
// and makes "why was Tuesday bad?" a single lookup instead of a seven-way join.
//
// Every contributor is nullable: Oura omits one it could not compute, and a nil
// here stores NULL rather than a zero that would read as the worst sub-score.
type SleepContributors struct {
	Day         string // YYYY-MM-DD
	DeepSleep   *int
	Efficiency  *int
	Latency     *int
	RemSleep    *int
	Restfulness *int
	Timing      *int
	TotalSleep  *int
}

// DB is the local sample database.
type DB struct {
	sql *sql.DB
}

// schema is applied on every Open; all statements are idempotent.
//
// One generic table rather than a table per metric: adding a new Oura field
// later is then a code change only, with no migration.
//
// CREATE TABLE IF NOT EXISTS only creates absent tables — it never alters an
// existing one, so changing a column here has no effect on a database that
// already exists. Oura's tables are a cache a fetch can rebuild, so the fix
// is to drop the one table that changed and let the next start refetch it
// (a start covers a year), with the guarded command that only drops Oura's:
//
//	wolfe-health reset sleep_periods
//
// Never delete the whole file to the same end. It also holds what Oura can't
// send back: the habit log, the settings (display schedule, habit and
// webhook tokens) and the display pairings (see backup.go). And history
// older than a year would go too, unless backfilled first with a large
// WH_DAYS. A table holding data made here needs a real migration instead.
const schema = `
/* daily samples - one number per metric per day */
CREATE TABLE IF NOT EXISTS samples (
	metric     TEXT NOT NULL,
	day        TEXT NOT NULL,
	value      REAL NOT NULL,
	qualifier  TEXT NOT NULL DEFAULT '',
	fetched_at TIMESTAMP NOT NULL,
	PRIMARY KEY (metric, day)
);

/* one row per sleep period - a night, a nap, or a fragment of a broken
   night; several can share a day. Times are RFC 3339 with the ring's local
   offset. The phase and movement columns are Oura's strings, one character
   per epoch; heart_rate and hrv are JSON {interval, items, timestamp}. */
CREATE TABLE IF NOT EXISTS sleep_periods (
	id             TEXT PRIMARY KEY,
	day            TEXT NOT NULL,
	type           TEXT NOT NULL,
	bedtime_start  TEXT NOT NULL,
	bedtime_end    TEXT NOT NULL,
	time_in_bed    INTEGER NOT NULL,
	total_sleep    INTEGER,
	deep_sleep     INTEGER,
	light_sleep    INTEGER,
	rem_sleep      INTEGER,
	awake_time     INTEGER,
	efficiency     INTEGER,
	latency        INTEGER,
	lowest_hr      INTEGER,
	average_hrv    INTEGER,
	restless_count INTEGER,
	average_hr     REAL,
	average_breath REAL,
	phases_5min    TEXT,
	phases_30sec   TEXT,
	movement_30sec TEXT,
	heart_rate     TEXT,
	hrv            TEXT,
	fetched_at     TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS sleep_periods_day ON sleep_periods (day);

/* the seven sub-scores behind each day's sleep score, one row per night.
   day is the key, matching samples.day, so this joins straight onto the
   'sleep' metric. every contributor is nullable - Oura leaves one out when
   it could not compute it, and NULL says that where 0 would lie. */
CREATE TABLE IF NOT EXISTS sleep_contributors (
	day         TEXT PRIMARY KEY,
	deep_sleep  INTEGER,
	efficiency  INTEGER,
	latency     INTEGER,
	rem_sleep   INTEGER,
	restfulness INTEGER,
	timing      INTEGER,
	total_sleep INTEGER,
	fetched_at  TIMESTAMP NOT NULL
);

/* e-ink displays (TRMNL) that have paired with this server, keyed by MAC.
   api_key is what the device sends back as Access-Token; the rest is what
   it last reported, for the index page. */
CREATE TABLE IF NOT EXISTS trmnl_devices (
	mac         TEXT PRIMARY KEY,
	api_key     TEXT NOT NULL,
	friendly_id TEXT NOT NULL,
	model       TEXT NOT NULL DEFAULT '',
	firmware    TEXT NOT NULL DEFAULT '',
	battery     REAL,
	rssi        INTEGER,
	width       INTEGER,
	height      INTEGER,
	last_screen TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMP NOT NULL,
	last_seen   TIMESTAMP
);

/* small named settings edited from the index page, e.g. the display
   schedule, stored as JSON text */
CREATE TABLE IF NOT EXISTS settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

/* phone alerts sent (internal/alerts): one row per occurrence, by key,
   so an alert goes out once; made here, so kept like the habit log */
CREATE TABLE IF NOT EXISTS alerts_sent (
	key     TEXT PRIMARY KEY,
	title   TEXT NOT NULL,
	message TEXT NOT NULL,
	sent_at INTEGER NOT NULL
);

-- add more tables here as needed. ie heart rate values

`

// Open opens the database at path, creating and migrating it if needed.
//
// Two pragmas matter here. WAL journalling lets a reader — a GUI browser or a
// sqlite3 shell left open — stay connected while this process writes, instead
// of the two blocking each other. busy_timeout then retries for five seconds
// on a lock rather than failing instantly, which is the default and surfaces
// as an unhelpful "database is locked".
func Open(path string) (*DB, error) {
	// URI-style DSN; the driver applies each _pragma to every pooled connection.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := dropSleepPeriodStub(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	// apply the schema, ignore the result, but keep the error.
	if _, err := sqlDB.Exec(schema + moreSchema + habitSchema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	// return a pointer to the address of this newly created struct of the sqlDB
	return &DB{sql: sqlDB}, nil
}

// dropSleepPeriodStub removes the placeholder sleep_periods table that
// earlier versions created (id, day, fetched_at only) so the schema can
// create the real one. CREATE TABLE IF NOT EXISTS would otherwise leave the
// stub in place forever. Nothing ever wrote to the stub, so dropping it loses
// nothing; a table that already has the real columns is left alone.
func dropSleepPeriodStub(db *sql.DB) error {
	var tables, realCols int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'sleep_periods'`).Scan(&tables); err != nil {
		return fmt.Errorf("store: inspect sleep_periods: %w", err)
	}
	if tables == 0 {
		return nil
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sleep_periods')
		WHERE name = 'bedtime_start'`).Scan(&realCols); err != nil {
		return fmt.Errorf("store: inspect sleep_periods: %w", err)
	}
	if realCols > 0 {
		return nil
	}
	if _, err := db.Exec(`DROP TABLE sleep_periods`); err != nil {
		return fmt.Errorf("store: drop sleep_periods stub: %w", err)
	}
	return nil
}

// Close releases the database.
func (d *DB) Close() error { return d.sql.Close() }

// Save writes samples in one transaction, replacing any existing row for the
// same metric and day.
//
// Upsert rather than insert because Oura revises recent days as more data
// syncs from the ring — re-fetching the same day must overwrite, not fail.
func (d *DB) Save(ctx context.Context, samples []Sample) error {
	// begin a transaction with the database
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	// prepare a sql statement to place the samples into the database
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO samples (metric, day, value, qualifier, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(metric, day) DO UPDATE SET
			value      = excluded.value,
			qualifier  = excluded.qualifier,
			fetched_at = excluded.fetched_at
	`)
	if err != nil {
		return fmt.Errorf("store: prepare: %w", err)
	}
	// defer statment close to ensure we don't forget it later
	defer stmt.Close()

	now := time.Now().UTC()

	// _ discard the index, and just use each element in samples
	for _, s := range samples {
		// discard the result but keep any errors
		// execute the prepared stmt
		if _, err := stmt.ExecContext(ctx, s.Metric, s.Day, s.Value, s.Qualifier, now); err != nil {
			return fmt.Errorf("store: insert %s/%s: %w", s.Metric, s.Day, err)
		}
	}

	// finalize the transaction after the executed statement
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// SaveSleepContributors writes one row per night in one transaction, replacing
// any existing row for the same day.
//
// Same upsert reasoning as Save: Oura revises a night's contributors as the
// ring finishes syncing, so re-fetching a day must overwrite rather than fail.
func (d *DB) SaveSleepContributors(ctx context.Context, nights []SleepContributors) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO sleep_contributors
			(day, deep_sleep, efficiency, latency, rem_sleep,
			 restfulness, timing, total_sleep, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET
			deep_sleep  = excluded.deep_sleep,
			efficiency  = excluded.efficiency,
			latency     = excluded.latency,
			rem_sleep   = excluded.rem_sleep,
			restfulness = excluded.restfulness,
			timing      = excluded.timing,
			total_sleep = excluded.total_sleep,
			fetched_at  = excluded.fetched_at
	`)
	if err != nil {
		return fmt.Errorf("store: prepare contributors: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()

	for _, n := range nights {
		// A nil *int binds as NULL, which is exactly what a missing
		// contributor should store.
		if _, err := stmt.ExecContext(ctx, n.Day,
			n.DeepSleep, n.Efficiency, n.Latency, n.RemSleep,
			n.Restfulness, n.Timing, n.TotalSleep, now); err != nil {
			return fmt.Errorf("store: insert contributors %s: %w", n.Day, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// queryContributors runs a query and collects its rows.
//
// The contract mirrors querySamples: query must select the nine columns of
// sleep_contributors bar fetched_at, in table order. Scanning a nullable
// column into a **int leaves the field nil on NULL and allocates otherwise,
// which is what preserves "Oura had no number here".
func (d *DB) queryContributors(ctx context.Context, what string,
	query string, args ...any) ([]SleepContributors, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: %s: %w", what, err)
	}
	defer rows.Close()

	var out []SleepContributors
	for rows.Next() {
		var n SleepContributors
		if err := rows.Scan(&n.Day,
			&n.DeepSleep, &n.Efficiency, &n.Latency, &n.RemSleep,
			&n.Restfulness, &n.Timing, &n.TotalSleep); err != nil {
			return nil, fmt.Errorf("store: scan %s: %w", what, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %s: %w", what, err)
	}
	return out, nil
}

// RecentContributors returns up to limit nights of contributors, most recent
// day first.
func (d *DB) RecentContributors(ctx context.Context, limit int) ([]SleepContributors, error) {
	return d.queryContributors(ctx, "recent contributors", `
		SELECT `+contributorColumns+` FROM sleep_contributors
		ORDER BY day DESC
		LIMIT ?
	`, limit)
}

// ContributorsOnDay returns one night's contributors, or an empty slice if that
// day was never stored. day is the primary key, so the result holds at most one.
func (d *DB) ContributorsOnDay(ctx context.Context, date string) ([]SleepContributors, error) {
	return d.queryContributors(ctx, "contributors on day "+date, `
		SELECT `+contributorColumns+` FROM sleep_contributors
		WHERE day = ?
	`, date)
}

// querySamples runs a query and collects its rows into samples.
//
// Every reader below differs only in its SQL, so the row loop lives here once.
// The contract is that query selects exactly metric, day, value, in that order
// — that is what makes a single scan valid for all of them.
//
// what names the calling query ("recent sleep") and is folded into any error,
// so a failure points at the query that caused it.
func (d *DB) querySamples(ctx context.Context, what string,
	query string, args ...any) ([]Sample, error) {
	// args... spreads the caller's placeholder values into the variadic call
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: %s: %w", what, err)
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var s Sample
		// scan - convert columns read to useful types (ie string, float64)
		if err := rows.Scan(&s.Metric, &s.Day, &s.Value, &s.Qualifier); err != nil {
			return nil, fmt.Errorf("store: scan %s: %w", what, err)
		}
		// format read row back into sample struct
		out = append(out, s)
	}
	// rows.Err reports a failure that ended iteration early, which the loop
	// above cannot distinguish from a clean finish on its own.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %s: %w", what, err)
	}
	return out, nil
}

// Recent returns up to limit samples for a metric, most recent day first.
func (d *DB) Recent(ctx context.Context, metric string, limit int) ([]Sample, error) {
	return d.querySamples(ctx, "recent "+metric, `
		SELECT `+dailyColumns+` FROM samples
		WHERE metric = ?
		ORDER BY day DESC
		LIMIT ?
	`, metric, limit)
}

// Series returns up to limit samples for a metric, oldest day first.
//
// Recent returns newest-first, which suits a "latest value" readout. Charts
// need the opposite, so the inner query takes the most recent rows and the
// outer one flips them back into chronological order — left to right.
func (d *DB) Series(ctx context.Context, metric string, limit int) ([]Sample, error) {
	return d.querySamples(ctx, "series "+metric, `
		SELECT `+dailyColumns+` FROM (
			SELECT `+dailyColumns+` FROM samples
			WHERE metric = ?
			ORDER BY day DESC
			LIMIT ?
		)
		ORDER BY day ASC
	`, metric, limit)
}

// Range returns up to limit samples for a metric between startDate and endDate
// inclusive, oldest day first. Dates are ISO YYYY-MM-DD, matching the day column.
func (d *DB) Range(ctx context.Context, metric string, limit int,
	startDate string, endDate string) ([]Sample, error) {
	return d.querySamples(ctx, "range "+metric, `
		SELECT `+dailyColumns+` FROM samples
		WHERE metric = ?
		  AND day BETWEEN ? AND ?
		ORDER BY day ASC
		LIMIT ?
	`, metric, startDate, endDate, limit)
}

// MetricOnDay returns the sample for one metric on one day, or an empty slice
// if that day was never stored. (metric, day) is the primary key, so the result
// holds at most one element.
func (d *DB) MetricOnDay(ctx context.Context, metric string, date string) ([]Sample, error) {
	return d.querySamples(ctx, "metric on day "+metric+"/"+date, `
		SELECT `+dailyColumns+` FROM samples
		WHERE metric = ?
		  AND day IS ?
	`, metric, date)
}

// Count reports how many days are stored for a metric.
func (d *DB) Count(ctx context.Context, metric string) (int, error) {
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM samples WHERE metric = ?`, metric).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count %s: %w", metric, err)
	}
	return n, nil
}

// sleepPeriodColumns are selected by every sleep period reader, in the order
// scanSleepPeriod expects.
const sleepPeriodColumns = `id, day, type, bedtime_start, bedtime_end,
	time_in_bed, total_sleep, deep_sleep, light_sleep, rem_sleep, awake_time,
	efficiency, latency, lowest_hr, average_hrv, restless_count,
	average_hr, average_breath, phases_5min, phases_30sec, movement_30sec,
	heart_rate, hrv`

// SaveSleepPeriods writes periods in one transaction, replacing any existing
// row with the same id. Oura marks a period it has discarded with type
// "deleted"; those are removed rather than stored.
func (d *DB) SaveSleepPeriods(ctx context.Context, periods []SleepPeriod) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO sleep_periods (`+sleepPeriodColumns+`, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			day = excluded.day, type = excluded.type,
			bedtime_start = excluded.bedtime_start, bedtime_end = excluded.bedtime_end,
			time_in_bed = excluded.time_in_bed, total_sleep = excluded.total_sleep,
			deep_sleep = excluded.deep_sleep, light_sleep = excluded.light_sleep,
			rem_sleep = excluded.rem_sleep, awake_time = excluded.awake_time,
			efficiency = excluded.efficiency, latency = excluded.latency,
			lowest_hr = excluded.lowest_hr, average_hrv = excluded.average_hrv,
			restless_count = excluded.restless_count, average_hr = excluded.average_hr,
			average_breath = excluded.average_breath, phases_5min = excluded.phases_5min,
			phases_30sec = excluded.phases_30sec, movement_30sec = excluded.movement_30sec,
			heart_rate = excluded.heart_rate, hrv = excluded.hrv,
			fetched_at = excluded.fetched_at
	`)
	if err != nil {
		return fmt.Errorf("store: prepare sleep periods: %w", err)
	}
	defer stmt.Close()

	del, err := tx.PrepareContext(ctx, `DELETE FROM sleep_periods WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("store: prepare sleep period delete: %w", err)
	}
	defer del.Close()

	now := time.Now().UTC()
	for _, p := range periods {
		if p.Type == "deleted" {
			if _, err := del.ExecContext(ctx, p.ID); err != nil {
				return fmt.Errorf("store: delete sleep period %s: %w", p.ID, err)
			}
			continue
		}
		hr, err := seriesJSON(p.HeartRate)
		if err != nil {
			return err
		}
		hrv, err := seriesJSON(p.HRV)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, p.ID, p.Day, p.Type, p.BedtimeStart, p.BedtimeEnd,
			p.TimeInBed, p.TotalSleep, p.DeepSleep, p.LightSleep, p.RemSleep, p.AwakeTime,
			p.Efficiency, p.Latency, p.LowestHR, p.AverageHRV, p.RestlessCount,
			p.AverageHR, p.AverageBreath, p.Phases5Min, p.Phases30Sec, p.Movement30s,
			hr, hrv, now); err != nil {
			return fmt.Errorf("store: insert sleep period %s: %w", p.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// seriesJSON encodes a series for its TEXT column; nil stays NULL.
func seriesJSON(s *Series) (*string, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("store: encode series: %w", err)
	}
	str := string(b)
	return &str, nil
}

// SleepPeriodsOnDay returns the periods that count toward day, earliest
// bedtime first.
func (d *DB) SleepPeriodsOnDay(ctx context.Context, day string) ([]SleepPeriod, error) {
	return d.querySleepPeriods(ctx, "sleep periods on "+day, `
		SELECT `+sleepPeriodColumns+` FROM sleep_periods
		WHERE day = ?
		ORDER BY bedtime_start ASC
	`, day)
}

// SleepPeriodsBetween returns the periods whose day falls between from and
// to inclusive, ordered by day and then bedtime.
func (d *DB) SleepPeriodsBetween(ctx context.Context, from, to string) ([]SleepPeriod, error) {
	return d.querySleepPeriods(ctx, "sleep periods "+from+" to "+to, `
		SELECT `+sleepPeriodColumns+` FROM sleep_periods
		WHERE day BETWEEN ? AND ?
		ORDER BY day ASC, bedtime_start ASC
	`, from, to)
}

// querySleepPeriods runs a query selecting sleepPeriodColumns and collects
// the rows, decoding the JSON series columns.
func (d *DB) querySleepPeriods(ctx context.Context, what, query string, args ...any) ([]SleepPeriod, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: %s: %w", what, err)
	}
	defer rows.Close()

	var out []SleepPeriod
	for rows.Next() {
		var p SleepPeriod
		var hr, hrv *string
		if err := rows.Scan(&p.ID, &p.Day, &p.Type, &p.BedtimeStart, &p.BedtimeEnd,
			&p.TimeInBed, &p.TotalSleep, &p.DeepSleep, &p.LightSleep, &p.RemSleep, &p.AwakeTime,
			&p.Efficiency, &p.Latency, &p.LowestHR, &p.AverageHRV, &p.RestlessCount,
			&p.AverageHR, &p.AverageBreath, &p.Phases5Min, &p.Phases30Sec, &p.Movement30s,
			&hr, &hrv); err != nil {
			return nil, fmt.Errorf("store: scan sleep period: %w", err)
		}
		if p.HeartRate, err = parseSeries(hr); err != nil {
			return nil, err
		}
		if p.HRV, err = parseSeries(hrv); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %s: %w", what, err)
	}
	return out, nil
}

func parseSeries(s *string) (*Series, error) {
	if s == nil {
		return nil, nil
	}
	var out Series
	if err := json.Unmarshal([]byte(*s), &out); err != nil {
		return nil, fmt.Errorf("store: decode series: %w", err)
	}
	return &out, nil
}

// LatestSleepDay is the most recent day with a stored sleep period, or ""
// if there are none.
func (d *DB) LatestSleepDay(ctx context.Context) (string, error) {
	var day sql.NullString
	if err := d.sql.QueryRowContext(ctx,
		`SELECT MAX(day) FROM sleep_periods`).Scan(&day); err != nil {
		return "", fmt.Errorf("store: latest sleep day: %w", err)
	}
	return day.String, nil
}

// SleepDays returns up to limit days on or before through (YYYY-MM-DD)
// that have a stored sleep period, most recent first.
func (d *DB) SleepDays(ctx context.Context, through string, limit int) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT DISTINCT day FROM sleep_periods WHERE day <= ? ORDER BY day DESC LIMIT ?`, through, limit)
	if err != nil {
		return nil, fmt.Errorf("store: sleep days: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("store: scan sleep day: %w", err)
		}
		out = append(out, day)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: sleep days: %w", err)
	}
	return out, nil
}

// LastFetched is when daily samples were last written, or the zero time if
// never. The driver stores time.Time in Go's default text form, which is
// parsed back here.
func (d *DB) LastFetched(ctx context.Context) (time.Time, error) {
	var s sql.NullString
	if err := d.sql.QueryRowContext(ctx, `SELECT MAX(fetched_at) FROM samples`).Scan(&s); err != nil {
		return time.Time{}, fmt.Errorf("store: last fetched: %w", err)
	}
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(timeLayout, s.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: last fetched: parse %q: %w", s.String, err)
	}
	return t, nil
}

// Device is an e-ink display paired with this server.
type Device struct {
	MAC        string
	APIKey     string
	FriendlyID string
	Model      string
	Firmware   string
	Battery    *float64 // volts, as last reported
	RSSI       *int     // Wi-Fi signal, dBm
	Width      *int
	Height     *int
	LastScreen string
	CreatedAt  time.Time
	LastSeen   time.Time // zero until the first display request
}

const deviceColumns = `mac, api_key, friendly_id, model, firmware, battery, rssi,
	width, height, last_screen, created_at, last_seen`

// timeLayout parses times this driver wrote with Go's default formatting.
const timeLayout = "2006-01-02 15:04:05.999999999 -0700 MST"

func scanDevice(sc interface{ Scan(...any) error }) (Device, error) {
	var dv Device
	var created, seen any
	if err := sc.Scan(&dv.MAC, &dv.APIKey, &dv.FriendlyID, &dv.Model, &dv.Firmware,
		&dv.Battery, &dv.RSSI, &dv.Width, &dv.Height, &dv.LastScreen, &created, &seen); err != nil {
		return dv, err
	}
	dv.CreatedAt, dv.LastSeen = asTime(created), asTime(seen)
	return dv, nil
}

// asTime reads a timestamp column. The driver returns a TIMESTAMP-typed
// column as time.Time, but an expression over one (MAX(...)) as the text it
// stored, in Go's default time format; this accepts either, and NULL as zero.
func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		if p, err := time.Parse(timeLayout, t); err == nil {
			return p
		}
		if p, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return p
		}
	}
	return time.Time{}
}

// DeviceByMAC returns the paired device with this MAC, and whether there is one.
func (d *DB) DeviceByMAC(ctx context.Context, mac string) (Device, bool, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM trmnl_devices WHERE mac = ?`, mac)
	dv, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return dv, false, nil
	}
	if err != nil {
		return dv, false, fmt.Errorf("store: device %s: %w", mac, err)
	}
	return dv, true, nil
}

// SaveDevice inserts or replaces a device record.
func (d *DB) SaveDevice(ctx context.Context, dv Device) error {
	var seen any
	if !dv.LastSeen.IsZero() {
		seen = dv.LastSeen.UTC()
	}
	if dv.CreatedAt.IsZero() {
		dv.CreatedAt = time.Now()
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO trmnl_devices (`+deviceColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET
			api_key = excluded.api_key, friendly_id = excluded.friendly_id,
			model = excluded.model, firmware = excluded.firmware,
			battery = excluded.battery, rssi = excluded.rssi,
			width = excluded.width, height = excluded.height,
			last_screen = excluded.last_screen, last_seen = excluded.last_seen
	`, dv.MAC, dv.APIKey, dv.FriendlyID, dv.Model, dv.Firmware, dv.Battery, dv.RSSI,
		dv.Width, dv.Height, dv.LastScreen, dv.CreatedAt.UTC(), seen)
	if err != nil {
		return fmt.Errorf("store: save device %s: %w", dv.MAC, err)
	}
	return nil
}

// Devices returns every paired device, most recently seen first.
func (d *DB) Devices(ctx context.Context) ([]Device, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+deviceColumns+` FROM trmnl_devices
		ORDER BY last_seen IS NULL, last_seen DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: devices: %w", err)
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		dv, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan device: %w", err)
		}
		out = append(out, dv)
	}
	return out, rows.Err()
}

// Setting returns a stored setting's value, and whether it is set.
func (d *DB) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := d.sql.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: setting %s: %w", key, err)
	}
	return v, true, nil
}

// SetSetting stores a setting, replacing any earlier value.
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("store: set %s: %w", key, err)
	}
	return nil
}

// DeleteSetting removes a setting, so its default applies again.
func (d *DB) DeleteSetting(ctx context.Context, key string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("store: delete %s: %w", key, err)
	}
	return nil
}
