package oura

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth2 endpoints, from https://cloud.ouraring.com/docs/authentication.
const (
	defaultAuthorizeURL = "https://cloud.ouraring.com/oauth/authorize"
	defaultTokenURL     = "https://api.ouraring.com/oauth/token"
)

// DefaultRedirectURI is where Oura sends the browser after approval, unless
// OAuth.RedirectURI says otherwise. Whatever is used must match an address
// registered on the application at developer.ouraring.com exactly — Oura
// rejected 127.0.0.1 when only localhost was registered.
const DefaultRedirectURI = "http://localhost:8080/callback"

// RedirectForPort is the redirect address for a callback port other than
// 8080, for machines where that port is taken. Register it on the Oura app
// first.
func RedirectForPort(port int) string {
	return fmt.Sprintf("http://localhost:%d/callback", port)
}

// Scopes requested at login: every scope on the application form, so data
// a new version starts using (SpO2, daytime heart rate, workouts) is already
// covered without logging in again. A token carries only the scopes its
// login asked for, whatever the application allows — Oura reports them back
// as "extapi:<scope>". The approval page lists each with a checkbox (locked
// on for those ticked on the application): one the user leaves unticked
// isn't granted, and its collections are skipped at fetch time.
const Scopes = "email personal daily heartrate workout tag session spo2 ring_configuration stress heart_health"

// userAgent is sent on every request. Oura requires one and may reject
// requests without it.
const userAgent = "wolfe-health/0.1"

// OAuth holds an application's credentials and Oura's OAuth2 endpoints.
type OAuth struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string // where Oura returns the browser; see DefaultRedirectURI

	// Endpoints and HTTP client, overridable for tests.
	AuthorizeURL string
	TokenURL     string
	HTTP         *http.Client
}

// NewOAuth returns an OAuth for the given application credentials, pointed at
// Oura's real endpoints.
func NewOAuth(clientID, clientSecret string) *OAuth {
	return &OAuth{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  DefaultRedirectURI,
		AuthorizeURL: defaultAuthorizeURL,
		TokenURL:     defaultTokenURL,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

// Exchange trades an authorization code for a token pair.
//
// Codes are short-lived — about a minute in practice — which is why Login
// exchanges the moment the callback arrives rather than leaving it to a human
// to copy across.
func (o *OAuth) Exchange(ctx context.Context, code, verifier string) (*Token, error) {
	return o.tokenRequest(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {o.RedirectURI},
		"code_verifier": {verifier},
	})
}

// Refresh trades a refresh token for a new token pair. Oura's refresh tokens
// are single-use: the one passed in is dead after this call, whatever happens,
// so the caller must persist the returned token.
func (o *OAuth) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	return o.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

// tokenRequest POSTs a form to the token endpoint, authenticating the app with
// HTTP Basic auth, and stamps the result with the time it was obtained.
func (o *OAuth) tokenRequest(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(o.ClientID, o.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	// note the time before sending: errs on the side of an earlier expiry
	obtained := time.Now().Unix()
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// error bodies carry no secrets ({"error": "...", ...}), and Oura's
		// error_description is the only clue to what went wrong
		return nil, fmt.Errorf("token request: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var t Token
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("token request: decode: %w", err)
	}
	if t.AccessToken == "" {
		return nil, errors.New("token request: response had no access_token")
	}
	t.ObtainedAt = obtained
	return &t, nil
}

// AuthCodeURL builds the browser URL that asks the user to approve this app.
func (o *OAuth) AuthCodeURL(state, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {o.ClientID},
		"redirect_uri":          {o.RedirectURI},
		"scope":                 {Scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return o.AuthorizeURL + "?" + q.Encode()
}

// randomString returns n random bytes as unpadded base64url.
func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// pkceChallenge derives the S256 code challenge for a PKCE verifier.
//
// PKCE ties the token exchange to the process that started the login: only
// something holding the verifier can redeem the code, so a code leaked from
// the browser's address bar is useless on its own.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// callbackResult is what the redirect handler extracts from Oura's redirect.
type callbackResult struct {
	code string
	err  error
}

// callbackHandler serves the redirect, checks state, and reports the result
// on ch exactly once.
func callbackHandler(state string, ch chan<- callbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var res callbackResult
		switch {
		case q.Get("state") != state:
			// a stale tab from an earlier attempt, or a forged request
			res.err = errors.New("state mismatch: approve again from the newest link")
		case q.Get("error") != "":
			res.err = fmt.Errorf("authorization denied: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("callback had no code")
		default:
			res.code = q.Get("code")
		}
		if res.err != nil {
			http.Error(w, res.err.Error(), http.StatusBadRequest)
		} else {
			fmt.Fprintln(w, "wolfe-health is authorized. You can close this tab.")
		}
		// non-blocking: a second hit (a browser retry, favicon) is dropped
		select {
		case ch <- res:
		default:
		}
	})
}

// Login runs the browser approval flow and returns a fresh token.
//
// It listens on the redirect address's port on localhost for Oura's
// redirect, calls open with the
// approval URL, and waits for the user to approve. "localhost" can resolve to
// IPv4 or IPv6 depending on the browser, so it listens on both loopback
// addresses; one being unavailable is fine as long as the other works.
func (o *OAuth) Login(ctx context.Context, open func(url string)) (*Token, error) {
	state := randomString(16)
	verifier := randomString(48) // 64 chars; PKCE allows 43-128
	ch := make(chan callbackResult, 1)

	srv := &http.Server{Handler: callbackHandler(state, ch), ReadHeaderTimeout: 10 * time.Second}
	var listening int
	u, err := url.Parse(o.RedirectURI)
	if err != nil || u.Port() == "" {
		return nil, fmt.Errorf("redirect address %q needs a port, like http://localhost:8080/callback", o.RedirectURI)
	}
	port := u.Port()
	for _, addr := range []string{"127.0.0.1:" + port, "[::1]:" + port} {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		listening++
		go srv.Serve(ln)
	}
	if listening == 0 {
		return nil, fmt.Errorf("could not listen on localhost:%s (is something else using the port? try auth -port)", port)
	}
	defer srv.Close()

	open(o.AuthCodeURL(state, pkceChallenge(verifier)))

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for approval: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return o.Exchange(ctx, res.code, verifier)
	}
}
