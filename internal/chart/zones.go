package chart

import "image"

// Zone is a part of a screen that a tap can open on its own: a name the
// server maps to a detail view, and where the part is drawn. A screen
// names its zones as it draws them, so they always match the drawing,
// whatever the size.
type Zone struct {
	Name string
	Rect image.Rectangle
}

// zoneSink collects the zones a chart names while BrandedFrameZones draws
// it. Drawing is one screen at a time (drawMu), so one sink will do; nil
// while nothing is collecting, when addZone does nothing.
var zoneSink *[]Zone

// addZone names a part of the chart being drawn, in the chart's own
// coordinates.
func addZone(name string, r image.Rectangle) {
	if zoneSink != nil {
		*zoneSink = append(*zoneSink, Zone{name, r})
	}
}
