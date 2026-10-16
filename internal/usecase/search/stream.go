package search

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// Event is one provider's batch on a leg, or, when Summary is set, the leg's closing metadata.
type Event struct {
	Leg      int // index into the criteria's legs
	Provider string
	Flights  []Flight // scored and sorted within the batch, not paged
	Summary  *Leg     // its Flights is nil
}

// Stream emits each provider's flights as soon as they arrive, then a summary per leg; emit is never
// called concurrently. Like Search, it fails only when no provider answered on any leg.
func (s *Service) Stream(ctx context.Context, c Criteria, emit func(Event)) error {
	c.Sort = c.Sort.withDefaults()
	if err := c.check(); err != nil {
		return err
	}
	out := &emitter{emit: emit}
	queries := c.legs()
	answered := make([]bool, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Go(func() { answered[i] = s.streamLeg(ctx, i, q, c, out) })
	}
	wg.Wait()
	if !slices.Contains(answered, true) {
		return fmt.Errorf("stream %d legs: %w", len(queries), apperr.ErrAllProvidersFailed)
	}
	return nil
}

// streamLeg sends each provider's batch from that provider's own goroutine, so a slow batch (an FX call)
// never holds up another, then the leg summary. It reports whether any provider answered.
func (s *Service) streamLeg(ctx context.Context, i int, q LegQuery, c Criteria, out *emitter) bool {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.deadline)
	defer cancel()

	ls := &legStream{svc: s, leg: i, criteria: c, out: out, cheapest: map[string]domain.Money{}}
	sources := make([]source, len(s.sources))
	for j, src := range s.sources {
		sources[j] = source{provider: src.provider, search: streamingSource{src, ls}.search}
	}
	fo := fanout(ctx, sources, leg{q, c.Cabin})
	ls.sendSummary(q, fo, time.Since(start))
	return fo.succeeded > 0
}

// emitter serializes events from every leg and provider onto one emit func.
type emitter struct {
	mu   sync.Mutex
	emit func(Event)
}

func (e *emitter) send(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emit(ev)
}

// legStream keeps one leg's totals while its providers send batches concurrently.
type legStream struct {
	svc      *Service
	leg      int
	criteria Criteria
	out      *emitter

	mu       sync.Mutex // guards the totals below and keeps every batch before the summary
	total    int
	dropped  int
	cheapest map[string]domain.Money
}

// sendBatch processes one provider's flights and sends them; past the leg deadline it sends nothing
// and returns the context error, since the summary may already be out.
func (ls *legStream) sendBatch(ctx context.Context, provider string, flights []domain.Flight) error {
	ranked, kept, dropped := ls.svc.process(ctx, flights, ls.criteria)

	ls.mu.Lock()
	defer ls.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	ls.total += len(ranked)
	ls.dropped += dropped
	maps.Copy(ls.cheapest, cheapestByProvider(kept))
	ls.out.send(Event{Leg: ls.leg, Provider: provider, Flights: ranked})
	return nil
}

// sendSummary holds the lock while sending, so a batch that is still processing cannot follow it.
func (ls *legStream) sendSummary(q LegQuery, fo fanoutResult, elapsed time.Duration) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.out.send(Event{Leg: ls.leg, Summary: &Leg{
		LegQuery:           q,
		TotalResults:       ls.total,
		ProvidersQueried:   fo.queried,
		ProvidersSucceeded: fo.succeeded,
		ProvidersFailed:    fo.failed,
		FlightsRejected:    fo.rejected + ls.dropped,
		CheapestByProvider: ls.cheapest,
		SearchTime:         elapsed,
		CacheHit:           fo.cacheHit,
		ProviderErrors:     fo.errors,
	}})
}

// streamingSource is a source that sends its own batch the moment it answers.
type streamingSource struct {
	source
	ls *legStream
}

func (s streamingSource) search(ctx context.Context, l leg) (sourceResult, error) {
	res, err := s.source.search(ctx, l)
	if err != nil {
		return res, err
	}
	if err := s.ls.sendBatch(ctx, s.provider, res.flights); err != nil {
		return sourceResult{attempts: res.attempts}, err // too late: fanout reports it as a timeout
	}
	return res, nil
}
