package store

import (
	"context"
	"fmt"
	"time"
)

// SentAlert is a phone alert that went out.
type SentAlert struct {
	Key, Title, Message string
	SentAt              time.Time
}

// AlertSent says whether the alert with key has gone out.
func (d *DB) AlertSent(ctx context.Context, key string) (bool, error) {
	var n int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts_sent WHERE key = ?`, key).Scan(&n); err != nil {
		return false, fmt.Errorf("store: alert %s: %w", key, err)
	}
	return n > 0, nil
}

// RecordAlert notes that an alert went out. A key already recorded keeps
// its first sending.
func (d *DB) RecordAlert(ctx context.Context, a SentAlert) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO alerts_sent (key, title, message, sent_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO NOTHING
	`, a.Key, a.Title, a.Message, a.SentAt.Unix())
	if err != nil {
		return fmt.Errorf("store: record alert %s: %w", a.Key, err)
	}
	return nil
}

// AlertsSince counts the alerts sent at or after t, leaving out those
// whose keys start with any of exclude.
func (d *DB) AlertsSince(ctx context.Context, t time.Time, exclude ...string) (int, error) {
	q := `SELECT COUNT(*) FROM alerts_sent WHERE sent_at >= ?`
	args := []any{t.Unix()}
	for _, p := range exclude {
		q += ` AND substr(key, 1, ?) != ?`
		args = append(args, len(p), p)
	}
	var n int
	if err := d.sql.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count alerts: %w", err)
	}
	return n, nil
}

// RecentAlerts is up to n of the latest alerts sent, newest first, in loc.
func (d *DB) RecentAlerts(ctx context.Context, n int, loc *time.Location) ([]SentAlert, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT key, title, message, sent_at FROM alerts_sent ORDER BY sent_at DESC, key LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("store: recent alerts: %w", err)
	}
	defer rows.Close()
	var out []SentAlert
	for rows.Next() {
		var a SentAlert
		var at int64
		if err := rows.Scan(&a.Key, &a.Title, &a.Message, &at); err != nil {
			return nil, fmt.Errorf("store: recent alerts: %w", err)
		}
		a.SentAt = time.Unix(at, 0).In(loc)
		out = append(out, a)
	}
	return out, rows.Err()
}
