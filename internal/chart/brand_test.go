package chart

import (
	"fmt"
	"image"
	"strings"
	"testing"
)

// A 10x10 square fills exactly 100 pixels: pixel-center sampling neither
// grows nor shrinks shapes.
func TestFillPolygonSquare(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 20, 20))
	fillRect(img, img.Bounds(), surface)
	fillPolygon(img, [][2]float64{{5, 5}, {15, 5}, {15, 15}, {5, 15}}, ink)
	n := 0
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			if img.GrayAt(x, y).Y == ink.Y {
				n++
				if x < 5 || x >= 15 || y < 5 || y >= 15 {
					t.Fatalf("pixel (%d,%d) inked outside the square", x, y)
				}
			}
		}
	}
	if n != 100 {
		t.Errorf("inked %d pixels, want 100", n)
	}
}

// The branded image is the requested size; the chart is rendered into the
// space under the band; the band is black with the mark drawn in white.
func TestBranded(t *testing.T) {
	var gotW, gotH int
	img, err := Branded("Daily brief", 800, 480, func(w, h int) (*image.Gray, error) {
		gotW, gotH = w, h
		body := image.NewGray(image.Rect(0, 0, w, h))
		fillRect(body, body.Bounds(), surface)
		return body, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bh := mastheadHeight(800, 480)
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 480 {
		t.Errorf("size %v", b.Size())
	}
	if gotW != 800 || gotH != 480-bh {
		t.Errorf("chart rendered at %dx%d, want 800x%d", gotW, gotH, 480-bh)
	}
	if img.GrayAt(400, 2).Y != ink.Y || img.GrayAt(400, bh+2).Y != surface.Y {
		t.Error("band should be black above the chart, white chart below")
	}
	white := 0
	for y := 0; y < bh; y++ {
		for x := 0; x < 50; x++ {
			if img.GrayAt(x, y).Y == surface.Y {
				white++
			}
		}
	}
	if white < 60 {
		t.Errorf("only %d white pixels where the mark should be", white)
	}
}

func TestMastheadScales(t *testing.T) {
	if h := mastheadHeight(800, 480); h != 30 {
		t.Errorf("800x480: %d", h)
	}
	if h := mastheadHeight(640, 384); h != 24 {
		t.Errorf("640x384: %d", h)
	}
	if h := mastheadHeight(480, 288); h != 22 {
		t.Errorf("floor: %d", h)
	}
}

func TestWolfMarkSVG(t *testing.T) {
	s := WolfMarkSVG(30, "#fff", "var(--bg)")
	for _, want := range []string{`viewBox="0 0 100 100"`, `style="fill:#fff"`, `style="fill:var(--bg)"`, "M12.0 2.0"} {
		if !strings.Contains(s, want) {
			t.Errorf("SVG missing %q", want)
		}
	}
}

// With an alert, the chart gets the space under masthead and banner, and the
// banner is drawn; with none, BrandedAlert is exactly Branded.
func TestBrandedAlert(t *testing.T) {
	var gotH int
	render := func(w, h int) (*image.Gray, error) {
		gotH = h
		body := image.NewGray(image.Rect(0, 0, w, h))
		fillRect(body, body.Bounds(), surface)
		return body, nil
	}
	img, err := BrandedAlert("Daily brief", "Temperature +0.8 °C, above your normal range", 800, 480, render)
	if err != nil {
		t.Fatal(err)
	}
	bh, ah := mastheadHeight(800, 480), alertHeight(800, 480)
	if gotH != 480-bh-ah {
		t.Errorf("chart got %d, want %d", gotH, 480-bh-ah)
	}
	inked := 0
	for y := bh; y < bh+ah; y++ {
		for x := 0; x < 800; x++ {
			if img.GrayAt(x, y).Y == ink.Y {
				inked++
			}
		}
	}
	if inked < 1500 { // border, triangle and text
		t.Errorf("banner looks empty: %d ink pixels", inked)
	}
	if _, err := BrandedAlert("Daily brief", "", 800, 480, render); err != nil || gotH != 480-bh {
		t.Errorf("no alert: chart got %d, want %d", gotH, 480-bh)
	}
}

// The footer takes its height from the chart, which still renders; a chart
// that can't fit above it is drawn without one.
func TestBrandedFrameFooter(t *testing.T) {
	foot := Footer{Left: "ring 64% · synced 2:28 PM", Right: "updated 2:40 PM"}
	var got int
	img, err := BrandedFrame("Test", "", foot, 800, 480, func(w, h int) (*image.Gray, error) {
		got = h
		b := image.NewGray(image.Rect(0, 0, w, h))
		fillRect(b, b.Bounds(), surface)
		return b, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 480-mastheadHeight(800, 480)-footerHeight(800, 480) {
		t.Errorf("chart height %d", got)
	}
	inked := false
	for x := 0; x < 200; x++ {
		for y := 480 - footerHeight(800, 480) + 2; y < 480; y++ {
			if img.GrayAt(x, y).Y < 128 {
				inked = true
			}
		}
	}
	if !inked {
		t.Error("no footer text at the bottom left")
	}

	// a chart needing more height than the footer leaves: drawn without it
	heights := []int{}
	if _, err := BrandedFrame("Test", "", foot, 800, 480, func(w, h int) (*image.Gray, error) {
		heights = append(heights, h)
		if h < 450 {
			return nil, fmt.Errorf("too short")
		}
		return image.NewGray(image.Rect(0, 0, w, h)), nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(heights) != 2 || heights[1] != 480-mastheadHeight(800, 480) {
		t.Errorf("tried heights %v", heights)
	}
}

// A 1px line exactly on a pixel boundary is one pixel wide, not missing:
// the sleep radar's top spoke once vanished this way.
func TestThickLineOnPixelBoundary(t *testing.T) {
	for _, x := range []float64{10, 10.25, 10.5} {
		img := image.NewGray(image.Rect(0, 0, 20, 20))
		fillRect(img, img.Bounds(), surface)
		thickLine(img, x, 18, x, 2, 1, ink)
		cols := map[int]bool{}
		n := 0
		for yy := 0; yy < 20; yy++ {
			for xx := 0; xx < 20; xx++ {
				if img.GrayAt(xx, yy).Y == ink.Y {
					cols[xx] = true
					n++
				}
			}
		}
		if len(cols) != 1 || n < 15 {
			t.Errorf("vertical line at x=%v: %d pixels in %d columns", x, n, len(cols))
		}
	}
}

// The outline mark traces the head's edge without filling it.
func TestDrawWolfOutline(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 100, 100))
	fillRect(img, img.Bounds(), surface)
	DrawWolfOutline(img, 50, 50, 80, 1.5, rule)
	traced := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			if img.GrayAt(x, y).Y == rule.Y {
				traced++
			}
		}
	}
	if traced < 100 || traced > 2500 {
		t.Errorf("outline covers %d pixels: want an edge, not nothing or a fill", traced)
	}
}
