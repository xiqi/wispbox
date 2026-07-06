package security

import (
	"testing"
	"time"
)

func TestRateLimiterBurstThenDeny(t *testing.T) {
	l := NewRateLimiter(1, 3)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !l.Allow("login:1.2.3.4") {
			t.Fatalf("attempt %d denied, want burst of 3 allowed", i+1)
		}
	}
	if l.Allow("login:1.2.3.4") {
		t.Fatal("attempt 4 allowed, want denied after burst exhausted")
	}

	// Keys are independent buckets.
	if !l.Allow("login:5.6.7.8") {
		t.Fatal("fresh key denied, want its own burst")
	}
}

func TestRateLimiterRefillOverTime(t *testing.T) {
	l := NewRateLimiter(2, 2) // 2 tokens/sec, burst 2
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return now })
	key := "login:1.2.3.4"

	// Drain the burst.
	if !l.Allow(key) || !l.Allow(key) {
		t.Fatal("burst of 2 not allowed")
	}
	if l.Allow(key) {
		t.Fatal("allowed with empty bucket, want denied")
	}

	// Half a second at 2/sec refills exactly one token.
	now = now.Add(500 * time.Millisecond)
	if !l.Allow(key) {
		t.Fatal("denied after refill, want one token available")
	}
	if l.Allow(key) {
		t.Fatal("allowed twice after a one-token refill")
	}

	// A long wait refills only up to the burst cap.
	now = now.Add(time.Hour)
	if !l.Allow(key) || !l.Allow(key) {
		t.Fatal("burst not restored after long idle")
	}
	if l.Allow(key) {
		t.Fatal("tokens accumulated past the burst cap")
	}
}
