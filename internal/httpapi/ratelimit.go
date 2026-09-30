package httpapi

import (
	"sync"
	"time"
)

// rateLimiter is a token bucket per name: perMinute requests a minute, with bursts of up
// to perMinute. It holds one small entry per MCP key name, which is a bounded set.
type rateLimiter struct {
	mu        sync.Mutex
	perMinute int
	buckets   map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter(perMinute int) *rateLimiter {
	return &rateLimiter{perMinute: perMinute, buckets: map[string]*tokenBucket{}}
}

// take spends one request for name and returns how long to wait when none is left.
func (l *rateLimiter) take(name string, now time.Time) time.Duration {
	if l == nil || l.perMinute <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	capacity := float64(l.perMinute)
	perSecond := capacity / 60
	b := l.buckets[name]
	if b == nil {
		b = &tokenBucket{tokens: capacity, at: now}
		l.buckets[name] = b
	}
	b.tokens = min(capacity, b.tokens+now.Sub(b.at).Seconds()*perSecond)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	return time.Duration((1 - b.tokens) / perSecond * float64(time.Second))
}
