package oura

import (
	"context"
	"fmt"
	"time"
)

// SleepContributors is the breakdown behind a daily sleep score.
//
// It arrives as a nested object rather than a list, so it decodes into its own
// struct and not a slice. Each contributor is itself a 1-100 sub-score, and
// Oura reports one as null on a night it lacked the data to compute it — a nap
// too short to score for REM, say. The pointers keep that distinguishable, for
// the same reason as in DailyStress: a plain int would decode null as a real
// zero, which reads as the worst possible sub-score rather than as "unknown".
type SleepContributors struct {
	DeepSleep   *int `json:"deep_sleep"`
	Efficiency  *int `json:"efficiency"`
	Latency     *int `json:"latency"`
	RemSleep    *int `json:"rem_sleep"`
	Restfulness *int `json:"restfulness"`
	Timing      *int `json:"timing"`
	TotalSleep  *int `json:"total_sleep"`
}

// DailySleep is one night's sleep score from the daily_sleep collection.
type DailySleep struct {
	ID           string            `json:"id"`
	Day          string            `json:"day"`
	Score        int               `json:"score"`
	Timestamp    string            `json:"timestamp"`
	Contributors SleepContributors `json:"contributors"`
}

// DailyActivity is one day's activity summary from the daily_activity collection.
type DailyActivity struct {
	ID          string `json:"id"`
	Day         string `json:"day"`
	Score       int    `json:"score"`
	Steps       int    `json:"steps"`
	ActiveCal   int    `json:"active_calories"`
	TargetCal   int    `json:"target_calories"`  // the day's goal, set by Oura from readiness
	MetersToGo  int    `json:"meters_to_target"` // walking distance still to the goal; 0 once met
	TotalCal    int    `json:"total_calories"`
	SedentaryHr int    `json:"sedentary_time"`
}

// DailyReadiness is one day's readiness summary from the daily_readiness collection.
type DailyReadiness struct {
	ID    string `json:"id"`
	Day   string `json:"day"`
	Score int    `json:"score"`
	// Body temperature against the user's baseline, in °C. Null on days the
	// ring couldn't measure, hence pointers: zero would read as "exactly
	// normal" rather than "unknown".
	TemperatureDev *float64 `json:"temperature_deviation"`
	TempTrendDev   *float64 `json:"temperature_trend_deviation"`
	// The parts behind the score, each 1-100, null when Oura couldn't
	// compute one, as with sleep's.
	Contributors map[string]*int `json:"contributors"`
}

// DailyStress is one day's stress summary from the daily_stress collection.
//
// Unlike the other daily collections there is no score. Oura reports seconds
// spent in the high-stress and high-recovery zones, and only id and day are
// guaranteed — the rest are null on days it could not compute. The pointers
// keep that distinguishable: a plain int would decode null as a real zero.
type DailyStress struct {
	ID           string `json:"id"`
	Day          string `json:"day"`
	StressHigh   *int   `json:"stress_high"`
	RecoveryHigh *int   `json:"recovery_high"`
	DaySummary   string `json:"day_summary"`
}

// Sample is a regularly spaced series, as the sleep endpoint reports heart
// rate and HRV: Items[i] was recorded Interval*i seconds after Timestamp.
// An item is null where the ring had no reading, hence the pointers.
type Sample struct {
	Interval  float64    `json:"interval"`
	Items     []*float64 `json:"items"`
	Timestamp string     `json:"timestamp"`
}

// SleepPeriod is one continuous stretch of sleep from the sleep collection —
// a night, a nap, or a fragment of a broken night. Several may share a day.
//
// The phase strings hold one character per epoch from BedtimeStart:
// '1' deep, '2' light, '3' REM, '4' awake. Movement30s is likewise one per
// 30 seconds: '1' still, '2' restless, '3' tossing and turning, '4' active.
type SleepPeriod struct {
	ID           string `json:"id"`
	Day          string `json:"day"`  // the day it counts toward: the wake-up day
	Type         string `json:"type"` // long_sleep, sleep, late_nap, rest, deleted
	BedtimeStart string `json:"bedtime_start"`
	BedtimeEnd   string `json:"bedtime_end"`

	TimeInBed     int  `json:"time_in_bed"` // seconds
	TotalSleep    *int `json:"total_sleep_duration"`
	DeepSleep     *int `json:"deep_sleep_duration"`
	LightSleep    *int `json:"light_sleep_duration"`
	RemSleep      *int `json:"rem_sleep_duration"`
	AwakeTime     *int `json:"awake_time"`
	Efficiency    *int `json:"efficiency"`
	Latency       *int `json:"latency"` // seconds to fall asleep
	LowestHR      *int `json:"lowest_heart_rate"`
	AverageHRV    *int `json:"average_hrv"`
	RestlessCount *int `json:"restless_periods"`

	AverageHR     *float64 `json:"average_heart_rate"`
	AverageBreath *float64 `json:"average_breath"`

	Phases5Min  *string `json:"sleep_phase_5_min"`
	Phases30Sec *string `json:"sleep_phase_30_sec"`
	Movement30s *string `json:"movement_30_sec"`

	HeartRate *Sample `json:"heart_rate"`
	HRV       *Sample `json:"hrv"`
}

// listResponse is the envelope Oura wraps every collection response in.
// NextToken is null on the last page, which decodes to "".
type listResponse[T any] struct {
	Data      []T    `json:"data"`
	NextToken string `json:"next_token"`
}

// Sleep returns daily sleep scores over the inclusive date range.
func (c *Client) Sleep(ctx context.Context, start string, end string) ([]DailySleep, error) {
	return fetchAll[DailySleep](ctx, c, "daily_sleep", start, end)
}

// Activity returns daily activity summaries over the inclusive date range.
func (c *Client) Activity(ctx context.Context, start string, end string) ([]DailyActivity, error) {
	return fetchAll[DailyActivity](ctx, c, "daily_activity", start, end)
}

// Stress returns daily stress summaries over the inclusive date range.
func (c *Client) Stress(ctx context.Context, start string, end string) ([]DailyStress, error) {
	return fetchAll[DailyStress](ctx, c, "daily_stress", start, end)
}

// Readiness returns daily readiness summaries over the inclusive date range.
func (c *Client) Readiness(ctx context.Context, start string, end string) ([]DailyReadiness, error) {
	return fetchAll[DailyReadiness](ctx, c, "daily_readiness", start, end)
}

// SleepPeriods returns every sleep period whose day falls in the range.
//
// Unlike the daily collections, this endpoint treats end_date as exclusive,
// so the end is pushed a day later here to keep the range inclusive like the
// rest of the client.
func (c *Client) SleepPeriods(ctx context.Context, start string, end string) ([]SleepPeriod, error) {
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return nil, fmt.Errorf("sleep periods: bad end date %q: %w", end, err)
	}
	return fetchAll[SleepPeriod](ctx, c, "sleep", start, FormatDate(e.AddDate(0, 0, 1)))
}
