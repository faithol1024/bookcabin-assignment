package search

import (
	"context"
	"errors"
	"sync"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// Provider error codes reported in metadata.provider_errors.
const (
	CodeUnavailable = "upstream_unavailable"
	CodeRateLimited = "rate_limited"
	CodeMalformed   = "malformed_response"
	CodeTimeout     = "timeout"
)

type leg struct {
	LegQuery
	cabin domain.Cabin
}

// source is one airline gateway plus its mapper, in the one shape fan-out needs.
type source struct {
	provider string
	search   func(ctx context.Context, l leg) (sourceResult, error)
}

type sourceResult struct {
	flights  []domain.Flight
	rejected int
	attempts int
	cacheHit bool
}

type ProviderError struct {
	Provider string
	Code     string
	Attempts int
}

type fanoutResult struct {
	flights   []domain.Flight
	rejected  int
	queried   int
	succeeded int
	failed    int
	errors    []ProviderError
	cacheHit  bool // every source answered from its cache
}

// fanout queries every source in parallel and keeps what arrives before ctx ends; a failed or late
// source never fails the leg. Results keep source order.
func fanout(ctx context.Context, sources []source, l leg) fanoutResult {
	type outcome struct {
		res  sourceResult
		err  error
		done bool
	}
	type indexed struct {
		i int
		outcome
	}
	ch := make(chan indexed, len(sources))
	var wg sync.WaitGroup
	for i, s := range sources {
		wg.Go(func() {
			res, err := s.search(ctx, l)
			ch <- indexed{i, outcome{res, err, true}}
		})
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	outcomes := make([]outcome, len(sources))
collect:
	for {
		select {
		case o, ok := <-ch:
			if !ok {
				break collect
			}
			outcomes[o.i] = o.outcome
		case <-ctx.Done():
			// keep what already arrived; the rest are late
			for {
				select {
				case o, ok := <-ch:
					if ok {
						outcomes[o.i] = o.outcome
						continue
					}
				default:
				}
				break collect
			}
		}
	}

	n := 0
	for _, o := range outcomes {
		n += len(o.res.flights)
	}
	out := fanoutResult{queried: len(sources), flights: make([]domain.Flight, 0, n)}
	hits := 0
	for i, o := range outcomes {
		switch {
		case !o.done:
			out.errors = append(out.errors, ProviderError{Provider: sources[i].provider, Code: CodeTimeout})
		case o.err != nil:
			out.errors = append(out.errors, ProviderError{Provider: sources[i].provider, Code: errorCode(o.err), Attempts: o.res.attempts})
		default:
			out.succeeded++
			out.flights = append(out.flights, o.res.flights...)
			out.rejected += o.res.rejected
			if o.res.cacheHit {
				hits++
			}
		}
	}
	out.failed = len(out.errors)
	out.cacheHit = len(sources) > 0 && hits == len(sources)
	return out
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, apperr.ErrRateLimited):
		return CodeRateLimited
	case errors.Is(err, apperr.ErrMalformedResponse):
		return CodeMalformed
	case errors.Is(err, context.DeadlineExceeded):
		return CodeTimeout
	default:
		return CodeUnavailable
	}
}
