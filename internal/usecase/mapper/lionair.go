package mapper

import (
	"errors"
	"fmt"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	"github.com/faithol1024/bookcabin-assignment/internal/util"
)

const ProviderLionAir = "Lion Air"

var lionAirCabins = map[string]domain.Cabin{
	"ECONOMY":  domain.CabinEconomy,
	"BUSINESS": domain.CabinBusiness,
	"FIRST":    domain.CabinFirst,
}

func (m Mapper) LionAir(f lionair.Flight) (domain.Flight, error) {
	if f.ID == "" {
		return domain.Flight{}, errors.New("lionair: missing id")
	}
	var cabin domain.Cabin
	fail := func(err error) (domain.Flight, error) {
		return domain.Flight{Cabin: cabin}, fmt.Errorf("lionair %s: %w", f.ID, err)
	}
	cabin, ok := lionAirCabins[f.Pricing.FareType]
	if !ok {
		return fail(fmt.Errorf("unknown fare type %q", f.Pricing.FareType))
	}
	if f.Pricing.Total == "" || f.Pricing.Currency == "" {
		return fail(errors.New("missing price"))
	}
	dep, err := lionAirAirport(f.Route.From, f.Schedule.Departure, f.Schedule.DepartureTimezone)
	if err != nil {
		return fail(fmt.Errorf("departure: %w", err))
	}
	arr, err := lionAirAirport(f.Route.To, f.Schedule.Arrival, f.Schedule.ArrivalTimezone)
	if err != nil {
		return fail(fmt.Errorf("arrival: %w", err))
	}
	var layovers []domain.Layover
	for i, l := range f.Layovers {
		if l.Airport == "" {
			return fail(fmt.Errorf("layover %d: missing airport", i))
		}
		layovers = append(layovers, domain.Layover{Airport: l.Airport, Duration: minutes(l.DurationMinutes)})
	}
	var amenities []string
	if f.Services.WifiAvailable {
		amenities = append(amenities, "wifi")
	}
	if f.Services.MealsIncluded {
		amenities = append(amenities, "meal")
	}

	out := domain.Flight{
		ID:             flightID(f.ID, ProviderLionAir),
		Provider:       ProviderLionAir,
		Airline:        domain.Airline{Name: f.Carrier.Name, Code: f.Carrier.IATA},
		FlightNumber:   f.ID,
		Departure:      dep,
		Arrival:        arr,
		Layovers:       layovers,
		Fare:           domain.Fare{Amount: f.Pricing.Total.String(), Currency: f.Pricing.Currency},
		AvailableSeats: f.SeatsLeft,
		Cabin:          cabin,
		Aircraft:       optional(f.PlaneType),
		Amenities:      amenities,
		Baggage:        domain.Baggage{CarryOn: f.Services.BaggageAllowance.Cabin, Checked: f.Services.BaggageAllowance.Hold},
	}
	if m.mismatch(minutes(f.FlightTime), out.Duration()) {
		out.Warnings = append(out.Warnings, domain.WarningDurationMismatch)
	}
	if f.StopCount != len(layovers) {
		out.Warnings = append(out.Warnings, domain.WarningSegmentInconsistent)
	}
	return out, nil
}

func lionAirAirport(a lionair.Airport, localTime, zone string) (domain.Airport, error) {
	if a.Code == "" {
		return domain.Airport{}, errors.New("missing airport")
	}
	t, err := util.ParseZonedTime(localTime, zone)
	if err != nil {
		return domain.Airport{}, err
	}
	return domain.Airport{Code: a.Code, City: optional(a.City), Time: t}, nil
}
