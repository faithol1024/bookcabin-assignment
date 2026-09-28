package mapper

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
)

func TestGaruda_Golden(t *testing.T) {
	assertGolden(t, "../../gateway/garuda/mockdata", "garuda",
		func(r garuda.Response) []garuda.Flight { return r.Flights }, testMapper.Garuda)
}

func garudaRecord(t *testing.T) garuda.Flight {
	t.Helper()
	var f garuda.Flight
	err := json.Unmarshal([]byte(`{
		"flight_id": "GA400", "airline": "Garuda Indonesia", "airline_code": "GA",
		"departure": {"airport": "CGK", "city": "Jakarta", "time": "2025-12-15T06:00:00+07:00", "terminal": "3"},
		"arrival": {"airport": "DPS", "city": "Denpasar", "time": "2025-12-15T08:50:00+08:00", "terminal": "I"},
		"duration_minutes": 110, "stops": 0,
		"price": {"amount": 1250000, "currency": "IDR"},
		"available_seats": 28, "fare_class": "economy",
		"baggage": {"carry_on": 1, "checked": 2}
	}`), &f)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// GA315's top level says CGK→SUB nonstop, but its segments continue to DPS.
func ga315(t *testing.T) garuda.Flight {
	t.Helper()
	f := garudaRecord(t)
	err := json.Unmarshal([]byte(`{
		"flight_id": "GA315",
		"arrival": {"airport": "SUB", "city": "Surabaya", "time": "2025-12-15T15:30:00+07:00", "terminal": "2"},
		"departure": {"airport": "CGK", "city": "Jakarta", "time": "2025-12-15T14:00:00+07:00", "terminal": "3"},
		"duration_minutes": 90,
		"segments": [
			{"flight_number": "GA315", "departure": {"airport": "CGK", "time": "2025-12-15T14:00:00+07:00"},
			 "arrival": {"airport": "SUB", "time": "2025-12-15T15:30:00+07:00"}, "duration_minutes": 90},
			{"flight_number": "GA332", "departure": {"airport": "SUB", "time": "2025-12-15T17:15:00+07:00"},
			 "arrival": {"airport": "DPS", "time": "2025-12-15T18:45:00+08:00"}, "duration_minutes": 90, "layover_minutes": 105}
		]
	}`), &f)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGaruda_GA315BuiltFromSegments(t *testing.T) {
	f, err := testMapper.Garuda(ga315(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Departure.Code != "CGK" || f.Arrival.Code != "DPS" {
		t.Errorf("route = %s→%s, want CGK→DPS", f.Departure.Code, f.Arrival.Code)
	}
	if f.Stops() != 1 {
		t.Errorf("Stops() = %d, want 1", f.Stops())
	}
	if want := []domain.Layover{{Airport: "SUB", Duration: 105 * time.Minute}}; !slices.Equal(f.Layovers, want) {
		t.Errorf("Layovers = %v, want %v", f.Layovers, want)
	}
	if f.Duration() != 225*time.Minute {
		t.Errorf("Duration() = %v, want 225m", f.Duration())
	}
	if !slices.Contains(f.Warnings, domain.WarningSegmentInconsistent) {
		t.Errorf("Warnings = %v, want segment_inconsistent", f.Warnings)
	}
	if !slices.Contains(f.Warnings, domain.WarningDurationMismatch) {
		t.Errorf("Warnings = %v, want duration_mismatch (claimed 90, actual 225)", f.Warnings)
	}
	if got := f.Arrival.Time.Format(time.RFC3339); got != "2025-12-15T18:45:00+08:00" {
		t.Errorf("arrival = %s", got)
	}
	if f.Arrival.City != nil {
		t.Error("DPS city must not come from the top-level SUB arrival")
	}
	if f.Departure.City == nil || *f.Departure.City != "Jakarta" {
		t.Error("CGK city must come from the top level")
	}
}

func TestGaruda_SegmentInconsistent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*garuda.Flight)
		want   bool
	}{
		{"consistent", func(f *garuda.Flight) {
			f.Arrival = f.Segments[1].Arrival
			f.Stops = 1
			f.Segments[1].Arrival.Time = "2025-12-15T19:45:00+08:00"
		}, false},
		{"stops disagree only", func(f *garuda.Flight) {
			f.Arrival = f.Segments[1].Arrival
			f.Segments[1].Arrival.Time = "2025-12-15T19:45:00+08:00"
		}, true},
		{"claimed layover off", func(f *garuda.Flight) {
			f.Arrival = f.Segments[1].Arrival
			f.Stops = 1
			f.Segments[1].Arrival.Time = "2025-12-15T19:45:00+08:00"
			f.Segments[1].LayoverMinutes = 60
		}, true},
		{"chain break", func(f *garuda.Flight) {
			f.Arrival = f.Segments[1].Arrival
			f.Stops = 1
			f.Segments[1].Arrival.Time = "2025-12-15T19:45:00+08:00"
			f.Segments[1].Departure.Airport = "UPG"
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ga315(t)
			tt.mutate(&r)
			f, err := testMapper.Garuda(r)
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(f.Warnings, domain.WarningSegmentInconsistent); got != tt.want {
				t.Errorf("segment_inconsistent = %v, want %v (warnings %v)", got, tt.want, f.Warnings)
			}
		})
	}
}

func TestGaruda_DirectFlight(t *testing.T) {
	f, err := testMapper.Garuda(garudaRecord(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "GA400_GarudaIndonesia" || f.Provider != "Garuda Indonesia" || f.Airline != (domain.Airline{Name: "Garuda Indonesia", Code: "GA"}) {
		t.Errorf("identity = %s, %s, %+v", f.ID, f.Provider, f.Airline)
	}
	if f.Stops() != 0 || f.Layovers != nil || f.Duration() != 110*time.Minute || len(f.Warnings) != 0 {
		t.Errorf("stops %d, layovers %v, duration %v, warnings %v", f.Stops(), f.Layovers, f.Duration(), f.Warnings)
	}
	if f.Fare != (domain.Fare{Amount: "1250000", Currency: "IDR"}) || f.Price != 0 {
		t.Errorf("Fare = %+v, Price = %d; want raw fare and zero price", f.Fare, f.Price)
	}
	if f.Aircraft != nil {
		t.Errorf("Aircraft = %v, want nil when missing", *f.Aircraft)
	}
}

func TestGaruda_Rejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*garuda.Flight)
	}{
		{"missing flight id", func(f *garuda.Flight) { f.FlightID = "" }},
		{"unknown cabin", func(f *garuda.Flight) { f.FareClass = "premium" }},
		{"missing amount", func(f *garuda.Flight) { f.Price.Amount = "" }},
		{"missing currency", func(f *garuda.Flight) { f.Price.Currency = "" }},
		{"time without offset", func(f *garuda.Flight) { f.Departure.Time = "2025-12-15T06:00:00" }},
		{"unparseable arrival", func(f *garuda.Flight) { f.Arrival.Time = "soon" }},
		{"missing airport", func(f *garuda.Flight) { f.Arrival.Airport = "" }},
		{"bad segment time", func(f *garuda.Flight) {
			f.Segments = []garuda.Segment{{Departure: garuda.Point{Airport: "CGK", Time: "x"}, Arrival: f.Arrival}}
		}},
		{"segment departs before previous arrives", func(f *garuda.Flight) {
			f.Segments = []garuda.Segment{
				{Departure: f.Departure, Arrival: garuda.Point{Airport: "SUB", Time: "2025-12-15T07:30:00+07:00"}},
				{Departure: garuda.Point{Airport: "SUB", Time: "2025-12-15T07:00:00+07:00"}, Arrival: f.Arrival},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := garudaRecord(t)
			tt.mutate(&r)
			if _, err := testMapper.Garuda(r); err == nil {
				t.Error("Garuda() succeeded, want error")
			}
		})
	}
}

func TestGaruda_Baggage(t *testing.T) {
	one, two, zero, neg := 1, 2, 0, -1
	tests := []struct {
		name         string
		baggage      *garuda.Baggage
		want         domain.Baggage
		wantUnparsed bool
	}{
		{"pieces", &garuda.Baggage{CarryOn: &one, Checked: &two}, domain.Baggage{CarryOn: "1 piece", Checked: "2 pieces"}, false},
		{"zero checked", &garuda.Baggage{CarryOn: &one, Checked: &zero}, domain.Baggage{CarryOn: "1 piece", Checked: "0 pieces"}, false},
		{"checked missing", &garuda.Baggage{CarryOn: &one}, domain.Baggage{CarryOn: "1 piece"}, false},
		{"no baggage", nil, domain.Baggage{}, false},
		{"negative count", &garuda.Baggage{CarryOn: &neg, Checked: &one}, domain.Baggage{CarryOn: "-1", Checked: "1 piece"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := garudaRecord(t)
			r.Baggage = tt.baggage
			f, err := testMapper.Garuda(r)
			if err != nil {
				t.Fatal(err)
			}
			if f.Baggage != tt.want {
				t.Errorf("Baggage = %+v, want %+v", f.Baggage, tt.want)
			}
			if got := slices.Contains(f.Warnings, domain.WarningBaggageUnparsed); got != tt.wantUnparsed {
				t.Errorf("baggage_unparsed = %v, want %v", got, tt.wantUnparsed)
			}
		})
	}
}

func TestGaruda_DecimalFare(t *testing.T) {
	r := garudaRecord(t)
	r.Price = garuda.Price{Amount: "65.50", Currency: "USD"}
	f, err := testMapper.Garuda(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.Fare != (domain.Fare{Amount: "65.50", Currency: "USD"}) {
		t.Errorf("Fare = %+v", f.Fare)
	}
}
