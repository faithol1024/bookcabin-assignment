package garuda

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
		{"given route and date", Query{"CGK", "DPS", "2025-12-15"}, []string{"GA400", "GA410", "GA315"}},
		{"next day only", Query{"CGK", "DPS", "2025-12-16"}, []string{"GA402"}},
		{"return date", Query{"DPS", "CGK", "2025-12-18"}, []string{"GA401"}},
		{"day before", Query{"CGK", "DPS", "2025-12-14"}, nil},
		{"GA315 top-level route is not its itinerary", Query{"CGK", "SUB", "2025-12-15"}, nil},
		{"connection airport is not an origin", Query{"SUB", "DPS", "2025-12-15"}, nil},
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
				got = append(got, f.FlightID)
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
	ga315 := flights[2]
	if ga315.FlightID != "GA315" || ga315.Arrival.Airport != "SUB" || ga315.Stops != 0 || len(ga315.Segments) != 2 {
		t.Errorf("GA315 = %+v, want the provider's record unchanged", ga315)
	}
	if got := ga315.Price.Amount.String(); got != "1850000" {
		t.Errorf("price amount = %q, want 1850000", got)
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
		c := newMock(simulate.Profile{FailureRate: 0.5}, rand.New(rand.NewPCG(7, 7)))
		var b strings.Builder
		for range 30 {
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
	if !strings.Contains(first, "x") || !strings.Contains(first, ".") {
		t.Errorf("outcomes = %s, want a mix at rate 0.5", first)
	}
}

func TestNewMock_UsesAssignmentLatency(t *testing.T) {
	start := time.Now()
	if _, err := NewMock().Search(context.Background(), Query{"CGK", "DPS", "2025-12-15"}); err != nil {
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
