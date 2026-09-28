package garuda

import "encoding/json"

type Query struct {
	Origin      string
	Destination string // final destination, after all segments
	Date        string // YYYY-MM-DD in the origin's local zone
}

type Response struct {
	Status  string   `json:"status"`
	Flights []Flight `json:"flights"`
}

type Flight struct {
	FlightID        string    `json:"flight_id"`
	Airline         string    `json:"airline"`
	AirlineCode     string    `json:"airline_code"`
	Departure       Point     `json:"departure"`
	Arrival         Point     `json:"arrival"`
	DurationMinutes int       `json:"duration_minutes"`
	Stops           int       `json:"stops"`
	Aircraft        string    `json:"aircraft"`
	Price           Price     `json:"price"`
	AvailableSeats  int       `json:"available_seats"`
	FareClass       string    `json:"fare_class"`
	Baggage         *Baggage  `json:"baggage"`
	Amenities       []string  `json:"amenities"`
	Segments        []Segment `json:"segments"`
}

type Point struct {
	Airport  string `json:"airport"`
	City     string `json:"city"`
	Time     string `json:"time"`
	Terminal string `json:"terminal"`
}

type Price struct {
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
}

type Baggage struct {
	CarryOn *int `json:"carry_on"` // pieces
	Checked *int `json:"checked"`  // pieces
}

type Segment struct {
	FlightNumber    string `json:"flight_number"`
	Departure       Point  `json:"departure"`
	Arrival         Point  `json:"arrival"`
	DurationMinutes int    `json:"duration_minutes"`
	LayoverMinutes  int    `json:"layover_minutes"` // before this segment
}

// Itinerary follows the segments when present: GA315's top level stops at SUB but its segments continue to DPS.
func (f Flight) Itinerary() (Point, Point) {
	if len(f.Segments) > 0 {
		return f.Segments[0].Departure, f.Segments[len(f.Segments)-1].Arrival
	}
	return f.Departure, f.Arrival
}
