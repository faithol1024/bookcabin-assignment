package mapper

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
)

func TestAirAsia_Golden(t *testing.T) {
	assertGolden(t, "../../gateway/airasia/mockdata", "airasia",
		func(r airasia.Response) []airasia.Flight { return r.Flights }, testMapper.AirAsia)
}

func qz7250(t *testing.T) airasia.Flight {
	t.Helper()
	var f airasia.Flight
	err := json.Unmarshal([]byte(`{
		"flight_code": "QZ7250", "airline": "AirAsia", "from_airport": "CGK", "to_airport": "DPS",
		"depart_time": "2025-12-15T15:15:00+07:00", "arrive_time": "2025-12-15T20:35:00+08:00",
		"duration_hours": 4.33, "direct_flight": false,
		"stops": [{"airport": "SOC", "wait_time_minutes": 95}],
		"price_idr": 485000, "seats": 88, "cabin_class": "economy",
		"baggage_note": "Cabin baggage only, checked bags additional fee"
	}`), &f)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// QZ7250 claims 4.33 h (259.8 min); the timestamps give 260, within the tolerance.
func TestAirAsia_QZ7250(t *testing.T) {
	f, err := testMapper.AirAsia(qz7250(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Duration() != 260*time.Minute {
		t.Errorf("Duration() = %v, want 260m", f.Duration())
	}
	if len(f.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", f.Warnings)
	}
	if f.ID != "QZ7250_AirAsia" || f.Provider != "AirAsia" || f.Airline != (domain.Airline{Name: "AirAsia", Code: "QZ"}) {
		t.Errorf("identity = %s, %s, %+v", f.ID, f.Provider, f.Airline)
	}
	if want := []domain.Layover{{Airport: "SOC", Duration: 95 * time.Minute}}; !slices.Equal(f.Layovers, want) {
		t.Errorf("Layovers = %v, want %v", f.Layovers, want)
	}
	if f.Fare != (domain.Fare{Amount: "485000", Currency: "IDR"}) || f.Price != 0 || f.Cabin != domain.CabinEconomy {
		t.Errorf("Fare = %+v, Price = %d, Cabin = %q", f.Fare, f.Price, f.Cabin)
	}
	if f.Baggage != (domain.Baggage{CarryOn: "Cabin baggage only", Checked: "Additional fee"}) {
		t.Errorf("Baggage = %+v", f.Baggage)
	}
	if f.Aircraft != nil || f.Amenities != nil || f.AvailableSeats != 88 {
		t.Errorf("aircraft %v, amenities %v, seats %d", f.Aircraft, f.Amenities, f.AvailableSeats)
	}
}

func TestAirAsia_DurationMismatch(t *testing.T) {
	r := qz7250(t)
	r.DurationHours = 4.5
	f, err := testMapper.AirAsia(r)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Warnings, []domain.Warning{domain.WarningDurationMismatch}) {
		t.Errorf("Warnings = %v, want duration_mismatch (claimed 270m, actual 260m)", f.Warnings)
	}
}

func TestAirAsia_Baggage(t *testing.T) {
	tests := []struct {
		in           string
		want         domain.Baggage
		wantUnparsed bool
	}{
		{"Cabin baggage only, checked bags additional fee", domain.Baggage{CarryOn: "Cabin baggage only", Checked: "Additional fee"}, false},
		{"checked bags additional fee , CABIN BAGGAGE ONLY", domain.Baggage{CarryOn: "Cabin baggage only", Checked: "Additional fee"}, false},
		{"Cabin baggage only", domain.Baggage{CarryOn: "Cabin baggage only"}, false},
		{"", domain.Baggage{}, false},
		{"20kg checked included", domain.Baggage{CarryOn: "20kg checked included", Checked: "20kg checked included"}, true},
		{"Cabin baggage only, free snacks", domain.Baggage{CarryOn: "Cabin baggage only, free snacks", Checked: "Cabin baggage only, free snacks"}, true},
		{"Cabin baggage only, cabin baggage only", domain.Baggage{CarryOn: "Cabin baggage only, cabin baggage only", Checked: "Cabin baggage only, cabin baggage only"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r := qz7250(t)
			r.BaggageNote = tt.in
			f, err := testMapper.AirAsia(r)
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

func TestAirlineCode(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"QZ7250", "QZ", true},
		{"QZ520", "QZ", true},
		{"D7123", "D7", true},
		{"QZ", "", false},
		{"Q", "", false},
		{"", "", false},
		{"QZX12", "", false},
		{"QZ12A", "", false},
	}
	for _, tt := range tests {
		if got, ok := airlineCode(tt.in); got != tt.want || ok != tt.wantOK {
			t.Errorf("airlineCode(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestAirAsia_Rejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*airasia.Flight)
	}{
		{"missing flight code", func(f *airasia.Flight) { f.FlightCode = "" }},
		{"no airline code", func(f *airasia.Flight) { f.FlightCode = "AIRASIA" }},
		{"unknown cabin", func(f *airasia.Flight) { f.CabinClass = "premium" }},
		{"missing price", func(f *airasia.Flight) { f.PriceIDR = "" }},
		{"time without offset", func(f *airasia.Flight) { f.DepartTime = "2025-12-15T15:15:00" }},
		{"unparseable arrival", func(f *airasia.Flight) { f.ArriveTime = "later" }},
		{"missing airport", func(f *airasia.Flight) { f.FromAirport = "" }},
		{"missing stop airport", func(f *airasia.Flight) { f.Stops[0].Airport = "" }},
		{"negative duration", func(f *airasia.Flight) { f.DurationHours = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := qz7250(t)
			tt.mutate(&r)
			if _, err := testMapper.AirAsia(r); err == nil {
				t.Error("AirAsia() succeeded, want error")
			}
		})
	}
}

func TestAirAsia_DirectFlagMismatch(t *testing.T) {
	for _, tt := range []struct {
		name   string
		direct bool
		stops  []airasia.Stop
		want   []domain.Warning
	}{
		{"one stop, not direct", false, []airasia.Stop{{Airport: "SOC", WaitTimeMinutes: 95}}, nil},
		{"one stop, claims direct", true, []airasia.Stop{{Airport: "SOC", WaitTimeMinutes: 95}}, []domain.Warning{domain.WarningSegmentInconsistent}},
		{"no stops, claims not direct", false, nil, []domain.Warning{domain.WarningSegmentInconsistent}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := qz7250(t)
			r.DirectFlight, r.Stops = tt.direct, tt.stops
			f, err := testMapper.AirAsia(r)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(f.Warnings, tt.want) {
				t.Errorf("warnings %v, want %v", f.Warnings, tt.want)
			}
		})
	}
}
