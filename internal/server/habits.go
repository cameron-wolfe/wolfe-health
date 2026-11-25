package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
	"github.com/cameron-wolfe/wolfe-health/internal/habits"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// habitTokenKey is the setting holding the habit log's token, made once.
const habitTokenKey = "habit_log_token"

// habitToken returns the habit log's token, making and saving one the first
// time.
func (s *server) habitToken(ctx context.Context) (string, error) {
	tok, ok, err := s.db.Setting(ctx, habitTokenKey)
	if err != nil || ok {
		return tok, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok = hex.EncodeToString(b)
	return tok, s.db.SetSetting(ctx, habitTokenKey, tok)
}

// habitRequest is what a shortcut sends: the habit, and optionally how
// many and the token, as JSON or a form. The token may come in the
// X-Log-Token header instead.
type habitRequest struct {
	Habit string  `json:"habit"`
	Count float64 `json:"count"`
	Token string  `json:"token"`
	At    string  `json:"at"` // when, for a log made after the fact; empty for now
}

func readHabitRequest(r *http.Request) (habitRequest, error) {
	var req habitRequest
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/json" {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		if err != nil {
			return req, err
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return req, errors.New("the body isn't JSON like {\"habit\": \"caffeine\"}")
			}
		}
	} else {
		r.Body = http.MaxBytesReader(nil, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			return req, err
		}
		req.Habit, req.Token, req.At = r.Form.Get("habit"), r.Form.Get("token"), r.Form.Get("at")
		if v := r.Form.Get("count"); v != "" {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return req, errors.New("count must be a number")
			}
			req.Count = n
		}
	}
	if t := r.Header.Get("X-Log-Token"); t != "" {
		req.Token = t
	}
	return req, nil
}

// checkHabitToken answers 401 and returns false unless the request carries
// the habit log's token.
func (s *server) checkHabitToken(w http.ResponseWriter, r *http.Request, got string) bool {
	want, err := s.habitToken(r.Context())
	if err != nil {
		serverError(w, err)
		return false
	}
	got = strings.TrimSpace(got) // a pasted token often brings a space or newline
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		// why, for the server log, without the token itself
		why := "no token sent (is the header named X-Log-Token?)"
		if got != "" {
			why = fmt.Sprintf("a %d-character token that doesn't match (the token is %d)", len(got), len(want))
		}
		log.Printf("habit log: refused a request from %s: %s", r.RemoteAddr, why)
		http.Error(w, "Not logged: the shortcut's token doesn't match. Copy it again from the index page.", http.StatusUnauthorized)
		return false
	}
	return true
}

// habitList serves the menu for a shortcut's "What are you logging?": one
// habit per line. It isn't secret, so it needs no token.
func (s *server) habitList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, h := range habits.Catalog {
		fmt.Fprintln(w, h.Name)
	}
}

// logHabit records one habit now and answers in a sentence a shortcut can
// speak: "Logged 1 double shot of espresso at 2:14 PM, about 130 mg of
// caffeine. That's 2 today."
func (s *server) logHabit(w http.ResponseWriter, r *http.Request) {
	req, err := readHabitRequest(r)
	if err != nil {
		http.Error(w, "Not logged: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !s.checkHabitToken(w, r, req.Token) {
		return
	}
	h, ok := habits.Find(req.Habit)
	if !ok {
		http.Error(w, fmt.Sprintf("Not logged: I don't know %q. Pick one from GET /log/habits.", req.Habit), http.StatusBadRequest)
		return
	}
	n := req.Count
	if n == 0 {
		n = 1
	}
	if !h.Counted {
		n = 1 // what matters is that it happened
	}
	if n < 0.5 || n > 10 || math.IsNaN(n) {
		http.Error(w, "Not logged: count must be from 0.5 to 10", http.StatusBadRequest)
		return
	}
	now := s.now()
	at, err := parseLogTime(req.At, now)
	if err != nil {
		http.Error(w, "Not logged: "+err.Error(), http.StatusBadRequest)
		return
	}
	l, err := s.db.AddHabitLog(r.Context(), store.HabitLog{At: at, Habit: h.Name, Count: n, Source: "shortcut"})
	if err != nil {
		serverError(w, err)
		return
	}
	today, err := s.habitTotal(r.Context(), h.Name, at)
	if err != nil {
		serverError(w, err)
		return
	}
	what := h.Amount(n)
	if !h.Counted {
		what = strings.ToLower(h.Name)
	}
	msg := fmt.Sprintf("Logged %s at %s", what, l.At.Format("3:04 PM"))
	if day := l.At.Format("2006-01-02"); day != now.Format("2006-01-02") {
		msg += " " + l.At.Format("Monday") // backdated to another day: say which
	}
	if h.Note != "" && n == 1 {
		msg += ", " + h.Note
	}
	// a meal, sweets or a workout: say whether it's close to bed
	if h.LateWithin > 0 {
		if bed, err := dashboard.UsualBedtime(r.Context(), s.db, at); err == nil && h.IsLate(at, bed) {
			msg += fmt.Sprintf(", within %.0f hours of your usual %s bedtime", h.LateWithin.Hours(), bed.Format("3:04 PM"))
		}
	}
	msg += "."
	if h.Counted && today > n {
		when := "today"
		if l.At.Format("2006-01-02") != now.Format("2006-01-02") {
			when = "that day"
		}
		msg += fmt.Sprintf(" That's %s %s.", strconv.FormatFloat(today, 'f', -1, 64), when)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, msg)
}

// maxBackdate is how far back a log can be dated.
const maxBackdate = 7 * 24 * time.Hour

// parseLogTime reads when a log happened: empty for now; a full date and
// time (ISO 8601, as Shortcuts' Format Date gives it, or a date-time
// picker's 2006-01-02T15:04); or a time of day ("8:30 AM", "08:30"), taken
// as today, or yesterday when that would be later than now. It refuses the
// future (beyond a few minutes' clock skew) and anything over a week old.
func parseLogTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return now, nil
	}
	loc := now.Location()
	var t time.Time
	parsed := false
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		v, err := time.ParseInLocation(layout, s, loc)
		if err == nil {
			t, parsed = v.In(loc), true
			break
		}
	}
	if !parsed {
		norm := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
		for _, layout := range []string{"3:04PM", "3PM", "15:04"} {
			v, err := time.ParseInLocation(layout, norm, loc)
			if err == nil {
				t = time.Date(now.Year(), now.Month(), now.Day(), v.Hour(), v.Minute(), 0, 0, loc)
				if t.After(now.Add(5 * time.Minute)) {
					t = t.AddDate(0, 0, -1) // 11 PM, said at 1 AM: last night
				}
				parsed = true
				break
			}
		}
	}
	switch {
	case !parsed:
		return now, fmt.Errorf("I can't read the time %q; send it like 8:30 AM or 2026-11-21T08:30", s)
	case t.After(now.Add(5 * time.Minute)):
		return now, errors.New("that time is in the future")
	case now.Sub(t) > maxBackdate:
		return now, errors.New("that's more than a week ago")
	}
	return t, nil
}

// editHabit handles the index page's Save on one log: its new time, and
// for a counted habit its count. Trusted like deleteHabit.
func (s *server) editHabit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PostForm.Get("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	now := s.now()
	l, ok, err := s.db.HabitLogByID(r.Context(), id, now.Location())
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		http.Redirect(w, r, "/habits", http.StatusSeeOther)
		return
	}
	at, err := time.ParseInLocation("2006-01-02T15:04", r.PostForm.Get("at"), now.Location())
	if err != nil || at.After(now.Add(5*time.Minute)) {
		http.Error(w, "the time must be a date and time, not in the future", http.StatusBadRequest)
		return
	}
	n := l.Count
	if v := r.PostForm.Get("count"); v != "" && habits.ByName(l.Habit).Counted {
		if n, err = strconv.ParseFloat(v, 64); err != nil || n < 0.5 || n > 10 {
			http.Error(w, "count must be from 0.5 to 10", http.StatusBadRequest)
			return
		}
	}
	if _, err := s.db.UpdateHabitLog(r.Context(), id, at, n); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

// habitTotal is how much of a habit has been logged on now's day.
func (s *server) habitTotal(ctx context.Context, name string, now time.Time) (float64, error) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	logs, err := s.db.HabitLogsBetween(ctx, day, day.AddDate(0, 0, 1), now.Location())
	if err != nil {
		return 0, err
	}
	var n float64
	for _, l := range logs {
		if l.Habit == name {
			n += l.Count
		}
	}
	return n, nil
}

// undoHabit removes the most recent log, for an "undo" shortcut, if it was
// made in the last day.
func (s *server) undoHabit(w http.ResponseWriter, r *http.Request) {
	req, err := readHabitRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.checkHabitToken(w, r, req.Token) {
		return
	}
	now := s.now()
	l, ok, err := s.db.LastHabitLog(r.Context(), now.Location())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !ok || now.Sub(l.At) > 24*time.Hour {
		fmt.Fprintln(w, "Nothing to undo: nothing logged in the last day.")
		return
	}
	if _, err := s.db.DeleteHabitLog(r.Context(), l.ID); err != nil {
		serverError(w, err)
		return
	}
	fmt.Fprintf(w, "Removed %s, logged at %s.\n", habits.ByName(l.Habit).Amount(l.Count), l.At.Format("3:04 PM"))
}

// deleteHabit handles the index page's delete button for one log, by ID,
// trusted like the schedule form: the page is only on the home network and
// tailnet.
func (s *server) deleteHabit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PostForm.Get("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if _, err := s.db.DeleteHabitLog(r.Context(), id); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

// habitRow is one log, formatted for the index page.
type habitRow struct {
	ID              int64
	Day, Time, What string
	At              string // for the time picker: 2006-01-02T15:04
	Count           float64
	Counted         bool
	DayStart        bool // the newest log of its day: the page heads a section with Day
}

// recentHabits is this week's logs for the index page, newest first: weeks
// run Sunday to Saturday, as on the Habits · Week screen, so the list starts
// empty each Sunday at midnight. Older logs stay in the database.
func (s *server) recentHabits(ctx context.Context) ([]habitRow, error) {
	now := s.now()
	sunday := time.Date(now.Year(), now.Month(), now.Day()-int(now.Weekday()), 0, 0, 0, 0, now.Location())
	logs, err := s.db.HabitLogsBetween(ctx, sunday, now.Add(time.Minute), now.Location())
	if err != nil {
		return nil, err
	}
	rows := make([]habitRow, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		h := habits.ByName(l.Habit)
		what := l.Habit // an uncounted habit's amount would only repeat its name
		if h.Counted {
			what += " · " + h.Amount(l.Count)
		}
		day := l.At.Format("Monday, Jan 2")
		rows = append(rows, habitRow{ID: l.ID, Day: day, Time: l.At.Format("3:04 PM"),
			What: what, At: l.At.Format("2006-01-02T15:04"), Count: l.Count, Counted: h.Counted,
			DayStart: len(rows) == 0 || rows[len(rows)-1].Day != day})
	}
	return rows, nil
}

// exportHabits serves every habit log as CSV to save: the copy of the habit
// log that lives off the server, since unlike Oura's data it can't be
// fetched again. `wolfe-health habits import` reads it back. Trusted like
// the index page: home network and tailnet only.
func (s *server) exportHabits(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	logs, err := s.db.HabitLogsBetween(r.Context(), time.Unix(0, 0), now.AddDate(0, 0, 1), now.Location())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="habit-log-`+now.Format("2006-01-02")+`.csv"`)
	if err := store.WriteHabitCSV(w, logs); err != nil {
		log.Printf("habit log export: %v", err)
	}
}
