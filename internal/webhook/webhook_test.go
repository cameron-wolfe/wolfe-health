package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func sign(secret, ts, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + body))
	return strings.ToUpper(hex.EncodeToString(m.Sum(nil)))
}

// The handler answers Oura's verification only with the right token, takes
// a signed notification and refuses a forged one, and serves nothing else.
func TestHandler(t *testing.T) {
	var got []Event
	h := Handler("tok", "secret", func(e Event) { got = append(got, e) })
	do := func(method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := do("GET", Path+"?verification_token=tok&challenge=abc123", "", nil)
	var ch map[string]string
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &ch) != nil || ch["challenge"] != "abc123" {
		t.Errorf("verification: %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", Path+"?verification_token=nope&challenge=abc", "", nil); rec.Code != 401 {
		t.Errorf("wrong token: %d", rec.Code)
	}

	body := `{"event_type": "update", "data_type": "sleep", "object_id": "x1", "event_time": "2026-11-20T08:00:00+00:00", "user_id": "u"}`
	ok := map[string]string{"x-oura-timestamp": "1795125600", "x-oura-signature": sign("secret", "1795125600", body)}
	if rec := do("POST", Path, body, ok); rec.Code != 200 || len(got) != 1 || got[0].DataType != "sleep" || got[0].ObjectID != "x1" {
		t.Errorf("signed notification: %d, events %+v", rec.Code, got)
	}
	// signed as JSON.stringify re-encodes it: no whitespace
	compact := `{"event_type":"update","data_type":"sleep","object_id":"x1","event_time":"2026-11-20T08:00:00+00:00","user_id":"u"}`
	if rec := do("POST", Path, body, map[string]string{"x-oura-timestamp": "1", "x-oura-signature": sign("secret", "1", compact)}); rec.Code != 200 {
		t.Errorf("compact-signed notification: %d", rec.Code)
	}
	for name, hdr := range map[string]map[string]string{
		"no signature":    {"x-oura-timestamp": "1"},
		"wrong secret":    {"x-oura-timestamp": "1", "x-oura-signature": sign("other", "1", body)},
		"wrong timestamp": {"x-oura-timestamp": "2", "x-oura-signature": sign("secret", "1", body)},
	} {
		if rec := do("POST", Path, body, hdr); rec.Code != 401 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if len(got) != 2 {
		t.Errorf("forged notifications got through: %d events", len(got))
	}
	for _, u := range []string{"/", "/current.png", "/schedule"} {
		if rec := do("GET", u, "", nil); rec.Code != 404 {
			t.Errorf("%s: %d, want 404 (the webhook port serves nothing else)", u, rec.Code)
		}
	}
}

// fakeOura is Oura's subscription API, in memory.
type fakeOura struct {
	mu   sync.Mutex
	subs map[string]Subscription
	n    int
	auth bool
}

func (f *fakeOura) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("x-client-id") != "id" || r.Header.Get("x-client-secret") != "sec" {
		http.Error(w, "unauthorized", 401)
		return
	}
	f.auth = true
	switch {
	case r.Method == "GET":
		out := []Subscription{}
		for _, s := range f.subs {
			out = append(out, s)
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == "POST":
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		f.n++
		s := Subscription{ID: "s" + string(rune('a'+f.n)), CallbackURL: in["callback_url"], EventType: in["event_type"],
			DataType: in["data_type"], Expires: time.Now().Add(90 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000000")}
		f.subs[s.ID] = s
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(s)
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/renew/"):
		id := strings.TrimPrefix(r.URL.Path, "/renew/")
		s, ok := f.subs[id]
		if !ok {
			http.Error(w, "no such webhook", 403)
			return
		}
		s.Expires = time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
		f.subs[id] = s
		json.NewEncoder(w).Encode(s)
	default:
		http.Error(w, "unexpected", 400)
	}
}

// Ensure subscribes every type and event once, renews one that's about to
// lapse, and leaves another URL's subscriptions alone.
func TestEnsure(t *testing.T) {
	f := &fakeOura{subs: map[string]Subscription{
		"old":   {ID: "old", CallbackURL: "https://me/oura/webhook", EventType: "update", DataType: "sleep", Expires: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)},
		"other": {ID: "other", CallbackURL: "https://elsewhere", EventType: "update", DataType: "sleep", Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := NewClient("id", "sec").WithAPI(srv.URL)
	r, err := c.Ensure(context.Background(), "https://me/oura/webhook", "tok", 7*24*time.Hour)
	if err != nil || len(r.Errors) > 0 {
		t.Fatal(err, r.Errors)
	}
	want := len(DataTypes)*len(EventTypes) - 1
	if r.Created != want || r.Renewed != 1 {
		t.Errorf("created %d renewed %d, want %d and 1", r.Created, r.Renewed, want)
	}
	if time.Until(f.subs["other"].ExpiresAt()) > 2*time.Hour {
		t.Error("another URL's subscription was renewed")
	}
	// a second run has nothing to do
	r, err = c.Ensure(context.Background(), "https://me/oura/webhook", "tok", 7*24*time.Hour)
	if err != nil || r.Created != 0 || r.Renewed != 0 || r.Kept != want+1 {
		t.Errorf("second run: %+v, %v", r, err)
	}
	if r.NextExpiry.IsZero() {
		t.Error("no next expiry")
	}
	if _, err := NewClient("id", "wrong").WithAPI(srv.URL).List(context.Background()); err == nil {
		t.Error("a wrong secret should fail")
	}
}
