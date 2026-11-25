package oura

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The time-series collections take a datetime range, not dates; nulls
// decode as nil, not zero.
func TestTimeSeriesAndNulls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/heartrate":
			if q.Get("start_datetime") != "2026-11-19T00:00:00Z" || q.Get("end_datetime") != "2026-11-20T00:00:00Z" || q.Get("start_date") != "" {
				t.Errorf("heartrate query %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"data":[{"timestamp":"2026-11-19T08:00:00+00:00","bpm":61,"source":"awake"}],"next_token":null}`)
		case "/daily_spo2":
			fmt.Fprint(w, `{"data":[{"id":"a","day":"2026-11-19","spo2_percentage":{"average":96.4},"breathing_disturbance_index":3},
				{"id":"b","day":"2026-11-18","spo2_percentage":null,"breathing_disturbance_index":null}],"next_token":null}`)
		case "/personal_info":
			fmt.Fprint(w, `{"id":"p","age":40,"weight":null,"height":1.8,"biological_sex":"male","email":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(StaticToken("t")).WithBaseURL(srv.URL + "/")
	ctx := context.Background()

	day := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	hr, err := c.HeartRates(ctx, day, day.AddDate(0, 0, 1))
	if err != nil || len(hr) != 1 || hr[0].BPM != 61 || hr[0].Source != "awake" {
		t.Errorf("heart rate %+v, %v", hr, err)
	}
	sp, err := c.SpO2(ctx, "2026-11-18", "2026-11-19")
	if err != nil || len(sp) != 2 {
		t.Fatalf("spo2 %+v, %v", sp, err)
	}
	if *sp[0].SpO2.Average != 96.4 || *sp[0].BreathingDisturbance != 3 || sp[1].SpO2 != nil || sp[1].BreathingDisturbance != nil {
		t.Errorf("spo2 decoded wrong: %+v / %+v", sp[0], sp[1])
	}
	p, err := c.Personal(ctx)
	if err != nil || *p.Age != 40 || p.Weight != nil || p.Email != nil {
		t.Errorf("personal %+v, %v", p, err)
	}
}
