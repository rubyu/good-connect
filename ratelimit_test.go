package goodconnect

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

// waitAll spawns n goroutines calling wait concurrently and returns their admission times.
func waitAll(t *testing.T, l *hostLimiter, host string, n int) []time.Time {
	t.Helper()
	var wg sync.WaitGroup
	times := make([]time.Time, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := l.wait(context.Background(), host); err != nil {
				t.Errorf("wait: %v", err)
			}
			times[i] = time.Now()
		}(i)
	}
	wg.Wait()
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	return times
}

// TestWaitConcurrentPacing verifies that concurrent requests are not fired all
// at once and are admitted 1/qps seconds apart per host.
func TestWaitConcurrentPacing(t *testing.T) {
	l := newHostLimiter(1, 1) // defaults: at most 1 request per second
	times := waitAll(t, l, "host-a", 5)
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 800*time.Millisecond {
			t.Fatalf("gap[%d] = %v: concurrent requests were not spaced 1 second apart", i, gap)
		}
	}
	if span := times[len(times)-1].Sub(times[0]); span < 3*time.Second {
		t.Fatalf("span = %v: 5 admissions did not take at least 3 seconds", span)
	}
}

// TestWaitConcurrentDifferentHosts verifies that hosts are limited independently
// and the debt of one host never leaks into another.
func TestWaitConcurrentDifferentHosts(t *testing.T) {
	l := newHostLimiter(1, 1)
	// Empty host-a's bucket.
	if err := l.wait(context.Background(), "host-a"); err != nil {
		t.Fatalf("host-a 1st: %v", err)
	}
	// A different host right after: independent buckets, so it is admitted immediately.
	start := time.Now()
	if err := l.wait(context.Background(), "host-b"); err != nil {
		t.Fatalf("host-b: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("host-b waited %v because of host-a", elapsed)
	}
	// Another request to the same host right after: this one waits 1 second.
	start = time.Now()
	if err := l.wait(context.Background(), "host-b"); err != nil {
		t.Fatalf("host-b 2nd: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Fatalf("host-b 2nd was admitted immediately: %v", elapsed)
	}
}

// TestWaitBurst verifies that Burst requests are admitted immediately and
// further ones are spaced out.
func TestWaitBurst(t *testing.T) {
	l := newHostLimiter(1, 3)
	times := waitAll(t, l, "host-c", 4)
	if gap := times[2].Sub(times[0]); gap > 500*time.Millisecond {
		t.Fatalf("burst quota was not admitted immediately: 3rd at %v", gap)
	}
	if gap := times[3].Sub(times[2]); gap < 800*time.Millisecond {
		t.Fatalf("past-burst request was admitted immediately: %v", gap)
	}
}

// TestWaitContextCancel verifies that wait returns an error when the context is
// cancelled while waiting.
func TestWaitContextCancel(t *testing.T) {
	l := newHostLimiter(1, 1)
	if err := l.wait(context.Background(), "host-d"); err != nil {
		t.Fatalf("1st: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := l.wait(ctx, "host-d")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wait with a cancelled context did not return context.Canceled: %v", err)
	}
}
