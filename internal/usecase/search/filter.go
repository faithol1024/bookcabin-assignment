package search

import (
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// Window is minutes after midnight, inclusive; From > To wraps midnight.
type Window struct {
	From int
	To   int
}

// contains reads t in its own zone, which is the airport's local time.
func (w Window) contains(t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	if w.From <= w.To {
		return m >= w.From && m <= w.To
	}
	return m >= w.From || m <= w.To
}

// Filters are all optional and inclusive; prices are per passenger in IDR.
type Filters struct {
	PriceMin      *domain.Money
	PriceMax      *domain.Money
	MaxStops      *int
	DepartureTime *Window
	ArrivalTime   *Window
	Airlines      []string // IATA codes
	MaxDuration   *time.Duration
}

// filter keeps the flights matching every set filter with a seat for each passenger.
func filter(flights []domain.Flight, fl Filters, passengers int) []domain.Flight {
	airlines := make(map[string]bool, len(fl.Airlines))
	for _, code := range fl.Airlines {
		airlines[code] = true
	}
	kept := make([]domain.Flight, 0, len(flights))
	for _, f := range flights {
		if f.AvailableSeats >= passengers && fl.match(f, airlines) {
			kept = append(kept, f)
		}
	}
	return kept
}

func (fl Filters) match(f domain.Flight, airlines map[string]bool) bool {
	switch {
	case fl.PriceMin != nil && f.Price < *fl.PriceMin,
		fl.PriceMax != nil && f.Price > *fl.PriceMax,
		fl.MaxStops != nil && f.Stops() > *fl.MaxStops,
		fl.DepartureTime != nil && !fl.DepartureTime.contains(f.Departure.Time),
		fl.ArrivalTime != nil && !fl.ArrivalTime.contains(f.Arrival.Time),
		len(airlines) > 0 && !airlines[f.Airline.Code],
		fl.MaxDuration != nil && f.Duration() > *fl.MaxDuration:
		return false
	}
	return true
}
