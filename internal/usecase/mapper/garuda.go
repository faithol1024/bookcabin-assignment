package mapper

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
)

const ProviderGaruda = "Garuda Indonesia"

// Garuda builds the itinerary from segments when present, since the top level may describe only the first one (GA315).
func (m Mapper) Garuda(f garuda.Flight) (domain.Flight, error) {
	if f.FlightID == "" {
		return domain.Flight{}, errors.New("garuda: missing flight_id")
	}
	var cabin domain.Cabin
	fail := func(err error) (domain.Flight, error) {
		return domain.Flight{Cabin: cabin}, fmt.Errorf("garuda %s: %w", f.FlightID, err)
	}
	cabin, ok := domain.ParseCabin(f.FareClass)
	if !ok {
		return fail(fmt.Errorf("unknown cabin %q", f.FareClass))
	}
	if f.Price.Amount == "" || f.Price.Currency == "" {
		return fail(errors.New("missing price"))
	}
	itinDep, itinArr := f.Itinerary()
	dep, err := garudaAirport(itinDep, f.Departure)
	if err != nil {
		return fail(fmt.Errorf("departure: %w", err))
	}
	arr, err := garudaAirport(itinArr, f.Arrival)
	if err != nil {
		return fail(fmt.Errorf("arrival: %w", err))
	}
	layovers, consistent, err := m.garudaLayovers(f)
	if err != nil {
		return fail(err)
	}
	var bagCarryOn, bagChecked *int
	if f.Baggage != nil {
		bagCarryOn, bagChecked = f.Baggage.CarryOn, f.Baggage.Checked
	}
	carryOn, okC := pieces(bagCarryOn)
	checked, okH := pieces(bagChecked)

	out := domain.Flight{
		ID:             flightID(f.FlightID, ProviderGaruda),
		Provider:       ProviderGaruda,
		Airline:        domain.Airline{Name: f.Airline, Code: f.AirlineCode},
		FlightNumber:   f.FlightID,
		Departure:      dep,
		Arrival:        arr,
		Layovers:       layovers,
		Fare:           domain.Fare{Amount: f.Price.Amount.String(), Currency: f.Price.Currency},
		AvailableSeats: f.AvailableSeats,
		Cabin:          cabin,
		Aircraft:       optional(f.Aircraft),
		Amenities:      f.Amenities,
		Baggage:        domain.Baggage{CarryOn: carryOn, Checked: checked},
	}
	if m.mismatch(minutes(f.DurationMinutes), out.Duration()) {
		out.Warnings = append(out.Warnings, domain.WarningDurationMismatch)
	}
	if len(f.Segments) > 0 && (f.Departure.Airport != itinDep.Airport || f.Arrival.Airport != itinArr.Airport || f.Stops != len(f.Segments)-1) {
		consistent = false
	}
	if !consistent {
		out.Warnings = append(out.Warnings, domain.WarningSegmentInconsistent)
	}
	if !okC || !okH {
		out.Warnings = append(out.Warnings, domain.WarningBaggageUnparsed)
	}
	return out, nil
}

// garudaAirport parses p; segment points carry no city, so it comes from the top level for the same airport.
func garudaAirport(p, top garuda.Point) (domain.Airport, error) {
	if p.Airport == "" {
		return domain.Airport{}, errors.New("missing airport")
	}
	t, err := time.Parse(time.RFC3339, p.Time)
	if err != nil {
		return domain.Airport{}, err
	}
	a := domain.Airport{Code: p.Airport, City: optional(p.City), Time: t}
	if a.City == nil && p.Airport == top.Airport {
		a.City = optional(top.City)
	}
	return a, nil
}

// garudaLayovers derives layovers from segment timestamps; consistent is false when a segment's claims disagree with them.
func (m Mapper) garudaLayovers(f garuda.Flight) ([]domain.Layover, bool, error) {
	consistent := true
	var layovers []domain.Layover
	var prev domain.Airport // previous segment's arrival
	for i, s := range f.Segments {
		dep, err := garudaAirport(s.Departure, garuda.Point{})
		if err != nil {
			return nil, false, fmt.Errorf("segment %d departure: %w", i, err)
		}
		arr, err := garudaAirport(s.Arrival, garuda.Point{})
		if err != nil {
			return nil, false, fmt.Errorf("segment %d arrival: %w", i, err)
		}
		if m.mismatch(minutes(s.DurationMinutes), arr.Time.Sub(dep.Time)) {
			consistent = false
		}
		if i > 0 {
			layover := dep.Time.Sub(prev.Time)
			if layover < 0 {
				return nil, false, fmt.Errorf("segment %d departs before segment %d arrives", i, i-1)
			}
			if prev.Code != dep.Code || m.mismatch(minutes(s.LayoverMinutes), layover) {
				consistent = false
			}
			layovers = append(layovers, domain.Layover{Airport: prev.Code, Duration: layover})
		}
		prev = arr
	}
	return layovers, consistent, nil
}

// pieces renders a piece count as "1 piece" or "2 pieces"; a negative count is kept raw and reported unparsed.
func pieces(n *int) (string, bool) {
	switch {
	case n == nil:
		return "", true
	case *n < 0:
		return strconv.Itoa(*n), false
	case *n == 1:
		return "1 piece", true
	}
	return strconv.Itoa(*n) + " pieces", true
}
