// Package ratelimit은 토큰 버킷 방식의 rate limiter를 제공한다.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter는 초당 rate개의 토큰을 채우고 최대 burst개까지 보관하는 토큰 버킷이다.
// 여러 goroutine에서 동시에 사용해도 안전하다.
type Limiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time

	// now는 현재 시각을 반환한다. 테스트에서 교체할 수 있다.
	now func() time.Time
}

// New는 초당 rate개의 토큰을 채우고 최대 burst개까지 허용하는 Limiter를 만든다.
// 버킷은 가득 찬 상태로 시작한다.
func New(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:   rate,
		burst:  float64(burst),
		tokens: float64(burst),
		now:    time.Now,
	}
}

// Allow는 AllowN(1)과 같다.
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN은 토큰이 n개 이상 남아 있으면 n개를 소비하고 true를 반환한다.
// n이 burst보다 크면 절대 채워질 수 없으므로 항상 false를 반환한다.
// n이 0 이하이면 토큰을 소비하지 않고 true를 반환한다.
func (l *Limiter) AllowN(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if float64(n) > l.burst {
		return false
	}
	if n <= 0 {
		return true
	}

	now := l.now()
	// last는 첫 호출 시점에 정한다. New 이후 now를 교체해도 기준 시각이 어긋나지 않는다.
	if l.last.IsZero() {
		l.last = now
	}
	if elapsed := now.Sub(l.last); elapsed > 0 && l.rate > 0 {
		l.tokens += elapsed.Seconds() * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
	}
	// 시계가 뒤로 가면 last를 되돌리지 않아 토큰이 중복으로 채워지는 것을 막는다.
	if now.After(l.last) {
		l.last = now
	}

	if l.tokens < float64(n) {
		return false
	}
	l.tokens -= float64(n)
	return true
}
