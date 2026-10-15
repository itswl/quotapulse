package httpapi

import (
	"testing"
	"time"
)

// A fresh bucket grants a full burst, then refills at perMinute/60 per second.
func TestRateLimiterBurstThenRefill(t *testing.T) {
	l := newRateLimiter(6) // capacity 6, refill 0.1 token per second
	start := time.Unix(1_000_000, 0)

	for i := 0; i < 6; i++ {
		if wait := l.take("agent", start); wait != 0 {
			t.Fatalf("request %d within the burst should pass, wait=%v", i+1, wait)
		}
	}
	if wait := l.take("agent", start); wait <= 0 {
		t.Fatal("request 7 must be throttled")
	} else if want := 10 * time.Second; wait != want {
		t.Errorf("wait = %v, want %v (one token at 0.1/s)", wait, want)
	}

	// 60 seconds later the bucket has fully refilled
	if wait := l.take("agent", start.Add(time.Minute)); wait != 0 {
		t.Errorf("after a minute the burst should be available again, wait=%v", wait)
	}
}

// Buckets are per name: one throttled caller must not affect another.
func TestRateLimiterNamesAreIsolated(t *testing.T) {
	l := newRateLimiter(2)
	now := time.Unix(2_000_000, 0)

	for i := 0; i < 2; i++ {
		l.take("a", now)
	}
	if wait := l.take("a", now); wait <= 0 {
		t.Fatal("caller a exhausted its bucket")
	}
	if wait := l.take("b", now); wait != 0 {
		t.Errorf("caller b must be unaffected, wait=%v", wait)
	}
}

// Refill never exceeds capacity: idling cannot bank more than one burst.
func TestRateLimiterCapsRefill(t *testing.T) {
	l := newRateLimiter(3)
	start := time.Unix(3_000_000, 0)

	l.take("agent", start)
	// a long idle period may only restore the capacity, not more
	l.take("agent", start.Add(time.Hour))
	for i := 0; i < 2; i++ {
		if wait := l.take("agent", start.Add(time.Hour)); wait != 0 {
			t.Errorf("request %d should pass on a full bucket, wait=%v", i+1, wait)
		}
	}
	if wait := l.take("agent", start.Add(time.Hour)); wait <= 0 {
		t.Error("the fourth request must be throttled despite the idle hour")
	}
}

// A nil or disabled limiter lets everything through.
func TestRateLimiterDisabled(t *testing.T) {
	var l *rateLimiter
	if wait := l.take("x", time.Now()); wait != 0 {
		t.Errorf("nil limiter must pass, wait=%v", wait)
	}
	if wait := newRateLimiter(0).take("x", time.Now()); wait != 0 {
		t.Errorf("perMinute=0 must disable limiting, wait=%v", wait)
	}
}
