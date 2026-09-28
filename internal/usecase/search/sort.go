package search

import (
	"cmp"
	"slices"
	"strings"
)

type SortKey string

const (
	SortBestValue SortKey = "best_value"
	SortPrice     SortKey = "price"
	SortDuration  SortKey = "duration"
	SortDeparture SortKey = "departure"
	SortArrival   SortKey = "arrival"
)

type Order string

const (
	Asc  Order = "asc"
	Desc Order = "desc"
)

// Sort's zero value is best_value asc.
type Sort struct {
	By    SortKey
	Order Order
}

var keys = map[SortKey]func(a, b Flight) int{
	SortBestValue: func(a, b Flight) int { return cmp.Compare(a.Score, b.Score) },
	SortPrice:     func(a, b Flight) int { return cmp.Compare(a.Price, b.Price) },
	SortDuration:  func(a, b Flight) int { return cmp.Compare(a.Duration(), b.Duration()) },
	SortDeparture: func(a, b Flight) int { return a.Departure.Time.Compare(b.Departure.Time) },
	SortArrival:   func(a, b Flight) int { return a.Arrival.Time.Compare(b.Arrival.Time) },
}

func (s Sort) withDefaults() Sort {
	if s.By == "" {
		s.By = SortBestValue
	}
	if s.Order == "" {
		s.Order = Asc
	}
	return s
}

func (k SortKey) Valid() bool {
	_, ok := keys[k]
	return ok
}

func (o Order) Valid() bool { return o == Asc || o == Desc }

// sortFlights orders by the key, then by ascending ID in either order, so the order is total.
func sortFlights(flights []Flight, s Sort) {
	byKey := keys[s.By]
	slices.SortFunc(flights, func(a, b Flight) int {
		c := byKey(a, b)
		if s.Order == Desc {
			c = -c
		}
		if c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}
