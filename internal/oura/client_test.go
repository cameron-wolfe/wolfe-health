package oura

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Three pages chained by next_token must come back as one list, in order,
// with the date range resent alongside every token.
func TestFetchAllFollowsNextToken(t *testing.T) {
	pages := map[string]string{
		"":   `{"data":[{"day":"2026-01-01","score":70},{"day":"2026-01-02","score":71}],"next_token":"p2"}`,
		"p2": `{"data":[{"day":"2026-01-03","score":72}],"next_token":"p3"}`,
		"p3": `{"data":[{"day":"2026-01-04","score":73}],"next_token":null}`,
	}
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if q.Get("start_date") != "2026-01-01" || q.Get("end_date") != "2026-01-04" {
			t.Errorf("request %d lost the date range: %s", requests, r.URL.RawQuery)
		}
		body, ok := pages[q.Get("next_token")]
		if !ok {
			http.Error(w, "unknown token", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := New(StaticToken("t"))
	c.baseURL = srv.URL + "/"
	days, err := c.Sleep(context.Background(), "2026-01-01", "2026-01-04")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Errorf("requests = %d, want 3", requests)
	}
	if len(days) != 4 {
		t.Fatalf("got %d days, want 4", len(days))
	}
	for i, d := range days {
		if want := fmt.Sprintf("2026-01-0%d", i+1); d.Day != want || d.Score != 70+i {
			t.Errorf("days[%d] = %s/%d, want %s/%d", i, d.Day, d.Score, want, 70+i)
		}
	}
}

// A server that keeps returning the same token would loop forever.
func TestFetchAllStopsOnRepeatedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[],"next_token":"stuck"}`)
	}))
	defer srv.Close()

	c := New(StaticToken("t"))
	c.baseURL = srv.URL + "/"
	if _, err := c.Sleep(context.Background(), "2026-01-01", "2026-01-02"); err == nil {
		t.Fatal("expected an error for a next_token that never advances")
	}
}
