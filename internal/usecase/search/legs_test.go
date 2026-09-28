package search

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
)

// testConfig mirrors the committed config files.
var testConfig = Config{Deadline: 800 * time.Millisecond, Exponents: exponents, Score: committedScore}

func testService(sources ...source) *Service {
	return &Service{
		sources:   sources,
		converter: converter{exponents: exponents, fx: newFakeFX(mockRates)},
		deadline:  testConfig.Deadline,
		score:     committedScore,
	}
}

// offer is flight sold by provider, with its Price as an IDR fare for the converter.
func offer(id, provider string, opts ...option) domain.Flight {
	f := flight(id, opts...)
	f.Provider = provider
	f.Fare = domain.Fare{Amount: strconv.FormatInt(int64(f.Price), 10), Currency: "IDR"}
	f.Price = 0
	return f
}

// routeSource answers each route with its flights, fails the routes in down and returns nothing elsewhere.
func routeSource(provider string, flights map[string][]domain.Flight, down ...string) source {
	return source{provider: provider, search: func(_ context.Context, l leg) (sourceResult, error) {
		route := l.Origin + l.Destination
		if slices.Contains(down, route) {
			return sourceResult{attempts: 3}, apperr.ErrUnavailable
		}
		return sourceResult{flights: flights[route], attempts: 1}, nil
	}}
}

func oneWay(limit int) Criteria {
	return Criteria{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", Passengers: 1, Cabin: domain.CabinEconomy, Limit: limit}
}

func pageIDs(l Leg) []string {
	return rankedIDs(l.Flights)
}

func TestCriteria_Legs(t *testing.T) {
	multi := []LegQuery{{"CGK", "DPS", "2025-12-15"}, {"DPS", "SUB", "2025-12-17"}, {"SUB", "CGK", "2025-12-20"}}
	roundTrip := oneWay(20)
	roundTrip.ReturnDate = "2025-12-18"
	tests := []struct {
		name string
		c    Criteria
		want []LegQuery
	}{
		{"one-way", oneWay(20), []LegQuery{{"CGK", "DPS", "2025-12-15"}}},
		{"round trip", roundTrip, []LegQuery{{"CGK", "DPS", "2025-12-15"}, {"DPS", "CGK", "2025-12-18"}}},
		{"multi-city", Criteria{Legs: multi}, multi},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.legs(); !slices.Equal(got, tt.want) {
				t.Errorf("legs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearch_RejectsInvalidCriteria(t *testing.T) {
	tests := map[string]func(*Criteria){
		"zero limit":    func(c *Criteria) { c.Limit = 0 },
		"negative skip": func(c *Criteria) { c.Offset = -1 },
		"no passengers": func(c *Criteria) { c.Passengers = 0 },
		"sort key":      func(c *Criteria) { c.Sort.By = "cheapest" },
		"sort order":    func(c *Criteria) { c.Sort.Order = "up" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := oneWay(20)
			mutate(&c)
			if _, err := testService().Search(context.Background(), c); !errors.Is(err, apperr.ErrInvalidInput) {
				t.Errorf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestSearch_OneWay(t *testing.T) {
	cgkDps := map[string][]domain.Flight{"CGKDPS": {
		offer("GA1", "Garuda Indonesia", price(1_200_000)),
		offer("GA2", "Garuda Indonesia", price(900_000), stops(1)),
		offer("GA3", "Garuda Indonesia", price(2_500_000)), // over priceMax
		offer("GA4", "Garuda Indonesia", price(0)),         // rejected by validation
	}}
	jtDps := map[string][]domain.Flight{"CGKDPS": {
		offer("JT1", "Lion Air", price(700_000), airline("JT"), minutesLong(180)),
		offer("JT2", "Lion Air", price(800_000), airline("JT"), seats(1)), // too few seats
	}}
	s := testService(routeSource("Garuda Indonesia", cgkDps), routeSource("Lion Air", jtDps))
	c := oneWay(2)
	c.Passengers = 2
	c.Filters.PriceMax = ptr[domain.Money](2_000_000)

	res, err := s.Search(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Legs) != 1 {
		t.Fatalf("got %d legs, want 1", len(res.Legs))
	}
	l := res.Legs[0]
	// default best_value asc: GA2 0.2 price + 0.075 stop, JT1 0.3 slowest, GA1 0.5 dearest
	if got := pageIDs(l); !slices.Equal(got, []string{"GA2", "JT1"}) {
		t.Errorf("page = %v, want GA2 JT1", got)
	}
	if l.LegQuery != (LegQuery{"CGK", "DPS", "2025-12-15"}) || l.TotalResults != 3 || l.FlightsRejected != 1 {
		t.Errorf("leg %+v, total %d, rejected %d; want CGK→DPS, 3, 1", l.LegQuery, l.TotalResults, l.FlightsRejected)
	}
	if l.ProvidersQueried != 2 || l.ProvidersSucceeded != 2 || l.ProvidersFailed != 0 || l.CacheHit || len(l.ProviderErrors) != 0 {
		t.Errorf("providers %d/%d/%d, cache %v, errors %v", l.ProvidersQueried, l.ProvidersSucceeded, l.ProvidersFailed, l.CacheHit, l.ProviderErrors)
	}
	want := map[string]domain.Money{"Garuda Indonesia": 900_000, "Lion Air": 700_000}
	if !maps.Equal(l.CheapestByProvider, want) {
		t.Errorf("cheapest = %v, want %v", l.CheapestByProvider, want)
	}
	jt1 := l.Flights[1]
	if jt1.Price != 700_000 || jt1.Total != 1_400_000 || jt1.Score != 0.3 {
		t.Errorf("JT1 price %d, total %d, score %v; want 700000, 1400000, 0.3", jt1.Price, jt1.Total, jt1.Score)
	}
	if city(jt1.Departure) != "Jakarta" || city(jt1.Arrival) != "Denpasar" {
		t.Errorf("JT1 cities %s, %s", city(jt1.Departure), city(jt1.Arrival))
	}
	if l.SearchTime <= 0 {
		t.Errorf("SearchTime = %v", l.SearchTime)
	}
}

func TestSearch_SortAndPage(t *testing.T) {
	flights := map[string][]domain.Flight{"CGKDPS": {
		offer("A", "P", price(300)), offer("B", "P", price(100)), offer("C", "P", price(200)),
	}}
	s := testService(routeSource("P", flights))
	c := oneWay(2)
	c.Sort = Sort{By: SortPrice, Order: Desc}
	c.Offset = 1
	res, err := s.Search(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if got := pageIDs(res.Legs[0]); !slices.Equal(got, []string{"C", "B"}) || res.Legs[0].TotalResults != 3 {
		t.Errorf("page %v of %d, want C B of 3", got, res.Legs[0].TotalResults)
	}
}

func TestSearch_OffsetPastEnd(t *testing.T) {
	s := testService(routeSource("P", map[string][]domain.Flight{"CGKDPS": {offer("A", "P"), offer("B", "P")}}))
	for _, offset := range []int{2, 3, 1 << 40} {
		c := oneWay(20)
		c.Offset = offset
		res, err := s.Search(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if l := res.Legs[0]; len(l.Flights) != 0 || l.TotalResults != 2 {
			t.Errorf("offset %d: page %v of %d, want empty of 2", offset, pageIDs(l), l.TotalResults)
		}
	}
}

func TestSearch_RoundTripWithEmptyInbound(t *testing.T) {
	s := testService(routeSource("P", map[string][]domain.Flight{"CGKDPS": {offer("OUT", "P")}}))
	c := oneWay(20)
	c.ReturnDate = "2025-12-18"
	res, err := s.Search(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Legs) != 2 {
		t.Fatalf("got %d legs, want outbound and inbound", len(res.Legs))
	}
	out, in := res.Legs[0], res.Legs[1]
	if out.LegQuery != (LegQuery{"CGK", "DPS", "2025-12-15"}) || !slices.Equal(pageIDs(out), []string{"OUT"}) {
		t.Errorf("outbound %+v %v", out.LegQuery, pageIDs(out))
	}
	if in.LegQuery != (LegQuery{"DPS", "CGK", "2025-12-18"}) || len(in.Flights) != 0 || in.TotalResults != 0 || in.ProvidersSucceeded != 1 {
		t.Errorf("inbound %+v %v, total %d, succeeded %d; want empty DPS→CGK", in.LegQuery, pageIDs(in), in.TotalResults, in.ProvidersSucceeded)
	}
}

func TestSearch_MultiCityWithOneLegFailing(t *testing.T) {
	flights := map[string][]domain.Flight{
		"CGKDPS": {offer("L1", "P")},
		"SUBCGK": {offer("L3", "P")},
	}
	s := testService(routeSource("P", flights, "DPSSUB"), routeSource("Q", nil, "DPSSUB"))
	c := Criteria{
		Legs:       []LegQuery{{"CGK", "DPS", "2025-12-15"}, {"DPS", "SUB", "2025-12-17"}, {"SUB", "CGK", "2025-12-20"}},
		Passengers: 1, Cabin: domain.CabinEconomy, Limit: 20,
	}
	res, err := s.Search(context.Background(), c)
	if err != nil {
		t.Fatalf("err = %v, want partial success", err)
	}
	if len(res.Legs) != 3 {
		t.Fatalf("got %d legs, want 3", len(res.Legs))
	}
	failed := res.Legs[1]
	want := []ProviderError{{"P", CodeUnavailable, 3}, {"Q", CodeUnavailable, 3}}
	if failed.ProvidersSucceeded != 0 || failed.ProvidersFailed != 2 || !slices.Equal(failed.ProviderErrors, want) || len(failed.Flights) != 0 {
		t.Errorf("failed leg: succeeded %d, failed %d, errors %v, flights %v", failed.ProvidersSucceeded, failed.ProvidersFailed, failed.ProviderErrors, pageIDs(failed))
	}
	if !slices.Equal(pageIDs(res.Legs[0]), []string{"L1"}) || !slices.Equal(pageIDs(res.Legs[2]), []string{"L3"}) {
		t.Errorf("legs 1 and 3 = %v, %v; want L1, L3", pageIDs(res.Legs[0]), pageIDs(res.Legs[2]))
	}
}

func TestSearch_AllLegsFail(t *testing.T) {
	s := testService(routeSource("P", nil, "CGKDPS", "DPSCGK"))
	c := oneWay(20)
	c.ReturnDate = "2025-12-18"
	if _, err := s.Search(context.Background(), c); !errors.Is(err, apperr.ErrAllProvidersFailed) {
		t.Errorf("err = %v, want ErrAllProvidersFailed", err)
	}
}

func TestSearch_DeadlineBoundsEachLeg(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := testService(fakeSource("Fast", 10*time.Millisecond, sourceResult{flights: []domain.Flight{offer("F", "Fast")}}, nil),
			fakeSource("Slow", time.Hour, sourceResult{}, nil))
		c := oneWay(20)
		c.ReturnDate = "2025-12-18"
		start := time.Now()
		res, err := s.Search(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != testConfig.Deadline {
			t.Errorf("search took %v, want the %v deadline for both legs in parallel", elapsed, testConfig.Deadline)
		}
		for _, l := range res.Legs {
			if l.TotalResults != 1 || !slices.Equal(l.ProviderErrors, []ProviderError{{"Slow", CodeTimeout, 0}}) {
				t.Errorf("%+v: total %d, errors %v", l.LegQuery, l.TotalResults, l.ProviderErrors)
			}
		}
	})
}

func TestService_SearchesMockGateways(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(mockGateways(), mapper.New(5*time.Minute), testConfig)
		res, err := s.Search(context.Background(), oneWay(100))
		if err != nil {
			t.Fatal(err)
		}
		l := res.Legs[0]
		if l.ProvidersQueried != 4 || l.FlightsRejected != 0 {
			t.Fatalf("queried %d, rejected %d, errors %v; want 4, 0", l.ProvidersQueried, l.FlightsRejected, l.ProviderErrors)
		}
		for _, f := range l.Flights {
			if f.Departure.Code != "CGK" || f.Arrival.Code != "DPS" || f.Price <= 0 || f.Total != f.Price || f.Departure.City == nil {
				t.Errorf("%s: %s→%s, price %d, total %d", f.ID, f.Departure.Code, f.Arrival.Code, f.Price, f.Total)
			}
		}
		// the seeded AirAsia mock answers its first request, so all 13 given flights come back
		if want := 13; l.ProvidersFailed != 0 || l.TotalResults != want || len(l.Flights) != want {
			t.Errorf("total %d, page %d, want %d (errors %v)", l.TotalResults, len(l.Flights), want, l.ProviderErrors)
		}
	})
}
