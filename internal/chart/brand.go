package chart

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// The Wolfe Health mark: a geometric wolf head, front on — pointed ears,
// a broad brow, cheeks flaring out, and a muzzle narrowing to the chin. It
// is defined once, in a unit square (x right, y down), and drawn solid at
// any size, so it stays crisp on a 1-bit panel and identical everywhere it
// appears, including the index page (WolfMarkSVG).
var wolfHead = [][2]float64{
	{0.12, 0.02}, // left ear tip
	{0.36, 0.26}, // left inner ear base
	{0.50, 0.22}, // crown
	{0.64, 0.26}, // right inner ear base
	{0.88, 0.02}, // right ear tip
	{0.92, 0.38}, // right temple
	{1.00, 0.50}, // right ruff: two tufts of cheek fur, the wolf's tell
	{0.91, 0.56},
	{0.98, 0.68},
	{0.76, 0.72},
	{0.64, 0.90}, // a broad muzzle...
	{0.56, 0.98}, // ...with a flat chin, not a fox's point
	{0.44, 0.98},
	{0.36, 0.90},
	{0.24, 0.72},
	{0.02, 0.68}, // left ruff
	{0.09, 0.56},
	{0.00, 0.50},
	{0.08, 0.38}, // left temple
}

// Features cut out of the head in the background color.
var wolfCuts = [][][2]float64{
	{{0.17, 0.13}, {0.31, 0.27}, {0.20, 0.31}},               // left inner ear
	{{0.83, 0.13}, {0.69, 0.27}, {0.80, 0.31}},               // right inner ear
	{{0.25, 0.46}, {0.44, 0.52}, {0.31, 0.56}},               // left eye, angled
	{{0.75, 0.46}, {0.56, 0.52}, {0.69, 0.56}},               // right eye
	{{0.50, 0.32}, {0.53, 0.44}, {0.50, 0.56}, {0.47, 0.44}}, // forehead facet
	{{0.42, 0.84}, {0.58, 0.84}, {0.50, 0.92}},               // nose
}

// DrawWolfMark draws the mark with its top-left at (x, y), size pixels
// square, in fg on whatever is beneath; the cut-out features take bg.
func DrawWolfMark(img *image.Gray, x, y, size float64, fg, bg color.Gray) {
	scale := func(pts [][2]float64) [][2]float64 {
		out := make([][2]float64, len(pts))
		for i, p := range pts {
			out[i] = [2]float64{x + p[0]*size, y + p[1]*size}
		}
		return out
	}
	fillPolygon(img, scale(wolfHead), fg)
	for _, c := range wolfCuts {
		fillPolygon(img, scale(c), bg)
	}
}

// DrawWolfOutline draws the mark as an outline — its edge and the cut-out
// features traced in lines w pixels wide — centered at (cx, cy), size
// pixels square: a watermark that sits behind a chart's data.
func DrawWolfOutline(img *image.Gray, cx, cy, size, w float64, c color.Gray) {
	x, y := cx-size/2, cy-size/2
	trace := func(pts [][2]float64) {
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			thickLine(img, x+a[0]*size, y+a[1]*size, x+b[0]*size, y+b[1]*size, w, c)
		}
	}
	trace(wolfHead)
	for _, cut := range wolfCuts {
		trace(cut)
	}
}

// WolfMarkSVG is the mark as an inline SVG, for the index page.
func WolfMarkSVG(size int, fg, bg string) string {
	path := func(pts [][2]float64) string {
		var b strings.Builder
		for i, p := range pts {
			if i == 0 {
				b.WriteString("M")
			} else {
				b.WriteString(" L")
			}
			fmt.Fprintf(&b, "%.1f %.1f", p[0]*100, p[1]*100)
		}
		return b.String() + " Z"
	}
	var cuts strings.Builder
	for _, c := range wolfCuts {
		cuts.WriteString(path(c) + " ")
	}
	// fills go in style attributes, which (unlike fill="...") accept CSS
	// variables, so the cut-outs can follow a page's light or dark background
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 100 100" aria-hidden="true">`+
		`<path d="%s" style="fill:%s"/><path d="%s" style="fill:%s"/></svg>`,
		size, size, path(wolfHead), fg, strings.TrimSpace(cuts.String()), bg)
}

// fillPolygon fills a simple polygon by scanlines, sampling pixel centers
// with the even-odd rule: a pixel is either inside or not, no anti-aliasing.
func fillPolygon(img *image.Gray, pts [][2]float64, c color.Gray) {
	if len(pts) < 3 {
		return
	}
	minY, maxY := pts[0][1], pts[0][1]
	for _, p := range pts {
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	var xs []float64
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		cy := float64(y) + 0.5
		xs = xs[:0]
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= cy) != (b[1] <= cy) {
				xs = append(xs, a[0]+(cy-a[1])/(b[1]-a[1])*(b[0]-a[0]))
			}
		}
		// insertion sort: a handful of crossings per row
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
			}
		}
		for i := 0; i+1 < len(xs); i += 2 {
			for x := int(math.Ceil(xs[i] - 0.5)); float64(x)+0.5 < xs[i+1]; x++ {
				img.SetGray(x, y, c)
			}
		}
	}
}

// drawTracked draws s with extra space between letters — the open, spaced
// capitals of a wordmark — and returns the x where it ends.
func drawTracked(img *image.Gray, f font.Face, x, y int, s string, tracking float64, c color.Gray) int {
	d := &font.Drawer{Dst: img, Src: &image.Uniform{C: c}, Face: f}
	pos := fixed.I(x)
	for i, r := range s {
		if i > 0 {
			pos += fixed.Int26_6(tracking * 64)
		}
		d.Dot = fixed.Point26_6{X: pos, Y: fixed.I(y)}
		d.DrawString(string(r))
		adv, _ := f.GlyphAdvance(r)
		pos += adv
	}
	return pos.Round()
}

func trackedWidth(f font.Face, s string, tracking float64) int {
	var w fixed.Int26_6
	for i, r := range s {
		if i > 0 {
			w += fixed.Int26_6(tracking * 64)
		}
		adv, _ := f.GlyphAdvance(r)
		w += adv
	}
	return w.Round()
}

// mastheadHeight is the band's height at a canvas size: 30px at 800x480,
// scaling with the canvas like the daily layout, never under 22px.
func mastheadHeight(width, height int) int {
	k := math.Min(float64(width)/800, float64(height)/480)
	return max(22, int(math.Round(30*k)))
}

// Branded renders a chart beneath the Wolfe Health masthead: a solid black
// band across the top with the wolf mark and wordmark in white on the left
// and the screen's name on the right. The chart is rendered into the space
// below the band at the full width, so every chart carries the brand without
// its own layout having to know about it.
func Branded(screen string, width, height int, render func(w, h int) (*image.Gray, error)) (*image.Gray, error) {
	return BrandedAlert(screen, "", width, height, render)
}

// BrandedAlert is Branded with an early-warning banner under the masthead
// when alert is non-empty; with an empty alert it is exactly Branded. The
// banner's height comes out of the chart's, like the masthead's.
func BrandedAlert(screen, alert string, width, height int, render func(w, h int) (*image.Gray, error)) (*image.Gray, error) {
	return BrandedFrame(screen, alert, Footer{}, width, height, render)
}

// Footer is the status line along the bottom of every screen: the ring's
// state on the left, when the data was last updated on the right.
type Footer struct {
	Left, Right string
	Warn        bool // Left needs attention (a low battery): drawn bold, in ink
}

// BrandedFrame is BrandedAlert with a footer along the bottom, when the
// footer has text and the canvas has room for it as well as the chart: if
// the chart can't fit above the footer, it's drawn without one, so the
// footer never costs a screen that would otherwise render.
func BrandedFrame(screen, alert string, foot Footer, width, height int, render func(w, h int) (*image.Gray, error)) (*image.Gray, error) {
	img, _, err := BrandedFrameZones(screen, alert, foot, width, height, render)
	return img, err
}

// BrandedFrameZones is BrandedFrame that also returns the zones the chart
// named (addZone), placed where the frame put the chart.
func BrandedFrameZones(screen, alert string, foot Footer, width, height int, render func(w, h int) (*image.Gray, error)) (*image.Gray, []Zone, error) {
	drawMu.Lock() // the chart draws inside render, under the same turn
	defer drawMu.Unlock()
	var zones []Zone
	zoneSink = &zones
	defer func() { zoneSink = nil }()
	bh := mastheadHeight(width, height)
	ah := 0
	if alert != "" {
		ah = alertHeight(width, height)
	}
	fh := 0
	if foot.Left != "" || foot.Right != "" {
		fh = footerHeight(width, height)
	}
	body, err := render(width, height-bh-ah-fh)
	if err != nil && fh > 0 {
		fh = 0 // too short for the footer too: the chart alone
		zones = zones[:0]
		body, err = render(width, height-bh-ah)
	}
	if err != nil {
		return nil, nil, err
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	bodyR := image.Rect(0, bh+ah, width, height-fh)
	draw.Draw(img, bodyR, body, image.Point{}, draw.Src)
	for i := range zones { // into the frame's coordinates, inside the chart
		zones[i].Rect = zones[i].Rect.Add(bodyR.Min).Intersect(bodyR)
	}
	if fh > 0 {
		drawFooter(img, image.Rect(0, height-fh, width, height), foot)
	}
	if ah > 0 {
		drawAlert(img, image.Rect(0, bh, width, bh+ah), alert)
	}
	fillRect(img, image.Rect(0, 0, width, bh), ink)

	k := float64(bh) / 30
	pad := int(math.Round(16 * k))
	mark := math.Round(20 * k)
	DrawWolfMark(img, float64(pad), math.Round((float64(bh)-mark)/2), mark, surface, ink)

	base := bh/2 + int(math.Round(5*k))
	word := face(true, math.Round(14*k*2)/2)
	drawTracked(img, word, pad+int(mark)+int(math.Round(10*k)), base, "WOLFE HEALTH", 2.6*k, surface)

	if screen != "" {
		f := face(false, math.Round(12*k*2)/2)
		s := strings.ToUpper(screen)
		tr := 1.6 * k
		drawTracked(img, f, width-pad-trackedWidth(f, s, tr), base, s, tr, surface)
	}
	return img, zones, nil
}

// footerHeight is the footer's height: 18px at 800x480, scaling with the
// canvas like the masthead.
func footerHeight(width, height int) int {
	k := math.Min(float64(width)/800, float64(height)/480)
	return max(14, int(math.Round(18*k)))
}

// drawFooter draws the footer: a hairline rule across, then small muted
// text at either end, the left in bold ink when it warns.
func drawFooter(img *image.Gray, r image.Rectangle, f Footer) {
	k := float64(r.Dy()) / 18
	fillRect(img, r, surface)
	pad := int(math.Round(16 * k))
	hline(img, pad, r.Max.X-pad-1, r.Min.Y, rule)
	base := r.Max.Y - int(math.Round(5*k))
	size := math.Round(11*k*2) / 2
	if f.Warn {
		drawText(img, face(true, size), pad, base, f.Left, ink, alignLeft)
	} else {
		drawText(img, face(false, size), pad, base, f.Left, muted, alignLeft)
	}
	drawText(img, face(false, size), r.Max.X-pad, base, f.Right, muted, alignRight)
}

// alertHeight is the warning banner's height: 32px at 800x480, scaling with
// the canvas like the masthead.
func alertHeight(width, height int) int {
	k := math.Min(float64(width)/800, float64(height)/480)
	return max(24, int(math.Round(32*k)))
}

// drawAlert draws the warning banner in r: a white band boxed by a heavy
// border, a warning triangle drawn as a polygon with the "!" cut out (no
// font glyph needed), and the message in bold. Pure black and white, so it
// reads the same on any panel.
func drawAlert(img *image.Gray, r image.Rectangle, text string) {
	k := float64(r.Dy()) / 32
	fillRect(img, r, surface)
	inner := image.Rect(r.Min.X+int(8*k), r.Min.Y+int(4*k), r.Max.X-int(8*k), r.Max.Y-int(3*k))
	outline(img, inner, max(2, int(math.Round(2*k))), ink)

	size := math.Round(18 * k)
	ix := float64(inner.Min.X) + 10*k
	iy := float64(inner.Min.Y+inner.Max.Y)/2 - size/2
	tri := func(pts [][2]float64) [][2]float64 {
		out := make([][2]float64, len(pts))
		for i, p := range pts {
			out[i] = [2]float64{ix + p[0]*size, iy + p[1]*size}
		}
		return out
	}
	fillPolygon(img, tri([][2]float64{{0.5, 0}, {1, 0.92}, {0, 0.92}}), ink)
	fillPolygon(img, tri([][2]float64{{0.44, 0.3}, {0.56, 0.3}, {0.53, 0.62}, {0.47, 0.62}}), surface)
	fillPolygon(img, tri([][2]float64{{0.44, 0.7}, {0.56, 0.7}, {0.56, 0.82}, {0.44, 0.82}}), surface)

	f := face(true, math.Round(14*k*2)/2)
	drawText(img, f, int(ix+size+10*k), (inner.Min.Y+inner.Max.Y)/2+int(5*k), text, ink, alignLeft)
}

// SetupScreen is what a display shows the moment it connects: the mark,
// large, the wordmark, and the device's short ID, so the first thing on the
// wall confirms both the brand and that pairing worked.
func SetupScreen(friendlyID string, width, height int) (*image.Gray, error) {
	drawMu.Lock()
	defer drawMu.Unlock()
	if width < 200 || height < 150 {
		return nil, fmt.Errorf("chart: setup screen needs at least 200x150, got %dx%d", width, height)
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), surface)
	k := math.Min(float64(width)/800, float64(height)/480)

	mark := math.Round(170 * k)
	cx := float64(width) / 2
	top := float64(height)*0.5 - mark*0.85
	DrawWolfMark(img, cx-mark/2, top, mark, ink, surface)

	word := face(true, math.Round(30*k*2)/2)
	tr := 6 * k
	y := int(top + mark + 56*k)
	drawTracked(img, word, int(cx)-trackedWidth(word, "WOLFE HEALTH", tr)/2, y, "WOLFE HEALTH", tr, ink)
	y += int(36 * k)
	drawText(img, face(false, math.Round(16*k*2)/2), int(cx), y,
		"Connected · device "+friendlyID, muted, alignCenter)
	y += int(24 * k)
	drawText(img, face(false, math.Round(14*k*2)/2), int(cx), y,
		"Your first screen arrives on the next refresh", muted, alignCenter)
	return img, nil
}
