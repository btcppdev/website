package api

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
	now     func() time.Time
	lastGC  time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string]rateBucket), now: time.Now}
}

func (limiter *rateLimiter) allow(key string, capacity, refillPerSecond float64) (bool, int) {
	if limiter == nil || key == "" {
		return true, 0
	}
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	bucket, exists := limiter.buckets[key]
	if !exists {
		bucket = rateBucket{tokens: capacity, last: now}
	}
	if elapsed := now.Sub(bucket.last).Seconds(); elapsed > 0 {
		bucket.tokens = math.Min(capacity, bucket.tokens+elapsed*refillPerSecond)
		bucket.last = now
	}
	if bucket.tokens >= 1 {
		bucket.tokens--
		limiter.buckets[key] = bucket
		limiter.gc(now)
		return true, 0
	}
	limiter.buckets[key] = bucket
	retry := int(math.Ceil((1 - bucket.tokens) / refillPerSecond))
	if retry < 1 {
		retry = 1
	}
	return false, retry
}

func (limiter *rateLimiter) gc(now time.Time) {
	if now.Sub(limiter.lastGC) < 10*time.Minute {
		return
	}
	for key, bucket := range limiter.buckets {
		if now.Sub(bucket.last) > 2*time.Hour {
			delete(limiter.buckets, key)
		}
	}
	limiter.lastGC = now
}

func remoteIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func bearerRateKey(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:12])
}
