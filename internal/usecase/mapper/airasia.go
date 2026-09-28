package mapper

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/util"
)

const ProviderAirAsia = "AirAsia"

type baggagePhrase struct {
	checked bool
	text    string
}

// airAsiaBaggage maps each comma-separated phrase of baggage_note, lower-cased.
var airAsiaBaggage = map[string]baggagePhrase{
	"cabin baggage only":          {text: "Cabin baggage only"},
	"checked bags additional fee": {checked: true, text: "Additional fee"},
}

func (m Mapper) AirAsia(f airasia.Flight) (domain.Flight, error) {
	if f.FlightCode == "" {
		return domain.Flight{}, errors.New("airasia: missing flight_code")
	}
	var cabin domain.Cabin
	fail := func(err error) (domain.Flight, error) {
		return domain.Flight{Cabin: cabin}, fmt.Errorf("airasia %s: %w", f.FlightCode, err)
	}
	cabin, ok := domain.ParseCabin(f.CabinClass)
	if !ok {
		return fail(fmt.Errorf("unknown cabin %q", f.CabinClass))
	}
	code, ok := airlineCode(f.FlightCode)
	if !ok {
		return fail(errors.New("no airline code in flight_code"))
	}
	if f.PriceIDR == "" {
		return fail(errors.New("missing price"))
	}
	dep, err := airAsiaAirport(f.FromAirport, f.DepartTime)
	if err != nil {
		return fail(fmt.Errorf("departure: %w", err))
	}
	arr, err := airAsiaAirport(f.ToAirport, f.ArriveTime)
	if err != nil {
		return fail(fmt.Errorf("arrival: %w", err))
	}
	var layovers []domain.Layover
	for i, s := range f.Stops {
		if s.Airport == "" {
			return fail(fmt.Errorf("stop %d: missing airport", i))
		}
		layovers = append(layovers, domain.Layover{Airport: s.Airport, Duration: minutes(s.WaitTimeMinutes)})
	}
	claimed, err := util.HoursToDuration(f.DurationHours)
	if err != nil {
		return fail(err)
	}
	baggage, bagOK := airAsiaBaggageNote(f.BaggageNote)

	out := domain.Flight{
		ID:             flightID(f.FlightCode, ProviderAirAsia),
		Provider:       ProviderAirAsia,
		Airline:        domain.Airline{Name: f.Airline, Code: code},
		FlightNumber:   f.FlightCode,
		Departure:      dep,
		Arrival:        arr,
		Layovers:       layovers,
		Fare:           domain.Fare{Amount: f.PriceIDR.String(), Currency: "IDR"},
		AvailableSeats: f.Seats,
		Cabin:          cabin,
		Baggage:        baggage,
	}
	if m.mismatch(claimed, out.Duration()) {
		out.Warnings = append(out.Warnings, domain.WarningDurationMismatch)
	}
	if f.DirectFlight != (len(layovers) == 0) {
		out.Warnings = append(out.Warnings, domain.WarningSegmentInconsistent)
	}
	if !bagOK {
		out.Warnings = append(out.Warnings, domain.WarningBaggageUnparsed)
	}
	return out, nil
}

// airlineCode takes the 2-character IATA designator in front of the flight number digits, e.g. QZ7250 → QZ.
func airlineCode(flightNumber string) (string, bool) {
	if len(flightNumber) < 3 {
		return "", false
	}
	for _, c := range flightNumber[2:] {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return flightNumber[:2], true
}

func airAsiaAirport(code, t string) (domain.Airport, error) {
	if code == "" {
		return domain.Airport{}, errors.New("missing airport")
	}
	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return domain.Airport{}, err
	}
	return domain.Airport{Code: code, Time: parsed}, nil
}

// airAsiaBaggageNote maps a note through the phrase table; a note with an unknown phrase is kept raw in both fields.
func airAsiaBaggageNote(s string) (domain.Baggage, bool) {
	if strings.TrimSpace(s) == "" {
		return domain.Baggage{}, true
	}
	var b domain.Baggage
	for part := range strings.SplitSeq(s, ",") {
		p, ok := airAsiaBaggage[strings.ToLower(strings.TrimSpace(part))]
		field := &b.CarryOn
		if p.checked {
			field = &b.Checked
		}
		if !ok || *field != "" {
			return domain.Baggage{CarryOn: s, Checked: s}, false
		}
		*field = p.text
	}
	return b, true
}
