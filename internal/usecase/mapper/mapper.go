package mapper

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

type Mapper struct {
	durationMismatch time.Duration // provider-claimed vs computed duration tolerance
}

func New(durationMismatch time.Duration) Mapper {
	return Mapper{durationMismatch: durationMismatch}
}

// Records maps raw provider records and keeps only the requested cabin. It counts the records it cannot
// map, except those of another cabin: a mapper that fails after reading the cabin still returns it.
func Records[T any](ctx context.Context, records []T, toFlight func(T) (domain.Flight, error), cabin domain.Cabin) ([]domain.Flight, int) {
	var flights []domain.Flight
	rejected := 0
	for _, r := range records {
		f, err := toFlight(r)
		if err != nil {
			if f.Cabin != "" && f.Cabin != cabin {
				continue
			}
			rejected++
			slog.DebugContext(ctx, "flight rejected", "error", err)
			continue
		}
		if f.Cabin == cabin {
			flights = append(flights, f)
		}
	}
	return flights, rejected
}

func flightID(flightNumber, provider string) string {
	return flightNumber + "_" + strings.ReplaceAll(provider, " ", "")
}

// mismatch reports a claimed duration off by more than the tolerance; a zero claim means none was given.
func (m Mapper) mismatch(claimed, actual time.Duration) bool {
	if claimed == 0 {
		return false
	}
	diff := claimed - actual
	return diff > m.durationMismatch || -diff > m.durationMismatch
}

func minutes(n int) time.Duration {
	return time.Duration(n) * time.Minute
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
