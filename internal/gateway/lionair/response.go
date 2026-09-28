package lionair

import "encoding/json"

type Query struct {
	Origin      string
	Destination string
	Date        string // YYYY-MM-DD in the origin's local zone
}

type Response struct {
	Success bool `json:"success"`
	Data    struct {
		AvailableFlights []Flight `json:"available_flights"`
	} `json:"data"`
}

type Flight struct {
	ID         string    `json:"id"`
	Carrier    Carrier   `json:"carrier"`
	Route      Route     `json:"route"`
	Schedule   Schedule  `json:"schedule"`
	FlightTime int       `json:"flight_time"` // minutes
	IsDirect   bool      `json:"is_direct"`
	StopCount  int       `json:"stop_count"`
	Layovers   []Layover `json:"layovers"`
	Pricing    Pricing   `json:"pricing"`
	SeatsLeft  int       `json:"seats_left"`
	PlaneType  string    `json:"plane_type"`
	Services   Services  `json:"services"`
}

type Carrier struct {
	Name string `json:"name"`
	IATA string `json:"iata"`
}

type Route struct {
	From Airport `json:"from"`
	To   Airport `json:"to"`
}

type Airport struct {
	Code string `json:"code"`
	Name string `json:"name"`
	City string `json:"city"`
}

// Schedule times are naive local times in the paired IANA zone.
type Schedule struct {
	Departure         string `json:"departure"`
	DepartureTimezone string `json:"departure_timezone"`
	Arrival           string `json:"arrival"`
	ArrivalTimezone   string `json:"arrival_timezone"`
}

type Layover struct {
	Airport         string `json:"airport"`
	DurationMinutes int    `json:"duration_minutes"`
}

type Pricing struct {
	Total    json.Number `json:"total"`
	Currency string      `json:"currency"`
	FareType string      `json:"fare_type"`
}

type Services struct {
	WifiAvailable    bool             `json:"wifi_available"`
	MealsIncluded    bool             `json:"meals_included"`
	BaggageAllowance BaggageAllowance `json:"baggage_allowance"`
}

type BaggageAllowance struct {
	Cabin string `json:"cabin"`
	Hold  string `json:"hold"`
}
