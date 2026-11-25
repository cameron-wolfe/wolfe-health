package chart

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func grayImg(w, h int, fill uint8) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	fillRect(img, img.Bounds(), color.Gray{Y: fill})
	return img
}

// On white, each gray role gets its own treatment in 1-bit.
func TestToEinkOneBitRoles(t *testing.T) {
	img := grayImg(20, 20, 255)
	img.SetGray(10, 10, grid)               // hairline: checker
	img.SetGray(11, 10, grid)               // hairline, odd square
	img.SetGray(10, 12, laneShade)          // shade: white
	img.SetGray(10, 14, muted)              // gray text: black
	img.SetGray(10, 16, color.Gray{Y: 160}) // soft text edge: black
	img.SetGray(10, 18, color.Gray{Y: 200}) // faint edge: white
	p, err := ToEink(img, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int]uint8{{10, 10}: 0, {11, 10}: 1, {10, 12}: 1, {10, 14}: 0, {10, 16}: 0, {10, 18}: 1}
	for pt, idx := range want {
		if got := p.ColorIndexAt(pt[0], pt[1]); got != idx {
			t.Errorf("pixel %v: index %d, want %d", pt, got, idx)
		}
	}
}

// On black — the masthead — a light pixel is white text, kept whole and
// never turned into a dotted hairline.
func TestToEinkOneBitDarkContext(t *testing.T) {
	img := grayImg(20, 20, 0)
	img.SetGray(10, 10, grid)               // a hairline value inside white text
	img.SetGray(12, 10, color.Gray{Y: 120}) // a soft white-text edge
	img.SetGray(14, 10, color.Gray{Y: 60})  // mostly black
	p, _ := ToEink(img, 1)
	for pt, idx := range map[[2]int]uint8{{10, 10}: 1, {12, 10}: 1, {14, 10}: 0} {
		if got := p.ColorIndexAt(pt[0], pt[1]); got != idx {
			t.Errorf("pixel %v on black: index %d, want %d", pt, got, idx)
		}
	}
}

func TestToEinkTwoBitSnaps(t *testing.T) {
	img := grayImg(4, 1, 255)
	for x, v := range []uint8{0, 90, 185, 246} {
		img.SetGray(x, 0, color.Gray{Y: v})
	}
	p, err := ToEink(img, 2)
	if err != nil {
		t.Fatal(err)
	}
	for x, want := range []uint8{0, 1, 2, 3} { // black, 85, 170, white
		if got := p.ColorIndexAt(x, 0); got != want {
			t.Errorf("x=%d: index %d, want %d", x, got, want)
		}
	}
	if _, err := ToEink(img, 3); err == nil {
		t.Error("3 bits should be rejected")
	}
}

// 800x480 at 1 bit is exactly 48,062 bytes; rows run bottom-up; index 0 is
// black and a set bit is white.
func TestEncodeBMP1(t *testing.T) {
	img := grayImg(800, 480, 255)
	fillRect(img, image.Rect(0, 0, 8, 1), ink) // top-left 8 pixels black
	p, _ := ToEink(img, 1)
	var buf bytes.Buffer
	if err := EncodeBMP1(&buf, p); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if len(b) != 48062 || string(b[:2]) != "BM" || binary.LittleEndian.Uint32(b[2:]) != 48062 {
		t.Fatalf("size %d, header %q", len(b), b[:2])
	}
	if bpp := binary.LittleEndian.Uint16(b[28:]); bpp != 1 {
		t.Errorf("bits per pixel = %d", bpp)
	}
	if b[54] != 0 || b[58] != 255 {
		t.Errorf("palette = %v %v, want black then white", b[54], b[58])
	}
	topRow := b[62+479*100:] // the top image row is the last one stored
	if topRow[0] != 0x00 || topRow[1] != 0xFF {
		t.Errorf("top row starts %02x %02x, want 00 ff", topRow[0], topRow[1])
	}
}

func TestSetupScreenRenders(t *testing.T) {
	if _, err := SetupScreen("261900", 800, 480); err != nil {
		t.Fatal(err)
	}
}

// The bedtime window's light fill becomes one black pixel in four in 1-bit,
// and light gray in 2-bit.
func TestWindowShadeEink(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	fillRect(img, img.Bounds(), windowShade)
	one, err := ToEink(img, 1)
	if err != nil {
		t.Fatal(err)
	}
	black := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if one.ColorIndexAt(x, y) == 0 {
				black++
			}
		}
	}
	if black != 16 {
		t.Errorf("1-bit: %d of 64 black, want 16", black)
	}
	two, err := ToEink(img, 2)
	if err != nil {
		t.Fatal(err)
	}
	if two.ColorIndexAt(3, 3) != 2 {
		t.Errorf("2-bit: index %d, want light gray (2)", two.ColorIndexAt(3, 3))
	}
}
