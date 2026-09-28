package airasia

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/pkg/cache/cachetest"
	"github.com/faithol1024/bookcabin-assignment/pkg/resilience"
	"github.com/faithol1024/bookcabin-assignment/pkg/simulate"
)

// stub serves the client's requests with h instead of the AirAsia API.
func stub(h http.HandlerFunc) *Client {
	return New(Deps{HTTP: &http.Client{Transport: simulate.NewTransport(simulate.New(simulate.Profile{}, rand.New(rand.NewPCG(1, 1))), h)}, BaseURL: "http://airasia.test"})
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestSearch_SendsQuery(t *testing.T) {
	var got *http.Request
	c := stub(func(w http.ResponseWriter, r *http.Request) {
		got = r
		reply(http.StatusOK, `{"status": "ok", "flights": []}`)(w, r)
	})
	if _, err := c.Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if got.Method != http.MethodGet || got.URL.Host != "airasia.test" || got.URL.Path != "/flights" ||
		q.Get("origin") != "CGK" || q.Get("destination") != "DPS" || q.Get("date") != "2025-12-15" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
}

func TestSearch_DecodesFlights(t *testing.T) {
	c := stub(reply(http.StatusOK, `{"status": "ok", "flights": [{"flight_code": "QZ520", "price_idr": 650000}]}`))
	flights, err := records(c.Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(flights) != 1 || flights[0].FlightCode != "QZ520" || flights[0].PriceIDR.String() != "650000" {
		t.Errorf("flights = %+v", flights)
	}
}

func TestSearch_UpstreamErrors(t *testing.T) {
	tests := []struct {
		name string
		h    http.HandlerFunc
		want error
	}{
		{"503", reply(http.StatusServiceUnavailable, ""), apperr.ErrUnavailable},
		{"500", reply(http.StatusInternalServerError, ""), apperr.ErrUnavailable},
		{"404", reply(http.StatusNotFound, ""), apperr.ErrMalformedResponse},
		{"400", reply(http.StatusBadRequest, ""), apperr.ErrMalformedResponse},
		{"garbage body", reply(http.StatusOK, `{"flights": [`), apperr.ErrMalformedResponse},
		{"error status", reply(http.StatusOK, `{"status": "error", "flights": []}`), apperr.ErrMalformedResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := stub(tt.h).Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); !errors.Is(err, tt.want) {
				t.Errorf("Search() error = %v, want %v", err, tt.want)
			}
		})
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestSearch_TransportErrorIsUnavailable(t *testing.T) {
	c := New(Deps{HTTP: &http.Client{Transport: failingTransport{}}, BaseURL: "http://airasia.test"})
	if _, err := c.Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); !errors.Is(err, apperr.ErrUnavailable) {
		t.Errorf("Search() error = %v, want ErrUnavailable", err)
	}
}

func TestDecode_Malformed(t *testing.T) {
	for _, in := range []string{`{"status": "error", "flights": []}`, `{"flights": [`, `{"status": "ok", "flights": [{"price_idr": "abc"}]}`} {
		if _, err := decode([]byte(in)); err == nil {
			t.Errorf("decode(%s) succeeded, want error", in)
		}
	}
}

func records(r Result, err error) ([]Flight, error) {
	return r.Flights, err
}

var given = Query{"CGK", "DPS", "2025-12-15"}

func guarded(retries int) *resilience.GuardSettings {
	return &resilience.GuardSettings{
		RateLimit: resilience.RateLimit{RPS: 1000, Burst: 1000},
		Breaker:   resilience.Breaker{Failures: 5, Cooldown: time.Minute, HalfOpenRequests: 1},
		Retry:     resilience.Policy{Timeout: 100 * time.Millisecond, MaxRetries: retries, Base: time.Millisecond, MaxBackoff: time.Millisecond},
	}
}

// upstream serves the mock data after the given latency, failing the first calls with the given statuses.
type upstream struct {
	latency  time.Duration
	failures []int
	calls    atomic.Int32
}

func (u *upstream) client(d Deps) *Client {
	d.HTTP = &http.Client{Transport: simulate.NewTransport(simulate.New(simulate.Profile{LatencyMin: u.latency, LatencyMax: u.latency}, rand.New(rand.NewPCG(1, 1))), u)}
	d.BaseURL = "http://airasia.test"
	return New(d)
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if n := int(u.calls.Add(1)); n <= len(u.failures) {
		w.WriteHeader(u.failures[n-1])
		return
	}
	mockHandler().ServeHTTP(w, r)
}

func TestSearch_RetriesUnavailable(t *testing.T) {
	u := &upstream{failures: []int{http.StatusServiceUnavailable, http.StatusBadGateway}}
	res, err := u.client(Deps{Guard: guarded(2)}).Search(context.Background(), given)
	if err != nil || res.Attempts != 3 || len(res.Flights) == 0 {
		t.Errorf("Search() = %d flights in %d attempts, %v; want flights in 3 attempts", len(res.Flights), res.Attempts, err)
	}
}

func TestSearch_RetriesExhaustedIsUnavailable(t *testing.T) {
	u := &upstream{failures: []int{503, 503, 503, 503}}
	res, err := u.client(Deps{Guard: guarded(2)}).Search(context.Background(), given)
	if !errors.Is(err, apperr.ErrUnavailable) || res.Attempts != 3 {
		t.Errorf("Search() = %d attempts, %v; want 3, ErrUnavailable", res.Attempts, err)
	}
}

func TestSearch_MalformedNotRetried(t *testing.T) {
	u := &upstream{failures: []int{http.StatusBadRequest}}
	res, err := u.client(Deps{Guard: guarded(2)}).Search(context.Background(), given)
	if !errors.Is(err, apperr.ErrMalformedResponse) || res.Attempts != 1 {
		t.Errorf("Search() = %d attempts, %v; want 1, ErrMalformedResponse", res.Attempts, err)
	}
}

func TestSearch_AttemptTimeoutIsUnavailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := &upstream{latency: time.Second}
		res, err := u.client(Deps{Guard: guarded(1)}).Search(context.Background(), given)
		if !errors.Is(err, apperr.ErrUnavailable) || res.Attempts != 2 || u.calls.Load() != 0 {
			t.Errorf("Search() = %d attempts, %v; want 2 timed-out attempts, ErrUnavailable", res.Attempts, err)
		}
	})
}

func TestSearch_RateLimited(t *testing.T) {
	s := guarded(2)
	s.RateLimit = resilience.RateLimit{RPS: 0.001, Burst: 1}
	u := &upstream{}
	c := u.client(Deps{Guard: s})
	if _, err := c.Search(context.Background(), given); err != nil {
		t.Fatal(err)
	}
	res, err := c.Search(context.Background(), given)
	if !errors.Is(err, apperr.ErrRateLimited) || res.Attempts != 0 || u.calls.Load() != 1 {
		t.Errorf("Search() = %d attempts, %v (upstream calls %d); want 0, ErrRateLimited, 1", res.Attempts, err, u.calls.Load())
	}
}

func TestSearch_OpenBreakerIsUnavailable(t *testing.T) {
	s := guarded(0)
	s.Breaker.Failures = 1
	u := &upstream{failures: []int{503}}
	c := u.client(Deps{Guard: s})
	_, _ = c.Search(context.Background(), given)
	res, err := c.Search(context.Background(), given)
	if !errors.Is(err, apperr.ErrUnavailable) || !errors.Is(err, resilience.ErrOpen) || res.Attempts != 0 || u.calls.Load() != 1 {
		t.Errorf("Search() = %d attempts, %v (upstream calls %d); want 0, open breaker, 1", res.Attempts, err, u.calls.Load())
	}
}

func TestSearch_ConcurrentIdenticalSearchesShareOneFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := &upstream{latency: 50 * time.Millisecond}
		c := u.client(Deps{Guard: guarded(2), FetchTimeout: time.Second})
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				if res, err := c.Search(context.Background(), given); err != nil || len(res.Flights) == 0 {
					t.Errorf("Search() = %d flights, %v", len(res.Flights), err)
				}
			})
		}
		wg.Wait()
		if got := u.calls.Load(); got != 1 {
			t.Errorf("upstream calls = %d, want 1", got)
		}
	})
}

func TestSearch_FirstCallerCancellingDoesNotFailOthers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := &upstream{latency: 50 * time.Millisecond}
		c := u.client(Deps{Guard: guarded(2), FetchTimeout: time.Second})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		first := make(chan error)
		go func() {
			_, err := c.Search(ctx, given)
			first <- err
		}()
		synctest.Wait()
		res, err := c.Search(context.Background(), given)
		if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("first Search() error = %v, want its own deadline", err)
		}
		if err != nil || len(res.Flights) == 0 || u.calls.Load() != 1 {
			t.Errorf("second Search() = %d flights, %v (upstream calls %d); want the shared fetch's flights", len(res.Flights), err, u.calls.Load())
		}
	})
}

func TestSearch_FetchTimeoutBoundsSharedFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := &upstream{latency: time.Hour}
		c := u.client(Deps{FetchTimeout: 20 * time.Millisecond})
		start := time.Now()
		if _, err := c.Search(context.Background(), given); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Search() error = %v, want context.DeadlineExceeded", err)
		}
		if got := time.Since(start); got != 20*time.Millisecond {
			t.Errorf("Search() took %v, want 20ms", got)
		}
	})
}

func TestSearch_UnguardedCallsOnce(t *testing.T) {
	u := &upstream{failures: []int{503}}
	c := u.client(Deps{})
	res, err := c.Search(context.Background(), given)
	if !errors.Is(err, apperr.ErrUnavailable) || res.Attempts != 1 || u.calls.Load() != 1 {
		t.Errorf("Search() = %d attempts, %v (upstream calls %d); want 1, ErrUnavailable, 1", res.Attempts, err, u.calls.Load())
	}
}

func cached(d Deps) (Deps, *cachetest.Store) {
	store := cachetest.New()
	d.Cache, d.CacheTTL, d.KeyPrefix = store, time.Minute, "flights:v1"
	return d, store
}

func TestSearch_CacheMissThenHit(t *testing.T) {
	d, store := cached(Deps{Guard: guarded(2)})
	u := &upstream{}
	c := u.client(d)
	miss, err := c.Search(context.Background(), given)
	if err != nil || miss.CacheHit || miss.Attempts != 1 || len(miss.Flights) == 0 {
		t.Fatalf("first Search() = %+v, %v; want a fetched miss", miss, err)
	}
	if _, ok, _ := store.Get(context.Background(), "flights:v1:airasia:CGK:DPS:2025-12-15"); !ok {
		t.Error("records not cached under flights:v1:airasia:CGK:DPS:2025-12-15")
	}
	hit, err := c.Search(context.Background(), given)
	if err != nil || !hit.CacheHit || hit.Attempts != 0 || u.calls.Load() != 1 {
		t.Fatalf("second Search() = hit %v in %d attempts, %v (upstream calls %d); want a hit, no call", hit.CacheHit, hit.Attempts, err, u.calls.Load())
	}
	if !reflect.DeepEqual(hit.Flights, miss.Flights) {
		t.Errorf("cached records differ from fetched:\n%+v\n%+v", hit.Flights, miss.Flights)
	}
}

func TestSearch_FailedFetchNotCached(t *testing.T) {
	d, store := cached(Deps{Guard: guarded(0)})
	u := &upstream{failures: []int{http.StatusServiceUnavailable}}
	c := u.client(d)
	if _, err := c.Search(context.Background(), given); !errors.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("Search() error = %v, want ErrUnavailable", err)
	}
	if store.Len() != 0 {
		t.Error("failed fetch was cached")
	}
	if res, err := c.Search(context.Background(), given); err != nil || res.CacheHit || u.calls.Load() != 2 {
		t.Errorf("retry Search() = hit %v, %v (upstream calls %d); want a fresh fetch", res.CacheHit, err, u.calls.Load())
	}
}

func TestSearch_CacheHitSkipsRateLimiter(t *testing.T) {
	s := guarded(0)
	s.RateLimit = resilience.RateLimit{RPS: 0.001, Burst: 1}
	d, _ := cached(Deps{Guard: s})
	c := (&upstream{}).client(d)
	for i := range 3 {
		if _, err := c.Search(context.Background(), given); err != nil {
			t.Fatalf("Search() #%d error = %v; a hit must not need a token", i+1, err)
		}
	}
	other := given
	other.Date = "2025-12-16"
	if _, err := c.Search(context.Background(), other); !errors.Is(err, apperr.ErrRateLimited) {
		t.Errorf("miss error = %v, want ErrRateLimited: the one token went to the first fetch", err)
	}
}

func TestSearch_CorruptCacheIsMalformed(t *testing.T) {
	d, store := cached(Deps{})
	_ = store.Set(context.Background(), "flights:v1:airasia:CGK:DPS:2025-12-15", []byte("{"), time.Minute)
	if _, err := (&upstream{}).client(d).Search(context.Background(), given); !errors.Is(err, apperr.ErrMalformedResponse) {
		t.Errorf("Search() error = %v, want ErrMalformedResponse", err)
	}
}
