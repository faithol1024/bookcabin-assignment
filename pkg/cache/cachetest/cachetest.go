// Package cachetest is an in-memory cache store for tests; it ignores TTLs.
package cachetest

import (
	"context"
	"sync"
	"time"
)

type Store struct {
	mu    sync.Mutex
	items map[string][]byte
}

func New() *Store {
	return &Store{items: map[string][]byte{}}
}

func (s *Store) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[key]
	return v, ok, nil
}

func (s *Store) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = value
	return nil
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
