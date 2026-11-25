package fetch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every runs immediately, then on each tick, never overlapping itself, and
// stops when the context ends.
func TestEvery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs, running, overlapped atomic.Int32
	var nexts atomic.Int32
	done := make(chan struct{})
	go func() {
		Every(ctx, 20*time.Millisecond, nil, func(context.Context) error {
			if running.Add(1) > 1 {
				overlapped.Add(1)
			}
			runs.Add(1)
			time.Sleep(30 * time.Millisecond) // longer than the interval
			running.Add(-1)
			return nil
		}, func(time.Time) { nexts.Add(1) })
		close(done)
	}()
	time.Sleep(130 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Every did not stop after cancel")
	}
	if n := runs.Load(); n < 2 {
		t.Errorf("runs = %d, want at least 2", n)
	}
	if overlapped.Load() != 0 {
		t.Error("fn ran concurrently with itself")
	}
	if nexts.Load() != runs.Load() {
		t.Errorf("next reported %d times for %d runs", nexts.Load(), runs.Load())
	}
}

// A failure is retried soon, backing off with each failure in a row, and
// once a run succeeds the next waits the full interval.
func TestEveryRetriesFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var waits []time.Duration
	runs := 0
	done := make(chan struct{})
	go func() {
		Every(ctx, time.Hour, []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}, func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			runs++
			if runs <= 3 {
				return errors.New("x509: certificate is not yet valid") // the clock not set yet
			}
			return nil
		}, func(at time.Time) {
			mu.Lock()
			waits = append(waits, time.Until(at).Round(5*time.Millisecond))
			mu.Unlock()
		})
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(waits)
		mu.Unlock()
		if n >= 4 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d runs reported", n)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	want := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, time.Hour}
	for i, w := range want {
		if waits[i] != w {
			t.Errorf("wait after run %d = %v, want %v (all: %v)", i+1, waits[i], w, waits[:4])
		}
	}
}

func TestRetryDelay(t *testing.T) {
	ds := RetryDelays
	for _, c := range []struct {
		failures int
		most     time.Duration
		want     time.Duration
	}{{1, 6 * time.Hour, time.Minute}, {3, 6 * time.Hour, 5 * time.Minute}, {6, 6 * time.Hour, time.Hour},
		{20, 6 * time.Hour, time.Hour}, {6, 30 * time.Minute, 30 * time.Minute}, {0, 6 * time.Hour, 6 * time.Hour}} {
		if got := RetryDelay(ds, c.failures, c.most); got != c.want {
			t.Errorf("RetryDelay(%d, %v) = %v, want %v", c.failures, c.most, got, c.want)
		}
	}
}

func TestActivityEndIncludesToday(t *testing.T) {
	for to, want := range map[string]string{
		"2026-11-19": "2026-11-20",
		"2026-11-30": "2026-12-01",
		"2026-12-31": "2027-01-01",
	} {
		if got := activityEnd(to); got != want {
			t.Errorf("activityEnd(%s) = %s, want %s", to, got, want)
		}
	}
}
