package chart

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// fillRect paints r in c.
func fillRect(img *image.Gray, r image.Rectangle, c color.Gray) {
	draw.Draw(img, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

// hline draws a 1px horizontal line from x0 to x1 inclusive.
func hline(img *image.Gray, x0, x1, y int, c color.Gray) {
	for x := x0; x <= x1; x++ {
		img.SetGray(x, y, c)
	}
}

// vline draws a 1px vertical line from y0 to y1 inclusive.
func vline(img *image.Gray, x, y0, y1 int, c color.Gray) {
	for y := y0; y <= y1; y++ {
		img.SetGray(x, y, c)
	}
}

// disc fills every pixel whose center lies within r of (cx, cy).
//
// No anti-aliasing: a pixel is either ink or not. Soft edges would become
// speckle once the display reduces gray levels for the e-ink panel.
func disc(img *image.Gray, cx, cy, r float64, c color.Gray) {
	for y := int(math.Floor(cy - r)); y <= int(math.Ceil(cy+r)); y++ {
		for x := int(math.Floor(cx - r)); x <= int(math.Ceil(cx+r)); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				img.SetGray(x, y, c)
			}
		}
	}
}

// thickLine draws a line of the given width with round ends, which keeps a
// polyline's corners from looking notched.
func thickLine(img *image.Gray, x0, y0, x1, y1, width float64, c color.Gray) {
	// Every pixel whose center is within width/2 of the segment, with round
	// ends. Chaining discs instead lost lines that lie exactly on a pixel
	// boundary — a 1px vertical line at a whole x drew nothing — so the
	// distance is measured directly, and a center exactly on the edge is
	// settled by a tiny fixed nudge: it counts on one side only, so such a
	// line is exactly one pixel wide rather than none or two.
	r := width / 2
	dx, dy := x1-x0, y1-y0
	l2 := dx*dx + dy*dy
	const nudge = 1e-7
	minX, maxX := int(math.Floor(math.Min(x0, x1)-r)), int(math.Ceil(math.Max(x0, x1)+r))
	minY, maxY := int(math.Floor(math.Min(y0, y1)-r)), int(math.Ceil(math.Max(y0, y1)+r))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5+nudge, float64(y)+0.5+nudge
			t := 0.0
			if l2 > 0 {
				t = math.Max(0, math.Min(1, ((px-x0)*dx+(py-y0)*dy)/l2))
			}
			ex, ey := px-(x0+t*dx), py-(y0+t*dy)
			if ex*ex+ey*ey <= r*r {
				img.SetGray(x, y, c)
			}
		}
	}
}

type align int

const (
	alignLeft align = iota
	alignCenter
	alignRight
)

// textWidth is the rendered width of s in pixels.
func textWidth(f font.Face, s string) int {
	return font.MeasureString(f, s).Round()
}

// drawText draws s with its baseline at y, anchored at x by a.
func drawText(img *image.Gray, face font.Face, x, y int, s string, c color.Gray, a align) {
	d := &font.Drawer{Dst: img, Src: &image.Uniform{C: c}, Face: face}
	w := d.MeasureString(s).Round()
	switch a {
	case alignCenter:
		x -= w / 2
	case alignRight:
		x -= w
	}
	d.Dot = fixed.P(x, y)
	d.DrawString(s)
}

// Fonts are the Go fonts, embedded in golang.org/x/image as byte slices, so
// there are no font files to install on the Pi. Each is parsed once, and each
// size is built once, on first use.
var (
	fontsOnce     sync.Once
	regular, bold *opentype.Font

	facesMu sync.Mutex
	faces   = map[faceKey]font.Face{}

	// drawMu lets one screen draw at a time. The cached faces are shared,
	// and an opentype face isn't safe for concurrent use: it rasterizes
	// each glyph into a buffer of its own, which the caller then draws.
	// Two screens drawing text at once (the index page loads many) corrupt
	// that buffer and panic in the rasterizer. A screen draws in
	// milliseconds, so taking turns costs nothing noticeable. The entry
	// points that draw text, BrandedFrame and SetupScreen, hold it.
	drawMu sync.Mutex
)

type faceKey struct {
	bold bool
	size float64
}

// face returns the Go font at size points, regular or bold.
func face(isBold bool, size float64) font.Face {
	fontsOnce.Do(func() {
		regular = mustParse(goregular.TTF)
		bold = mustParse(gobold.TTF)
	})
	facesMu.Lock()
	defer facesMu.Unlock()
	k := faceKey{isBold, size}
	if f, ok := faces[k]; ok {
		return f
	}
	src := regular
	if isBold {
		src = bold
	}
	f := mustFace(src, size)
	faces[k] = f
	return f
}

func labelFace() font.Face { return face(false, 13) }
func valueFace() font.Face { return face(true, 16) }

// The embedded fonts are known-good, so a parse failure is a build problem,
// not a runtime condition worth an error path.
func mustParse(ttf []byte) *opentype.Font {
	f, err := opentype.Parse(ttf)
	if err != nil {
		panic(err)
	}
	return f
}

func mustFace(f *opentype.Font, size float64) font.Face {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	return face
}
