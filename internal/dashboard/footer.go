package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// lowBattery is the charge, in percent, below which the footer asks for a
// charge.
const lowBattery = 20

// Footer builds the status line every screen carries: the ring's battery
// and when it last synced, and when the data was last updated from Oura.
// Times are clock times, not "12m ago": a screen can stay up for an hour,
// and a relative time would go stale on the wall.
//
// The ring's last sync is its newest reading, battery or heart rate; Oura
// only has the ring's data once the phone app has synced it.
func Footer(ctx context.Context, db *store.DB, now time.Time) (chart.Footer, error) {
	var f chart.Footer
	at := func(t time.Time) string {
		t = t.In(now.Location())
		if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
			return t.Format("3:04 PM")
		}
		return t.Format("Jan 2, 3:04 PM")
	}

	b, hasBattery, err := db.LatestBattery(ctx)
	if err != nil {
		return f, err
	}
	synced, hasHR, err := db.LatestHeartRateTime(ctx)
	if err != nil {
		return f, err
	}
	if hasBattery && b.Time.After(synced) {
		synced, hasHR = b.Time, true
	}
	if hasBattery {
		charging := b.Charging != nil && *b.Charging
		switch {
		case charging:
			f.Left = fmt.Sprintf("ring %d%%, charging", b.Level)
		case b.Level < lowBattery:
			f.Left, f.Warn = fmt.Sprintf("ring %d%%: charge soon", b.Level), true
		default:
			f.Left = fmt.Sprintf("ring %d%%", b.Level)
		}
	}
	if hasHR {
		if f.Left != "" {
			f.Left += " · "
		}
		f.Left += "synced " + at(synced)
	}

	if t, err := db.LastFetched(ctx); err != nil {
		return f, err
	} else if !t.IsZero() {
		f.Right = "updated " + at(t)
	}
	return f, nil
}
