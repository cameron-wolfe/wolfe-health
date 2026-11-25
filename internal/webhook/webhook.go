// Package webhook receives Oura's webhook notifications and manages the
// subscriptions that send them.
//
// Oura calls a public HTTPS URL about 30 seconds after a ring's data syncs,
// so the daemon can fetch then, rather than waiting for its next poll. The
// handler answers Oura's one-time verification, checks each notification's
// signature, and hands verified events on; it serves nothing else, so it
// can be exposed to the internet on a port of its own while the charts stay
// on the home network.
//
// The protocol, from Oura's API v2 docs:
//
//   - verification: GET callback?verification_token=T&challenge=C, answered
//     with {"challenge": C} when T is the subscription's token;
//   - notifications: POST {event_type, data_type, object_id, event_time,
//     user_id}, signed in x-oura-signature as the upper-case hex HMAC-SHA256
//     of x-oura-timestamp followed by the body, keyed with the app's client
//     secret; answered within 10 seconds with a 2xx, or Oura retries.
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// Path is where the handler listens.
const Path = "/oura/webhook"

// Event is one notification: what changed, by Oura's names.
type Event struct {
	EventType string `json:"event_type"` // create, update or delete
	DataType  string `json:"data_type"`  // daily_sleep, sleep, workout, ...
	ObjectID  string `json:"object_id"`
	EventTime string `json:"event_time"`
	UserID    string `json:"user_id"`
}

// Handler serves Path: Oura's verification and its notifications. token is
// the verification token the subscriptions were made with, secret the
// app's client secret that signs notifications, and onEvent is called
// with each verified event; it must return quickly.
func Handler(token, secret string, onEvent func(Event)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("verification_token")), []byte(token)) != 1 || q.Get("challenge") == "" {
			http.Error(w, "invalid verification token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": q.Get("challenge")})
	})
	mux.HandleFunc("POST "+Path, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		if !validSignature(secret, r.Header.Get("x-oura-timestamp"), r.Header.Get("x-oura-signature"), body) {
			log.Printf("webhook: rejected a notification with a bad signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var ev Event
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(w, "bad notification", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		onEvent(ev)
	})
	return mux
}

// validSignature checks a notification's signature: the upper-case hex
// HMAC-SHA256, keyed with the client secret, of the timestamp followed by
// the body. Oura's example signs the body as JSON.stringify re-encodes it,
// which is the body without whitespace, so that form is accepted too.
func validSignature(secret, timestamp, signature string, body []byte) bool {
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(got) == 0 || timestamp == "" {
		return false
	}
	sign := func(b []byte) []byte {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(timestamp))
		m.Write(b)
		return m.Sum(nil)
	}
	if hmac.Equal(got, sign(body)) {
		return true
	}
	var compact bytes.Buffer
	if json.Compact(&compact, body) == nil && !bytes.Equal(compact.Bytes(), body) {
		return hmac.Equal(got, sign(compact.Bytes()))
	}
	return false
}
