package search

import (
	"context"
	"log/slog"
	"slices"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// rules each report a flight to reject.
var rules = []func(domain.Flight) bool{arrivalNotAfterDeparture, priceNotPositive}

// validate applies every rule to the mapped flights and returns the kept ones and how many were rejected.
func validate(ctx context.Context, flights []domain.Flight) ([]domain.Flight, int) {
	kept := make([]domain.Flight, 0, len(flights))
	rejected := 0
	for _, f := range flights {
		if slices.ContainsFunc(rules, func(reject func(domain.Flight) bool) bool { return reject(f) }) {
			rejected++
			slog.DebugContext(ctx, "flight rejected", "id", f.ID, "departure", f.Departure.Time, "arrival", f.Arrival.Time, "price", f.Price)
			continue
		}
		kept = append(kept, f)
	}
	return kept, rejected
}

func arrivalNotAfterDeparture(f domain.Flight) bool {
	return !f.Arrival.Time.After(f.Departure.Time)
}

func priceNotPositive(f domain.Flight) bool {
	return f.Price <= 0
}
