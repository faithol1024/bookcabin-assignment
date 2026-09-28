package domain

import "time"

type Cabin string

const (
	CabinEconomy  Cabin = "economy"
	CabinBusiness Cabin = "business"
	CabinFirst    Cabin = "first"
)

func ParseCabin(s string) (Cabin, bool) {
	switch c := Cabin(s); c {
	case CabinEconomy, CabinBusiness, CabinFirst:
		return c, true
	}
	return "", false
}

type Airline struct {
	Name string
	Code string
}

type Airport struct {
	Code string
	City *string
	Time time.Time
}

type Layover struct {
	Airport  string
	Duration time.Duration
}

// Fare is the provider's raw price; only the currency converter reads it.
type Fare struct {
	Amount   string
	Currency string
}

// Money is whole rupiah.
type Money int64

type ForeignPrice struct {
	Amount   int64 // minor units
	Currency string
	Exponent int
	Rate     int64 // IDR per 1 unit
}

type Baggage struct {
	CarryOn string
	Checked string
}

type Flight struct {
	ID             string
	Provider       string
	Airline        Airline
	FlightNumber   string
	Departure      Airport
	Arrival        Airport
	Layovers       []Layover
	Fare           Fare
	Price          Money
	Original       *ForeignPrice
	AvailableSeats int
	Cabin          Cabin
	Aircraft       *string
	Amenities      []string
	Baggage        Baggage
	Warnings       []Warning
}

// Duration spans first departure to last arrival, layovers included.
func (f Flight) Duration() time.Duration {
	return f.Arrival.Time.Sub(f.Departure.Time)
}

func (f Flight) Stops() int {
	return len(f.Layovers)
}
