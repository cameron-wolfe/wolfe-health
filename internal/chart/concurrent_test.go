package chart

import (
	"image"
	"sync"
	"testing"
	"time"
)

// Screens render at the same time when a browser asks for several at once,
// as the index page does. The fonts' faces are shared and aren't safe for
// concurrent use, so drawing must take turns; this once panicked inside
// the font rasterizer. Run with -race to see a regression as a data race.
func TestConcurrentRendering(t *testing.T) {
	night := Night{Start: time.Date(2026, 11, 20, 23, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 21, 7, 0, 0, 0, time.UTC),
		StageEpoch: 30 * time.Minute, Stages: []Stage{StageLight, StageDeep, StageREM, StageLight, StageAwake, StageLight}}
	radar := SleepRadar{Title: "WolfeSense · Sleep", Score: "88", AvgLabel: "30-night average",
		Parts: []RadarPart{{Label: "A", Value: 90, Has: true}, {Label: "B", Value: 60, Has: true}, {Label: "C", Value: 80, Has: true}}}
	views := []func(w, h int) (*image.Gray, error){
		func(w, h int) (*image.Gray, error) { return MoonView(night, w, h) },
		func(w, h int) (*image.Gray, error) { return RadarView(radar, w, h) },
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				if _, err := BrandedFrame("Test", "", Footer{Left: "ring 62% · synced 7:12 PM", Right: "updated 7:53 PM"},
					800, 480, views[(i+j)%len(views)]); err != nil {
					t.Error(err)
				}
				if _, err := SetupScreen("ABC123", 800, 480); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
}
