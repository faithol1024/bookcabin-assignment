package mapper

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	"github.com/faithol1024/bookcabin-assignment/internal/util"
)

const (
	ProviderBatik   = "Batik Air"
	batikTimeLayout = "2006-01-02T15:04:05-0700"
)

var batikCabins = map[string]domain.Cabin{
	"Y": domain.CabinEconomy,
	"C": domain.CabinBusiness,
	"J": domain.CabinBusiness,
}

func (m Mapper) Batik(f batik.Flight) (domain.Flight, error) {
	if f.FlightNumber == "" {
		return domain.Flight{}, errors.New("batik: missing flightNumber")
	}
	var cabin domain.Cabin
	fail := func(err error) (domain.Flight, error) {
		return domain.Flight{Cabin: cabin}, fmt.Errorf("batik %s: %w", f.FlightNumber, err)
	}
	cabin, ok := batikCabins[f.Fare.Class]
	if !ok {
		return fail(fmt.Errorf("unknown fare class %q", f.Fare.Class))
	}
	if f.Fare.TotalPrice == "" || f.Fare.CurrencyCode == "" {
		return fail(errors.New("missing price"))
	}
	dep, err := batikAirport(f.Origin, f.DepartureDateTime)
	if err != nil {
		return fail(fmt.Errorf("departure: %w", err))
	}
	arr, err := batikAirport(f.Destination, f.ArrivalDateTime)
	if err != nil {
		return fail(fmt.Errorf("arrival: %w", err))
	}
	var layovers []domain.Layover
	for i, c := range f.Connections {
		if c.StopAirport == "" {
			return fail(fmt.Errorf("connection %d: missing airport", i))
		}
		d, err := util.ParseHoursMinutes(c.StopDuration)
		if err != nil {
			return fail(fmt.Errorf("connection %d: %w", i, err))
		}
		layovers = append(layovers, domain.Layover{Airport: c.StopAirport, Duration: d})
	}
	var claimed time.Duration
	if f.TravelTime != "" {
		if claimed, err = util.ParseHoursMinutes(f.TravelTime); err != nil {
			return fail(err)
		}
	}
	var amenities []string
	for _, s := range f.OnboardServices {
		amenities = append(amenities, strings.ToLower(s))
	}
	baggage, bagOK := batikBaggage(f.BaggageInfo)

	out := domain.Flight{
		ID:             flightID(f.FlightNumber, ProviderBatik),
		Provider:       ProviderBatik,
		Airline:        domain.Airline{Name: f.AirlineName, Code: f.AirlineIATA},
		FlightNumber:   f.FlightNumber,
		Departure:      dep,
		Arrival:        arr,
		Layovers:       layovers,
		Fare:           domain.Fare{Amount: f.Fare.TotalPrice.String(), Currency: f.Fare.CurrencyCode},
		AvailableSeats: f.SeatsAvailable,
		Cabin:          cabin,
		Aircraft:       optional(f.AircraftModel),
		Amenities:      amenities,
		Baggage:        baggage,
	}
	if m.mismatch(claimed, out.Duration()) {
		out.Warnings = append(out.Warnings, domain.WarningDurationMismatch)
	}
	if f.NumberOfStops != len(layovers) {
		out.Warnings = append(out.Warnings, domain.WarningSegmentInconsistent)
	}
	if !bagOK {
		out.Warnings = append(out.Warnings, domain.WarningBaggageUnparsed)
	}
	return out, nil
}

func batikAirport(code, localTime string) (domain.Airport, error) {
	if code == "" {
		return domain.Airport{}, errors.New("missing airport")
	}
	t, err := parseBatikTime(localTime)
	if err != nil {
		return domain.Airport{}, err
	}
	return domain.Airport{Code: code, Time: t}, nil
}

// parseBatikTime parses an offset without colon, e.g. 2025-12-15T07:15:00+0700.
func parseBatikTime(s string) (time.Time, error) {
	t, err := time.Parse(batikTimeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}

// batikBaggage splits "7kg cabin, 20kg checked"; text it can't split is kept raw in both fields.
func batikBaggage(s string) (domain.Baggage, bool) {
	if strings.TrimSpace(s) == "" {
		return domain.Baggage{}, true
	}
	var b domain.Baggage
	for part := range strings.SplitSeq(s, ",") {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			return domain.Baggage{CarryOn: s, Checked: s}, false
		}
		var field *string
		switch strings.ToLower(fields[1]) {
		case "cabin":
			field = &b.CarryOn
		case "checked":
			field = &b.Checked
		}
		if field == nil || *field != "" {
			return domain.Baggage{CarryOn: s, Checked: s}, false
		}
		*field = fields[0]
	}
	return b, true
}
