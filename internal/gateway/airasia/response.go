package airasia

import "encoding/json"

type Query struct {
	Origin      string
	Destination string
	Date        string // YYYY-MM-DD in the origin's local zone
}

type Response struct {
	Status  string   `json:"status"`
	Flights []Flight `json:"flights"`
}

// Flight has no airline code (it prefixes FlightCode) and no cities.
type Flight struct {
	FlightCode    string      `json:"flight_code"`
	Airline       string      `json:"airline"`
	FromAirport   string      `json:"from_airport"`
	ToAirport     string      `json:"to_airport"`
	DepartTime    string      `json:"depart_time"`
	ArriveTime    string      `json:"arrive_time"`
	DurationHours float64     `json:"duration_hours"`
	DirectFlight  bool        `json:"direct_flight"`
	Stops         []Stop      `json:"stops"`
	PriceIDR      json.Number `json:"price_idr"`
	Seats         int         `json:"seats"`
	CabinClass    string      `json:"cabin_class"`
	BaggageNote   string      `json:"baggage_note"`
}

type Stop struct {
	Airport         string `json:"airport"`
	WaitTimeMinutes int    `json:"wait_time_minutes"`
}
