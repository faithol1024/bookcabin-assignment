// Package resilience runs upstream calls behind a rate limiter, a circuit breaker and deadline-aware retries.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// ErrAttemptTimeout marks a call whose last attempt ran out of its own timeout; it is always retried.
var ErrAttemptTimeout = errors.New("attempt timed out")

type Policy struct {
	Timeout     time.Duration // per attempt
	MaxRetries  int           // retries after the first attempt
	Base        time.Duration
	MaxBackoff  time.Duration
	IsRetryable func(error) bool // nil retries only attempt timeouts
}

// Do calls fn until it succeeds, fails with a non-retryable error, runs out of retries or would outlive ctx.
// Backoff is full jitter: random in [0, min(MaxBackoff, Base·2^retry)].
func Do(ctx context.Context, p Policy, fn func(context.Context) error) (attempts int, err error) {
	for {
		attempts++
		err = attempt(ctx, p.Timeout, fn)
		switch {
		case err == nil:
			return attempts, nil
		case ctx.Err() != nil:
			if !errors.Is(err, ctx.Err()) {
				err = fmt.Errorf("%w: %w", ctx.Err(), err)
			}
			return attempts, err
		case attempts > p.MaxRetries:
			return attempts, err
		case !errors.Is(err, ErrAttemptTimeout) && (p.IsRetryable == nil || !p.IsRetryable(err)):
			return attempts, err
		}
		wait := time.Duration(rand.Int64N(int64(backoff(p, attempts-1)) + 1))
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= wait {
			return attempts, err
		}
		select {
		case <-ctx.Done():
			return attempts, fmt.Errorf("%w: %w", ctx.Err(), err)
		case <-time.After(wait):
		}
	}
}

func attempt(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := fn(actx)
	if err != nil && ctx.Err() == nil && errors.Is(actx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrAttemptTimeout, err)
	}
	return err
}

func backoff(p Policy, retry int) time.Duration {
	d := p.Base
	for range retry {
		if d >= p.MaxBackoff {
			break
		}
		d *= 2
	}
	return min(d, p.MaxBackoff)
}
