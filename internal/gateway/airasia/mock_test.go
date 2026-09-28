package airasia

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/pkg/cache/cachetest"
	"github.com/faithol1024/bookcabin-assignment/pkg/simulate"
)

func newMock(p simulate.Profile, rng *rand.Rand) *Client {
	return New(mockDeps(simulate.New(p, rng)))
}

func instant() *Client {
	return newMock(simulate.Profile{}, rand.New(rand.NewPCG(1, 1)))
}

func TestSearch_Filters(t *testing.T) {
	tests := []struct {
		name string
		q    Query
		want []string
	}{
		{"given route and date", Query{"CGK", "DPS", "2025-12-15"}, []string{"QZ520", "QZ524", "QZ532", "QZ7250"}},
		{"other date", Query{"CGK", "DPS", "2025-12-16"}, nil},
		{"day before", Query{"CGK", "DPS", "2025-12-14"}, nil},
		{"stop airport is not a destination", Query{"CGK", "SOC", "2025-12-15"}, nil},
		{"reverse route", Query{"DPS", "CGK", "2025-12-15"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flights, err := records(instant().Search(context.Background(), tt.q))
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			var got []string
			for _, f := range flights {
				got = append(got, f.FlightCode)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("flights = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearch_ReturnsRawRecords(t *testing.T) {
	flights, err := records(instant().Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}))
	if err != nil {
		t.Fatal(err)
	}
	qz7250 := flights[3]
	if qz7250.FlightCode != "QZ7250" || qz7250.DurationHours != 4.33 || qz7250.DirectFlight {
		t.Errorf("QZ7250 = %+v, want the provider's record unchanged", qz7250)
	}
	if len(qz7250.Stops) != 1 || qz7250.Stops[0] != (Stop{Airport: "SOC", WaitTimeMinutes: 95}) {
		t.Errorf("stops = %+v", qz7250.Stops)
	}
	if got := qz7250.PriceIDR.String(); got != "485000" {
		t.Errorf("price = %q, want 485000", got)
	}
}

func TestMockHandler_RequiresQuery(t *testing.T) {
	for _, target := range []string{"/flights", "/flights?origin=CGK&destination=DPS", "/flights?origin=CGK&date=2025-12-15"} {
		rec := httptest.NewRecorder()
		mockHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, rec.Code)
		}
	}
}

func TestSearch_HonoursCancelledContext(t *testing.T) {
	c := newMock(simulate.Profile{LatencyMin: time.Hour, LatencyMax: time.Hour}, rand.New(rand.NewPCG(1, 1)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := c.Search(ctx, Query{"CGK", "DPS", "2025-12-15"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Search() error = %v, want context.Canceled", err)
	}
	if got := time.Since(start); got > time.Second {
		t.Errorf("Search() took %v after cancel", got)
	}
}

func TestSearch_SimulatedFailureIsUnavailable(t *testing.T) {
	c := newMock(simulate.Profile{FailureRate: 1}, rand.New(rand.NewPCG(1, 1)))
	if _, err := c.Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); !errors.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("Search() error = %v, want ErrUnavailable", err)
	}
}

func TestSearch_DeterministicFailuresWithSeed(t *testing.T) {
	outcomes := func() string {
		c := newMock(simulate.Profile{FailureRate: failureRate}, rand.New(rand.NewPCG(7, 7)))
		var b strings.Builder
		for range 100 {
			if _, err := c.Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); err != nil {
				b.WriteByte('x')
			} else {
				b.WriteByte('.')
			}
		}
		return b.String()
	}
	first := outcomes()
	if second := outcomes(); first != second {
		t.Errorf("outcomes differ with the same seed:\n%s\n%s", first, second)
	}
	if n := strings.Count(first, "x"); n == 0 || n > 25 {
		t.Errorf("%d failures in 100 calls, want some near the 10%% rate: %s", n, first)
	}
}

func TestNewMock_UsesAssignmentProfile(t *testing.T) {
	if profile.FailureRate != 0.1 {
		t.Errorf("failure rate = %v, want 0.1 (90%% success)", profile.FailureRate)
	}
	start := time.Now()
	// a simulated failure still waits out the latency first
	if _, err := NewMock().Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); err != nil && !errors.Is(err, apperr.ErrUnavailable) {
		t.Fatal(err)
	}
	if got := time.Since(start); got < minLatency {
		t.Errorf("Search() took %v, want at least %v", got, minLatency)
	}
}

func TestMockDeps_CachesMockRecords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := MockDeps()
		d.Cache, d.CacheTTL = cachetest.New(), time.Minute
		c := New(d)
		var hits []bool
		for range 2 {
			res, err := c.Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"})
			if err != nil || len(res.Flights) == 0 {
				t.Fatalf("Search() = %d flights, %v", len(res.Flights), err)
			}
			hits = append(hits, res.CacheHit)
		}
		if !hits[1] || hits[0] {
			t.Errorf("cache hits = %v, want a miss then a hit", hits)
		}
	})
}

func TestMockResponses_ParsedOnce(t *testing.T) {
	a, errA := mockResponses()
	b, errB := mockResponses()
	if errA != nil || errB != nil || len(a) == 0 || &a[0] != &b[0] {
		t.Errorf("mockResponses() errors %v, %v; want the same parsed datasets on every call", errA, errB)
	}
}
