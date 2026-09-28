package cache

import (
	"context"
	"sync"
	"time"
)

// Loaded is a cached value or a fetch's result; Fetched is what the fetch returned besides the value
// and is zero on a hit.
type Loaded[T any] struct {
	Value   []byte
	Fetched T
	Hit     bool
}

// Loader reads through a Store; concurrent misses of one key share a single fetch, cached unless it fails.
type Loader[T any] struct {
	store        Store // nil only shares fetches
	ttl          time.Duration
	fetchTimeout time.Duration // 0 means none

	mu    sync.Mutex
	calls map[string]*call[T]
}

// call is one shared fetch. It stays registered until its value is stored, so callers arriving
// meanwhile get the value without waiting for the store or fetching again.
type call[T any] struct {
	done chan struct{} // closed when the fetch returns
	res  Loaded[T]
	err  error
}

func NewLoader[T any](store Store, ttl, fetchTimeout time.Duration) *Loader[T] {
	return &Loader[T]{store: store, ttl: ttl, fetchTimeout: fetchTimeout, calls: map[string]*call[T]{}}
}

// Load returns on ctx's end while the shared fetch runs on for the callers still waiting.
// A store error counts as a miss.
func (l *Loader[T]) Load(ctx context.Context, key string, fetch func(context.Context) ([]byte, T, error)) (Loaded[T], error) {
	if l.store != nil {
		if v, ok, err := l.store.Get(ctx, key); err == nil && ok {
			return Loaded[T]{Value: v, Hit: true}, nil
		}
	}
	l.mu.Lock()
	c, joined := l.calls[key]
	if !joined {
		c = &call[T]{done: make(chan struct{})}
		l.calls[key] = c
		go l.run(context.WithoutCancel(ctx), key, c, fetch)
	}
	l.mu.Unlock()

	if joined {
		select {
		case <-c.done: // fetched and being stored: as good as a hit
			return Loaded[T]{Value: c.res.Value, Hit: true}, nil
		default:
		}
	}
	select {
	case <-ctx.Done():
		return Loaded[T]{}, ctx.Err()
	case <-c.done:
		return c.res, c.err
	}
}

func (l *Loader[T]) run(ctx context.Context, key string, c *call[T], fetch func(context.Context) ([]byte, T, error)) {
	fctx := ctx
	if l.fetchTimeout > 0 {
		var cancel context.CancelFunc
		fctx, cancel = context.WithTimeout(ctx, l.fetchTimeout)
		defer cancel()
	}
	v, fetched, err := fetch(fctx)
	c.res, c.err = Loaded[T]{Value: v, Fetched: fetched}, err
	if err != nil || l.store == nil {
		l.forget(key) // before done, so a joiner never takes a failure for a hit
		close(c.done)
		return
	}
	close(c.done)
	// a failed write only costs the next caller a fetch
	_ = l.store.Set(ctx, key, v, l.ttl)
	l.forget(key)
}

func (l *Loader[T]) forget(key string) {
	l.mu.Lock()
	delete(l.calls, key)
	l.mu.Unlock()
}
