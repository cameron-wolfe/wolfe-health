package version

import (
	"testing"
	"time"
)

// The stamp reads as a commit, a mark for uncommitted changes, and when it
// was committed and built; without a commit, it says it's unstamped.
func TestString(t *testing.T) {
	c := time.Date(2026, 11, 25, 4, 57, 0, 0, time.UTC)
	i := Info{Commit: "e44ea92", CommitTime: c, Dirty: true, Built: c.Add(13 * time.Minute)}
	if got := i.String(time.UTC); got != "e44ea92 + uncommitted changes, committed Nov 25 4:57 AM, built Nov 25 5:10 AM" {
		t.Errorf("String: %q", got)
	}
	if i.Short() != "e44ea92+" {
		t.Errorf("Short: %q", i.Short())
	}
	i.Dirty = false
	if i.Short() != "e44ea92" {
		t.Errorf("Short, clean: %q", i.Short())
	}
	if got := (Info{}).String(time.UTC); got != "an unstamped build (no git commit recorded)" {
		t.Errorf("unstamped: %q", got)
	}
	r := Info{Release: "v1.0.0", Commit: "a6feadd", Built: c}
	if got := r.String(time.UTC); got != "v1.0.0 (a6feadd, built Nov 25 4:57 AM)" || r.Short() != "v1.0.0" {
		t.Errorf("release: %q, %q", got, r.Short())
	}
	if Get().Go == "" {
		t.Error("Get: no Go version")
	}
}
