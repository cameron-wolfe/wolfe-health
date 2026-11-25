package server

// The display emulator: a page that shows what a TRMNL would, for a desk
// without one. It shows /current.png converted as the TRMNL receives it
// (four grays by default, or whatever -trmnl-bits and -trmnl-format say),
// in a panel-shaped frame, and checks every minute for a new screen,
// swapping it in with an e-ink style flash only when the image changes.
//
// Three views: framed, as the device looks; edge to edge, the panel's
// 800x480 as large as the window allows; and fill screen, every screen
// drawn at the window's own size, so it fills a tablet whatever its shape
// (laid out for that size, so not pixel for pixel what a TRMNL shows).
//
// Added to an iPad's or iPhone's Home Screen, it opens full screen, as an
// app: the page names itself a standalone web app, with a manifest and an
// icon of the mark.
//
// Tapping the screen opens its detail views (details.go) as a strip of
// buttons along the bottom: tap one to show it, and its own details are a
// tap away in turn. Back steps out one; the daily brief, home, is a button
// on every screen's strip and in the caption, whatever the schedule shows;
// and after three minutes without a tap the page goes back to the
// schedule by itself. Some
// screens have zones (details.go's screenZones: the daily brief's
// readiness, last night and today) that a tap opens directly; the image's
// X-Zones header says where they are. A tap outside them opens the strip,
// and with the strip open, a tap anywhere but its buttons just closes it.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
)

// emulatorIcon is the Home Screen icon: the mark, white on the emulator's
// dark gray, at the size iOS asks for.
func (s *server) emulatorIcon(w http.ResponseWriter, r *http.Request) {
	const size = 180
	img := image.NewGray(image.Rect(0, 0, size, size))
	bg := color.Gray{Y: 0x27}
	for i := range img.Pix {
		img.Pix[i] = bg.Y
	}
	chart.DrawWolfMark(img, size*0.18, size*0.18, size*0.64, color.Gray{Y: 0xf4}, bg)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(b.Bytes())
}

// emulatorManifest names the emulator a full-screen web app, for a
// browser that installs it.
func (s *server) emulatorManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	json.NewEncoder(w).Encode(map[string]any{
		"name": "Wolfe Health display", "short_name": "Wolfe Health",
		"start_url": "/emulator", "scope": "/emulator", "display": "fullscreen",
		"orientation": "landscape", "background_color": "#26272b", "theme_color": "#26272b",
		"icons": []map[string]string{{"src": "/emulator/icon.png", "sizes": "180x180", "type": "image/png"}},
	})
}

// appManifest names the control panel a web app, so a phone, tablet or Mac
// can add it to its Home Screen or Dock and open it like one, with no
// browser bars.
func (s *server) appManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	json.NewEncoder(w).Encode(map[string]any{
		"name": "Wolfe Health", "short_name": "Wolfe Health",
		"start_url": "/", "scope": "/", "display": "standalone",
		"background_color": "#111111", "theme_color": "#111111",
		"icons": []map[string]string{{"src": "/emulator/icon.png", "sizes": "180x180", "type": "image/png"}},
	})
}

// emulatorNow is what the emulator's caption shows: the screen up now,
// ?advance screens on when Next screen has been pressed, and when the
// schedule next changes. slot names the schedule's own slot, so the page
// can tell when a scheduled change has come and drop its advance. details
// are the detail views a tap offers: those of ?show, the detail view open,
// or else of the screen up now.
func (s *server) emulatorNow(w http.ResponseWriter, r *http.Request) {
	advance, err := qInt(r.URL.Query(), "advance", 0, 0, 1000)
	if err != nil {
		writeErr(w, err)
		return
	}
	sl := s.schedule(r.Context()).at(s.now())
	up := sl.advanced(advance)
	from := up
	if show := r.URL.Query().Get("show"); show != "" {
		if !knownScreen(show) {
			writeErr(w, badRequest(fmt.Errorf("show: no screen %q", show)))
			return
		}
		from = show
	}
	showing := screenTitle(from)
	if date := r.URL.Query().Get("date"); date != "" {
		d, err := time.Parse("2006-01-02", date)
		if err != nil {
			writeErr(w, badRequest(fmt.Errorf("date: want YYYY-MM-DD, got %q", date)))
			return
		}
		showing += " · " + d.Format("Mon, Jan 2")
	}
	type link struct {
		Screen string `json:"screen"`
		Title  string `json:"title"`
	}
	details := []link{}
	for _, d := range detailsOf(from) {
		details = append(details, link{d, screenTitle(d)})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{
		"screen":    screenTitle(up),
		"scheduled": screenTitle(sl.Screen),
		"until":     sl.Until.Format("3:04 PM"),
		"slot":      fmt.Sprintf("%d %s", sl.Part, sl.Until.Format(time.RFC3339)),
		"showing":   showing,
		"name":      from,
		"home":      homeScreen,
		"homeTitle": screenTitle(homeScreen),
		"details":   details,
	})
}

func (s *server) emulator(w http.ResponseWriter, r *http.Request) {
	cfg := s.trmnlCfg
	cfg.defaults()
	base := fmt.Sprintf("/current.png?bits=%d", cfg.Bits)
	if cfg.Format == "bmp" {
		base += "&format=bmp"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := emulatorPage.Execute(w, map[string]any{"Base": base, "W": defaultW, "H": defaultH, "MaxSide": maxSide}); err != nil {
		writeErr(w, err)
	}
}

var emulatorPage = template.Must(template.New("emulator").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="apple-mobile-web-app-title" content="Wolfe Health">
<meta name="theme-color" content="#26272b">
<link rel="manifest" href="/emulator/manifest.webmanifest">
<link rel="apple-touch-icon" href="/emulator/icon.png">
<link rel="icon" type="image/png" href="/emulator/icon.png">
<title>Wolfe Health display</title>
<style>
  :root { --bg: #26272b; --bezel: #f4f3ef; --edge: #d9d7d0; --caption: #9a9ba1; }
  * { box-sizing: border-box; }
  html, body { height: 100%; margin: 0; }
  body {
    background: var(--bg); color: var(--caption);
    font: 12px -apple-system, BlinkMacSystemFont, sans-serif;
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 10px; overflow: hidden;
    /* clear of the status bar and rounded corners when full screen */
    padding: max(16px, env(safe-area-inset-top)) max(16px, env(safe-area-inset-right))
      max(16px, env(safe-area-inset-bottom)) max(16px, env(safe-area-inset-left));
  }
  .bezel {
    background: var(--bezel); border-radius: 14px; padding: 4.5% 4.5% 6%;
    box-shadow: 0 1px 0 #fff inset, 0 0 0 1px var(--edge), 0 18px 40px rgba(0,0,0,.45);
  }
  .panel { position: relative; aspect-ratio: {{.W}} / {{.H}}; background: #e9e8e3; overflow: hidden;
    box-shadow: 0 0 0 1px #c9c7c0; }
  .panel img { position: absolute; inset: 0; width: 100%; height: 100%; image-rendering: pixelated; }
  /* paper look: e-ink white is a warm light gray and its black a soft charcoal */
  .paper .panel img { filter: contrast(.86) brightness(.97) sepia(.08); }
  .flash { position: absolute; inset: 0; background: #111; opacity: 0; pointer-events: none; }
  .flash.on { animation: flash .7s steps(1) forwards; }
  @keyframes flash { 0% { opacity: 1; background: #111; } 35% { opacity: 1; background: #f4f4f0; } 70% { opacity: 0; } }
  .caption { display: flex; gap: 14px; align-items: center; }
  .caption b { color: #d6d7db; font-weight: 600; }
  button { font: inherit; color: var(--caption); background: none; border: 1px solid #44464c;
    border-radius: 6px; padding: 2px 8px; cursor: pointer; }
  button:hover { color: #d6d7db; }
  .err { color: #e8a36b; }
  .panel { cursor: pointer; -webkit-tap-highlight-color: transparent; }
  /* the detail strip: the screen's closer looks, over its bottom edge */
  .strip { position: absolute; left: 0; right: 0; bottom: 0; display: none; flex-wrap: wrap;
    gap: 8px; justify-content: center; padding: 12px; background: rgba(28,29,32,.92); }
  .strip.on { display: flex; }
  .strip button { color: #f4f4f0; border-color: #6a6c73; background: #34363b;
    padding: 10px 16px; font-size: 15px; border-radius: 8px; }
  .strip button.nav { background: none; }
  .strip .none { color: #c6c7cb; padding: 10px; font-size: 14px; }
  #next { color: #d6d7db; border-color: #6a6c73; }
  /* on a tablet, buttons big enough to tap */
  @media (pointer: coarse) { button { padding: 8px 14px; font-size: 13px; } }
  /* no frame: the panel edge to edge, for a tablet on a desk; the default
     when opened from the Home Screen */
  body.full { gap: 6px;
    padding: max(6px, env(safe-area-inset-top)) max(6px, env(safe-area-inset-right))
      max(6px, env(safe-area-inset-bottom)) max(6px, env(safe-area-inset-left)); }
  .full .bezel { background: none; padding: 0; border-radius: 0; box-shadow: none; }
  .full .panel { border-radius: 4px; box-shadow: none; }
  .full .caption { font-size: 11px; opacity: .8; }
  .scaled .panel img { image-rendering: auto; }
  /* a short screen (a phone on its side): one compact caption line, so the
     panel gets the height; after the tablet rule above, so it wins */
  .caption #now { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  @media (max-height: 500px) {
    body, body.full { gap: 4px;
      padding: max(4px, env(safe-area-inset-top)) max(8px, env(safe-area-inset-right))
        max(4px, env(safe-area-inset-bottom)) max(8px, env(safe-area-inset-left)); }
    .caption, .full .caption { font-size: 10px; gap: 8px; max-width: 100%; flex-wrap: nowrap; }
    .caption #checked { display: none; }
    .caption button { padding: 3px 8px; font-size: 11px; white-space: nowrap; flex: none; }
    .bezel { padding: 2.5% 2.5% 3%; border-radius: 10px; }
    .strip { gap: 6px; padding: 8px; }
    .strip button { padding: 6px 10px; font-size: 13px; }
  }
  /* a narrow screen (a phone upright): the caption wraps, centered */
  @media (max-width: 600px) {
    .caption { flex-wrap: wrap; justify-content: center; row-gap: 4px; }
  }
</style>
</head>
<body class="paper">
<div class="bezel"><div class="panel">
  <img id="screen" alt="The display's current screen" width="{{.W}}" height="{{.H}}">
  <div class="flash" id="flash"></div>
  <div class="strip" id="strip"></div>
</div></div>
<div class="caption">
  <span id="now">Loading…</span>
  <span id="checked"></span>
  <button id="back" type="button" hidden title="Step back out of this detail view">‹ Back</button>
  <button id="home" type="button" hidden title="Show the daily brief, home, whatever the schedule shows">Daily brief</button>
  <button id="next" type="button" title="Show the next screen in the rotation now; the schedule takes over again at its next change">Next screen ›</button>
  <button id="look" type="button" title="Switch between the paper look and the image's exact pixels">Exact pixels</button>
  <button id="view" type="button" title="Framed, as the device looks; edge to edge, the panel's 800×480 as large as it fits; or fill screen, drawn at this screen's own size">Framed</button>
</div>
<script>
const base = {{.Base}};
const img = document.getElementById("screen");
const flash = document.getElementById("flash");
const nowEl = document.getElementById("now");
const checked = document.getElementById("checked");
const look = document.getElementById("look");
let etag = null, url = null;
// Next screen: how many screens past the schedule's to show, and the
// schedule slot that count belongs to; a scheduled change resets it
let advance = 0, slot = null;
let seq = 0; // each update's number: only the latest may change the screen
// detail views tapped open, outermost first; the last is on screen. Empty:
// the schedule's screen. Left alone for idleMs, the page goes home.
// each view is {screen, date}: date (YYYY-MM-DD) for a view as of a past
// day, as a week's day card opens, and inherited by the views tapped open
// from it
let stack = [], details = [], zones = [], touched = Date.now();
let onScreen = null, home = "daily", homeTitle = "Daily brief"; // what's up, and home
const idleMs = 3 * 60 * 1000;
const shown = () => stack.length ? stack[stack.length - 1] : null;
// ?show=<screen> opens on that screen, as if tapped open from the schedule's
{ const p = new URLSearchParams(location.search), s = p.get("show"); if (s) stack = [{ screen: s, date: p.get("date") || "" }]; }
// viewQuery is a view's show= and date= parameters
const viewQuery = v => "&show=" + encodeURIComponent(v.screen) + (v.date ? "&date=" + v.date : "");
// from: a new view opened from the one up now, keeping its date
const from = (screen, date) => ({ screen, date: date || (shown() || {}).date || "" });

function setLook(paper) {
  document.body.classList.toggle("paper", paper);
  look.textContent = paper ? "Exact pixels" : "Paper look";
  try { localStorage.setItem("emulatorPaper", paper ? "1" : "0"); } catch (e) {}
}
let paper = true;
try { paper = localStorage.getItem("emulatorPaper") !== "0"; } catch (e) {}
setLook(paper);
look.onclick = () => setLook(!document.body.classList.contains("paper"));

// the view: framed in a browser, filling the screen from the Home Screen;
// either way the choice is remembered
const views = ["framed", "edge", "fill"];
const viewNames = { framed: "Framed", edge: "Edge to edge", fill: "Fill screen" };
const viewBtn = document.getElementById("view");
const bezel = document.querySelector(".bezel");
const panel = document.querySelector(".panel");
const caption = document.querySelector(".caption");
let view = "framed", size = { w: {{.W}}, h: {{.H}} };

// fillSize is the room the panel has in fill-screen view, in CSS pixels:
// the window less its padding and the caption
function fillSize() {
  const cs = getComputedStyle(document.body);
  const px = k => parseFloat(cs[k]) || 0;
  const w = document.body.clientWidth - px("paddingLeft") - px("paddingRight");
  const h = document.body.clientHeight - px("paddingTop") - px("paddingBottom") - caption.offsetHeight - px("rowGap");
  return { w: Math.max(1, Math.floor(w)), h: Math.max(1, Math.floor(h)) };
}
// drawSize is the size to draw at for a box: the box itself, or, where it's
// smaller than every screen can draw at (a phone on its side is about 350
// tall), the same shape scaled up to that, shown scaled down to the box
function drawSize(box) {
  const s = Math.max(1, 640 / box.w, 384 / box.h);
  return { w: Math.min({{.MaxSide}}, Math.round(box.w * s)), h: Math.min({{.MaxSide}}, Math.round(box.h * s)) };
}
// layout sizes the panel to the room the page has, measured rather than
// taken from CSS viewport units, which phones report unevenly: fill screen
// takes the whole box (drawn at that shape); edge to edge and framed fit
// the display's own shape into it, framed with room for its bezel.
function layout() {
  const box = fillSize();
  const aspect = {{.W}} / {{.H}};
  let w;
  if (view === "fill" && box.w / box.h >= 1.25) {
    w = box.w;
    size = drawSize(box);
    panel.style.aspectRatio = box.w + " / " + box.h;
  } else {
    // the display's shape; fill screen too on a portrait screen, where its
    // own shape would draw every screen tall and thin
    size = view === "fill" ? drawSize({ w: {{.W}}, h: {{.H}} }) : { w: {{.W}}, h: {{.H}} };
    panel.style.aspectRatio = "";
    if (view === "framed") {
      // the bezel's padding is a share of its width: its sides each take
      // side, its top and bottom together ends, so its height is
      // (1 - 2·side)·w / aspect + ends·w
      const p = getComputedStyle(bezel), pw = v => parseFloat(p[v]) / bezel.offsetWidth || 0;
      const side = pw("paddingLeft"), ends = pw("paddingTop") + pw("paddingBottom");
      w = Math.min(box.w, box.h / ((1 - 2 * side) / aspect + ends));
    } else {
      w = Math.min(box.w, box.h * aspect);
    }
  }
  bezel.style.width = Math.floor(w) + "px";
  // drawn larger than shown: smooth it, rather than dropping whole pixels
  document.body.classList.toggle("scaled", size.w > w + 1);
}
function setView(v) {
  view = views.includes(v) ? v : "framed";
  document.body.classList.toggle("full", view !== "framed");
  viewBtn.textContent = viewNames[view];
  try { localStorage.setItem("emulatorView", view); } catch (e) {}
  const before = size.w + "x" + size.h;
  layout();
  if (size.w + "x" + size.h !== before) { etag = null; update(); return true; } // a new size: redraw, no flash
  return false;
}
const installed = navigator.standalone === true ||
  matchMedia("(display-mode: standalone), (display-mode: fullscreen)").matches;
let saved = null;
try { saved = localStorage.getItem("emulatorView"); } catch (e) {}
viewBtn.onclick = () => setView(views[(views.indexOf(view) + 1) % views.length]);
// turning the tablet, or resizing the window, redraws at the new size
let resizing;
addEventListener("resize", () => {
  clearTimeout(resizing);
  resizing = setTimeout(() => {
    const before = size.w + "x" + size.h;
    layout();
    if (size.w + "x" + size.h !== before) { etag = null; update(); }
  }, 300);
});

async function update() {
  const mine = ++seq;
  try {
    // the schedule first: a scheduled change since the last look ends
    // any skipping ahead
    const show = shown();
    const now = await (await fetch("/emulator/now?advance=" + advance + (show ? viewQuery(show) : ""), { cache: "no-store" })).json();
    if (mine !== seq) return; // a newer update (another tap) has started
    if (slot !== null && now.slot !== slot && advance > 0) {
      advance = 0;
      return update();
    }
    slot = now.slot;
    details = now.details || [];
    onScreen = now.name; home = now.home || home; homeTitle = now.homeTitle || homeTitle;
    const res = await fetch(base + "&w=" + size.w + "&h=" + size.h +
      (show ? viewQuery(show) : advance ? "&advance=" + advance : ""), { cache: "no-cache" });
    if (!res.ok) throw new Error("the server answered " + res.status);
    const tag = res.headers.get("ETag");
    if (mine !== seq) return;
    try { zones = JSON.parse(res.headers.get("X-Zones") || "[]"); } catch (e) { zones = []; }
    if (tag === null || tag !== etag) {
      const blob = await res.blob();
      if (mine !== seq) return;
      const next = URL.createObjectURL(blob);
      if (etag !== null) { // a new screen: flash as a panel does on a full refresh
        flash.classList.remove("on"); void flash.offsetWidth; flash.classList.add("on");
        await new Promise(r => setTimeout(r, 250));
      }
      img.src = next;
      if (url) URL.revokeObjectURL(url);
      url = next; etag = tag;
    }
    nowEl.innerHTML = "";
    const b = document.createElement("b");
    if (show) {
      b.textContent = now.showing;
      nowEl.append(b, " · detail of " + now.screen + "; home after 3 minutes untouched");
    } else {
      b.textContent = now.screen;
      nowEl.append(b, advance ? " · skipped ahead; back to the schedule at " + now.until : " until " + now.until,
        details.length ? " · tap for details" : "");
    }
    backBtn.hidden = !show;
    homeBtn.hidden = onScreen === home && !(show && show.date);
    checked.className = "";
    checked.textContent = "· checked " + new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  } catch (e) {
    checked.className = "err";
    checked.textContent = "· can't reach the server, retrying (" + e.message + ")";
  }
}
// the caption's first text can change its height: fit again once loaded
addEventListener("load", () => layout());
// ?view=framed|edge|fill opens in that view, for this visit
const asked = new URLSearchParams(location.search).get("view");
if (!setView(views.includes(asked) ? asked : saved || (installed ? "fill" : "framed"))) update();
setInterval(update, 60 * 1000);
document.getElementById("next").onclick = () => { advance++; stack = []; hideStrip(); update(); };

// detail views: a tap on the screen opens the strip; a choice shows it
const strip = document.getElementById("strip");
const backBtn = document.getElementById("back");
const homeBtn = document.getElementById("home");
let stripTimer;
function hideStrip() { strip.classList.remove("on"); clearTimeout(stripTimer); }
function go(next) { touched = Date.now(); stack = next; hideStrip(); update(); }
function button(text, cls, act) {
  const el = document.createElement("button");
  el.type = "button"; el.textContent = text; el.className = cls;
  el.onclick = e => { e.stopPropagation(); act(); };
  return el;
}
function showStrip() {
  strip.innerHTML = "";
  if (stack.length) {
    strip.append(button("‹ Back", "nav", () => go(stack.slice(0, -1))));
  }
  for (const d of details) {
    strip.append(button(d.title, "", () => go([...stack, from(d.screen)])));
  }
  if (!details.length) {
    const none = document.createElement("span");
    none.className = "none"; none.textContent = "No detail views for this screen";
    strip.append(none);
  }
  if (onScreen !== home || (shown() && shown().date)) { // home, today's, is always a tap away
    strip.append(button("⌂ " + homeTitle, "nav", goHome));
  }
  strip.classList.add("on");
  clearTimeout(stripTimer);
  stripTimer = setTimeout(hideStrip, 15 * 1000); // not chosen: get out of the way
}
panel.addEventListener("click", e => {
  touched = Date.now();
  if (strip.classList.contains("on")) { hideStrip(); return; } // the strip first: a tap off it closes it
  // a tap in a zone opens its view directly; anywhere else, the strip
  const r = img.getBoundingClientRect();
  const fx = (e.clientX - r.left) / r.width, fy = (e.clientY - r.top) / r.height;
  const z = zones.find(z => fx >= z.x && fx <= z.x + z.w && fy >= z.y && fy <= z.y + z.h);
  z ? go([...stack, from(z.screen, z.date)]) : showStrip();
});
backBtn.onclick = () => go(stack.slice(0, -1));
homeBtn.onclick = () => goHome();
// home: the daily brief, on its own (Back from it returns to the
// schedule), or the schedule itself when that's what it shows
function goHome() { go(stack.length === 0 && onScreen === home ? [] : [{ screen: home, date: "" }]); }
// untouched for a while: back to the schedule
setInterval(() => {
  if (stack.length && Date.now() - touched > idleMs) go([]);
}, 15 * 1000);
document.addEventListener("visibilitychange", () => { if (!document.hidden) update(); });
</script>
</body>
</html>
`))
