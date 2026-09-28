package search

import (
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
)

var (
	wib  = time.FixedZone("WIB", 7*3600)
	wita = time.FixedZone("WITA", 8*3600)
)

// clock is 2025-12-15 hh:mm in zone; hh ≥ 24 rolls into the next day.
func clock(zone *time.Location, hh, mm int) time.Time {
	return time.Date(2025, 12, 15, hh, mm, 0, 0, zone)
}

type option func(*domain.Flight)

// flight is a nonstop CGK 08:00 WIB → DPS 11:00 WITA (120 min), Rp 1.000.000, 9 seats, on GA.
func flight(id string, opts ...option) domain.Flight {
	f := domain.Flight{
		ID:             id,
		Airline:        domain.Airline{Code: "GA"},
		Departure:      domain.Airport{Code: "CGK", Time: clock(wib, 8, 0)},
		Arrival:        domain.Airport{Code: "DPS", Time: clock(wita, 11, 0)},
		Price:          1_000_000,
		AvailableSeats: 9,
	}
	for _, o := range opts {
		o(&f)
	}
	return f
}

func price(m domain.Money) option { return func(f *domain.Flight) { f.Price = m } }
func seats(n int) option          { return func(f *domain.Flight) { f.AvailableSeats = n } }
func airline(code string) option  { return func(f *domain.Flight) { f.Airline.Code = code } }
func departs(t time.Time) option  { return func(f *domain.Flight) { f.Departure.Time = t } }
func arrives(t time.Time) option  { return func(f *domain.Flight) { f.Arrival.Time = t } }

func stops(n int) option {
	return func(f *domain.Flight) { f.Layovers = make([]domain.Layover, n) }
}

func minutesLong(n int) option {
	return func(f *domain.Flight) { f.Arrival.Time = f.Departure.Time.Add(time.Duration(n) * time.Minute) }
}

func ptr[T any](v T) *T { return &v }

func TestFilter_InclusiveBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		filters Filters
		flights []domain.Flight
		want    []string
	}{
		{
			"price min",
			Filters{PriceMin: ptr[domain.Money](500_000)},
			[]domain.Flight{flight("below", price(499_999)), flight("at", price(500_000)), flight("above", price(500_001))},
			[]string{"at", "above"},
		},
		{
			"price max",
			Filters{PriceMax: ptr[domain.Money](1_500_000)},
			[]domain.Flight{flight("below", price(1_499_999)), flight("at", price(1_500_000)), flight("above", price(1_500_001))},
			[]string{"below", "at"},
		},
		{
			"max stops",
			Filters{MaxStops: ptr(1)},
			[]domain.Flight{flight("nonstop"), flight("one", stops(1)), flight("two", stops(2))},
			[]string{"nonstop", "one"},
		},
		{
			"max stops zero",
			Filters{MaxStops: ptr(0)},
			[]domain.Flight{flight("nonstop"), flight("one", stops(1))},
			[]string{"nonstop"},
		},
		{
			"departure window",
			Filters{DepartureTime: &Window{From: 6 * 60, To: 12 * 60}},
			[]domain.Flight{
				flight("05:59", departs(clock(wib, 5, 59))),
				flight("06:00", departs(clock(wib, 6, 0))),
				flight("12:00", departs(clock(wib, 12, 0))),
				flight("12:01", departs(clock(wib, 12, 1))),
			},
			[]string{"06:00", "12:00"},
		},
		{
			"arrival window",
			Filters{ArrivalTime: &Window{From: 10 * 60, To: 11 * 60}},
			[]domain.Flight{
				flight("09:59", arrives(clock(wita, 9, 59))),
				flight("10:00", arrives(clock(wita, 10, 0))),
				flight("11:00", arrives(clock(wita, 11, 0))),
				flight("11:01", arrives(clock(wita, 11, 1))),
			},
			[]string{"10:00", "11:00"},
		},
		{
			"max duration",
			Filters{MaxDuration: ptr(240 * time.Minute)},
			[]domain.Flight{flight("239", minutesLong(239)), flight("240", minutesLong(240)), flight("241", minutesLong(241))},
			[]string{"239", "240"},
		},
		{
			"airlines",
			Filters{Airlines: []string{"GA", "JT"}},
			[]domain.Flight{flight("ga"), flight("jt", airline("JT")), flight("id", airline("ID"))},
			[]string{"ga", "jt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flightIDs(filter(tt.flights, tt.filters, 1)); !slices.Equal(got, tt.want) {
				t.Errorf("kept %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_SeatsForEveryPassenger(t *testing.T) {
	flights := []domain.Flight{flight("two", seats(2)), flight("three", seats(3)), flight("four", seats(4))}
	if got := flightIDs(filter(flights, Filters{}, 3)); !slices.Equal(got, []string{"three", "four"}) {
		t.Errorf("kept %v, want three four", got)
	}
}

func TestFilter_WindowWrapsMidnight(t *testing.T) {
	flights := []domain.Flight{
		flight("21:59", departs(clock(wib, 21, 59))),
		flight("22:00", departs(clock(wib, 22, 0))),
		flight("23:59", departs(clock(wib, 23, 59))),
		flight("00:00", departs(clock(wib, 24, 0))),
		flight("02:00", departs(clock(wib, 26, 0))),
		flight("02:01", departs(clock(wib, 26, 1))),
		flight("12:00", departs(clock(wib, 12, 0))),
	}
	got := flightIDs(filter(flights, Filters{DepartureTime: &Window{From: 22 * 60, To: 2 * 60}}, 1))
	if want := []string{"22:00", "23:59", "00:00", "02:00"}; !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

func TestFilter_WindowUsesAirportLocalTime(t *testing.T) {
	// 06:30 WITA is 05:30 WIB: the arrival window reads the arrival airport's own zone
	f := flight("wita", departs(clock(wib, 4, 0)), arrives(clock(wita, 6, 30)))
	if got := filter([]domain.Flight{f}, Filters{ArrivalTime: &Window{From: 6 * 60, To: 7 * 60}}, 1); len(got) != 1 {
		t.Error("06:30 WITA arrival dropped by a 06:00–07:00 window")
	}
}

func TestFilter_EmptyFiltersKeepAll(t *testing.T) {
	flights := []domain.Flight{flight("a"), flight("b", stops(2), airline("QZ")), flight("c", price(1))}
	if got := flightIDs(filter(flights, Filters{Airlines: []string{}}, 1)); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("kept %v, want a b c", got)
	}
}

func TestFilter_AirlineMatchesDerivedAirAsiaCode(t *testing.T) {
	flights, _ := givenFlights(t)
	got := filter(flights, Filters{Airlines: []string{"QZ"}}, 1)
	if len(got) == 0 {
		t.Fatal("no QZ flights kept")
	}
	for _, f := range got {
		if f.Provider != mapper.ProviderAirAsia {
			t.Errorf("%s from %s kept by QZ filter", f.ID, f.Provider)
		}
	}
	for _, f := range flights {
		if f.Provider == mapper.ProviderAirAsia && !slices.ContainsFunc(got, func(g domain.Flight) bool { return g.ID == f.ID }) {
			t.Errorf("AirAsia %s dropped by QZ filter", f.ID)
		}
	}
}
