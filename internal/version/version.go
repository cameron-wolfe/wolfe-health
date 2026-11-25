// Package version says which build is running: the git commit it was built
// from, when that was committed, whether the working tree had uncommitted
// changes, and when it was built. Go records the first three itself
// (go build in a git checkout); the build time comes from the Makefile:
//
//	go build -ldflags "-X github.com/cameron-wolfe/wolfe-health/internal/version.BuildTime=2026-11-24T21:10:00Z"
//
// A build made outside a checkout, or with go run, has no commit, and says so.
package version

import (
	"fmt"
	"runtime/debug"
	"time"
)

// BuildTime is when this binary was built, RFC 3339 in UTC, set at link
// time by the Makefile; empty for a plain go build.
var BuildTime string

// Info is the build's stamp.
type Info struct {
	Commit     string    `json:"commit,omitempty"`     // short hash: "e44ea92"
	CommitTime time.Time `json:"commit_time,omitzero"` // when that commit was made
	Dirty      bool      `json:"dirty,omitempty"`      // built with uncommitted changes
	Built      time.Time `json:"built,omitzero"`       // when the binary was built
	Go         string    `json:"go"`                   // the Go version that built it
}

// Get reads the stamp of the running binary.
func Get() Info {
	var i Info
	if t, err := time.Parse(time.RFC3339, BuildTime); err == nil {
		i.Built = t
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return i
	}
	i.Go = bi.GoVersion
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			i.Commit = s.Value
			if len(i.Commit) > 7 {
				i.Commit = i.Commit[:7]
			}
		case "vcs.time":
			i.CommitTime, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.modified":
			i.Dirty = s.Value == "true"
		}
	}
	return i
}

// String words the stamp in loc, for the log and the index page:
// "e44ea92 + uncommitted changes, committed Nov 24 8:57 PM, built Nov 24 9:10 PM".
func (i Info) String(loc *time.Location) string {
	if i.Commit == "" {
		s := "an unstamped build (no git commit recorded)"
		if !i.Built.IsZero() {
			s += ", built " + i.Built.In(loc).Format("Jan 2 3:04 PM")
		}
		return s
	}
	s := i.Commit
	if i.Dirty {
		s += " + uncommitted changes"
	}
	if !i.CommitTime.IsZero() {
		s += ", committed " + i.CommitTime.In(loc).Format("Jan 2 3:04 PM")
	}
	if !i.Built.IsZero() {
		s += ", built " + i.Built.In(loc).Format("Jan 2 3:04 PM")
	}
	return s
}

// Short is the commit with a mark for uncommitted changes: "e44ea92+".
func (i Info) Short() string {
	if i.Commit == "" {
		return "unstamped"
	}
	if i.Dirty {
		return fmt.Sprintf("%s+", i.Commit)
	}
	return i.Commit
}
