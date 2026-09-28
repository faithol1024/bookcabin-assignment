package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ctx = context.Background()

// mapStore is a Store that fails every op while down.
type mapStore struct {
	mu   sync.Mutex
	data map[string][]byte
	ttls map[string]time.Duration
	down atomic.Bool
	gets atomic.Int32
	sets atomic.Int32
	// setDelay slows every Set, like a Redis near its op timeout
	setDelay time.Duration
}

func newMapStore() *mapStore {
	return &mapStore{data: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

var errDown = errors.New("store down")

func (m *mapStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.gets.Add(1)
	if m.down.Load() {
		return nil, false, errDown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *mapStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.sets.Add(1)
	time.Sleep(m.setDelay)
	if m.down.Load() {
		return errDown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	m.ttls[key] = ttl
	return nil
}

func (m *mapStore) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok
}
