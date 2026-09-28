package simulate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCall_LatencyWithinProfile(t *testing.T) {
	p := Profile{LatencyMin: 5 * time.Millisecond, LatencyMax: 10 * time.Millisecond}
	s := NewSeeded(p, 1)
	for range 5 {
		start := time.Now()
		if err := s.Call(context.Background()); err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := time.Since(start); got < p.LatencyMin {
			t.Errorf("Call() took %v, want at least %v", got, p.LatencyMin)
		}
	}
}

func TestDraw_WithinRange(t *testing.T) {
	p := Profile{LatencyMin: 50 * time.Millisecond, LatencyMax: 100 * time.Millisecond}
	s := NewSeeded(p, 1)
	for range 1000 {
		if d, _ := s.draw(); d < p.LatencyMin || d > p.LatencyMax {
			t.Fatalf("draw() latency = %v, want within [%v, %v]", d, p.LatencyMin, p.LatencyMax)
		}
	}
}

func TestDraw_FixedLatency(t *testing.T) {
	s := NewSeeded(Profile{LatencyMin: 7 * time.Millisecond, LatencyMax: 7 * time.Millisecond}, 1)
	if d, _ := s.draw(); d != 7*time.Millisecond {
		t.Errorf("draw() latency = %v, want 7ms", d)
	}
}

func TestCall_HonoursCancelledContext(t *testing.T) {
	s := NewSeeded(Profile{LatencyMin: time.Hour, LatencyMax: time.Hour}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := s.Call(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Call() error = %v, want context.Canceled", err)
	}
	if got := time.Since(start); got > time.Second {
		t.Errorf("Call() took %v after cancel", got)
	}
}

func TestCall_HonoursDeadline(t *testing.T) {
	s := NewSeeded(Profile{LatencyMin: time.Hour, LatencyMax: time.Hour}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.Call(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestCall_FailureRateBounds(t *testing.T) {
	tests := []struct {
		rate     float64
		wantFail bool
	}{
		{0, false},
		{1, true},
	}
	for _, tt := range tests {
		s := NewSeeded(Profile{FailureRate: tt.rate}, 1)
		for range 100 {
			err := s.Call(context.Background())
			if got := errors.Is(err, ErrFailure); got != tt.wantFail {
				t.Fatalf("rate %v: Call() error = %v, want failure %v", tt.rate, err, tt.wantFail)
			}
		}
	}
}

func TestCall_DeterministicWithSeed(t *testing.T) {
	p := Profile{FailureRate: 0.5}
	outcomes := func() []bool {
		s := NewSeeded(p, 42)
		var got []bool
		for range 50 {
			got = append(got, s.Call(context.Background()) != nil)
		}
		return got
	}
	first, second := outcomes(), outcomes()
	failures := 0
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("call %d: outcome differs between runs with the same seed", i)
		}
		if first[i] {
			failures++
		}
	}
	if failures == 0 || failures == len(first) {
		t.Errorf("failures = %d of %d, want a mix at rate 0.5", failures, len(first))
	}
}

func TestCall_ConcurrentUse(t *testing.T) {
	s := NewSeeded(Profile{LatencyMax: time.Millisecond, FailureRate: 0.5}, 1)
	errs := make(chan error, 50)
	var wg sync.WaitGroup
	for range cap(errs) {
		wg.Go(func() { errs <- s.Call(context.Background()) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrFailure) {
			t.Errorf("Call() error = %v, want nil or ErrFailure", err)
		}
	}
}
