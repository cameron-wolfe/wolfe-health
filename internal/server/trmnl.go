package server

// TRMNL "bring your own server" support: the endpoints a TRMNL e-ink device
// calls when pointed at this server during its Wi-Fi setup, per
// https://docs.trmnl.com/go/diy/byos.
//
//	GET  /api/setup    once after Wi-Fi setup: trade the device's MAC for a key
//	GET  /api/display  every wake: which image to show, and how long to sleep
//	POST /api/log      when something goes wrong on the device
//
// The device saves each image to flash under the response's filename and
// downloads only names it hasn't seen, so filenames here change exactly when
// the image does.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// TRMNLConfig sets what the display endpoint serves.
type TRMNLConfig struct {
	Bits    int           // 1 (black and white) or 2 (four grays); 2 needs OG firmware 1.6+
	Format  string        // "png" or "bmp" (BMP is always 1-bit)
	Refresh time.Duration // how long the device sleeps between requests
}

func (c *TRMNLConfig) defaults() {
	if c.Bits == 0 {
		c.Bits = 2
	}
	if c.Format == "" {
		c.Format = "png"
	}
	if c.Format == "bmp" {
		c.Bits = 1
	}
	if c.Refresh <= 0 {
		c.Refresh = 15 * time.Minute
	}
}

// maxImageBytes is the largest image the TRMNL OG accepts.
const maxImageBytes = 90_000

// DeviceLog is one message a device reported through /api/log.
type DeviceLog struct {
	At      time.Time
	MAC     string
	Message string
}

// trmnlState holds the images handed to devices, and their recent logs.
type trmnlState struct {
	mu        sync.Mutex
	images    map[string]cachedImage // by filename
	order     []string               // filenames, oldest first, for pruning
	firstSeen map[string]int64       // screen+hash -> when that image was first made
	logs      []DeviceLog            // most recent last
	steps     map[string]*buttonStep // by MAC: where each device's button has stepped it
}

// buttonStep is how far a device's button has stepped it through its
// screen's detail views (detailStep), and when it was told to wake next.
//
// A TRMNL doesn't say why it woke, but a press wakes it before the time it
// was given, and nothing else does: so a request well before that time is
// a press. Each press steps on; ten minutes after the last, or when the
// schedule moves on, it's back to the main screen.
type buttonStep struct {
	step    int
	pressed time.Time // the last press
	part    string    // the schedule slot the steps belong to
	expect  time.Time // when the device was told to wake
	slept   time.Duration
}

// detailHold is how long a stepped-to detail view stays before the device
// goes back to its main screen.
const detailHold = 10 * time.Minute

// press reports where a device that has just woken should be: the step
// through its detail views, and whether this wake was a button press.
func (t *trmnlState) press(mac, part string, now time.Time) (step int, pressed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.steps[mac]
	if st == nil {
		st = &buttonStep{}
		t.steps[mac] = st
	}
	// early by more than a timing wobble: a third of the sleep, at most
	// two minutes (so a one-minute sleep still counts a press at 40s)
	margin := min(2*time.Minute, st.slept/3)
	pressed = !st.expect.IsZero() && now.Before(st.expect.Add(-margin))
	switch {
	case pressed && st.part != part: // a press in a new slot starts over
		st.step = 1
	case pressed:
		st.step++
	case st.part != part || now.Sub(st.pressed) >= detailHold:
		st.step = 0
	}
	if pressed {
		st.pressed = now
	}
	st.part = part
	return st.step, pressed
}

// slept records how long a device was told to sleep.
func (t *trmnlState) sleep(mac string, now time.Time, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st := t.steps[mac]; st != nil {
		st.expect, st.slept = now.Add(d), d
	}
}

type cachedImage struct {
	data  []byte
	ctype string
}

const (
	keepImages = 16
	keepLogs   = 50
)

func newTRMNLState() *trmnlState {
	return &trmnlState{images: map[string]cachedImage{}, firstSeen: map[string]int64{}, steps: map[string]*buttonStep{}}
}

// filename names an image for the device's cache: <screen>-<hash>-<unix>,
// where the hash is of the image bytes and the time is when that exact image
// was first made. The same image always gets the same name, so the device
// skips the download; any change in the image changes the name. At most 26
// characters, inside the device's 31 — longer names get truncated and can
// collide.
func (t *trmnlState) filename(screen string, data []byte, now time.Time) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:3])
	short := screen
	if len(short) > 8 {
		short = short[:8]
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := screen + "-" + hash
	first, ok := t.firstSeen[key]
	if !ok {
		first = now.Unix()
		t.firstSeen[key] = first
	}
	return fmt.Sprintf("%s-%s-%d", short, hash, first)
}

func (t *trmnlState) put(name string, img cachedImage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.images[name]; ok {
		return
	}
	t.images[name] = img
	t.order = append(t.order, name)
	for len(t.order) > keepImages {
		delete(t.images, t.order[0])
		t.order = t.order[1:]
	}
}

func (t *trmnlState) get(name string) (cachedImage, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	img, ok := t.images[name]
	return img, ok
}

func (t *trmnlState) addLog(l DeviceLog) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, l)
	if len(t.logs) > keepLogs {
		t.logs = t.logs[len(t.logs)-keepLogs:]
	}
}

func (t *trmnlState) recentLogs(n int) []DeviceLog {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := max(0, len(t.logs)-n)
	out := make([]DeviceLog, len(t.logs)-start)
	copy(out, t.logs[start:])
	return out
}

// baseURL is how the device reached this server, which is the address its
// image URLs must use — never localhost.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// friendlyID is a short, stable ID for a MAC, shown on the setup screen.
func friendlyID(mac string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(mac)))
	return strings.ToUpper(hex.EncodeToString(sum[:3]))
}

func randomKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// trmnlSetup pairs a device: any device that asks gets a key (this is a
// home server), and asking again returns the same key.
func (s *server) trmnlSetup(w http.ResponseWriter, r *http.Request) {
	mac := strings.TrimSpace(r.Header.Get("ID"))
	if mac == "" {
		writeJSON(w, map[string]any{"status": 404, "message": "missing ID header"})
		return
	}
	dv, ok, err := s.db.DeviceByMAC(r.Context(), mac)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		dv = store.Device{MAC: mac, APIKey: randomKey(), FriendlyID: friendlyID(mac), CreatedAt: s.now()}
		log.Printf("trmnl: paired new device %s (%s)", dv.FriendlyID, mac)
	}
	noteHeaders(&dv, r)
	if err := s.db.SaveDevice(r.Context(), dv); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"status":      200,
		"api_key":     dv.APIKey,
		"friendly_id": dv.FriendlyID,
		"image_url":   baseURL(r) + "/trmnl/setup.png?id=" + dv.FriendlyID,
		"message":     "Connected to Wolfe Health as " + dv.FriendlyID,
	})
}

// noteHeaders records what a device reports about itself on each request.
func noteHeaders(dv *store.Device, r *http.Request) {
	if v := r.Header.Get("Model"); v != "" {
		dv.Model = v
	}
	if v := r.Header.Get("FW-Version"); v != "" {
		dv.Firmware = v
	}
	if v, err := strconv.ParseFloat(r.Header.Get("Battery-Voltage"), 64); err == nil {
		dv.Battery = &v
	}
	if v, err := strconv.Atoi(r.Header.Get("RSSI")); err == nil {
		dv.RSSI = &v
	}
	if v, err := strconv.Atoi(r.Header.Get("Width")); err == nil && v > 0 && v <= maxSide {
		dv.Width = &v
	}
	if v, err := strconv.Atoi(r.Header.Get("Height")); err == nil && v > 0 && v <= maxSide {
		dv.Height = &v
	}
}

// trmnlDisplay answers a device's wake: render the scheduled screen at its
// size, hand back a cache-correct filename and URL, and say when to wake —
// no later than the next scheduled change, so screens switch on time.
//
// It never sends reset_firmware: true, which wipes the device's Wi-Fi and
// key. A device this server doesn't know, or with a key it doesn't match —
// after the database was replaced, say — is adopted with the key it sent.
func (s *server) trmnlDisplay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mac := strings.TrimSpace(r.Header.Get("ID"))
	token := r.Header.Get("Access-Token")
	if mac == "" {
		writeJSON(w, map[string]any{"status": 404, "message": "missing ID header"})
		return
	}
	dv, ok, err := s.db.DeviceByMAC(ctx, mac)
	if err != nil {
		serverError(w, err)
		return
	}
	switch {
	case !ok && token == "":
		writeJSON(w, map[string]any{"status": 202}) // not set up yet: the device retries shortly
		return
	case !ok:
		dv = store.Device{MAC: mac, APIKey: token, FriendlyID: friendlyID(mac), CreatedAt: s.now()}
		log.Printf("trmnl: adopted unknown device %s (%s) with the key it sent", dv.FriendlyID, mac)
	case token != "" && token != dv.APIKey:
		log.Printf("trmnl: device %s sent a different key; adopting it", dv.FriendlyID)
		dv.APIKey = token
	}
	noteHeaders(&dv, r)
	width, height := defaultW, defaultH
	if dv.Width != nil && dv.Height != nil {
		width, height = *dv.Width, *dv.Height
	}

	// the schedule's screen, or a detail view the button has stepped to
	now := s.now()
	sl := s.schedule(ctx).at(now)
	step, pressed := s.trmnl.press(mac, fmt.Sprintf("%d %s", sl.Part, sl.Until.Format(time.RFC3339)), now)
	target := detailStep(sl.Screen, step)
	img, screen, err := s.displayImage(ctx, target, "", width, height)
	if err != nil {
		serverError(w, err)
		return
	}
	if pressed {
		log.Printf("trmnl: %s button: showing %s (step %d from %s)", dv.FriendlyID, screen, step, sl.Screen)
	}
	cfg := s.trmnlCfg
	data, ctype, err := encodeImage(img, cfg.Bits, cfg.Format)
	if err == nil && len(data) > maxImageBytes && cfg.Bits == 2 {
		data, ctype, err = encodeImage(img, 1, cfg.Format) // too big in four grays: fall back to two
	}
	if err != nil {
		serverError(w, err)
		return
	}
	name := s.trmnl.filename(screen, data, s.now())
	s.trmnl.put(name, cachedImage{data, ctype})
	ext := ".png"
	if ctype == "image/bmp" {
		ext = ".bmp"
	}

	dv.LastScreen, dv.LastSeen = screen, s.now()
	if err := s.db.SaveDevice(ctx, dv); err != nil {
		serverError(w, err)
		return
	}
	refresh := refreshFor(cfg.Refresh, sl.Until.Sub(now))
	if step > 0 { // stepped: wake when the detail's time is up, to go back
		refresh = min(refresh, int((detailHold + 30*time.Second).Seconds()))
	}
	s.trmnl.sleep(mac, now, time.Duration(refresh)*time.Second)
	writeJSON(w, map[string]any{
		"status":           0,
		"image_url":        baseURL(r) + "/trmnl/img/" + name + ext,
		"filename":         name,
		"refresh_rate":     refresh, // a number: a string reads as 0
		"reset_firmware":   false,
		"update_firmware":  false,
		"firmware_url":     nil,
		"special_function": "none",
	})
}

// refreshFor is how many seconds a device should sleep: its usual interval,
// cut short to wake just after the next scheduled screen change (a little
// late is better than early, which would show the old screen again), and
// never under a minute.
func refreshFor(usual, untilChange time.Duration) int {
	d := usual
	if c := untilChange + 30*time.Second; c < d {
		d = c
	}
	return int(max(d, time.Minute).Seconds())
}

// trmnlImage serves an image handed out by trmnlDisplay. Content-Type tells
// the device which decoder to use, and Content-Length is required.
func (s *server) trmnlImage(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	name := strings.TrimSuffix(strings.TrimSuffix(file, ".png"), ".bmp")
	img, ok := s.trmnl.get(name)
	if !ok {
		http.NotFound(w, r) // e.g. after a restart; the next wake asks for a fresh one
		return
	}
	w.Header().Set("Content-Type", img.ctype)
	w.Header().Set("Content-Length", strconv.Itoa(len(img.data)))
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable") // a name is one image forever
	w.Write(img.data)
}

// trmnlSetupImage is the branded screen shown when a device pairs.
func (s *server) trmnlSetupImage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if len(id) > 12 {
		id = id[:12]
	}
	img, err := chart.SetupScreen(id, defaultW, defaultH)
	if err != nil {
		serverError(w, err)
		return
	}
	data, ctype, err := encodeImage(img, s.trmnlCfg.Bits, s.trmnlCfg.Format)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// trmnlLog records what a device reports. The body is {"logs": [...]}; the
// entries' shape isn't specified, so each is kept as compact JSON.
func (s *server) trmnlLog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Logs []json.RawMessage `json:"logs"`
	}
	mac := r.Header.Get("ID")
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		http.Error(w, "bad log body", http.StatusBadRequest)
		return
	}
	for _, raw := range body.Logs {
		msg := string(raw)
		var entry map[string]any
		if json.Unmarshal(raw, &entry) == nil {
			if m, ok := entry["message"].(string); ok {
				msg = m
			}
		}
		log.Printf("trmnl: log from %s: %s", mac, msg)
		s.trmnl.addLog(DeviceLog{At: s.now(), MAC: mac, Message: msg})
	}
	writeJSON(w, map[string]any{"status": 200})
}
