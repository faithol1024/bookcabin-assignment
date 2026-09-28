package batik

import "encoding/json"

type Query struct {
	Origin      string
	Destination string
	Date        string // YYYY-MM-DD in the origin's local zone
}

type Response struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Results []Flight `json:"results"`
}

// Flight times carry an offset without colon, e.g. 2025-12-15T07:15:00+0700.
type Flight struct {
	FlightNumber      string       `json:"flightNumber"`
	AirlineName       string       `json:"airlineName"`
	AirlineIATA       string       `json:"airlineIATA"`
	Origin            string       `json:"origin"`
	Destination       string       `json:"destination"`
	DepartureDateTime string       `json:"departureDateTime"`
	ArrivalDateTime   string       `json:"arrivalDateTime"`
	TravelTime        string       `json:"travelTime"` // e.g. "1h 45m"
	NumberOfStops     int          `json:"numberOfStops"`
	Connections       []Connection `json:"connections"`
	Fare              Fare         `json:"fare"`
	SeatsAvailable    int          `json:"seatsAvailable"`
	AircraftModel     string       `json:"aircraftModel"`
	BaggageInfo       string       `json:"baggageInfo"` // e.g. "7kg cabin, 20kg checked"
	OnboardServices   []string     `json:"onboardServices"`
}

type Connection struct {
	StopAirport  string `json:"stopAirport"`
	StopDuration string `json:"stopDuration"` // e.g. "55m"
}

type Fare struct {
	BasePrice    json.Number `json:"basePrice"`
	Taxes        json.Number `json:"taxes"`
	TotalPrice   json.Number `json:"totalPrice"`
	CurrencyCode string      `json:"currencyCode"`
	Class        string      `json:"class"` // Y, C or J
}
