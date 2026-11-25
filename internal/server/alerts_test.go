package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/alerts"
)

type testPhone struct{ sent []alerts.Alert }

func (p *testPhone) Send(_ context.Context, a alerts.Alert, _ string) error {
	p.sent = append(p.sent, a)
	return nil
}
func (p *testPhone) Target() string { return "Test phone" }

// Off, the index page says how to turn alerts on and Send test is refused;
// on, it names the phone, Send test reaches it, and the alert is listed.
func TestAlertsSection(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 5, true)
	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/alerts", nil))
		return rec.Body.String()
	}
	if body := get(); !strings.Contains(body, "WH_HA_TOKEN") {
		t.Error("alerts off: the page doesn't say how to turn them on")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/alerts/test", nil))
	if rec.Code != 404 {
		t.Errorf("test with alerts off: %d", rec.Code)
	}

	p := &testPhone{}
	s.alerts = alerts.NewEngine(s.db, p, "")
	s.alerts.Now = s.now
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/alerts/test", nil))
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "alerts=test-sent") || len(p.sent) != 1 {
		t.Fatalf("send test: %d %v, phone got %d", rec.Code, rec.Header(), len(p.sent))
	}
	if body := get(); !strings.Contains(body, "Sent to Test phone") || !strings.Contains(body, "Wolfe Health test") {
		t.Error("alerts on: the page doesn't name the phone or list the test")
	}

	// the morning summary on demand: sent at once, any hour, without
	// taking the day's 8 AM one
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/alerts/summary", nil))
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "alerts=summary-sent") || len(p.sent) != 2 ||
		!strings.HasPrefix(p.sent[1].Key, "summary-now:") || p.sent[1].Message == "" {
		t.Fatalf("send summary: %d %v, phone got %+v", rec.Code, rec.Header(), p.sent)
	}
}

// /health.txt says exactly "ok" while the data is fresh, and the problem
// when no fetch has worked for 13 hours, alerts set up or not.
func TestHealth(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 5, true)
	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/health.txt", nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("health: %d %v", rec.Code, rec.Header())
		}
		return rec.Body.String()
	}
	s.status.Update(func(st *Status) { st.LastSuccess = s.now().Add(-time.Hour) })
	if got := get(); got != "ok" {
		t.Errorf("fresh: %q", got)
	}
	s.status.Update(func(st *Status) {
		st.LastSuccess, st.LastError = s.now().Add(-20*time.Hour), "refresh token (if this persists, run `wolfe-health auth` again): 400"
	})
	if got := get(); !strings.HasPrefix(got, "Wolfe Health isn't getting data: ") || !strings.Contains(got, "make auth") {
		t.Errorf("stale: %q", got)
	}
}
