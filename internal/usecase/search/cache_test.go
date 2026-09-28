package search

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
	"github.com/faithol1024/bookcabin-assignment/pkg/cache/cachetest"
	"github.com/faithol1024/bookcabin-assignment/pkg/resilience"
)

func hit(ids ...string) sourceResult {
	res := ok(ids...)
	res.attempts, res.cacheHit = 0, true
	return res
}

func TestFanout_CacheHitOnlyWhenEverySourceHit(t *testing.T) {
	tests := []struct {
		name    string
		sources []source
		want    bool
	}{
		{"all hit", []source{fakeSource("A", 0, hit("a"), nil), fakeSource("B", 0, hit(), nil)}, true},
		{"one fetched", []source{fakeSource("A", 0, hit("a"), nil), fakeSource("B", 0, ok("b"), nil)}, false},
		{"one failed", []source{fakeSource("A", 0, hit("a"), nil), fakeSource("B", 0, sourceResult{}, apperr.ErrUnavailable)}, false},
		{"no sources", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fanout(context.Background(), tt.sources, cgkDps); got.cacheHit != tt.want {
				t.Errorf("cacheHit = %v, want %v", got.cacheHit, tt.want)
			}
		})
	}
}

func TestFanout_LateSourceIsNotAHit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		got := fanout(ctx, []source{fakeSource("A", 0, hit("a"), nil), fakeSource("B", time.Second, hit("b"), nil)}, cgkDps)
		if got.cacheHit {
			t.Error("cacheHit = true with a source past the deadline")
		}
	})
}

// cachedAirAsia is the AirAsia mock behind a test cache and a guard that allows exactly one fetch.
func cachedAirAsia() *airasia.Client {
	d := airasia.MockDeps()
	d.Cache, d.CacheTTL, d.KeyPrefix = cachetest.New(), time.Minute, "flights:v1"
	d.Guard = &resilience.GuardSettings{
		RateLimit: resilience.RateLimit{RPS: 0.001, Burst: 1},
		Breaker:   resilience.Breaker{Failures: 5, Cooldown: time.Minute, HalfOpenRequests: 1},
		// absorbs the mock's simulated 503s
		Retry: resilience.Policy{Timeout: time.Second, MaxRetries: 5, Base: time.Millisecond, MaxBackoff: time.Millisecond},
	}
	return airasia.New(d)
}

func TestService_CacheHitKeepsRejections(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := []source{airAsiaSource(cachedAirAsia(), mapper.New(5*time.Minute))}
		// the mock's one CGK→BPN flight has an unparseable time, so the mapper rejects it
		l := leg{LegQuery{Origin: "CGK", Destination: "BPN", Date: "2025-12-22"}, domain.CabinEconomy}
		miss := fanout(context.Background(), src, l)
		hit := fanout(context.Background(), src, l)
		if miss.cacheHit || miss.failed != 0 {
			t.Fatalf("first search cacheHit %v, errors %+v; want a fetched miss", miss.cacheHit, miss.errors)
		}
		if !hit.cacheHit || hit.failed != 0 {
			t.Fatalf("second search cacheHit %v, errors %+v; want a hit without a rate-limit token", hit.cacheHit, hit.errors)
		}
		for name, r := range map[string]fanoutResult{"miss": miss, "hit": hit} {
			if len(r.flights) != 0 || r.rejected != 1 {
				t.Errorf("%s: %d flights, %d rejected; want 0, 1", name, len(r.flights), r.rejected)
			}
		}
	})
}
