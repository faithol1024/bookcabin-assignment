package search

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

type LegQuery struct {
	Origin      string
	Destination string
	Date        string // YYYY-MM-DD in the origin's local zone
}

// Criteria is a validated request: one-way, round trip (ReturnDate) or multi-city (Legs); bounds are the handler's.
type Criteria struct {
	Origin        string
	Destination   string
	DepartureDate string
	ReturnDate    string
	Legs          []LegQuery // multi-city, instead of the fields above
	Passengers    int
	Cabin         domain.Cabin
	Filters       Filters // applied to every leg
	Sort          Sort
	Limit         int
	Offset        int
}

// legs is 1 leg one-way, 2 for a round trip, or the multi-city legs as given.
func (c Criteria) legs() []LegQuery {
	if len(c.Legs) > 0 {
		return c.Legs
	}
	out := []LegQuery{{Origin: c.Origin, Destination: c.Destination, Date: c.DepartureDate}}
	if c.ReturnDate != "" {
		out = append(out, LegQuery{Origin: c.Destination, Destination: c.Origin, Date: c.ReturnDate})
	}
	return out
}

func (c Criteria) check() error {
	if c.Passengers < 1 || c.Limit < 1 || c.Offset < 0 || !c.Sort.By.Valid() || !c.Sort.Order.Valid() {
		return fmt.Errorf("%w: %d passengers, limit %d, offset %d, sort %s %s", apperr.ErrInvalidInput, c.Passengers, c.Limit, c.Offset, c.Sort.By, c.Sort.Order)
	}
	return nil
}

type Response struct {
	Legs []Leg // in criteria order
}

type Leg struct {
	LegQuery
	Flights            []Flight // the requested page
	TotalResults       int      // after filters, before paging
	ProvidersQueried   int
	ProvidersSucceeded int
	ProvidersFailed    int
	FlightsRejected    int
	CheapestByProvider map[string]domain.Money // per passenger, over the filtered set
	SearchTime         time.Duration
	CacheHit           bool
	ProviderErrors     []ProviderError
}

// Search runs every leg in parallel; it fails only when no provider answered on any leg.
func (s *Service) Search(ctx context.Context, c Criteria) (Response, error) {
	c.Sort = c.Sort.withDefaults()
	if err := c.check(); err != nil {
		return Response{}, err
	}
	queries := c.legs()
	legs := make([]Leg, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Go(func() { legs[i] = s.searchLeg(ctx, q, c) })
	}
	wg.Wait()
	if !slices.ContainsFunc(legs, func(l Leg) bool { return l.ProvidersSucceeded > 0 }) {
		return Response{}, fmt.Errorf("search %d legs: %w", len(legs), apperr.ErrAllProvidersFailed)
	}
	return Response{Legs: legs}, nil
}

func (s *Service) searchLeg(ctx context.Context, q LegQuery, c Criteria) Leg {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.deadline)
	defer cancel()

	fo := fanout(ctx, s.sources, leg{q, c.Cabin})
	flights, unconverted := s.converter.convert(ctx, fo.flights)
	flights, invalid := validate(ctx, flights)
	flights = filter(flights, c.Filters, c.Passengers)
	fillCities(flights)
	ranked := s.score.rank(flights, c.Passengers)
	sortFlights(ranked, c.Sort)

	return Leg{
		LegQuery:           q,
		Flights:            page(ranked, c.Limit, c.Offset),
		TotalResults:       len(ranked),
		ProvidersQueried:   fo.queried,
		ProvidersSucceeded: fo.succeeded,
		ProvidersFailed:    fo.failed,
		FlightsRejected:    fo.rejected + unconverted + invalid,
		CheapestByProvider: cheapestByProvider(flights),
		SearchTime:         time.Since(start),
		CacheHit:           fo.cacheHit,
		ProviderErrors:     fo.errors,
	}
}

func page(flights []Flight, limit, offset int) []Flight {
	start := min(offset, len(flights))
	return flights[start : start+min(limit, len(flights)-start)]
}

func cheapestByProvider(flights []domain.Flight) map[string]domain.Money {
	out := map[string]domain.Money{}
	for _, f := range flights {
		if p, ok := out[f.Provider]; !ok || f.Price < p {
			out[f.Provider] = f.Price
		}
	}
	return out
}
