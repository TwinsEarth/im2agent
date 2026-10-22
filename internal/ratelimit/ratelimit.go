// Package ratelimit 提供 IP 维度的令牌桶限流中间件。
//
// v0.5.1 最小实现：内存版（生产切换到 Redis）。
// 用法：
//
//	mux.Handle("/v1/", ratelimit.IPLimit(handler, 100, time.Minute))
package ratelimit

import (
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens int
	last   time.Time
}

// IPLimit 按 IP 限流：每秒最多 rps 个请求。
func IPLimit(next http.Handler, rps int, burst int) http.Handler {
	if rps <= 0 {
		return next
	}
	mu := &sync.Mutex{}
	buckets := map[string]*bucket{}
	refill := time.Second / time.Duration(rps)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		mu.Lock()
		b, ok := buckets[ip]
		if !ok {
			b = &bucket{tokens: burst, last: time.Now()}
			buckets[ip] = b
		}
		now := time.Now()
		elapsed := now.Sub(b.last)
		b.tokens += int(elapsed / refill)
		if b.tokens > burst {
			b.tokens = burst
		}
		b.last = now
		if b.tokens <= 0 {
			mu.Unlock()
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		b.tokens--
		mu.Unlock()
		next.ServeHTTP(w, r)
	})
}
