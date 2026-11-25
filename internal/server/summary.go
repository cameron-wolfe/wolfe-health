package server

import (
	"fmt"
	"net/http"

	"github.com/cameron-wolfe/wolfe-health/internal/alerts"
	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
)

// summary serves the morning's figures as a few plain sentences to be read
// aloud, for the "How did I sleep?" shortcut: "Sleep score 88, 5 above your
// weekly average. You slept 9 hours 30 minutes. Readiness 90, above your
// usual." ?date= words another day's, as on the screens. Like the screens it
// needs no token: the port is reachable only from the home network and the
// tailnet.
func (s *server) summary(w http.ResponseWriter, r *http.Request) {
	today, _, _, err := s.asOf(r.URL.Query().Get("date"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	text, err := dashboard.Summary(r.Context(), s.db, today)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintln(w, text)
}

// health answers "ok", or a line per problem with the data: no fetch from
// Oura working for a while, or the ring not syncing. It's for a check from
// outside, the "Check Wolfe Health" shortcut on a timer: a server that
// doesn't answer at all fails that request, which is the other alarm. The
// problems are the alerts' own rules and thresholds (WH_ALERT_STALE), so a
// phone without alerts set up can still watch for them.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	set := alerts.DefaultSettings()
	if s.alerts != nil {
		set = s.alerts.Settings
	}
	set.Off = map[string]bool{"summary": true, "illness": true} // the data's health only
	st := s.status.Get()
	found, err := alerts.Rules(r.Context(), s.db, alerts.FetchState{LastSuccess: st.LastSuccess, LastError: st.LastError}, s.now(), set)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if len(found) == 0 {
		fmt.Fprint(w, "ok") // exactly "ok", no newline: the shortcut compares it
		return
	}
	for i, a := range found {
		if i > 0 {
			fmt.Fprint(w, "\n")
		}
		fmt.Fprintf(w, "%s: %s", a.Title, a.Message)
	}
}
