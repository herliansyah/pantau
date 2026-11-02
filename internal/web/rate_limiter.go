package web

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

type limiterShard struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type shardedRateLimiter struct {
	shards    [16]limiterShard
	rate      float64 // tokens per second
	burst     float64 // maximum tokens
	maxPerMap int     // safety bound per shard map
}

func newShardedRateLimiter(rate, burst float64) *shardedRateLimiter {
	l := &shardedRateLimiter{rate: rate, burst: burst, maxPerMap: 5000}
	for i := 0; i < 16; i++ {
		l.shards[i].buckets = make(map[string]*tokenBucket)
	}
	return l
}

func hashIP(ip string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(ip); i++ {
		h ^= uint32(ip[i])
		h *= 16777619
	}
	return h
}

// Allow reports whether ip is allowed to proceed.
func (l *shardedRateLimiter) Allow(ip string) bool {
	shardIdx := hashIP(ip) % 16
	shard := &l.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	b, ok := shard.buckets[ip]
	if !ok {
		// ponytail: Bound map growth to mitigate spoofed IP memory exhaustion
		if len(shard.buckets) >= l.maxPerMap {
			return false
		}
		shard.buckets[ip] = &tokenBucket{
			tokens:   l.burst - 1,
			lastSeen: now,
		}
		return true
	}

	elapsed := now.Sub(b.lastSeen).Seconds()
	b.lastSeen = now
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// StartReaper removes idle IPs every interval.
func (l *shardedRateLimiter) StartReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			for i := 0; i < 16; i++ {
				shard := &l.shards[i]
				shard.mu.Lock()
				for ip, b := range shard.buckets {
					if now.Sub(b.lastSeen) > 10*time.Minute {
						delete(shard.buckets, ip)
					}
				}
				shard.mu.Unlock()
			}
		}
	}
}

// resolveClientIP extracts the client IP, verifying X-Forwarded-For only from trusted loopback/private proxies.
func resolveClientIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	ip := net.ParseIP(remoteHost)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			clientIP := strings.TrimSpace(parts[0])
			if net.ParseIP(clientIP) != nil {
				return clientIP
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			clientIP := strings.TrimSpace(xri)
			if net.ParseIP(clientIP) != nil {
				return clientIP
			}
		}
	}
	return remoteHost
}
