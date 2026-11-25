package oura

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RFC 7636 appendix B's worked example: a known verifier and its challenge.
func TestPKCEChallengeMatchesRFC(t *testing.T) {
	got := pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"; got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
}

func TestSaveTokenIsPrivateAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok.json")
	in := &Token{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600, ObtainedAt: 1000}
	if err := SaveToken(path, in); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
	out, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if *out != *in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
	// the temp file must not be left behind
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".oura-token-*")); len(m) != 0 {
		t.Errorf("leftover temp files: %v", m)
	}
}

// A token file made by hand with curl has no obtained_at; its expiry must
// then count from the file's modification time.
func TestLoadTokenFallsBackToModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok.json")
	body := `{"token_type":"bearer","access_token":"a","refresh_token":"r","expires_in":2592000,"id_token":null}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	tok, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := mtime.Add(30 * 24 * time.Hour); !tok.Expiry().Equal(want) {
		t.Errorf("expiry = %v, want %v", tok.Expiry(), want)
	}
}

// fakeOura serves the token endpoint and one API collection. It hands out
// rotating refresh tokens and rejects a reused one, as Oura does.
type fakeOura struct {
	t          *testing.T
	refreshes  int
	valid      string // the only refresh token currently accepted
	access     string // the only access token the API accepts
	userAgents []string
}

func (f *fakeOura) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.userAgents = append(f.userAgents, r.UserAgent())
	switch r.URL.Path {
	case "/oauth/token":
		id, secret, ok := r.BasicAuth()
		if !ok || id != "cid" || secret != "csecret" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != f.valid {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		f.refreshes++
		f.access = "access-" + string(rune('0'+f.refreshes))
		f.valid = "refresh-" + string(rune('0'+f.refreshes))
		json.NewEncoder(w).Encode(map[string]any{
			"token_type": "bearer", "access_token": f.access,
			"refresh_token": f.valid, "expires_in": 2592000,
		})
	case "/v2/usercollection/daily_sleep":
		if r.Header.Get("Authorization") != "Bearer "+f.access {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[{"day":"2026-11-19","score":66}],"next_token":null}`))
	default:
		http.NotFound(w, r)
	}
}

func newTestSource(t *testing.T, f *fakeOura, srv *httptest.Server, tok *Token) (*FileTokenSource, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tok.json")
	if err := SaveToken(path, tok); err != nil {
		t.Fatal(err)
	}
	o := NewOAuth("cid", "csecret")
	o.TokenURL = srv.URL + "/oauth/token"
	src, err := NewFileTokenSource(path, o)
	if err != nil {
		t.Fatal(err)
	}
	return src, path
}

// A token near expiry is refreshed before use, and the rotated refresh token
// is on disk afterwards — the next run must not hold the spent one.
func TestTokenSourceRefreshesNearExpiryAndPersists(t *testing.T) {
	f := &fakeOura{t: t, valid: "refresh-0", access: "access-0"}
	srv := httptest.NewServer(f)
	defer srv.Close()

	now := time.Unix(1_000_000, 0)
	src, path := newTestSource(t, f, srv, &Token{
		AccessToken: "access-0", RefreshToken: "refresh-0",
		ExpiresIn: 3600, ObtainedAt: now.Unix(), // expires in 1h < refreshMargin
	})
	src.now = func() time.Time { return now }

	got, err := src.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "access-1" {
		t.Errorf("token = %q, want refreshed access-1", got)
	}
	onDisk, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.RefreshToken != "refresh-1" {
		t.Errorf("refresh token on disk = %q, want rotated refresh-1", onDisk.RefreshToken)
	}

	// still fresh now: no second refresh
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", f.refreshes)
	}
}

// A token the server has stopped accepting (401) is refreshed once and the
// request retried, even though its recorded expiry says it is fine.
func TestClientRefreshesOn401(t *testing.T) {
	f := &fakeOura{t: t, valid: "refresh-0", access: "server-forgot-this"}
	srv := httptest.NewServer(f)
	defer srv.Close()

	src, _ := newTestSource(t, f, srv, &Token{
		AccessToken: "stale", RefreshToken: "refresh-0",
		ExpiresIn: 2592000, ObtainedAt: time.Now().Unix(),
	})
	c := New(src)
	c.baseURL = srv.URL + "/v2/usercollection/"

	days, err := c.Sleep(context.Background(), "2026-11-19", "2026-11-19")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Score != 66 {
		t.Errorf("got %+v", days)
	}
	if f.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", f.refreshes)
	}
	for _, ua := range f.userAgents {
		if !strings.HasPrefix(ua, "wolfe-health/") {
			t.Errorf("request sent User-Agent %q", ua)
		}
	}
}

func TestCallbackHandler(t *testing.T) {
	cases := []struct {
		name     string
		query    url.Values
		wantCode string
		wantErr  string
	}{
		{"ok", url.Values{"state": {"s"}, "code": {"c"}}, "c", ""},
		{"wrong state", url.Values{"state": {"x"}, "code": {"c"}}, "", "state mismatch"},
		{"denied", url.Values{"state": {"s"}, "error": {"access_denied"}}, "", "access_denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan callbackResult, 1)
			rec := httptest.NewRecorder()
			callbackHandler("s", ch).ServeHTTP(rec,
				httptest.NewRequest("GET", "/callback?"+tc.query.Encode(), nil))
			res := <-ch
			if res.code != tc.wantCode {
				t.Errorf("code = %q, want %q", res.code, tc.wantCode)
			}
			if (res.err == nil) != (tc.wantErr == "") ||
				(res.err != nil && !strings.Contains(res.err.Error(), tc.wantErr)) {
				t.Errorf("err = %v, want containing %q", res.err, tc.wantErr)
			}
		})
	}
}

func TestAuthCodeURL(t *testing.T) {
	o := NewOAuth("cid", "csecret")
	u, err := url.Parse(o.AuthCodeURL("st", "ch"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"response_type": "code", "client_id": "cid", "redirect_uri": DefaultRedirectURI,
		"scope": Scopes, "state": "st", "code_challenge": "ch", "code_challenge_method": "S256",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	// the client secret must never appear in a URL the browser sees
	if strings.Contains(u.String(), "csecret") {
		t.Error("client secret leaked into the authorize URL")
	}
}

// A non-default callback port flows into the authorize URL, and a redirect
// without a port is refused before anything listens.
func TestCustomRedirectPort(t *testing.T) {
	o := NewOAuth("cid", "csecret")
	o.RedirectURI = RedirectForPort(8085)
	u, err := url.Parse(o.AuthCodeURL("st", "ch"))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("redirect_uri"); got != "http://localhost:8085/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	o.RedirectURI = "http://localhost/callback"
	if _, err := o.Login(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "needs a port") {
		t.Errorf("Login with no port: %v", err)
	}
}
