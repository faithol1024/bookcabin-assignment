package search

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	fxgw "github.com/faithol1024/bookcabin-assignment/internal/gateway/fx"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
	"github.com/faithol1024/bookcabin-assignment/pkg/resilience"
)

var cgkDps = leg{LegQuery{Origin: "CGK", Destination: "DPS", Date: "2025-12-15"}, domain.CabinEconomy}

func fakeSource(provider string, delay time.Duration, res sourceResult, err error) source {
	return source{provider: provider, search: func(ctx context.Context, _ leg) (sourceResult, error) {
		select {
		case <-ctx.Done():
			return sourceResult{}, ctx.Err()
		case <-time.After(delay):
			return res, err
		}
	}}
}

func ok(ids ...string) sourceResult {
	res := sourceResult{attempts: 1}
	for _, id := range ids {
		res.flights = append(res.flights, domain.Flight{ID: id})
	}
	return res
}

func TestFanout_PartialResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sources := []source{
			fakeSource("A", 30*time.Millisecond, ok("a1", "a2"), nil),
			fakeSource("B", 10*time.Millisecond, sourceResult{attempts: 3}, fmt.Errorf("b: %w", apperr.ErrUnavailable)),
			fakeSource("C", 20*time.Millisecond, sourceResult{flights: []domain.Flight{{ID: "c1"}}, rejected: 2, attempts: 2}, nil),
			fakeSource("D", 0, sourceResult{}, fmt.Errorf("d: %w", apperr.ErrRateLimited)),
		}
		got := fanout(context.Background(), sources, cgkDps)
		if ids := flightIDs(got.flights); !reflect.DeepEqual(ids, []string{"a1", "a2", "c1"}) {
			t.Errorf("flights = %v, want a1 a2 c1 in source order", ids)
		}
		if got.queried != 4 || got.succeeded != 2 || got.failed != 2 || got.rejected != 2 {
			t.Errorf("queried/succeeded/failed/rejected = %d/%d/%d/%d, want 4/2/2/2", got.queried, got.succeeded, got.failed, got.rejected)
		}
		want := []ProviderError{{"B", CodeUnavailable, 3}, {"D", CodeRateLimited, 0}}
		if !reflect.DeepEqual(got.errors, want) {
			t.Errorf("errors = %+v, want %+v", got.errors, want)
		}
	})
}

func TestFanout_AllFail(t *testing.T) {
	sources := []source{
		fakeSource("A", 0, sourceResult{attempts: 3}, apperr.ErrUnavailable),
		fakeSource("B", 0, sourceResult{attempts: 1}, apperr.ErrMalformedResponse),
	}
	got := fanout(context.Background(), sources, cgkDps)
	if len(got.flights) != 0 || got.succeeded != 0 || got.failed != 2 {
		t.Errorf("got %d flights, %d succeeded, %d failed; want 0, 0, 2", len(got.flights), got.succeeded, got.failed)
	}
	want := []ProviderError{{"A", CodeUnavailable, 3}, {"B", CodeMalformed, 1}}
	if !reflect.DeepEqual(got.errors, want) {
		t.Errorf("errors = %+v, want %+v", got.errors, want)
	}
}

func TestFanout_RunsSourcesInParallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sources := []source{
			fakeSource("A", 100*time.Millisecond, ok("a"), nil),
			fakeSource("B", 100*time.Millisecond, ok("b"), nil),
			fakeSource("C", 100*time.Millisecond, ok("c"), nil),
		}
		start := time.Now()
		if got := fanout(context.Background(), sources, cgkDps); got.succeeded != 3 {
			t.Fatalf("succeeded = %d, want 3", got.succeeded)
		}
		if elapsed := time.Since(start); elapsed != 100*time.Millisecond {
			t.Errorf("fanout took %v, want 100ms", elapsed)
		}
	})
}

func TestFanout_DeadlineKeepsWhatArrived(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		stuck := source{provider: "Stuck", search: func(context.Context, leg) (sourceResult, error) {
			<-release // ignores ctx
			return ok("late"), nil
		}}
		sources := []source{
			fakeSource("Fast", 10*time.Millisecond, ok("fast"), nil),
			fakeSource("Slow", time.Second, ok("slow"), nil),
			stuck,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()
		start := time.Now()
		got := fanout(ctx, sources, cgkDps)
		if elapsed := time.Since(start); elapsed != 800*time.Millisecond {
			t.Errorf("fanout took %v, want the 800ms deadline", elapsed)
		}
		if ids := flightIDs(got.flights); !reflect.DeepEqual(ids, []string{"fast"}) {
			t.Errorf("flights = %v, want fast", ids)
		}
		want := []ProviderError{{"Slow", CodeTimeout, 0}, {"Stuck", CodeTimeout, 0}}
		if !reflect.DeepEqual(got.errors, want) || got.succeeded != 1 || got.failed != 2 {
			t.Errorf("errors = %+v, succeeded %d, failed %d; want %+v, 1, 2", got.errors, got.succeeded, got.failed, want)
		}
	})
}

func TestErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("x: %w", apperr.ErrUnavailable), CodeUnavailable},
		{fmt.Errorf("x: %w: %w", apperr.ErrUnavailable, resilience.ErrOpen), CodeUnavailable},
		{fmt.Errorf("%w: %w: x: %w", apperr.ErrUnavailable, resilience.ErrAttemptTimeout, context.DeadlineExceeded), CodeTimeout},
		{fmt.Errorf("x: %w", apperr.ErrRateLimited), CodeRateLimited},
		{fmt.Errorf("x: %w", apperr.ErrMalformedResponse), CodeMalformed},
		{fmt.Errorf("x: %w", context.DeadlineExceeded), CodeTimeout},
		{errors.New("anything else"), CodeUnavailable},
	}
	for _, tt := range tests {
		if got := errorCode(tt.err); got != tt.want {
			t.Errorf("errorCode(%v) = %s, want %s", tt.err, got, tt.want)
		}
	}
}

func mockGateways() Gateways {
	return Gateways{Garuda: garuda.NewMock(), LionAir: lionair.NewMock(), Batik: batik.NewMock(), AirAsia: airasia.NewMock(), FX: fxgw.NewMock()}
}

func TestService_FansOutToEveryAirline(t *testing.T) {
	s := New(mockGateways(), mapper.New(5*time.Minute), testConfig)
	got := fanout(context.Background(), s.sources, cgkDps)
	if got.queried != 4 || got.succeeded+got.failed != 4 {
		t.Fatalf("queried %d, succeeded %d, failed %d; want 4 in total", got.queried, got.succeeded, got.failed)
	}
	providers := map[string]bool{}
	for _, f := range got.flights {
		providers[f.Provider] = true
	}
	for _, e := range got.errors {
		providers[e.Provider] = true
	}
	for _, p := range []string{mapper.ProviderGaruda, mapper.ProviderLionAir, mapper.ProviderBatik, mapper.ProviderAirAsia} {
		if !providers[p] {
			t.Errorf("%s neither returned flights nor reported an error", p)
		}
	}
}

func TestService_FailingGatewayKeepsAttempts(t *testing.T) {
	gw := mockGateways()
	gw.Batik = failingBatik{}
	got := fanout(context.Background(), New(gw, mapper.New(5*time.Minute), testConfig).sources, cgkDps)
	want := ProviderError{Provider: mapper.ProviderBatik, Code: CodeUnavailable, Attempts: 3}
	if !containsError(got.errors, want) {
		t.Errorf("errors = %+v, want %+v", got.errors, want)
	}
}

func containsError(errs []ProviderError, want ProviderError) bool {
	for _, e := range errs {
		if e == want {
			return true
		}
	}
	return false
}

type failingBatik struct{}

func (failingBatik) Search(context.Context, batik.Query) (batik.Result, error) {
	return batik.Result{Attempts: 3}, fmt.Errorf("batik: %w", apperr.ErrUnavailable)
}
