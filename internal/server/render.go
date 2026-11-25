package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Screens, by the name used in routes, the display schedule and downloads.
const (
	scrDaily          = "daily"
	scrNight          = "night"
	scrStressMonth    = "stress-month"
	scrScoreMonth     = "score-month"
	scrOxygen         = "oxygen"
	scrHeartToday     = "heart-today"
	scrHeartYesterday = "heart-today/yesterday" // the day before, whole
	scrSleepSchedule  = "sleep-schedule"
	scrFitness        = "fitness"
	scrPayoff         = "bedtime-payoff"
	scrVitals         = "vital-signs"
	scrDeviation      = "deviation-radar"
	scrWeekRadar      = "week-radar" // the last complete week, as a radar
	scrMoon           = "wolfephase"
	scrDigest         = "daily-digest" // the first daily brief's calmer layout
	scrPhaseMonth     = "wolfephase-month"
	scrHabitWeek      = "habits-week"   // the week so far: each day's habits and the night after
	scrHabitEffects   = "habit-effects" // nights after each habit against nights without
	scrSleepStages    = "sleep-stages"
	scrWeeklyRhythm   = "weekly-rhythm"
	scrSleepBreakdown = "sleep-breakdown"
	scrReadinessParts = "readiness-breakdown"
	scrBodySignals    = "body-signals"
	scrEarlyWarning   = "early-warning"
	scrMonthReport    = "month-report"
	scrLastMonth      = "month-report/last" // the month report for the month before this one
	scrLastScores     = "score-month/last"  // the calendars for the month before this one,
	scrLastStress     = "stress-month/last" // shown whole on the 1st
	scrLastPhase      = "wolfephase-month/last"
	scrTrends         = "trends"
	scrWeekReview     = "week-review"      // the last complete week, Sunday to Saturday
	scrThisWeek       = "week-review/this" // the current week so far
	legacyLinePrefix  = "line/"            // the retired per-metric trend charts, now scrTrends
)

// httpErr carries the status an error should be answered with.
type httpErr struct {
	code int
	err  error
}

func (e httpErr) Error() string { return e.err.Error() }
func (e httpErr) Unwrap() error { return e.err }

func badRequest(err error) error { return httpErr{http.StatusBadRequest, err} }
func notFound(err error) error   { return httpErr{http.StatusNotFound, err} }

// writeErr answers with the error's status, or 500 for anything unmarked.
func writeErr(w http.ResponseWriter, err error) {
	var he httpErr
	if errors.As(err, &he) {
		http.Error(w, he.Error(), he.code)
		return
	}
	serverError(w, err)
}

// asOf reads a ?date= of interest: the day the screens are drawn as of,
// and the moment within it — the end of that day in local time, or the
// real now when the date is today. With no date it is today, undated.
// A future date is refused: there is nothing to draw yet.
func (s *server) asOf(v string) (today, now time.Time, dated bool, err error) {
	today, now = s.today(), s.now()
	if v == "" {
		return today, now, false, nil
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return today, now, false, errors.New("date must be YYYY-MM-DD")
	}
	if d.After(today) {
		return today, now, false, errors.New("date can't be in the future")
	}
	if d.Equal(today) {
		return today, now, false, nil
	}
	end := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, now.Location())
	return d, end, true, nil
}

// qInt reads an integer query parameter within [min, max], or def if absent.
func qInt(q url.Values, name string, def, min, max int) (int, error) {
	v := q.Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, badRequest(fmt.Errorf("%s must be a whole number from %d to %d", name, min, max))
	}
	return n, nil
}

// render draws one screen at w x h, with options from q, and returns the
// image and a name for its download. Every image route and the display
// endpoint go through here, so what a browser shows and what the panel
// shows can never drift apart.
func (s *server) render(ctx context.Context, screen string, q url.Values, w, h int) (*image.Gray, string, error) {
	// ?date= draws every screen as of that day: today is that date, and
	// "now" the end of it (or the real now, for today itself)
	today, now, dated, err := s.asOf(q.Get("date"))
	if err != nil {
		return nil, "", badRequest(err)
	}
	// the early-warning banner, on every screen while it applies
	alert, err := dashboard.Alert(ctx, s.db, today)
	if err != nil {
		return nil, "", err
	}
	if q.Get("alert") == "preview" { // browser only: displays never send it
		alert = dashboard.PreviewAlert
	}
	// the status line along the bottom: ring battery, last sync, last update
	foot, err := dashboard.Footer(ctx, s.db, s.now())
	if err != nil {
		return nil, "", err
	}
	draw := func(masthead string, f func(w, h int) (*image.Gray, error)) (*image.Gray, error) {
		if dated { // a screen for another day says which, in the masthead
			masthead += " · " + today.Format("Jan 2")
		}
		img, zones, err := chart.BrandedFrameZones(masthead, alert, foot, w, h, f)
		if err != nil {
			return nil, badRequest(err) // the charts reject only sizes they can't fit
		}
		if sink, ok := ctx.Value(zonesKey{}).(*[]chart.Zone); ok {
			*sink = zones // for a caller that asked (withZones)
		}
		return img, nil
	}

	switch {
	case screen == scrDaily:
		d, err := dashboard.Daily(ctx, s.db, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Daily brief", func(w, h int) (*image.Gray, error) { return chart.BriefView(d, w, h) })
		return img, "daily-" + today.Format("2006-01-02"), err

	case screen == scrDigest:
		d, err := dashboard.Daily(ctx, s.db, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Daily digest", func(w, h int) (*image.Gray, error) { return chart.DailyView(d, w, h) })
		return img, "daily-digest-" + today.Format("2006-01-02"), err

	case screen == scrNight || screen == scrMoon:
		// the night's wake-up day: ?day=, or with a date of interest, that
		// date (the night before it); otherwise the latest night
		day := q.Get("day")
		if day != "" {
			if _, err := time.Parse("2006-01-02", day); err != nil {
				return nil, "", badRequest(errors.New("day must be YYYY-MM-DD"))
			}
		} else if dated {
			day = today.Format("2006-01-02")
		}
		n, err := dashboard.Night(ctx, s.db, day)
		if errors.Is(err, dashboard.ErrNoSleep) {
			return nil, "", notFound(err)
		}
		if err != nil {
			return nil, "", err
		}
		if screen == scrMoon {
			name := "wolfephase"
			if day != "" {
				name += "-" + day
			}
			img, err := draw("WolfePhase", func(w, h int) (*image.Gray, error) { return chart.MoonView(n, w, h) })
			return img, name, err
		}
		masthead, name := "Last night", "night"
		if day != "" {
			masthead, name = "Night detail", "night-"+day
		}
		img, err := draw(masthead, func(w, h int) (*image.Gray, error) { return chart.NightView(n, w, h) })
		return img, name, err

	case screen == scrTrends:
		days, err := qInt(q, "days", 60, 14, 365)
		if err != nil {
			return nil, "", err
		}
		tr, err := dashboard.Trends(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Trends", func(w, h int) (*image.Gray, error) { return chart.TrendsView(tr, w, h) })
		return img, fmt.Sprintf("trends-%dd", days), err

	case screen == scrStressMonth || screen == scrLastStress:
		month, err := monthOf(q, today, screen == scrLastStress)
		if err != nil {
			return nil, "", err
		}
		m, err := dashboard.StressMonth(ctx, s.db, month, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Stress calendar", func(w, h int) (*image.Gray, error) { return chart.StressCalendar(m, w, h) })
		return img, "stress-" + month.Format("2006-01"), err

	case screen == scrHabitWeek:
		start := dashboard.WeekStart(today)
		start, err := weekOf(q, start)
		if err != nil {
			return nil, "", err
		}
		hw, err := dashboard.HabitWeek(ctx, s.db, start, today, s.now().Location())
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Habits · Week", func(w, h int) (*image.Gray, error) { return chart.HabitWeekView(hw, w, h) })
		return img, "habits-week-" + start.Format("2006-01-02"), err

	case screen == scrHabitEffects:
		days, err := qInt(q, "days", 90, 14, 730)
		if err != nil {
			return nil, "", err
		}
		he, err := dashboard.HabitEffects(ctx, s.db, days, today, s.now().Location())
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Habit effects", func(w, h int) (*image.Gray, error) { return chart.HabitEffectsView(he, w, h) })
		return img, fmt.Sprintf("habit-effects-%dd", days), err

	case screen == scrPhaseMonth || screen == scrLastPhase:
		month, err := monthOf(q, today, screen == scrLastPhase)
		if err != nil {
			return nil, "", err
		}
		m, err := dashboard.PhaseMonth(ctx, s.db, month, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("WolfePhase · Month", func(w, h int) (*image.Gray, error) { return chart.PhaseMonthView(m, w, h) })
		return img, "wolfephase-" + month.Format("2006-01"), err

	case screen == scrWeekRadar:
		start := dashboard.WeekStart(today).AddDate(0, 0, -7)
		start, err := weekOf(q, start)
		if err != nil {
			return nil, "", err
		}
		wr, err := dashboard.WeekRadar(ctx, s.db, start, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("WolfeSense · Week", func(w, h int) (*image.Gray, error) { return chart.DeviationView(wr, w, h) })
		return img, "week-radar-" + start.Format("2006-01-02"), err

	case screen == scrDeviation:
		dr, err := dashboard.Deviation(ctx, s.db, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("WolfeSense · Signals", func(w, h int) (*image.Gray, error) { return chart.DeviationView(dr, w, h) })
		return img, "deviation-radar-" + today.Format("2006-01-02"), err

	case screen == scrVitals:
		v, err := dashboard.Instruments(ctx, s.db, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Vital signs", func(w, h int) (*image.Gray, error) { return chart.InstrumentsView(v, w, h) })
		return img, "vital-signs-" + today.Format("2006-01-02"), err

	case screen == scrEarlyWarning:
		days, err := qInt(q, "days", dashboard.WarnDays, 7, 60)
		if err != nil {
			return nil, "", err
		}
		ew, err := dashboard.EarlyWarning(ctx, s.db, days, today, s.now().Location())
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Early warning", func(w, h int) (*image.Gray, error) { return chart.EarlyWarningView(ew, w, h) })
		return img, fmt.Sprintf("early-warning-%dd-%s", days, today.Format("2006-01-02")), err

	case screen == scrPayoff:
		days, err := qInt(q, "days", dashboard.PayoffWindowDays, 30, 365)
		if err != nil {
			return nil, "", err
		}
		po, err := dashboard.BedtimePayoff(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Bedtime payoff", func(w, h int) (*image.Gray, error) { return chart.PayoffView(po, w, h) })
		return img, fmt.Sprintf("bedtime-payoff-%dd", days), err

	case screen == scrFitness:
		days, err := qInt(q, "days", 365, 60, 730)
		if err != nil {
			return nil, "", err
		}
		ft, err := dashboard.Fitness(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Fitness and sleep", func(w, h int) (*image.Gray, error) { return chart.TrendsView(ft, w, h) })
		return img, fmt.Sprintf("fitness-%dd", days), err

	case screen == scrSleepSchedule:
		days, err := qInt(q, "days", 30, 7, 60)
		if err != nil {
			return nil, "", err
		}
		sc, err := dashboard.SleepSchedule(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Sleep schedule", func(w, h int) (*image.Gray, error) { return chart.ScheduleView(sc, w, h) })
		return img, fmt.Sprintf("sleep-schedule-%dd", days), err

	case screen == scrHeartToday || screen == scrHeartYesterday:
		day := today
		if screen == scrHeartYesterday {
			day = today.AddDate(0, 0, -1)
		}
		if v := q.Get("day"); v != "" {
			d, err := time.Parse("2006-01-02", v)
			if err != nil {
				return nil, "", badRequest(errors.New("day must be YYYY-MM-DD"))
			}
			day = d
		}
		hd, err := dashboard.HeartToday(ctx, s.db, day, now)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Heart rate", func(w, h int) (*image.Gray, error) { return chart.HeartDayView(hd, w, h) })
		return img, "heart-" + day.Format("2006-01-02"), err

	case screen == scrOxygen:
		days, err := qInt(q, "days", 90, 7, 365)
		if err != nil {
			return nil, "", err
		}
		o, err := dashboard.OxygenTide(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Oxygen tide", func(w, h int) (*image.Gray, error) { return chart.OxygenView(o, w, h) })
		return img, fmt.Sprintf("oxygen-%dd", days), err

	case screen == scrScoreMonth || screen == scrLastScores:
		month, err := monthOf(q, today, screen == scrLastScores)
		if err != nil {
			return nil, "", err
		}
		m, err := dashboard.ScoreMonth(ctx, s.db, month, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Score calendar", func(w, h int) (*image.Gray, error) { return chart.ScoreCalendar(m, w, h) })
		return img, "scores-" + month.Format("2006-01"), err

	case screen == scrSleepStages:
		days, err := qInt(q, "days", defaultLineDays, 2, 120)
		if err != nil {
			return nil, "", err
		}
		sn, err := dashboard.StageNights(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Stages by night", func(w, h int) (*image.Gray, error) { return chart.StageNightsView(sn, w, h) })
		return img, fmt.Sprintf("sleep-stages-%dd", days), err

	case screen == scrWeeklyRhythm:
		days, err := qInt(q, "days", 365, 7, 3650)
		if err != nil {
			return nil, "", err
		}
		rh, err := dashboard.WeeklyRhythm(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Weekly rhythm", func(w, h int) (*image.Gray, error) { return chart.RhythmView(rh, w, h) })
		return img, fmt.Sprintf("weekly-rhythm-%dd", days), err

	case screen == scrSleepBreakdown:
		days, err := qInt(q, "days", defaultLineDays, 2, 3650)
		if err != nil {
			return nil, "", err
		}
		m, err := dashboard.SleepBreakdown(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("WolfeSense · Sleep", func(w, h int) (*image.Gray, error) { return chart.RadarView(m, w, h) })
		return img, fmt.Sprintf("sleep-breakdown-%dd", days), err

	case screen == scrReadinessParts:
		days, err := qInt(q, "days", defaultLineDays, 2, 3650)
		if err != nil {
			return nil, "", err
		}
		m, err := dashboard.ReadinessBreakdown(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("WolfeSense · Readiness", func(w, h int) (*image.Gray, error) { return chart.RadarView(m, w, h) })
		return img, fmt.Sprintf("readiness-breakdown-%dd", days), err
	}
	if screen == scrWeekReview || screen == scrThisWeek {
		start := dashboard.WeekStart(today).AddDate(0, 0, -7)
		if screen == scrThisWeek {
			start = dashboard.WeekStart(today)
		}
		start, err := weekOf(q, start)
		if err != nil {
			return nil, "", err
		}
		wr, err := dashboard.WeekReview(ctx, s.db, start, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Week in review", func(w, h int) (*image.Gray, error) { return chart.WeekView(wr, w, h) })
		return img, "week-" + start.Format("2006-01-02"), err
	}
	if screen == scrBodySignals {
		days, err := qInt(q, "days", 60, 14, 365)
		if err != nil {
			return nil, "", err
		}
		sig, err := dashboard.BodySignals(ctx, s.db, days, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Body signals", func(w, h int) (*image.Gray, error) { return chart.SignalsView(sig, w, h) })
		return img, fmt.Sprintf("body-signals-%dd", days), err
	}
	if screen == scrMonthReport || screen == scrLastMonth {
		month, err := monthOf(q, today, screen == scrLastMonth)
		if err != nil {
			return nil, "", err
		}
		mr, err := dashboard.MonthReport(ctx, s.db, month, today)
		if err != nil {
			return nil, "", err
		}
		img, err := draw("Monthly report", func(w, h int) (*image.Gray, error) { return chart.MonthReportView(mr, w, h) })
		return img, "month-" + month.Format("2006-01"), err
	}
	return nil, "", notFound(fmt.Errorf("no screen %q", screen))
}

// imageHandler serves one screen as an image.
func (s *server) imageHandler(screen string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.serveScreen(w, r, screen)
	}
}

// lineRedirect sends the retired per-metric trend charts (/line/{metric}.png)
// to the trends screen that replaced them, keeping ?days= when it fits.
func (s *server) lineRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/trends.png"
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d >= 14 && d <= 365 {
		target += "?days=" + strconv.Itoa(d)
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func (s *server) serveScreen(w http.ResponseWriter, r *http.Request, screen string) {
	width, height, err := size(r)
	if err != nil {
		writeErr(w, badRequest(err))
		return
	}
	img, name, err := s.render(r.Context(), screen, r.URL.Query(), width, height)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeImage(w, r, img, name)
}

// encodeImage encodes a chart for delivery. bits 0 is the full grayscale
// PNG; 1 or 2 is converted for e-ink (chart.ToEink) — Go's PNG encoder then
// writes a 1- or 2-bit indexed PNG, since the palette has 2 or 4 colors.
// format "bmp" is a 1-bit BMP, for displays that want one.
func encodeImage(img *image.Gray, bits int, format string) ([]byte, string, error) {
	var buf bytes.Buffer
	switch {
	case format == "bmp":
		if bits != 1 {
			return nil, "", badRequest(errors.New("format=bmp needs bits=1"))
		}
		p, err := chart.ToEink(img, 1)
		if err != nil {
			return nil, "", err
		}
		if err := chart.EncodeBMP1(&buf, p); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/bmp", nil
	case format != "" && format != "png":
		return nil, "", badRequest(errors.New("format must be png or bmp"))
	case bits == 0:
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
	default:
		p, err := chart.ToEink(img, bits)
		if err != nil {
			return nil, "", badRequest(err)
		}
		if err := png.Encode(&buf, p); err != nil {
			return nil, "", err
		}
	}
	return buf.Bytes(), "image/png", nil
}

// monthOf is the month a calendar shows: ?month=YYYY-MM, else today's
// month, or with last the month before it.
func monthOf(q url.Values, today time.Time, last bool) (time.Time, error) {
	if v := q.Get("month"); v != "" {
		m, err := time.Parse("2006-01", v)
		if err != nil {
			return time.Time{}, badRequest(errors.New("month must be YYYY-MM"))
		}
		return m, nil
	}
	if last {
		return time.Date(today.Year(), today.Month()-1, 1, 0, 0, 0, 0, time.UTC), nil
	}
	return today, nil
}

// weekOf is the Sunday a week screen starts: that of ?week=YYYY-MM-DD (any
// day in the week), else def.
func weekOf(q url.Values, def time.Time) (time.Time, error) {
	v := q.Get("week")
	if v == "" {
		return def, nil
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, badRequest(errors.New("week must be YYYY-MM-DD, any day in the week"))
	}
	return dashboard.WeekStart(d), nil
}

// Render draws a screen exactly as the server serves it, for the command
// line: screen is a schedule name ("daily", "wolfephase-month"), q the
// options a screen's URL takes (date, day, week, month, days, alert), and
// the name returned is the server's file name for the image.
func Render(ctx context.Context, db *store.DB, screen string, q url.Values, w, h int) (*image.Gray, string, error) {
	return newServer(db, &Tracker{}, Config{}).render(ctx, screen, q, w, h)
}
