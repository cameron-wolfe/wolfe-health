package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The emulator page asks for the current screen as the TRMNL gets it, and
// its caption endpoint names the screen up now.
func TestEmulator(t *testing.T) {
	h := testServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/emulator", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "current.png?bits=2") {
		t.Fatalf("emulator page: %d, missing the 4-gray current image", rec.Code)
	}
	for _, u := range []string{"/emulator/icon.png", "/emulator/manifest.webmanifest"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", u, nil))
		if r.Code != 200 || r.Body.Len() == 0 {
			t.Errorf("%s: %d", u, r.Code)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/emulator/now", nil))
	var now emulatorNowJSON
	if err := json.NewDecoder(rec.Body).Decode(&now); err != nil || now.Screen == "" || now.Until == "" || now.Slot == "" {
		t.Errorf("emulator/now = %v, %v", now, err)
	}
	// Next screen: a step ahead names another screen in the same slot,
	// and current.png draws it
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/emulator/now?advance=1", nil))
	var ahead emulatorNowJSON
	json.NewDecoder(rec.Body).Decode(&ahead)
	if ahead.Screen == now.Screen || ahead.Slot != now.Slot || ahead.Scheduled != now.Screen {
		t.Errorf("advance=1: %v, after %v", ahead, now)
	}
	for _, u := range []string{"/current.png?advance=1", "/current.png?advance=0"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", u, nil))
		if r.Code != 200 {
			t.Errorf("%s: %d", u, r.Code)
		}
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/current.png?advance=-1", nil))
	if r.Code != 400 {
		t.Errorf("advance=-1: %d, want 400", r.Code)
	}
}

// The first daily layout stays available as the daily digest, beside the
// brief.
func TestDailyDigest(t *testing.T) {
	h := testServer(t)
	for _, u := range []string{"/daily.png", "/daily-digest.png"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d %s", u, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if screenTitle(scrDigest) != "Daily digest" {
		t.Errorf("digest isn't in the schedule menu")
	}
}

// emulatorNowJSON is /emulator/now's answer.
type emulatorNowJSON struct {
	Screen, Scheduled, Until, Slot, Showing string
	Name, Home, HomeTitle                   string
	Details                                 []struct{ Screen, Title string }
}
