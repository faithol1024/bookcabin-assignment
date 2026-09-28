// Package simulate injects latency and failures into mock upstream calls.
package simulate

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"
)

var ErrFailure = errors.New("simulated failure")

type Profile struct {
	LatencyMin  time.Duration
	LatencyMax  time.Duration
	FailureRate float64 // 0..1
}

// Simulator is safe for concurrent use; draws from rng are serialized.
type Simulator struct {
	profile Profile
	mu      sync.Mutex
	rng     *rand.Rand
}

func New(p Profile, rng *rand.Rand) *Simulator {
	return &Simulator{profile: p, rng: rng}
}

// NewSeeded builds a Simulator with its own generator, so the same seed replays the same draws.
func NewSeeded(p Profile, seed uint64) *Simulator {
	return New(p, rand.New(rand.NewPCG(seed, seed)))
}

// Call sleeps a random latency within the profile, then fails with ErrFailure at the failure rate.
func (s *Simulator) Call(ctx context.Context) error {
	latency, fail := s.draw()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(latency):
	}
	if fail {
		return ErrFailure
	}
	return nil
}

func (s *Simulator) draw() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latency := s.profile.LatencyMin
	if span := s.profile.LatencyMax - s.profile.LatencyMin; span > 0 {
		latency += time.Duration(s.rng.Int64N(int64(span) + 1))
	}
	return latency, s.rng.Float64() < s.profile.FailureRate
}
