package auth

import (
	"sync"
	"time"
)

// limiter counts failed attempts per IP and overall, in a fixed window. The global cap
// stops a botnet from spreading guesses over many addresses.
type limiter struct {
	mu       sync.Mutex
	perIP    int
	global   int
	window   time.Duration
	fails    map[string]*bucket
	allFails bucket
	nowFunc  func() time.Time
}

type bucket struct {
	n     int
	start time.Time
}

func newLimiter(perIP int, window time.Duration, global int) *limiter {
	return &limiter{perIP: perIP, global: global, window: window, fails: map[string]*bucket{}, nowFunc: time.Now}
}

func (l *limiter) current(b *bucket, now time.Time) int {
	if now.Sub(b.start) > l.window {
		b.n, b.start = 0, now
	}
	return b.n
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFunc()
	if l.current(&l.allFails, now) >= l.global {
		return false
	}
	b, ok := l.fails[ip]
	return !ok || l.current(b, now) < l.perIP
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFunc()
	l.current(&l.allFails, now)
	l.allFails.n++
	b, ok := l.fails[ip]
	if !ok {
		b = &bucket{start: now}
		l.fails[ip] = b
	}
	l.current(b, now)
	b.n++
	if len(l.fails) > 10000 {
		for k, v := range l.fails {
			if now.Sub(v.start) > l.window {
				delete(l.fails, k)
			}
		}
	}
}

func (l *limiter) reset(ip string) {
	l.mu.Lock()
	delete(l.fails, ip)
	l.mu.Unlock()
}
