package server

import (
	"context"
	"log"
	"net/http"
	"time"
)

// alertRow is one sent alert on the index page.
type alertRow struct{ At, Title, Message string }

// alertsData fills the index page's alerts section: where alerts go, and
// the latest sent.
func (s *server) alertsData(ctx context.Context, d *indexData) error {
	if s.alerts == nil {
		return nil
	}
	d.AlertsTo = s.alerts.Notify.Target()
	d.AlertsLimits = s.alerts.Limits()
	d.AlertsRules = s.alerts.Settings.Describe()
	sent, err := s.db.RecentAlerts(ctx, 8, s.now().Location())
	if err != nil {
		return err
	}
	for _, a := range sent {
		d.AlertsSent = append(d.AlertsSent, alertRow{a.SentAt.Format("Mon Jan 2, 3:04 PM"), a.Title, a.Message})
	}
	return nil
}

// testAlert sends a test alert to the phone at once, from the index page.
func (s *server) testAlert(w http.ResponseWriter, r *http.Request) {
	s.sendNow(w, r, "test", func(ctx context.Context) error { return s.alerts.Test(ctx) })
}

// summaryAlert sends the morning summary to the phone at once, from the
// index page, without using up the day's 8 AM one.
func (s *server) summaryAlert(w http.ResponseWriter, r *http.Request) {
	s.sendNow(w, r, "summary", func(ctx context.Context) error { return s.alerts.SendSummary(ctx) })
}

// sendNow sends an alert on demand and returns to the index page's alerts
// section, saying whether it went: ?alerts=<what>-sent or <what>-failed.
func (s *server) sendNow(w http.ResponseWriter, r *http.Request, what string, send func(context.Context) error) {
	if s.alerts == nil {
		http.Error(w, "alerts aren't set up: set WH_HA_URL, WH_HA_TOKEN and WH_HA_NOTIFY", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result := what + "-sent"
	if err := send(ctx); err != nil {
		log.Printf("alerts: %s: %v", what, err)
		result = what + "-failed"
	}
	http.Redirect(w, r, "/alerts?alerts="+result, http.StatusSeeOther)
}
