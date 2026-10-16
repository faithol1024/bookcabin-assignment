package search

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// streamed renders each event as "leg:provider:ids", or "leg:summary" for a leg's closing event.
func streamed(t *testing.T, s *Service, c Criteria) ([]string, []Leg, error) {
	t.Helper()
	var (
		got       []string
		summaries []Leg
	)
	err := s.Stream(context.Background(), c, func(e Event) {
		if e.Summary != nil {
			got = append(got, strconv.Itoa(e.Leg)+":summary")
			summaries = append(summaries, *e.Summary)
			return
		}
		got = append(got, strconv.Itoa(e.Leg)+":"+e.Provider+":"+strings.Join(rankedIDs(e.Flights), ","))
	})
	return got, summaries, err
}

func TestStream_EmitsEachProviderAsItAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := testService(
			fakeSource("Slow", 30*time.Millisecond, sourceResult{flights: []domain.Flight{offer("s1", "Slow", price(900_000))}, attempts: 1}, nil),
			fakeSource("Down", 5*time.Millisecond, sourceResult{attempts: 3}, apperr.ErrUnavailable),
			fakeSource("Fast", 10*time.Millisecond, sourceResult{flights: []domain.Flight{
				offer("f2", "Fast", price(2_000_000)), offer("f1", "Fast", price(1_000_000)),
			}, rejected: 1, attempts: 1}, nil),
		)
		c := oneWay(1)
		c.Sort = Sort{By: SortPrice}
		got, summaries, err := streamed(t, s, c)
		if err != nil {
			t.Fatal(err)
		}
		// each batch is sorted on its own and never paged
		want := []string{"0:Fast:f1,f2", "0:Slow:s1", "0:summary"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("events = %v, want %v", got, want)
		}
		sum := summaries[0]
		if sum.TotalResults != 3 || sum.ProvidersSucceeded != 2 || sum.ProvidersFailed != 1 || sum.FlightsRejected != 1 {
			t.Errorf("total/succeeded/failed/rejected = %d/%d/%d/%d, want 3/2/1/1", sum.TotalResults, sum.ProvidersSucceeded, sum.ProvidersFailed, sum.FlightsRejected)
		}
		if want := map[string]domain.Money{"Fast": 1_000_000, "Slow": 900_000}; !reflect.DeepEqual(sum.CheapestByProvider, want) {
			t.Errorf("cheapest = %v, want %v", sum.CheapestByProvider, want)
		}
		if want := []ProviderError{{"Down", CodeUnavailable, 3}}; !reflect.DeepEqual(sum.ProviderErrors, want) {
			t.Errorf("errors = %+v, want %+v", sum.ProviderErrors, want)
		}
	})
}

func TestStream_SummaryPerLeg(t *testing.T) {
	c := oneWay(20)
	c.ReturnDate = "2025-12-18"
	s := testService(routeSource("A", nil, "DPSCGK"))
	got, summaries, err := streamed(t, s, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(summaries) != 2 {
		t.Fatalf("events = %v, want one batch and two summaries", got)
	}
	for _, sum := range summaries {
		if sum.Flights != nil {
			t.Errorf("summary for %s carries flights", sum.Origin)
		}
	}
}

func TestStream_AllProvidersFail(t *testing.T) {
	s := testService(routeSource("A", nil, "CGKDPS"), routeSource("B", nil, "CGKDPS"))
	got, _, err := streamed(t, s, oneWay(20))
	if !errors.Is(err, apperr.ErrAllProvidersFailed) {
		t.Errorf("err = %v, want ErrAllProvidersFailed", err)
	}
	if !reflect.DeepEqual(got, []string{"0:summary"}) {
		t.Errorf("events = %v, want only the summary", got)
	}
}

func TestStream_RejectsInvalidCriteria(t *testing.T) {
	err := testService().Stream(context.Background(), Criteria{}, func(Event) { t.Error("emitted") })
	if !errors.Is(err, apperr.ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

// slowFX quotes every currency after delay.
type slowFX struct{ delay time.Duration }

func (f slowFX) Rate(ctx context.Context, _ string) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(f.delay):
		return 16250, nil
	}
}

func TestStream_SlowBatchDoesNotBlockOthers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		usd := offer("a1", "A", price(100))
		usd.Fare = domain.Fare{Amount: "65.50", Currency: "USD"}
		s := testService(
			fakeSource("A", 10*time.Millisecond, sourceResult{flights: []domain.Flight{usd}, attempts: 1}, nil),
			fakeSource("B", 20*time.Millisecond, sourceResult{flights: []domain.Flight{offer("b1", "B")}, attempts: 1}, nil),
		)
		s.converter.fx = slowFX{500 * time.Millisecond}
		start := time.Now()
		var got []string
		err := s.Stream(context.Background(), oneWay(20), func(e Event) {
			if e.Summary == nil {
				got = append(got, e.Provider+"@"+time.Since(start).String())
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		// B answers while A still waits on FX, so B goes out first and on time
		if want := []string{"B@20ms", "A@510ms"}; !reflect.DeepEqual(got, want) {
			t.Errorf("events = %v, want %v", got, want)
		}
	})
}

func TestStream_LateSourceNeverEmitsAfterSummary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		stuck := source{provider: "Stuck", search: func(context.Context, leg) (sourceResult, error) {
			<-release // ignores ctx, so it answers after the leg is over
			return sourceResult{flights: []domain.Flight{offer("late", "Stuck")}, attempts: 1}, nil
		}}
		s := testService(fakeSource("A", 10*time.Millisecond, sourceResult{flights: []domain.Flight{offer("a1", "A")}, attempts: 1}, nil), stuck)
		var got []string
		err := s.Stream(context.Background(), oneWay(20), func(e Event) {
			if e.Summary != nil {
				got = append(got, "summary")
				return
			}
			got = append(got, e.Provider)
		})
		if err != nil {
			t.Fatal(err)
		}
		close(release)
		synctest.Wait()
		if want := []string{"A", "summary"}; !reflect.DeepEqual(got, want) {
			t.Errorf("events = %v, want %v", got, want)
		}
	})
}
