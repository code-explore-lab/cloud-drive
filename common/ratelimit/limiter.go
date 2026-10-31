// Package ratelimit 提供一个进程内的令牌桶限流器。
//
// 为什么不用 Redis：登录/注册限流是"防暴力破解"的基本措施，
// 单实例进程内限流已经能挡住绝大多数情况，而且不引入额外依赖。
// 多副本部署时每副本独立计数，总量会放大到副本数倍——这个取舍写在 docs/security.md，
// 需要严格全局限流时再换成 Redis 令牌桶。
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// Limiter 是按 key（通常是客户端 IP）计数的令牌桶限流器，并发安全。
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // 每秒补充的令牌数
	burst   float64 // 桶容量
	now     func() time.Time
}

// New 创建限流器：rate 表示每秒允许的请求数，burst 表示允许的瞬时突发量。
func New(rate, burst float64) *Limiter {
	if rate <= 0 {
		rate = 1
	}
	if burst < 1 {
		burst = 1
	}
	l := &Limiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		now:     time.Now,
	}
	go l.cleanupLoop()
	return l
}

// Allow 判断该 key 当前是否还有配额。
func (l *Limiter) Allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, lastSeen: now}
		return true
	}

	// 按经过时间补充令牌，上限为桶容量。
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// cleanupLoop 定期清理长时间不活跃的 key，避免 map 无限增长。
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := l.now().Add(-30 * time.Minute)
		l.mu.Lock()
		for key, b := range l.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}
