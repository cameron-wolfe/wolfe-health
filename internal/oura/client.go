// Package oura is a client for the Oura Ring API v2.
//
// Authentication is OAuth2 (see auth.go and token.go). Oura retired personal
// access tokens in December 2025.
package oura

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const defaultBaseURL = "https://api.ouraring.com/v2/usercollection/"

// TokenSource supplies the bearer token for each request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// refresher is a TokenSource that can also force a refresh. When the API
// rejects a token with 401, the client refreshes and retries once if its
// source supports it.
type refresher interface {
	Refresh(ctx context.Context) error
}

// Client talks to the Oura API on behalf of a single user.
type Client struct {
	baseURL string
	tokens  TokenSource
	http    *http.Client
}

// New returns a Client that authenticates with tokens from src.
func New(src TokenSource) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		tokens:  src,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// FormatDate renders t in the form Oura's start_date/end_date params expect.
// reference time is always Jan 2nd 15:04:05 MST 2006 - deliberately 1 2 3 4 5 6
func FormatDate(t time.Time) string { return t.Format("2006-01-02") }

// WithBaseURL points the client at another server — a test double of the
// Oura API — and returns it. base ends in a slash, like the real one:
// ".../v2/usercollection/".
func (c *Client) WithBaseURL(base string) *Client {
	c.baseURL = base
	return c
}

// maxPages bounds the pagination loop. Oura pages hold far more than a day,
// so even years of history is a handful of pages; hitting this means the
// server is handing back tokens that never end, not that there is more data.
const maxPages = 1000

// fetchAll fetches every page of one collection endpoint over the inclusive
// date range and returns all records in order.
//
// Oura splits long responses into pages. Each response carries next_token,
// which is null on the last page; otherwise it goes back as a query parameter,
// with the same dates, to get the following page.
//
// It is a function rather than a method because Go methods cannot have type
// parameters, and T is what lets one loop serve every collection.
func fetchAll[T any](ctx context.Context, c *Client, endpoint, start, end string) ([]T, error) {
	return fetchQuery[T](ctx, c, endpoint, url.Values{"start_date": {start}, "end_date": {end}})
}

// fetchQuery is fetchAll with any fixed query: a datetime range for the
// time-series collections, or none for ring_configuration.
func fetchQuery[T any](ctx context.Context, c *Client, endpoint string, base url.Values) ([]T, error) {
	var all []T
	next := ""
	for page := 0; page < maxPages; page++ {
		q := url.Values{}
		for k, v := range base {
			q[k] = v
		}
		if next != "" {
			q.Set("next_token", next)
		}

		var resp listResponse[T]
		if err := c.get(ctx, endpoint, q, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)

		if resp.NextToken == "" {
			return all, nil
		}
		if resp.NextToken == next {
			return nil, fmt.Errorf("fetch %s: next_token did not advance", endpoint)
		}
		next = resp.NextToken
	}
	return nil, fmt.Errorf("fetch %s: gave up after %d pages", endpoint, maxPages)
}

// get fetches one page of an endpoint with the given query and decodes the
// response into out.
//
// A 401 means the access token was rejected. If the token source can refresh,
// get refreshes once and retries; a second 401 is reported as an error.
func (c *Client) get(ctx context.Context, endpoint string, q url.Values, out any) error {
	u := c.baseURL + endpoint + "?" + q.Encode()

	resp, err := c.do(ctx, u)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		if r, ok := c.tokens.(refresher); ok {
			resp.Body.Close()
			if err := r.Refresh(ctx); err != nil {
				return fmt.Errorf("fetch %s: got 401, then %w", endpoint, err)
			}
			if resp, err = c.do(ctx, u); err != nil {
				return fmt.Errorf("fetch %s: %w", endpoint, err)
			}
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: unexpected status %s", endpoint, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

// do sends one authenticated GET. The caller closes the response body.
func (c *Client) do(ctx context.Context, u string) (*http.Response, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// Oura requires a User-Agent and may reject requests without one
	req.Header.Set("User-Agent", userAgent)
	return c.http.Do(req)
}
