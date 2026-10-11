package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter는 토큰 버킷으로 요청을 제한한다. 사용 후에는 복사하면 안 된다.
// 영값은 모든 요청을 거절한다.
type Limiter struct {
	mu          sync.Mutex
	rate        float64
	burst       int
	tokens      float64
	last        time.Time
	initialized bool
	// 테스트에서는 첫 Allow 호출 전에 교체한다. 호출 중에는 변경하지 않는다.
	now func() time.Time
}

// New는 초당 rate개를 충전하고 최대 burst개를 보관하는 버킷을 만든다.
// 처음에는 버킷이 가득 차 있다. rate가 0이면 충전하지 않는다.
// rate는 유한한 음이 아닌 수, burst는 음이 아닌 정수여야 한다.
func New(rate float64, burst int) *Limiter {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || burst < 0 {
		panic("ratelimit: rate must be finite and non-negative; burst must be non-negative")
	}
	return &Limiter{
		rate:   rate,
		burst:  burst,
		tokens: float64(burst),
		now:    time.Now,
	}
}

// Allow는 토큰 하나를 소비할 수 있으면 true를 반환한다.
// 여러 고루틴에서 동시에 호출해도 안전하다.
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN은 토큰 n개를 한 번에 소비할 수 있으면 true를 반환한다.
// n이 음수이거나 burst보다 크면 false, 0이면 true를 반환한다.
// 여러 고루틴에서 동시에 호출해도 안전하다.
func (l *Limiter) AllowN(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if n < 0 || n > l.burst {
		return false
	}
	if n == 0 {
		return true
	}

	now := l.now()
	if !l.initialized {
		l.last = now
		l.initialized = true
	} else if now.After(l.last) {
		l.tokens = math.Min(float64(l.burst), l.tokens+now.Sub(l.last).Seconds()*l.rate)
		l.last = now
	}
	// 시간이 뒤로 가면 기준 시각을 유지해 같은 구간의 중복 충전을 막는다.
	if l.tokens < float64(n) {
		return false
	}
	l.tokens -= float64(n)
	return true
}
