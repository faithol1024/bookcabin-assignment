package resilience

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

var (
	errRetryable = errors.New("retryable")
	errFatal     = errors.New("fatal")
)

func policy(maxRetries int) Policy {
	return Policy{
		Timeout:     100 * time.Millisecond,
		MaxRetries:  maxRetries,
		Base:        50 * time.Millisecond,
		MaxBackoff:  200 * time.Millisecond,
		IsRetryable: func(err error) bool { return errors.Is(err, errRetryable) },
	}
}

// failing fails with errs in order, then succeeds.
func failing(errs ...error) (func(context.Context) error, *int) {
	calls := 0
	return func(context.Context) error {
		calls++
		if calls <= len(errs) {
			return errs[calls-1]
		}
		return nil
	}, &calls
}

func TestDo_SucceedsOnThirdAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fn, calls := failing(errRetryable, errRetryable)
		attempts, err := Do(context.Background(), policy(2), fn)
		if err != nil || attempts != 3 || *calls != 3 {
			t.Errorf("Do() = %d, %v (calls %d); want 3, nil", attempts, err, *calls)
		}
	})
}

func TestDo_RetriesExhausted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fn, calls := failing(errRetryable, errRetryable, errRetryable, errRetryable)
		attempts, err := Do(context.Background(), policy(2), fn)
		if !errors.Is(err, errRetryable) || attempts != 3 || *calls != 3 {
			t.Errorf("Do() = %d, %v (calls %d); want 3, errRetryable", attempts, err, *calls)
		}
	})
}

func TestDo_NonRetryableNotRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fn, calls := failing(errFatal)
		attempts, err := Do(context.Background(), policy(2), fn)
		if !errors.Is(err, errFatal) || attempts != 1 || *calls != 1 {
			t.Errorf("Do() = %d, %v (calls %d); want 1, errFatal", attempts, err, *calls)
		}
	})
}

func TestDo_AttemptTimeoutIsRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		slow := func(ctx context.Context) error {
			calls++
			<-ctx.Done()
			return ctx.Err()
		}
		p := policy(2)
		p.IsRetryable = func(error) bool { return false }
		attempts, err := Do(context.Background(), p, slow)
		if !errors.Is(err, ErrAttemptTimeout) || !errors.Is(err, context.DeadlineExceeded) || attempts != 3 || calls != 3 {
			t.Errorf("Do() = %d, %v (calls %d); want 3, ErrAttemptTimeout", attempts, err, calls)
		}
	})
}

func TestDo_NeverOutlivesDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		defer cancel()
		start := time.Now()
		attempts, err := Do(ctx, policy(100), func(context.Context) error { return errRetryable })
		if elapsed := time.Since(start); elapsed > 120*time.Millisecond {
			t.Errorf("Do() returned after %v, past the 120ms deadline", elapsed)
		}
		if !errors.Is(err, errRetryable) || attempts < 1 || attempts > 100 {
			t.Errorf("Do() = %d, %v; want errRetryable", attempts, err)
		}
	})
}

func TestDo_DeadlineCutsSlowAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		start := time.Now()
		attempts, err := Do(ctx, policy(2), func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		if time.Since(start) != 30*time.Millisecond || attempts != 1 {
			t.Errorf("Do() took %v over %d attempts; want 30ms, 1", time.Since(start), attempts)
		}
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrAttemptTimeout) {
			t.Errorf("Do() error = %v, want the caller's deadline, not an attempt timeout", err)
		}
	})
}

func TestDo_CancelStopsBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		p := policy(2)
		p.Base, p.MaxBackoff = time.Hour, time.Hour
		calls := 0
		go func() {
			time.Sleep(time.Millisecond)
			cancel()
		}()
		_, err := Do(ctx, p, func(context.Context) error {
			calls++
			return errRetryable
		})
		if !errors.Is(err, context.Canceled) || !errors.Is(err, errRetryable) || calls > 2 {
			t.Errorf("Do() error = %v after %d calls, want context.Canceled wrapping errRetryable", err, calls)
		}
	})
}

func TestBackoff_DoublesUpToMax(t *testing.T) {
	p := policy(0)
	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond}
	for retry, w := range want {
		if got := backoff(p, retry); got != w {
			t.Errorf("backoff(%d) = %v, want %v", retry, got, w)
		}
	}
	if got := backoff(p, 1000); got != p.MaxBackoff {
		t.Errorf("backoff(1000) = %v, want %v", got, p.MaxBackoff)
	}
}

func TestDo_NilIsRetryableNeverRetries(t *testing.T) {
	calls := 0
	p := Policy{Timeout: time.Second, MaxRetries: 2, Base: time.Millisecond, MaxBackoff: time.Millisecond}
	attempts, err := Do(context.Background(), p, func(context.Context) error {
		calls++
		return errors.New("boom")
	})
	if err == nil || attempts != 1 || calls != 1 {
		t.Errorf("Do() = %d, %v after %d calls; want 1 attempt, the error", attempts, err, calls)
	}
}
