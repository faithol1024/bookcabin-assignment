package httphandler

import (
	"strconv"
	"strings"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

type criteriaResponse struct {
	Origin        string        `json:"origin,omitempty"`
	Destination   string        `json:"destination,omitempty"`
	DepartureDate string        `json:"departure_date,omitempty"`
	ReturnDate    string        `json:"return_date,omitempty"`
	Legs          []legCriteria `json:"legs,omitempty"`
	Passengers    int           `json:"passengers"`
	CabinClass    domain.Cabin  `json:"cabin_class"`
}

type legCriteria struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Date        string `json:"date"`
}

type oneWayResponse struct {
	SearchCriteria criteriaResponse `json:"search_criteria"`
	Metadata       metadataResponse `json:"metadata"`
	Flights        []flightResponse `json:"flights"`
}

type roundTripResponse struct {
	SearchCriteria criteriaResponse `json:"search_criteria"`
	Outbound       legResponse      `json:"outbound"`
	Inbound        legResponse      `json:"inbound"`
}

type multiCityResponse struct {
	SearchCriteria criteriaResponse `json:"search_criteria"`
	Legs           []legResponse    `json:"legs"`
}

type legResponse struct {
	Origin      string           `json:"origin"`
	Destination string           `json:"destination"`
	Date        string           `json:"date"`
	Metadata    metadataResponse `json:"metadata"`
	Flights     []flightResponse `json:"flights"`
}

type metadataResponse struct {
	TotalResults       int                     `json:"total_results"`
	Limit              int                     `json:"limit"`
	Offset             int                     `json:"offset"`
	ProvidersQueried   int                     `json:"providers_queried"`
	ProvidersSucceeded int                     `json:"providers_succeeded"`
	ProvidersFailed    int                     `json:"providers_failed"`
	FlightsRejected    int                     `json:"flights_rejected"`
	CheapestByProvider map[string]domain.Money `json:"cheapest_by_provider"`
	SearchTimeMS       int64                   `json:"search_time_ms"`
	CacheHit           bool                    `json:"cache_hit"`
	ProviderErrors     []providerError         `json:"provider_errors"`
}

type providerError struct {
	Provider string `json:"provider"`
	Error    string `json:"error"`
	Attempts int    `json:"attempts"`
}

type flightResponse struct {
	ID             string            `json:"id"`
	Provider       string            `json:"provider"`
	Airline        airlineResponse   `json:"airline"`
	FlightNumber   string            `json:"flight_number"`
	Departure      airportResponse   `json:"departure"`
	Arrival        airportResponse   `json:"arrival"`
	Duration       durationResponse  `json:"duration"`
	Stops          int               `json:"stops"`
	Layovers       []layoverResponse `json:"layovers"`
	Price          priceResponse     `json:"price"`
	AvailableSeats int               `json:"available_seats"`
	CabinClass     domain.Cabin      `json:"cabin_class"`
	Aircraft       *string           `json:"aircraft"`
	Amenities      []string          `json:"amenities"`
	Baggage        baggageResponse   `json:"baggage"`
	Score          float64           `json:"score"`
	Warnings       []domain.Warning  `json:"warnings"`
}

type airlineResponse struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type airportResponse struct {
	Airport   string  `json:"airport"`
	City      *string `json:"city"`
	Datetime  string  `json:"datetime"`
	Timestamp int64   `json:"timestamp"`
}

type durationResponse struct {
	TotalMinutes int    `json:"total_minutes"`
	Formatted    string `json:"formatted"`
}

type layoverResponse struct {
	Airport         string `json:"airport"`
	DurationMinutes int    `json:"duration_minutes"`
}

type priceResponse struct {
	Amount         domain.Money      `json:"amount"`
	Currency       string            `json:"currency"`
	Formatted      string            `json:"formatted"`
	Total          domain.Money      `json:"total"`
	TotalFormatted string            `json:"total_formatted"`
	Original       *originalResponse `json:"original"`
}

type originalResponse struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Rate     int64  `json:"rate"`
}

type baggageResponse struct {
	CarryOn string `json:"carry_on"`
	Checked string `json:"checked"`
}

// newResponse picks the one-way, round-trip or multi-city shape from the criteria.
func newResponse(c search.Criteria, res search.Response) any {
	sc := criteriaResponse{
		Origin:        c.Origin,
		Destination:   c.Destination,
		DepartureDate: c.DepartureDate,
		ReturnDate:    c.ReturnDate,
		Passengers:    c.Passengers,
		CabinClass:    c.Cabin,
	}
	for _, l := range c.Legs {
		sc.Legs = append(sc.Legs, legCriteria(l))
	}
	legs := make([]legResponse, len(res.Legs))
	for i, l := range res.Legs {
		legs[i] = newLeg(l, c)
	}

	switch {
	case len(c.Legs) > 0:
		return multiCityResponse{SearchCriteria: sc, Legs: legs}
	case c.ReturnDate != "":
		return roundTripResponse{SearchCriteria: sc, Outbound: legs[0], Inbound: legs[1]}
	default:
		return oneWayResponse{SearchCriteria: sc, Metadata: legs[0].Metadata, Flights: legs[0].Flights}
	}
}

func newLeg(l search.Leg, c search.Criteria) legResponse {
	flights := make([]flightResponse, len(l.Flights))
	for i, f := range l.Flights {
		flights[i] = newFlight(f)
	}
	errs := make([]providerError, len(l.ProviderErrors))
	for i, e := range l.ProviderErrors {
		errs[i] = providerError{Provider: e.Provider, Error: e.Code, Attempts: e.Attempts}
	}
	cheapest := l.CheapestByProvider
	if cheapest == nil {
		cheapest = map[string]domain.Money{}
	}
	return legResponse{
		Origin:      l.Origin,
		Destination: l.Destination,
		Date:        l.Date,
		Metadata: metadataResponse{
			TotalResults:       l.TotalResults,
			Limit:              c.Limit,
			Offset:             c.Offset,
			ProvidersQueried:   l.ProvidersQueried,
			ProvidersSucceeded: l.ProvidersSucceeded,
			ProvidersFailed:    l.ProvidersFailed,
			FlightsRejected:    l.FlightsRejected,
			CheapestByProvider: cheapest,
			SearchTimeMS:       l.SearchTime.Milliseconds(),
			CacheHit:           l.CacheHit,
			ProviderErrors:     errs,
		},
		Flights: flights,
	}
}

func newFlight(f search.Flight) flightResponse {
	layovers := make([]layoverResponse, len(f.Layovers))
	for i, l := range f.Layovers {
		layovers[i] = layoverResponse{Airport: l.Airport, DurationMinutes: minutes(l.Duration)}
	}
	var original *originalResponse
	if o := f.Original; o != nil {
		original = &originalResponse{Amount: decimal(o.Amount, o.Exponent), Currency: o.Currency, Rate: o.Rate}
	}
	total := minutes(f.Duration())
	return flightResponse{
		ID:           f.ID,
		Provider:     f.Provider,
		Airline:      airlineResponse(f.Airline),
		FlightNumber: f.FlightNumber,
		Departure:    newAirport(f.Departure),
		Arrival:      newAirport(f.Arrival),
		Duration:     durationResponse{TotalMinutes: total, Formatted: domain.FormatDuration(total)},
		Stops:        f.Stops(),
		Layovers:     layovers,
		Price: priceResponse{
			Amount:         f.Price,
			Currency:       "IDR",
			Formatted:      domain.FormatIDR(f.Price),
			Total:          f.Total,
			TotalFormatted: domain.FormatIDR(f.Total),
			Original:       original,
		},
		AvailableSeats: f.AvailableSeats,
		CabinClass:     f.Cabin,
		Aircraft:       f.Aircraft,
		Amenities:      nonNil(f.Amenities),
		Baggage:        baggageResponse(f.Baggage),
		Score:          f.Score,
		Warnings:       nonNil(f.Warnings),
	}
}

func newAirport(a domain.Airport) airportResponse {
	return airportResponse{Airport: a.Code, City: a.City, Datetime: a.Time.Format(time.RFC3339), Timestamp: a.Time.Unix()}
}

func minutes(d time.Duration) int {
	return int(d / time.Minute)
}

// nonNil keeps empty lists as [] in JSON.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// decimal renders minor units with exp fraction digits: 6550, 2 → "65.50".
func decimal(minor int64, exp int) string {
	digits := strconv.FormatInt(minor, 10)
	sign := ""
	if minor < 0 {
		sign, digits = "-", digits[1:]
	}
	if exp <= 0 {
		return sign + digits
	}
	if len(digits) <= exp {
		digits = strings.Repeat("0", exp-len(digits)+1) + digits
	}
	return sign + digits[:len(digits)-exp] + "." + digits[len(digits)-exp:]
}
