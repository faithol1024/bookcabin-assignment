package search

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
	_ "time/tzdata" // tests must not depend on the host zoneinfo

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	fxgw "github.com/faithol1024/bookcabin-assignment/internal/gateway/fx"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
)

func decodeFile[R any](t *testing.T, path string) R {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r R
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// givenFlights normalizes and converts every flight of the four given provider files, then fills cities as one leg.
func givenFlights(t *testing.T) ([]domain.Flight, int) {
	t.Helper()
	m := mapper.New(5 * time.Minute)
	var flights []domain.Flight
	rejected := 0
	add := func(f []domain.Flight, r int) {
		flights = append(flights, f...)
		rejected += r
	}
	ctx := context.Background()
	add(mapper.Records(ctx, decodeFile[garuda.Response](t, "../../gateway/garuda/mockdata/given.json").Flights, m.Garuda, domain.CabinEconomy))
	add(mapper.Records(ctx, decodeFile[lionair.Response](t, "../../gateway/lionair/mockdata/given.json").Data.AvailableFlights, m.LionAir, domain.CabinEconomy))
	add(mapper.Records(ctx, decodeFile[batik.Response](t, "../../gateway/batik/mockdata/given.json").Results, m.Batik, domain.CabinEconomy))
	add(mapper.Records(ctx, decodeFile[airasia.Response](t, "../../gateway/airasia/mockdata/given.json").Flights, m.AirAsia, domain.CabinEconomy))
	flights, unconverted := converter{exponents: exponents, fx: fxgw.NewMock()}.convert(ctx, flights)
	fillCities(flights)
	return flights, rejected + unconverted
}

func TestGiven_AllFlightsNormalize(t *testing.T) {
	flights, rejected := givenFlights(t)
	if len(flights) != 13 || rejected != 0 {
		t.Fatalf("got %d flights, %d rejected; want 13, 0", len(flights), rejected)
	}
	for _, f := range flights {
		if f.Departure.City == nil || f.Arrival.City == nil {
			t.Errorf("%s: missing city", f.ID)
		}
		if slices.Contains(f.Warnings, domain.WarningAirportUnknown) {
			t.Errorf("%s: airport_unknown", f.ID)
		}
	}
}

// No flight is sold by two providers, so there is nothing to dedupe.
func TestGiven_NoFlightAtTwoProviders(t *testing.T) {
	flights, _ := givenFlights(t)
	providers := map[string]string{}
	for _, f := range flights {
		if p, ok := providers[f.FlightNumber]; ok {
			t.Errorf("%s at %s and %s", f.FlightNumber, p, f.Provider)
		}
		providers[f.FlightNumber] = f.Provider
		if f.Airline.Name != f.Provider {
			t.Errorf("%s: airline %s, provider %s; want the operating airline", f.ID, f.Airline.Name, f.Provider)
		}
	}
}

type expectedAirport struct {
	Airport   string `json:"airport"`
	City      string `json:"city"`
	Datetime  string `json:"datetime"`
	Timestamp int64  `json:"timestamp"`
}

type expectedFlight struct {
	ID           string          `json:"id"`
	Provider     string          `json:"provider"`
	Airline      domain.Airline  `json:"airline"`
	FlightNumber string          `json:"flight_number"`
	Departure    expectedAirport `json:"departure"`
	Arrival      expectedAirport `json:"arrival"`
	Duration     struct {
		TotalMinutes int `json:"total_minutes"`
	} `json:"duration"`
	Stops int `json:"stops"`
	Price struct {
		Amount   json.Number `json:"amount"`
		Currency string      `json:"currency"`
	} `json:"price"`
	AvailableSeats int      `json:"available_seats"`
	CabinClass     string   `json:"cabin_class"`
	Aircraft       *string  `json:"aircraft"`
	Amenities      []string `json:"amenities"`
	Baggage        struct {
		CarryOn string `json:"carry_on"`
		Checked string `json:"checked"`
	} `json:"baggage"`
}

func TestGiven_QZ7250MatchesExpectedResult(t *testing.T) {
	expected := decodeFile[struct {
		Flights []expectedFlight `json:"flights"`
	}](t, "../../../docs/assignment/expected_result.json")
	i := slices.IndexFunc(expected.Flights, func(f expectedFlight) bool { return f.ID == "QZ7250_AirAsia" })
	if i < 0 {
		t.Fatal("QZ7250_AirAsia not in expected_result.json")
	}
	want := expected.Flights[i]

	flights, _ := givenFlights(t)
	j := slices.IndexFunc(flights, func(f domain.Flight) bool { return f.ID == want.ID })
	if j < 0 {
		t.Fatal("QZ7250_AirAsia not normalized")
	}
	got := flights[j]

	if got.Provider != want.Provider || got.Airline != want.Airline || got.FlightNumber != want.FlightNumber {
		t.Errorf("identity = %s, %+v, %s; want %s, %+v, %s", got.Provider, got.Airline, got.FlightNumber, want.Provider, want.Airline, want.FlightNumber)
	}
	for _, p := range []struct {
		name string
		got  domain.Airport
		want expectedAirport
	}{{"departure", got.Departure, want.Departure}, {"arrival", got.Arrival, want.Arrival}} {
		if p.got.Code != p.want.Airport || city(p.got) != p.want.City || p.got.Time.Format(time.RFC3339) != p.want.Datetime {
			t.Errorf("%s = %s %s %s; want %s %s %s", p.name, p.got.Code, city(p.got), p.got.Time.Format(time.RFC3339), p.want.Airport, p.want.City, p.want.Datetime)
		}
	}
	// expected_result.json's timestamps are wrong (2024-12-15 UTC); these are the instants of its own datetimes
	if got.Departure.Time.Unix() != 1765786500 || got.Arrival.Time.Unix() != 1765802100 {
		t.Errorf("timestamps = %d, %d; want 1765786500, 1765802100", got.Departure.Time.Unix(), got.Arrival.Time.Unix())
	}
	if int(got.Duration().Minutes()) != want.Duration.TotalMinutes || got.Stops() != want.Stops {
		t.Errorf("duration %v, stops %d; want %dm, %d", got.Duration(), got.Stops(), want.Duration.TotalMinutes, want.Stops)
	}
	if got.Fare != (domain.Fare{Amount: want.Price.Amount.String(), Currency: want.Price.Currency}) {
		t.Errorf("Fare = %+v, want %s %s", got.Fare, want.Price.Amount, want.Price.Currency)
	}
	if strconv.FormatInt(int64(got.Price), 10) != want.Price.Amount.String() || got.Original != nil {
		t.Errorf("Price = %d, original %+v; want %s IDR, nil", got.Price, got.Original, want.Price.Amount)
	}
	if got.AvailableSeats != want.AvailableSeats || string(got.Cabin) != want.CabinClass {
		t.Errorf("seats %d, cabin %q; want %d, %q", got.AvailableSeats, got.Cabin, want.AvailableSeats, want.CabinClass)
	}
	if got.Aircraft != nil || want.Aircraft != nil || len(got.Amenities) != 0 || len(want.Amenities) != 0 {
		t.Errorf("aircraft %v, amenities %v; want none", got.Aircraft, got.Amenities)
	}
	if got.Baggage != (domain.Baggage{CarryOn: want.Baggage.CarryOn, Checked: want.Baggage.Checked}) {
		t.Errorf("Baggage = %+v, want %+v", got.Baggage, want.Baggage)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", got.Warnings)
	}
}
