package resilience

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

const cooldown = 10 * time.Second

func guard(failures int) *Guard {
	return NewGuard(GuardSettings{
		Name:      "test",
		RateLimit: RateLimit{RPS: 1000, Burst: 1000},
		Breaker:   Breaker{Failures: failures, Cooldown: cooldown, HalfOpenRequests: 1},
		Retry:     policy(0),
	})
}

func state(g *Guard) string { return g.breaker.State().String() }

func counting(err error) (func(context.Context) error, *int) {
	calls := 0
	return func(context.Context) error {
		calls++
		return err
	}, &calls
}

func TestGuard_RateLimitRejectionNotRetriedNorCounted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewGuard(GuardSettings{
			RateLimit: RateLimit{RPS: 1, Burst: 1},
			Breaker:   Breaker{Failures: 1, Cooldown: cooldown, HalfOpenRequests: 1},
			Retry:     policy(2),
		})
		fn, calls := counting(nil)
		if _, err := g.Do(context.Background(), fn); err != nil {
			t.Fatal(err)
		}
		for range 5 {
			attempts, err := g.Do(context.Background(), fn)
			if !errors.Is(err, ErrRateLimited) || attempts != 0 {
				t.Fatalf("Do() = %d, %v; want 0, ErrRateLimited", attempts, err)
			}
		}
		if *calls != 1 || state(g) != "closed" || g.breaker.Counts().Requests != 1 {
			t.Errorf("calls %d, state %s, breaker requests %d; want 1, closed, 1", *calls, state(g), g.breaker.Counts().Requests)
		}
	})
}

func TestGuard_RetriesInsideBreaker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := guard(1)
		g.policy = policy(2)
		fn, calls := failing(errRetryable, errRetryable)
		attempts, err := g.Do(context.Background(), fn)
		if err != nil || attempts != 3 || *calls != 3 || state(g) != "closed" {
			t.Errorf("Do() = %d, %v (calls %d, state %s); want 3, nil, closed", attempts, err, *calls, state(g))
		}
	})
}

func TestGuard_OpensAfterFailuresAndSkipsWhileOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := guard(3)
		fn, calls := counting(errFatal)
		for i := range 3 {
			if state(g) != "closed" {
				t.Fatalf("state after %d failures = %s, want closed", i, state(g))
			}
			if _, err := g.Do(context.Background(), fn); !errors.Is(err, errFatal) {
				t.Fatalf("Do() error = %v, want errFatal", err)
			}
		}
		if state(g) != "open" {
			t.Fatalf("state = %s, want open", state(g))
		}
		attempts, err := g.Do(context.Background(), fn)
		if !errors.Is(err, ErrOpen) || attempts != 0 || *calls != 3 {
			t.Errorf("Do() = %d, %v (calls %d); want 0, ErrOpen without calling upstream", attempts, err, *calls)
		}
	})
}

func TestGuard_HalfOpensAfterCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := guard(1)
		fail, _ := counting(errFatal)
		_, _ = g.Do(context.Background(), fail)
		time.Sleep(cooldown - time.Millisecond)
		if state(g) != "open" {
			t.Fatalf("state before cooldown = %s, want open", state(g))
		}
		time.Sleep(2 * time.Millisecond)
		if state(g) != "half-open" {
			t.Fatalf("state after cooldown = %s, want half-open", state(g))
		}

		release := make(chan struct{})
		probe := make(chan error)
		go func() {
			_, err := g.Do(context.Background(), func(context.Context) error {
				<-release
				return nil
			})
			probe <- err
		}()
		synctest.Wait()
		if attempts, err := g.Do(context.Background(), fail); !errors.Is(err, ErrOpen) || attempts != 0 {
			t.Errorf("second half-open call = %d, %v; want 0, ErrOpen", attempts, err)
		}
		close(release)
		if err := <-probe; err != nil {
			t.Fatal(err)
		}
		if state(g) != "closed" {
			t.Errorf("state after a successful probe = %s, want closed", state(g))
		}
	})
}

func TestGuard_FailedProbeReopens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := guard(1)
		fail, _ := counting(errFatal)
		_, _ = g.Do(context.Background(), fail)
		time.Sleep(cooldown + time.Millisecond)
		_, _ = g.Do(context.Background(), fail)
		if state(g) != "open" {
			t.Errorf("state after a failed probe = %s, want open", state(g))
		}
	})
}

func TestGuard_CallerCancellationNotCounted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := guard(1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := g.Do(ctx, func(ctx context.Context) error { return ctx.Err() })
		if !errors.Is(err, context.Canceled) || state(g) != "closed" {
			t.Errorf("Do() error = %v, state %s; want context.Canceled, closed", err, state(g))
		}
	})
}

func TestGuard_Nil(t *testing.T) {
	var g *Guard
	fn, calls := counting(errFatal)
	if attempts, err := g.Do(context.Background(), fn); !errors.Is(err, errFatal) || attempts != 1 || *calls != 1 {
		t.Errorf("Do() = %d, %v; want 1, errFatal", attempts, err)
	}
}
