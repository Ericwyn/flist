package middleware

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipRateLimiter 按 IP 维护令牌桶，惰性创建并定期清理空闲条目。
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	r        rate.Limit
	burst    int
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(r rate.Limit, burst int) *ipRateLimiter {
	l := &ipRateLimiter{
		limiters: make(map[string]*limiterEntry),
		r:        r,
		burst:    burst,
	}
	go l.cleanupLoop()
	return l
}

func (l *ipRateLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.limiters[ip]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(l.r, l.burst)}
		l.limiters[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

func (l *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for ip, e := range l.limiters {
			if time.Since(e.lastSeen) > 15*time.Minute {
				delete(l.limiters, ip)
			}
		}
		l.mu.Unlock()
	}
}

// RateLimit 全局限流：每 IP 令牌桶。Phase 0 默认 50 请求/秒。
func RateLimit(perSecond float64, burst int) func(http.Handler) http.Handler {
	limiter := newIPRateLimiter(rate.Limit(perSecond), burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.get(ClientIP(r)).Allow() {
				writeError(w, http.StatusTooManyRequests, 9002, "rate_limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NewWriteRateLimiter 构造写操作限流中间件（Phase 0 暂无写接口接入，供后续阶段使用）。
func NewWriteRateLimiter(perSecond float64, burst int) func(http.Handler) http.Handler {
	limiter := newIPRateLimiter(rate.Limit(perSecond), burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.get(ClientIP(r)).Allow() {
				writeError(w, http.StatusTooManyRequests, 9002, "rate_limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
