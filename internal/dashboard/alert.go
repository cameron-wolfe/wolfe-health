package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// MinAlertDeviation is how far above baseline a night's temperature must be,
// in °C, before it can raise the warning — on top of being above the normal
// range. A very steady history gives a tight range, and without this a
// +0.2 °C wobble would be flagged. The daemon sets it from
// WH_TEMP_ALERT_MIN at start, before anything draws.
var MinAlertDeviation = DefaultMinAlertDeviation

// DefaultMinAlertDeviation is MinAlertDeviation with nothing set.
const DefaultMinAlertDeviation = 0.3

// Alert words an early warning from last night's body signals, or returns
// "" when there is nothing worth showing — which is most of the time.
//
// It speaks up when last night's temperature was above the user's normal
// range (the body-signals range, built from their previous 30 nights) and at
// least MinAlertDeviation above baseline. It says for how many consecutive
// nights, and whether lowest heart rate and HRV agree that night, since a
// raised temperature with a raised resting heart rate and lowered HRV is the
// pattern that often comes before feeling ill. It describes; it doesn't
// diagnose. Only a fresh reading counts: if the latest temperature is more
// than a day old, the warning stays hidden rather than lingering.
func Alert(ctx context.Context, db *store.DB, today time.Time) (string, error) {
	text, _, err := AlertStatus(ctx, db, today)
	return text, err
}

// PreviewAlert is a sample banner, for seeing what the warning looks like
// before it ever has reason to appear. Marked as a preview so it can't be
// mistaken for a real one.
const PreviewAlert = "Preview: Temperature +0.8 °C, above your normal range for 2 nights · lowest heart rate also high"

// AlertStatus is Alert plus, when the banner is hidden, why: so the index
// page can show the warning is watching even while there's nothing to say.
func AlertStatus(ctx context.Context, db *store.DB, today time.Time) (text, why string, err error) {
	to := date(today)
	from := to.AddDate(0, 0, -(baselineDays + 7))
	hrv, hr, temp, err := loadSignals(ctx, db, from, to, baselineDays+8)
	if err != nil {
		return "", "", err
	}

	// the latest night with a temperature, if it's last night or the one before
	var last time.Time
	for d := to; !d.Before(to.AddDate(0, 0, -1)); d = d.AddDate(0, 0, -1) {
		if _, ok := temp[d]; ok {
			last = d
			break
		}
	}
	if last.IsZero() {
		return "", "no temperature reading from the last two nights yet", nil
	}
	elevated := func(d time.Time) bool {
		v, ok := temp[d]
		if !ok || v < MinAlertDeviation {
			return false
		}
		band, ok := baseline(temp, d, tempSignal.sdFloor)
		return ok && v > band.Hi
	}
	if !elevated(last) {
		// two decimals here, so a near miss shows how near: +0.29 against
		// +0.30, where one decimal would read "+0.3, under +0.3"
		v := temp[last]
		band, ok := baseline(temp, last, tempSignal.sdFloor)
		switch {
		case !ok:
			return "", fmt.Sprintf("building your normal range (needs %d nights of temperature)", minBaseline), nil
		case v > band.Hi:
			return "", fmt.Sprintf("%s %+.2f °C: above your normal range, but under the +%.2f °C threshold",
				nightName(last, to), v, MinAlertDeviation), nil
		}
		return "", fmt.Sprintf("%s %+.2f °C, within your normal range (%+.2f to %+.2f)",
			nightName(last, to), v, band.Lo, band.Hi), nil
	}
	nights := 0
	for d := last; elevated(d); d = d.AddDate(0, 0, -1) {
		nights++
	}

	parts := []string{fmt.Sprintf("Temperature %+.1f °C, above your normal range", temp[last])}
	if nights > 1 {
		parts[0] += fmt.Sprintf(" for %d nights", nights)
	} else {
		parts[0] += " last night"
	}
	if v, ok := hr[last]; ok {
		if band, ok := baseline(hr, last, hrSignal.sdFloor); ok && v > band.Hi {
			parts = append(parts, "lowest heart rate also high")
		}
	}
	if v, ok := hrv[last]; ok {
		if band, ok := baseline(hrv, last, hrvSignal.sdFloor); ok && v < band.Lo {
			parts = append(parts, "HRV below normal")
		}
	}
	return strings.Join(parts, " · "), "", nil
}

// nightName is "last night" for today's reading, else "the night before".
func nightName(d, today time.Time) string {
	if d.Equal(today) {
		return "last night"
	}
	return "the night before"
}
