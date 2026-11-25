package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Quiet, the log leaves out successful reads but keeps changes, errors
// and the TRMNL API; verbose keeps everything.
func TestLogRequests(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.png" {
			http.NotFound(w, r)
		}
	})
	do := func(verbose bool, method, path string) bool {
		buf.Reset()
		logRequests(h, verbose).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
		return strings.Contains(buf.String(), "http: "+method+" "+path)
	}
	for _, c := range []struct {
		method, path string
		quiet        bool
	}{
		{"GET", "/current.png", false},
		{"GET", "/emulator/now", false},
		{"GET", "/", false},
		{"GET", "/missing.png", true},
		{"GET", "/api/display", true},
		{"POST", "/alerts/test", true},
	} {
		if got := do(false, c.method, c.path); got != c.quiet {
			t.Errorf("quiet: %s %s logged %v, want %v", c.method, c.path, got, c.quiet)
		}
		if !do(true, c.method, c.path) {
			t.Errorf("verbose: %s %s not logged", c.method, c.path)
		}
	}
}
