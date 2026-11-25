package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
	"github.com/cameron-wolfe/wolfe-health/internal/server"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Every command-line view names a screen the server can draw: on an empty
// database a screen may find nothing to show, but never "no screen".
func TestChartViewsAreScreens(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seen := map[string]bool{}
	for _, v := range chartViews {
		if seen[v.view] {
			t.Errorf("view %q listed twice", v.view)
		}
		seen[v.view] = true
		_, _, err := server.Render(context.Background(), db, v.screen, nil, 800, 480)
		if err != nil && strings.HasPrefix(err.Error(), "no screen") {
			t.Errorf("view %q: %v", v.view, err)
		}
	}
}

// Flags' defaults come from WH_ settings, every bad one reported at once;
// the banner's threshold is set from WH_TEMP_ALERT_MIN.
func TestEnvDefaults(t *testing.T) {
	t.Setenv("WH_FETCH_EVERY", "6h")
	t.Setenv("WH_FETCH_DAYS", "10")
	var e envDefaults
	if e.duration("WH_FETCH_EVERY", time.Hour) != 6*time.Hour || e.int("WH_FETCH_DAYS", 7) != 10 ||
		e.int("WH_UNSET_FOR_TEST", 7) != 7 || e.err() != nil {
		t.Errorf("read wrong: %v", e.err())
	}
	t.Setenv("WH_TRMNL_REFRESH", "15")
	t.Setenv("WH_BACKUP_KEEP", "lots")
	e = envDefaults{}
	e.duration("WH_TRMNL_REFRESH", time.Minute)
	e.int("WH_BACKUP_KEEP", 14)
	if err := e.err(); err == nil || !strings.Contains(err.Error(), "WH_TRMNL_REFRESH") || !strings.Contains(err.Error(), "WH_BACKUP_KEEP") {
		t.Errorf("bad settings: %v", err)
	}

	defer func() { dashboard.MinAlertDeviation = dashboard.DefaultMinAlertDeviation }()
	t.Setenv("WH_TEMP_ALERT_MIN", "0.5")
	if err := applyTempAlertMin(); err != nil || dashboard.MinAlertDeviation != 0.5 {
		t.Errorf("0.5: %v, %v", dashboard.MinAlertDeviation, err)
	}
	t.Setenv("WH_TEMP_ALERT_MIN", "hot")
	if err := applyTempAlertMin(); err == nil {
		t.Error("a bad threshold was accepted")
	}
}
