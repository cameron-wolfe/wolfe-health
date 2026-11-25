package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAPI = "https://api.ouraring.com/v2/webhook/subscription"

// DataTypes are the Oura collections the daemon stores and so subscribes
// to. Heart rate has no webhook; the catch-up poll and the fetch each
// notification starts bring it in.
var DataTypes = []string{
	"daily_sleep", "sleep", "daily_readiness", "daily_activity", "daily_stress",
	"daily_spo2", "sleep_time", "workout", "session", "enhanced_tag",
	"daily_cardiovascular_age", "daily_resilience", "vo2_max",
}

// EventTypes are the events subscribed to: new and changed data. A
// deletion needs no fetch.
var EventTypes = []string{"create", "update"}

// Subscription is one of Oura's webhook subscriptions.
type Subscription struct {
	ID          string `json:"id"`
	CallbackURL string `json:"callback_url"`
	EventType   string `json:"event_type"`
	DataType    string `json:"data_type"`
	Expires     string `json:"expiration_time"`
}

// ExpiresAt is when the subscription lapses, or the zero time when Oura's
// value can't be read.
func (s Subscription) ExpiresAt() time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s.Expires); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Client manages the app's subscriptions, authenticated with its client ID
// and secret; no user's token is involved.
type Client struct {
	id, secret, api string
	http            *http.Client
}

// NewClient makes a subscription client for the app.
func NewClient(clientID, clientSecret string) *Client {
	return &Client{id: clientID, secret: clientSecret, api: defaultAPI, http: &http.Client{Timeout: 60 * time.Second}}
}

// WithAPI points the client at another server, for tests.
func (c *Client) WithAPI(url string) *Client {
	c.api = strings.TrimSuffix(url, "/")
	return c
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-client-id", c.id)
	req.Header.Set("x-client-secret", c.secret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("webhook subscriptions: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webhook subscriptions: %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// List returns the app's subscriptions.
func (c *Client) List(ctx context.Context) ([]Subscription, error) {
	var subs []Subscription
	return subs, c.do(ctx, http.MethodGet, "", nil, &subs)
}

// Create subscribes callbackURL to one event on one data type. Oura
// verifies the URL before answering, so the handler must already be
// reachable there.
func (c *Client) Create(ctx context.Context, callbackURL, token, eventType, dataType string) (Subscription, error) {
	var s Subscription
	err := c.do(ctx, http.MethodPost, "", map[string]string{
		"callback_url": callbackURL, "verification_token": token,
		"event_type": eventType, "data_type": dataType,
	}, &s)
	return s, err
}

// Renew extends a subscription before it lapses.
func (c *Client) Renew(ctx context.Context, id string) (Subscription, error) {
	var s Subscription
	return s, c.do(ctx, http.MethodPut, "/renew/"+id, nil, &s)
}

// Delete removes a subscription.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/"+id, nil, nil)
}

// Report is what Ensure did.
type Report struct {
	Created, Renewed, Kept int
	NextExpiry             time.Time // the soonest any of callbackURL's subscriptions lapses
	Errors                 []string
}

func (r Report) String() string {
	s := fmt.Sprintf("%d subscriptions (%d created, %d renewed)", r.Created+r.Renewed+r.Kept, r.Created, r.Renewed)
	if !r.NextExpiry.IsZero() {
		s += ", next expiring " + r.NextExpiry.Local().Format("Jan 2")
	}
	for _, e := range r.Errors {
		s += "; " + e
	}
	return s
}

// Ensure makes callbackURL subscribed to every event in EventTypes on every
// type in DataTypes: it creates what's missing and renews what lapses
// within renewWithin (or whose expiry can't be read). Subscriptions to
// other URLs are left alone. One failure doesn't stop the rest; each is
// reported.
func (c *Client) Ensure(ctx context.Context, callbackURL, token string, renewWithin time.Duration) (Report, error) {
	var r Report
	subs, err := c.List(ctx)
	if err != nil {
		return r, err
	}
	have := map[string]bool{}
	note := func(s Subscription) {
		if t := s.ExpiresAt(); !t.IsZero() && (r.NextExpiry.IsZero() || t.Before(r.NextExpiry)) {
			r.NextExpiry = t
		}
	}
	for _, s := range subs {
		if s.CallbackURL != callbackURL {
			continue
		}
		have[s.EventType+"/"+s.DataType] = true
		if t := s.ExpiresAt(); t.IsZero() || time.Until(t) < renewWithin {
			renewed, err := c.Renew(ctx, s.ID)
			if err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("renewing %s %s: %v", s.EventType, s.DataType, err))
				note(s)
				continue
			}
			r.Renewed++
			note(renewed)
			continue
		}
		r.Kept++
		note(s)
	}
	for _, dt := range DataTypes {
		for _, et := range EventTypes {
			if have[et+"/"+dt] {
				continue
			}
			s, err := c.Create(ctx, callbackURL, token, et, dt)
			if err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("subscribing to %s %s: %v", et, dt, err))
				continue
			}
			r.Created++
			note(s)
		}
	}
	return r, nil
}
