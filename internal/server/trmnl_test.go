package server

import (
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

func trmnlServer(t *testing.T, cfg TRMNLConfig, hour int, withSleep bool) (*server, http.Handler) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	db.Save(ctx, []store.Sample{{Metric: store.MetricReadiness, Day: "2026-11-19", Value: 67}})
	if withSleep {
		phases := "4422211113332224"
		db.SaveSleepPeriods(ctx, []store.SleepPeriod{{ID: "p", Day: "2026-11-19", Type: "long_sleep",
			BedtimeStart: "2026-11-18T23:00:00-08:00", BedtimeEnd: "2026-11-18T23:08:00-08:00",
			TimeInBed: 480, Phases30Sec: &phases}})
	}
	s := newServer(db, &Tracker{}, Config{TRMNL: cfg})
	s.now = func() time.Time { return time.Date(2026, 11, 19, hour, 0, 0, 0, time.Local) }
	return s, s.routes()
}

func call(t *testing.T, h http.Handler, method, path string, hdr map[string]string, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, "http://192.168.1.20:8090"+path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: %d %q", method, path, rec.Code, rec.Body.String())
	}
	return out
}

const mac = "AA:BB:CC:DD:EE:FF"

// Pairing hands out a key, the same one if asked again.
func TestSetupPairs(t *testing.T) {
	_, h := trmnlServer(t, TRMNLConfig{}, 12, false)
	a := call(t, h, "GET", "/api/setup", map[string]string{"ID": mac, "Model": "og"}, "")
	if a["status"] != 200.0 || a["api_key"] == "" || len(a["friendly_id"].(string)) != 6 {
		t.Fatalf("setup = %v", a)
	}
	if !strings.HasPrefix(a["image_url"].(string), "http://192.168.1.20:8090/trmnl/setup.png") {
		t.Errorf("image_url %v should use the address the device reached", a["image_url"])
	}
	b := call(t, h, "GET", "/api/setup", map[string]string{"ID": mac}, "")
	if b["api_key"] != a["api_key"] {
		t.Error("a second setup should return the same key")
	}
	if bad := call(t, h, "GET", "/api/setup", nil, ""); bad["status"] == 200.0 {
		t.Error("setup without ID should fail")
	}
}

var filenameRE = regexp.MustCompile(`^daily-[0-9a-f]{6}-\d{10}$`)

// A wake gets a cache-correct filename, a numeric refresh, a downloadable
// 4-gray PNG under the size limit, and the same filename while the image
// is unchanged. reset_firmware is never true.
func TestDisplay(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 5, true) // the default's daily brief
	key := call(t, h, "GET", "/api/setup", map[string]string{"ID": mac}, "")["api_key"].(string)
	hdr := map[string]string{"ID": mac, "Access-Token": key, "Battery-Voltage": "4.05", "RSSI": "-58",
		"Width": "800", "Height": "480"}
	d := call(t, h, "GET", "/api/display", hdr, "")
	name, _ := d["filename"].(string)
	if d["status"] != 0.0 || !filenameRE.MatchString(name) || len(name) > 31 {
		t.Fatalf("display = %v", d)
	}
	if d["refresh_rate"] != 900.0 || d["reset_firmware"] != false || d["special_function"] != "none" {
		t.Errorf("display = %v", d)
	}
	img := httptest.NewRecorder()
	h.ServeHTTP(img, httptest.NewRequest("GET", strings.TrimPrefix(d["image_url"].(string), "http://192.168.1.20:8090"), nil))
	if img.Code != 200 || img.Header().Get("Content-Type") != "image/png" || img.Header().Get("Content-Length") == "" {
		t.Fatalf("image: %d %v", img.Code, img.Header())
	}
	if img.Body.Len() > maxImageBytes {
		t.Errorf("image is %d bytes, over the device's limit", img.Body.Len())
	}
	cfg, err := png.DecodeConfig(img.Body)
	if err != nil || cfg.Width != 800 || cfg.Height != 480 {
		t.Errorf("image config %+v, %v", cfg, err)
	}
	// the next wake comes about on time (one well early is a button press),
	// still in the same slot of the schedule
	start := s.now()
	s.now = func() time.Time { return start.Add(14 * time.Minute) }
	if again := call(t, h, "GET", "/api/display", hdr, ""); again["filename"] != name {
		t.Errorf("unchanged image got a new filename: %v then %v", name, again["filename"])
	}
}

// After the brief, the morning shows last night as WolfePhase; with no
// night stored it falls back to daily.
func TestScheduledScreen(t *testing.T) {
	quarterPast := func() time.Time { return time.Date(2026, 11, 19, 5, 15, 0, 0, time.Local) }
	s, h := trmnlServer(t, TRMNLConfig{}, 5, true)
	s.now = quarterPast
	if d := call(t, h, "GET", "/api/display", map[string]string{"ID": mac, "Access-Token": "k"}, ""); !strings.HasPrefix(d["filename"].(string), "wolfepha") {
		t.Errorf("5:15 AM showed %v, want WolfePhase", d["filename"])
	}
	s2, h2 := trmnlServer(t, TRMNLConfig{}, 5, false)
	s2.now = quarterPast
	if d := call(t, h2, "GET", "/api/display", map[string]string{"ID": mac, "Access-Token": "k"}, ""); !strings.HasPrefix(d["filename"].(string), "daily-") {
		t.Errorf("no sleep stored: showed %v, want daily fallback", d["filename"])
	}
}

// An unknown device waits; one with a key is adopted, never reset.
func TestDisplayUnknownDevice(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, false)
	if d := call(t, h, "GET", "/api/display", map[string]string{"ID": mac}, ""); d["status"] != 202.0 {
		t.Errorf("unpaired, no key: %v, want 202", d)
	}
	d := call(t, h, "GET", "/api/display", map[string]string{"ID": mac, "Access-Token": "old-key"}, "")
	if d["status"] != 0.0 || d["reset_firmware"] != false {
		t.Errorf("unknown key: %v", d)
	}
	dv, ok, _ := s.db.DeviceByMAC(context.Background(), mac)
	if !ok || dv.APIKey != "old-key" {
		t.Errorf("device not adopted: %+v", dv)
	}
}

func TestDisplayBMP(t *testing.T) {
	_, h := trmnlServer(t, TRMNLConfig{Format: "bmp"}, 14, false)
	d := call(t, h, "GET", "/api/display", map[string]string{"ID": mac, "Access-Token": "k"}, "")
	u := d["image_url"].(string)
	if !strings.HasSuffix(u, ".bmp") {
		t.Fatalf("image_url %v", u)
	}
	img := httptest.NewRecorder()
	h.ServeHTTP(img, httptest.NewRequest("GET", strings.TrimPrefix(u, "http://192.168.1.20:8090"), nil))
	if img.Header().Get("Content-Type") != "image/bmp" || img.Body.Len() != 48062 {
		t.Errorf("bmp: %v, %d bytes", img.Header().Get("Content-Type"), img.Body.Len())
	}
}

func TestLogIsRecorded(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, false)
	r := call(t, h, "POST", "/api/log", map[string]string{"ID": mac}, `{"logs":[{"message":"decode failed"},{"other":1}]}`)
	if r["status"] != 200.0 {
		t.Errorf("log = %v", r)
	}
	logs := s.trmnl.recentLogs(5)
	if len(logs) != 2 || logs[0].Message != "decode failed" || logs[1].Message != `{"other":1}` {
		t.Errorf("logs = %+v", logs)
	}
}
