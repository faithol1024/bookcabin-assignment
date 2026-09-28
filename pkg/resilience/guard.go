package resilience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/sony/gobreaker/v2"
	"golang.org/x/time/rate"
)

var (
	ErrRateLimited = errors.New("rate limited")
	ErrOpen        = errors.New("circuit breaker open")
)

type RateLimit struct {
	RPS   float64
	Burst int
}

type Breaker struct {
	Failures         int // consecutive failures that open it
	Cooldown         time.Duration
	HalfOpenRequests int // probes allowed once the cooldown ends
}

type GuardSettings struct {
	Name      string
	RateLimit RateLimit
	Breaker   Breaker
	Retry     Policy
}

// Guard runs a call through a token bucket, then a circuit breaker, then Do.
type Guard struct {
	limiter *rate.Limiter
	breaker *gobreaker.CircuitBreaker[int]
	policy  Policy
}

func NewGuard(s GuardSettings) *Guard {
	failures := uint64(max(s.Breaker.Failures, 1))
	return &Guard{
		limiter: rate.NewLimiter(rate.Limit(s.RateLimit.RPS), s.RateLimit.Burst),
		breaker: gobreaker.NewCircuitBreaker[int](gobreaker.Settings{
			Name:        s.Name,
			MaxRequests: uint32(min(max(s.Breaker.HalfOpenRequests, 1), math.MaxUint32)),
			Timeout:     s.Breaker.Cooldown,
			ReadyToTrip: func(c gobreaker.Counts) bool { return uint64(c.ConsecutiveFailures) >= failures },
			// the caller gave up: says nothing about upstream health (rate-limit rejections never reach the breaker)
			IsExcluded: func(err error) bool { return errors.Is(err, context.Canceled) },
		}),
		policy: s.Retry,
	}
}

// Do reports how many upstream attempts ran: 0 when the limiter or the breaker rejected the call.
// A nil Guard runs fn once, unguarded.
func (g *Guard) Do(ctx context.Context, fn func(context.Context) error) (int, error) {
	if g == nil {
		return 1, fn(ctx)
	}
	if !g.limiter.Allow() {
		return 0, ErrRateLimited
	}
	attempts, err := g.breaker.Execute(func() (int, error) {
		return Do(ctx, g.policy, fn)
	})
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return 0, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	return attempts, err
}
