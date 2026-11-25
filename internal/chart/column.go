package chart

import (
	"fmt"
	"image"
	"strings"

	"golang.org/x/image/font"
)

// PanelRow is one line of a WolfeSense column: a label, a value, and a note
// at the far right. Alert inverts the value, white on black, to flag it.
type PanelRow struct {
	Label, Value, Note string
	Alert              bool
}

// column is the right-hand column the WolfeSense and WolfePhase screens
// share: a small-caps heading with a note at the right, one large figure,
// rows of figures, a takeaway, and the key at the foot.
type column struct {
	head, headNote  string // "LAST NIGHT", "vs avg"
	leadLabel, lead string // "Sleep score", "88": the large figure; left out when lead is ""
	rows            []PanelRow
	rowMax          int     // the most height a row takes (22 when 0); rows close up when the key needs room
	valueRight      int     // where the values end, from the column's right edge (58 when 0)
	noteSize        float64 // the notes' font size (11 when 0)

	takeawayHead  string   // a bold line under the rows: "Well short of usual:"
	takeaway      string   // text under it, wrapped to the column, up to three lines
	takeawayItems []string // or items joined "a, b and c", shortened to fit one line
	key           []keyEntry
	keyWidth      int // the widest mark in the key (22 when 0); labels start after it
}

// drawColumn draws c in r, with the rule that sets it off from the chart
// just left of r. The key's height is fixed by its entries; rows and the
// takeaway fit in what's left, the takeaway dropped when there's no room.
func drawColumn(img *image.Gray, r image.Rectangle, c column) {
	x0, x1 := r.Min.X, r.Max.X
	vline(img, x0-12, r.Min.Y, r.Max.Y-1, rule)
	drawText(img, face(true, 11), x0, r.Min.Y+14, c.head, muted, alignLeft)
	drawText(img, face(false, 11), x1, r.Min.Y+14, c.headNote, muted, alignRight)
	if c.lead != "" {
		drawText(img, face(false, 12), x0, r.Min.Y+36, c.leadLabel, muted, alignLeft)
		drawText(img, face(true, 22), x1, r.Min.Y+38, c.lead, ink, alignRight)
	}

	keyY := r.Max.Y - 17*len(c.key) - 1 // the first key line's baseline
	keyTop := keyY - 14
	y := r.Min.Y + 66
	hline(img, x0, x1-1, y-16, grid)
	rowH := min(orInt(c.rowMax, 22), (keyTop-40-y)/max(1, len(c.rows)))
	vx := x1 - orInt(c.valueRight, 58)
	nf := face(false, orFloat(c.noteSize, 11))
	for _, row := range c.rows {
		drawText(img, face(false, 12), x0, y, row.Label, ink, alignLeft)
		vf := face(true, 14)
		if row.Alert {
			w := textWidth(vf, row.Value)
			fillRect(img, image.Rect(vx-w-3, y-12, vx+3, y+4), ink)
			drawText(img, vf, vx, y, row.Value, surface, alignRight)
		} else {
			drawText(img, vf, vx, y, row.Value, ink, alignRight)
		}
		drawText(img, nf, x1, y, row.Note, muted, alignRight)
		y += rowH
	}

	text := c.takeaway
	if len(c.takeawayItems) > 0 {
		text = fitList(face(false, 12), c.takeawayItems, x1-x0)
	}
	if (text != "" || c.takeawayHead != "") && y+21 <= keyTop {
		y += 2
		hline(img, x0, x1-1, y-14, grid)
		ty := y + 4
		if c.takeawayHead != "" {
			drawText(img, face(true, 12), x0, ty, c.takeawayHead, ink, alignLeft)
			ty += 17
		}
		for i, line := range wrapText(face(false, 12), text, x1-x0) {
			if i > 2 || ty > keyTop-4 {
				break
			}
			drawText(img, face(false, 12), x0, ty, line, ink, alignLeft)
			ty += 16
		}
	}

	if len(c.key) == 0 {
		return
	}
	hline(img, x0, x1-1, keyTop, grid)
	lx := x0 + orInt(c.keyWidth, 22) + 8
	for i, e := range c.key {
		yy := keyY + 17*i
		e.mark(x0, yy)
		drawText(img, face(false, 11), lx, yy, e.label, muted, alignLeft)
	}
}

// fitList joins items "a, b and c", or as many as fit in w followed by
// "and N more".
func fitList(f font.Face, items []string, w int) string {
	line := joinAnd(items)
	for k := len(items) - 1; textWidth(f, line) > w && k >= 1; k-- {
		line = fmt.Sprintf("%s and %d more", strings.Join(items[:k], ", "), len(items)-k)
	}
	return line
}

func orInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func orFloat(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}
