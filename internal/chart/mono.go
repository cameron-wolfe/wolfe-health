package chart

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

// E-ink panels show few levels: the TRMNL OG takes 1-bit (black and white)
// or 2-bit (four grays) images. ToEink converts a rendered chart to one of
// those deliberately, rather than by blind threshold, which would erase
// every light gray the charts use for structure.
//
// In 1-bit, each gray role is mapped on purpose. What a gray pixel means
// depends on what it sits on, so each pixel's 5x5 neighborhood is checked
// first. On a dark background — the masthead, a stressful day's strip — it
// is white text on black: a plain low threshold keeps the letters whole.
// On a light background the charts draw with a small set of exact grays, so
// a pixel's value says what it is:
//   - hairlines (grid, rules, transition connectors, the lanes' hour grid)
//     become a 50% checker, so a 1px line reads as a dotted line;
//   - the alternating lane shade becomes white;
//   - the light fill of a shaded box (the sleep schedule's bedtime window)
//     becomes one black pixel in four, a light gray to the eye;
//   - everything else — ink, muted text, and the anti-aliased edges of text
//     and marks — is thresholded at oneBitThreshold, above the midpoint:
//     gray text is drawn at 90 with soft edges, and a midpoint cut drops so
//     much of each glyph that small gray labels break apart.
//
// In 2-bit, every pixel snaps to the nearest of black, dark gray, light gray
// and white, which keeps the hairlines as light gray lines.
func ToEink(img *image.Gray, bits int) (*image.Paletted, error) {
	var pal color.Palette
	switch bits {
	case 1:
		pal = color.Palette{color.Gray{Y: 0}, color.Gray{Y: 255}}
	case 2:
		pal = color.Palette{color.Gray{Y: 0}, color.Gray{Y: 85}, color.Gray{Y: 170}, color.Gray{Y: 255}}
	default:
		return nil, fmt.Errorf("chart: e-ink output is 1 or 2 bits, not %d", bits)
	}
	b := img.Bounds()
	out := image.NewPaletted(b, pal)
	hairline := map[uint8]bool{grid.Y: true, rule.Y: true, connector.Y: true, laneGrid.Y: true}
	var dark func(x, y int) bool
	if bits == 1 {
		dark = darkContext(img)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := img.GrayAt(x, y).Y
			var idx uint8
			if bits == 1 {
				switch {
				case dark(x, y):
					if v >= 85 {
						idx = 1
					}
				case hairline[v]:
					idx = uint8((x + y) & 1) // checker: black on even squares
				case v == laneShade.Y:
					idx = 1
				case v == windowShade.Y: // a light fill: one pixel in four
					if x%2 != 0 || y%2 != 0 {
						idx = 1
					}
				case v >= oneBitThreshold:
					idx = 1
				}
			} else {
				idx = uint8((int(v) + 42) / 85) // nearest of 0, 85, 170, 255
			}
			out.SetColorIndex(x, y, idx)
		}
	}
	return out, nil
}

// darkContext reports whether a pixel sits on a mostly dark background: the
// mean of its 5x5 neighborhood is below mid-gray. A summed-area table makes
// each lookup four reads.
func darkContext(img *image.Gray) func(x, y int) bool {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	sum := make([]int, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		row := 0
		for x := 0; x < w; x++ {
			row += int(img.GrayAt(b.Min.X+x, b.Min.Y+y).Y)
			sum[(y+1)*(w+1)+x+1] = sum[y*(w+1)+x+1] + row
		}
	}
	return func(x, y int) bool {
		x, y = x-b.Min.X, y-b.Min.Y
		x0, y0 := max(0, x-2), max(0, y-2)
		x1, y1 := min(w, x+3), min(h, y+3)
		total := sum[y1*(w+1)+x1] - sum[y0*(w+1)+x1] - sum[y1*(w+1)+x0] + sum[y0*(w+1)+x0]
		return total < 128*(x1-x0)*(y1-y0)
	}
}

// oneBitThreshold: gray at or above this is white in 1-bit output. 170
// keeps a pixel black when a gray (90) glyph covers about half of it.
const oneBitThreshold = 170

// EncodeBMP1 writes a 1-bit image as an uncompressed BMP (version 3: a
// 14-byte file header, a 40-byte info header, a two-entry palette, then
// rows bottom-up, each padded to 4 bytes). At 800x480 that is exactly
// 48,062 bytes, the size TRMNL's image docs give. Palette index 0 is black.
func EncodeBMP1(w io.Writer, p *image.Paletted) error {
	if len(p.Palette) != 2 {
		return fmt.Errorf("chart: BMP output needs a 1-bit image")
	}
	width, height := p.Rect.Dx(), p.Rect.Dy()
	rowBytes := (width + 31) / 32 * 4
	pixels := rowBytes * height
	const headers = 14 + 40 + 8
	buf := make([]byte, headers+pixels)

	le := binary.LittleEndian
	buf[0], buf[1] = 'B', 'M'
	le.PutUint32(buf[2:], uint32(len(buf)))
	le.PutUint32(buf[10:], headers) // pixel data offset
	le.PutUint32(buf[14:], 40)      // info header size
	le.PutUint32(buf[18:], uint32(width))
	le.PutUint32(buf[22:], uint32(height)) // positive: bottom-up rows
	le.PutUint16(buf[26:], 1)              // planes
	le.PutUint16(buf[28:], 1)              // bits per pixel
	le.PutUint32(buf[34:], uint32(pixels))
	le.PutUint32(buf[38:], 2835) // 72 dpi, in pixels per meter
	le.PutUint32(buf[42:], 2835)
	le.PutUint32(buf[46:], 2) // colors in the palette
	for i, c := range p.Palette {
		g := color.GrayModel.Convert(c).(color.Gray).Y
		buf[54+i*4], buf[55+i*4], buf[56+i*4] = g, g, g // B, G, R, reserved
	}
	for y := 0; y < height; y++ {
		row := buf[headers+(height-1-y)*rowBytes:]
		for x := 0; x < width; x++ {
			if p.ColorIndexAt(p.Rect.Min.X+x, p.Rect.Min.Y+y) == 1 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	_, err := w.Write(buf)
	return err
}
