package search

import (
	"math"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// Weights are normalized by their sum.
type Weights struct {
	Price    float64
	Duration float64
	Stops    float64
	Redeye   float64
}

type Score struct {
	Weights      Weights
	MaxStops     int
	RedeyeWindow Window // local departure time
}

// Flight is a search result: the flight, its best-value score (lower is better) and its price for all passengers.
type Flight struct {
	domain.Flight
	Score float64
	Total domain.Money
}

// rank scores each flight against the whole set, so it runs after filtering and before paging.
func (s Score) rank(flights []domain.Flight, passengers int) []Flight {
	if len(flights) == 0 {
		return nil
	}
	minPrice, maxPrice := flights[0].Price, flights[0].Price
	minDur, maxDur := flights[0].Duration(), flights[0].Duration()
	for _, f := range flights[1:] {
		minPrice, maxPrice = min(minPrice, f.Price), max(maxPrice, f.Price)
		minDur, maxDur = min(minDur, f.Duration()), max(maxDur, f.Duration())
	}
	w := s.Weights
	sum := w.Price + w.Duration + w.Stops + w.Redeye
	out := make([]Flight, len(flights))
	for i, f := range flights {
		stops := float64(min(f.Stops(), s.MaxStops)) / float64(s.MaxStops)
		redeye := 0.0
		if s.RedeyeWindow.contains(f.Departure.Time) {
			redeye = 1
		}
		score := (w.Price*normalized(float64(f.Price), float64(minPrice), float64(maxPrice)) +
			w.Duration*normalized(float64(f.Duration()), float64(minDur), float64(maxDur)) +
			w.Stops*stops + w.Redeye*redeye) / sum
		out[i] = Flight{Flight: f, Score: math.Round(score*1e4) / 1e4, Total: f.Price * domain.Money(passengers)}
	}
	return out
}

// normalized maps v into 0..1 over lo..hi; a zero range gives 0, never NaN.
func normalized(v, lo, hi float64) float64 {
	if hi == lo {
		return 0
	}
	return (v - lo) / (hi - lo)
}
