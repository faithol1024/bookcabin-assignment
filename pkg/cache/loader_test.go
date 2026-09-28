package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type fetcher struct {
	calls   atomic.Int32
	latency time.Duration
	err     error
}

func (f *fetcher) fetch(ctx context.Context) ([]byte, int, error) {
	n := int(f.calls.Add(1))
	select {
	case <-time.After(f.latency):
	case <-ctx.Done():
		return nil, n, ctx.Err()
	}
	if f.err != nil {
		return nil, n, f.err
	}
	return []byte("fresh"), n, nil
}

func TestLoader_MissFetchesAndCaches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, f := newMapStore(), &fetcher{}
		l := NewLoader[int](store, time.Minute, time.Second)
		got, err := l.Load(ctx, "k", f.fetch)
		if err != nil || got.Hit || string(got.Value) != "fresh" || got.Fetched != 1 {
			t.Errorf("Load() = %+v, %v; want a fresh miss", got, err)
		}
		synctest.Wait() // the value is stored in the background
		if !store.has("k") || store.ttls["k"] != time.Minute {
			t.Error("fetched value not cached with the TTL")
		}
	})
}

func TestLoader_HitSkipsFetch(t *testing.T) {
	store, f := newMapStore(), &fetcher{}
	_ = store.Set(ctx, "k", []byte("cached"), time.Minute)
	got, err := NewLoader[int](store, time.Minute, time.Second).Load(ctx, "k", f.fetch)
	if err != nil || !got.Hit || string(got.Value) != "cached" || got.Fetched != 0 || f.calls.Load() != 0 {
		t.Errorf("Load() = %+v, %v with %d fetches; want the cached value, no fetch", got, err, f.calls.Load())
	}
}

func TestLoader_FailedFetchNotCached(t *testing.T) {
	store, f := newMapStore(), &fetcher{err: errors.New("upstream down")}
	l := NewLoader[int](store, time.Minute, time.Second)
	got, err := l.Load(ctx, "k", f.fetch)
	if !errors.Is(err, f.err) || got.Fetched != 1 {
		t.Errorf("Load() = %+v, %v; want the fetch's error and its result", got, err)
	}
	if store.sets.Load() != 0 {
		t.Error("failed fetch was cached")
	}
	f.err = nil
	if got, _ := l.Load(ctx, "k", f.fetch); got.Hit || f.calls.Load() != 2 {
		t.Errorf("next Load() hit = %v after %d fetches; want a refetch", got.Hit, f.calls.Load())
	}
}

func TestLoader_StoreErrorIsMiss(t *testing.T) {
	store, f := newMapStore(), &fetcher{}
	store.down.Store(true)
	got, err := NewLoader[int](store, time.Minute, time.Second).Load(ctx, "k", f.fetch)
	if err != nil || string(got.Value) != "fresh" {
		t.Errorf("Load() = %+v, %v; want a fetch despite the store", got, err)
	}
}

func TestLoader_NilStoreOnlySharesFetches(t *testing.T) {
	f := &fetcher{}
	l := NewLoader[int](nil, time.Minute, time.Second)
	for range 2 {
		if got, err := l.Load(ctx, "k", f.fetch); err != nil || got.Hit {
			t.Fatalf("Load() = %+v, %v", got, err)
		}
	}
	if f.calls.Load() != 2 {
		t.Errorf("fetches = %d, want 2 (nothing cached)", f.calls.Load())
	}
}

func TestLoader_ConcurrentMissesShareOneFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, f := newMapStore(), &fetcher{latency: 50 * time.Millisecond}
		l := NewLoader[int](store, time.Minute, time.Second)
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				if got, err := l.Load(ctx, "k", f.fetch); err != nil || string(got.Value) != "fresh" || got.Fetched != 1 {
					t.Errorf("Load() = %+v, %v", got, err)
				}
			})
		}
		wg.Wait()
		synctest.Wait()
		if f.calls.Load() != 1 || store.sets.Load() != 1 {
			t.Errorf("fetches = %d, sets = %d; want 1 each", f.calls.Load(), store.sets.Load())
		}
	})
}

func TestLoader_CallerLeavingDoesNotCancelSharedFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, f := newMapStore(), &fetcher{latency: 50 * time.Millisecond}
		l := NewLoader[int](store, time.Minute, time.Second)
		short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		first := make(chan error)
		go func() {
			_, err := l.Load(short, "k", f.fetch)
			first <- err
		}()
		synctest.Wait()
		got, err := l.Load(ctx, "k", f.fetch)
		if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("first Load() error = %v, want its own deadline", err)
		}
		synctest.Wait()
		if err != nil || string(got.Value) != "fresh" || f.calls.Load() != 1 || !store.has("k") {
			t.Errorf("second Load() = %+v, %v; want the shared fetch, cached", got, err)
		}
	})
}

func TestLoader_FetchTimeoutBoundsSharedFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fetcher{latency: time.Hour}
		_, err := NewLoader[int](newMapStore(), time.Minute, time.Second).Load(ctx, "k", f.fetch)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Load() error = %v, want the fetch timeout", err)
		}
	})
}

func TestLoader_SlowSetDoesNotDelayCallers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, f := newMapStore(), &fetcher{latency: 50 * time.Millisecond}
		store.setDelay = time.Second
		l := NewLoader[int](store, time.Minute, 5*time.Second)

		start := time.Now()
		got, err := l.Load(ctx, "k", f.fetch)
		if err != nil || got.Hit || string(got.Value) != "fresh" {
			t.Fatalf("Load() = %+v, %v; want a fresh miss", got, err)
		}
		if waited := time.Since(start); waited != 50*time.Millisecond {
			t.Errorf("Load() took %v, want the 50ms fetch without the 1s Set", waited)
		}

		// arriving while the value is still being stored: served from memory, no refetch
		got, err = l.Load(ctx, "k", f.fetch)
		if err != nil || !got.Hit || string(got.Value) != "fresh" || got.Fetched != 0 || f.calls.Load() != 1 {
			t.Errorf("Load() during Set = %+v, %v after %d fetches; want a hit without a fetch", got, err, f.calls.Load())
		}

		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if !store.has("k") || store.sets.Load() != 1 {
			t.Errorf("stored %v with %d sets; want the value stored once", store.has("k"), store.sets.Load())
		}
	})
}
