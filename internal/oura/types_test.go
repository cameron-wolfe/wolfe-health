package oura

import (
	"encoding/json"
	"testing"
)

// Contributors arrive nested inside each daily_sleep record, so the tags that
// reach them are one level deeper than every other field's and are not
// exercised by anything else. This decodes a real response body to check them,
// including a null contributor, which must stay nil rather than become 0.
func TestDailySleepDecodesContributors(t *testing.T) {
	const body = `{
		"data": [{
			"id": "5ea93a8c-6f0e-4f4a-9b1a-0e0a1a2b3c4d",
			"contributors": {
				"deep_sleep": 57,
				"efficiency": 98,
				"latency": 82,
				"rem_sleep": null,
				"restfulness": 61,
				"timing": 94,
				"total_sleep": 70
			},
			"day": "2026-08-01",
			"score": 74,
			"timestamp": "2026-08-01T00:00:00+00:00"
		}],
		"next_token": null
	}`

	var out listResponse[DailySleep]
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(out.Data) != 1 {
		t.Fatalf("decoded %d records, want 1", len(out.Data))
	}

	d := out.Data[0]
	if d.Day != "2026-08-01" || d.Score != 74 {
		t.Errorf("day/score = %q/%d, want 2026-08-01/74", d.Day, d.Score)
	}
	if d.Timestamp != "2026-08-01T00:00:00+00:00" {
		t.Errorf("Timestamp = %q", d.Timestamp)
	}
	if d.Contributors.DeepSleep == nil || *d.Contributors.DeepSleep != 57 {
		t.Errorf("DeepSleep = %v, want 57", d.Contributors.DeepSleep)
	}
	if d.Contributors.TotalSleep == nil || *d.Contributors.TotalSleep != 70 {
		t.Errorf("TotalSleep = %v, want 70", d.Contributors.TotalSleep)
	}
	if d.Contributors.RemSleep != nil {
		t.Errorf("RemSleep = %d, want nil for a null contributor", *d.Contributors.RemSleep)
	}
}

// Readiness carries its parts the same way; a null part stays nil.
func TestDailyReadinessDecodesContributors(t *testing.T) {
	const body = `{"data": [{"id": "r1", "day": "2026-11-21", "score": 90,
		"contributors": {"hrv_balance": 71, "sleep_regularity": null, "previous_night": 93},
		"temperature_deviation": -0.04}], "next_token": null}`
	var out listResponse[DailyReadiness]
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	c := out.Data[0].Contributors
	if c["hrv_balance"] == nil || *c["hrv_balance"] != 71 || c["sleep_regularity"] != nil || *c["previous_night"] != 93 {
		t.Errorf("contributors = %v", c)
	}
}
