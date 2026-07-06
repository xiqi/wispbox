package security

import (
	"sync"
	"time"
)

// RateLimiter is a token-bucket limiter keyed by an arbitrary string
// (typically "route:client-ip"). It exists to slow credential stuffing on
// the admin and webmail login endpoints.
type RateLimiter struct {
	rate  float64 // tokens per second
	burst float64

	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time // swappable for tests
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows `burst` immediate attempts, refilling at `rate`/sec.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{rate: rate, burst: burst, buckets: map[string]*bucket{}, now: time.Now}
}

// Allow consumes one token for key if available.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// Opportunistic cleanup keeps the map bounded without a goroutine.
		if len(l.buckets) > 10000 {
			for k, old := range l.buckets {
				if now.Sub(old.last) > 10*time.Minute {
					delete(l.buckets, k)
				}
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// SetClock overrides the time source (tests only).
func (l *RateLimiter) SetClock(now func() time.Time) { l.now = now }
