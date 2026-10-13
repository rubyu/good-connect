// This file is a custom implementation inspired by the delay/stealth options of
// cloudscraper (https://github.com/VeNoMouS/cloudscraper, MIT License). The
// "debt" scheme that paces concurrent requests 1/qps seconds apart is the same
// as x/time/rate. See README.md / LICENSE for background and license details.
package goodconnect

import (
	"context"
	"math"
	"sync"
	"time"
)

// hostBucket is a per-host token bucket.
type hostBucket struct {
	tokens float64
	last   time.Time
}

// hostLimiter is a rate limiter with an independent token bucket per host.
// qps is the number of allowed requests per second, burst is the bucket capacity.
// During development we want at most one request per second per domain, so the
// defaults are qps=1, burst=1 (with burst=1, consecutive requests are always
// spaced 1 second apart = at most one per second).
type hostLimiter struct {
	qps, burst float64
	mu         sync.Mutex
	buckets    map[string]*hostBucket
}

func newHostLimiter(qps, burst float64) *hostLimiter {
	if qps <= 0 {
		qps = 1
	}
	if burst <= 0 {
		burst = 1
	}
	return &hostLimiter{qps: qps, burst: burst, buckets: make(map[string]*hostBucket)}
}

// wait reserves one request slot for the host, waiting until tokens are available.
// Shortfalls are kept in the bucket as "debt" (the same approach as x/time/rate),
// so concurrent callers are admitted 1/qps seconds apart per host; dropping the
// debt would let queued requests fire all at once.
func (l *hostLimiter) wait(ctx context.Context, host string) error {
	l.mu.Lock()
	b, ok := l.buckets[host]
	now := time.Now()
	if !ok {
		b = &hostBucket{tokens: l.burst, last: now}
		l.buckets[host] = b
	}
	// Refill for the elapsed time (up to the bucket capacity).
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.qps)
	var wait time.Duration
	if b.tokens >= 1 {
		b.tokens--
	} else {
		// Keep the shortfall as debt instead of consuming it: subsequent requests
		// are delayed by another 1/qps each.
		wait = time.Duration((1 - b.tokens) / l.qps * float64(time.Second))
		b.tokens -= 1
	}
	b.last = now
	l.mu.Unlock()

	if wait > 0 {
		select {
		case <-time.After(wait):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
