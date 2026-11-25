package oura

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

// The collections below arrived with the wider set of scopes (spo2,
// heartrate, workout, tag, session, ring_configuration, heart_health,
// personal, email). Each type keeps Oura's field names in its JSON tags and
// uses pointers wherever Oura may send null.

// DailySpO2 is one night's blood-oxygen summary from daily_spo2.
type DailySpO2 struct {
	ID   string `json:"id"`
	Day  string `json:"day"`
	SpO2 *struct {
		Average *float64 `json:"average"`
	} `json:"spo2_percentage"`
	// BreathingDisturbance counts breathing disturbances during sleep; Oura
	// reports it as the breathing disturbance index.
	BreathingDisturbance *int `json:"breathing_disturbance_index"`
}

// CardiovascularAge is one day's estimate from daily_cardiovascular_age.
type CardiovascularAge struct {
	ID                string   `json:"id"`
	Day               string   `json:"day"`
	VascularAge       *int     `json:"vascular_age"`
	PulseWaveVelocity *float64 `json:"pulse_wave_velocity"` // m/s
}

// Resilience is one day's long-term resilience from daily_resilience.
type Resilience struct {
	ID           string `json:"id"`
	Day          string `json:"day"`
	Level        string `json:"level"` // limited, adequate, solid, strong, exceptional
	Contributors struct {
		SleepRecovery   float64 `json:"sleep_recovery"`
		DaytimeRecovery float64 `json:"daytime_recovery"`
		Stress          float64 `json:"stress"`
	} `json:"contributors"`
}

// VO2Max is one estimate of cardiorespiratory fitness from vO2_max.
type VO2Max struct {
	ID        string  `json:"id"`
	Day       string  `json:"day"`
	Timestamp string  `json:"timestamp"`
	VO2Max    float64 `json:"vo2_max"` // ml/kg/min
}

// SleepTime is Oura's bedtime guidance for a day, from sleep_time. The
// window's offsets are seconds from midnight, in the day_tz offset.
type SleepTime struct {
	ID             string `json:"id"`
	Day            string `json:"day"`
	OptimalBedtime *struct {
		DayTZ       int `json:"day_tz"`
		StartOffset int `json:"start_offset"`
		EndOffset   int `json:"end_offset"`
	} `json:"optimal_bedtime"`
	Recommendation *string `json:"recommendation"`
	Status         *string `json:"status"`
}

// Workout is one workout from workout, logged or detected.
type Workout struct {
	ID        string   `json:"id"`
	Day       string   `json:"day"`
	Activity  string   `json:"activity"`  // walking, cycling, …
	Intensity string   `json:"intensity"` // easy, moderate, hard
	Source    string   `json:"source"`    // manual, autodetected, confirmed, …
	Label     *string  `json:"label"`
	Start     string   `json:"start_datetime"`
	End       string   `json:"end_datetime"`
	Calories  *float64 `json:"calories"`
	Distance  *float64 `json:"distance"` // meters
}

// Session is one guided or unguided session from session: breathing,
// meditation, a nap, rest.
type Session struct {
	ID        string  `json:"id"`
	Day       string  `json:"day"`
	Type      string  `json:"type"`
	Mood      *string `json:"mood"`
	Start     string  `json:"start_datetime"`
	End       string  `json:"end_datetime"`
	HeartRate *Sample `json:"heart_rate"`
	HRV       *Sample `json:"heart_rate_variability"`
}

// Tag is one tag from enhanced_tag: a moment or span the user marked, with
// Oura's tag code or a custom name.
type Tag struct {
	ID         string  `json:"id"`
	TypeCode   *string `json:"tag_type_code"`
	CustomName *string `json:"custom_name"`
	Comment    *string `json:"comment"`
	StartDay   string  `json:"start_day"`
	EndDay     *string `json:"end_day"`
	StartTime  string  `json:"start_time"`
	EndTime    *string `json:"end_time"`
}

// RestModePeriod is one stretch of rest mode, from rest_mode_period.
type RestModePeriod struct {
	ID        string          `json:"id"`
	StartDay  string          `json:"start_day"`
	EndDay    *string         `json:"end_day"`
	StartTime *string         `json:"start_time"`
	EndTime   *string         `json:"end_time"`
	Episodes  json.RawMessage `json:"episodes"` // kept as Oura sent it
}

// HeartRate is one heart-rate reading from heartrate, across the whole day.
type HeartRate struct {
	Timestamp string `json:"timestamp"` // UTC
	BPM       int    `json:"bpm"`
	Source    string `json:"source"` // awake, rest, sleep, workout, live, session
}

// BatteryLevel is one ring battery reading from ring_battery_level.
type BatteryLevel struct {
	Timestamp string `json:"timestamp"` // UTC
	Level     int    `json:"level"`     // percent
	Charging  *bool  `json:"charging"`
	InCharger *bool  `json:"in_charger"`
}

// RingConfiguration is one ring the account has set up.
type RingConfiguration struct {
	ID           string  `json:"id"`
	Color        *string `json:"color"`
	Design       *string `json:"design"`
	Firmware     *string `json:"firmware_version"`
	HardwareType *string `json:"hardware_type"` // gen3, gen4, …
	SetUpAt      *string `json:"set_up_at"`
	Size         *int    `json:"size"`
}

// PersonalInfo is the account's profile, from personal_info: one document,
// not a collection.
type PersonalInfo struct {
	ID            string   `json:"id"`
	Age           *int     `json:"age"`
	Weight        *float64 `json:"weight"` // kg
	Height        *float64 `json:"height"` // m
	BiologicalSex *string  `json:"biological_sex"`
	Email         *string  `json:"email"`
}

// SpO2 returns nightly blood-oxygen summaries over the inclusive date range.
func (c *Client) SpO2(ctx context.Context, start, end string) ([]DailySpO2, error) {
	return fetchAll[DailySpO2](ctx, c, "daily_spo2", start, end)
}

// CardiovascularAge returns daily cardiovascular-age estimates.
func (c *Client) CardiovascularAge(ctx context.Context, start, end string) ([]CardiovascularAge, error) {
	return fetchAll[CardiovascularAge](ctx, c, "daily_cardiovascular_age", start, end)
}

// Resilience returns daily resilience levels.
func (c *Client) Resilience(ctx context.Context, start, end string) ([]Resilience, error) {
	return fetchAll[Resilience](ctx, c, "daily_resilience", start, end)
}

// VO2Max returns VO2 max estimates.
func (c *Client) VO2Max(ctx context.Context, start, end string) ([]VO2Max, error) {
	return fetchAll[VO2Max](ctx, c, "vO2_max", start, end)
}

// SleepTimes returns Oura's daily bedtime guidance.
func (c *Client) SleepTimes(ctx context.Context, start, end string) ([]SleepTime, error) {
	return fetchAll[SleepTime](ctx, c, "sleep_time", start, end)
}

// Workouts returns workouts over the inclusive date range.
func (c *Client) Workouts(ctx context.Context, start, end string) ([]Workout, error) {
	return fetchAll[Workout](ctx, c, "workout", start, end)
}

// Sessions returns sessions over the inclusive date range.
func (c *Client) Sessions(ctx context.Context, start, end string) ([]Session, error) {
	return fetchAll[Session](ctx, c, "session", start, end)
}

// Tags returns tags over the inclusive date range.
func (c *Client) Tags(ctx context.Context, start, end string) ([]Tag, error) {
	return fetchAll[Tag](ctx, c, "enhanced_tag", start, end)
}

// RestModePeriods returns rest-mode periods over the inclusive date range.
func (c *Client) RestModePeriods(ctx context.Context, start, end string) ([]RestModePeriod, error) {
	return fetchAll[RestModePeriod](ctx, c, "rest_mode_period", start, end)
}

// datetimeRange is the query for the time-series collections, which take
// instants rather than dates.
func datetimeRange(start, end time.Time) url.Values {
	return url.Values{
		"start_datetime": {start.Format(time.RFC3339)},
		"end_datetime":   {end.Format(time.RFC3339)},
	}
}

// HeartRates returns every heart-rate reading between two instants.
func (c *Client) HeartRates(ctx context.Context, start, end time.Time) ([]HeartRate, error) {
	return fetchQuery[HeartRate](ctx, c, "heartrate", datetimeRange(start, end))
}

// BatteryLevels returns the ring's battery readings between two instants.
func (c *Client) BatteryLevels(ctx context.Context, start, end time.Time) ([]BatteryLevel, error) {
	return fetchQuery[BatteryLevel](ctx, c, "ring_battery_level", datetimeRange(start, end))
}

// RingConfigurations returns every ring set up on the account.
func (c *Client) RingConfigurations(ctx context.Context) ([]RingConfiguration, error) {
	return fetchQuery[RingConfiguration](ctx, c, "ring_configuration", url.Values{})
}

// Personal returns the account's profile.
func (c *Client) Personal(ctx context.Context) (PersonalInfo, error) {
	var p PersonalInfo
	err := c.get(ctx, "personal_info", url.Values{}, &p)
	return p, err
}
