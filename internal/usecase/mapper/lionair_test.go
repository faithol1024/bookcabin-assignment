package mapper

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
)

func TestLionAir_Golden(t *testing.T) {
	assertGolden(t, "../../gateway/lionair/mockdata", "lionair",
		func(r lionair.Response) []lionair.Flight { return r.Data.AvailableFlights }, testMapper.LionAir)
}

func lionAirRecord(t *testing.T) lionair.Flight {
	t.Helper()
	var f lionair.Flight
	err := json.Unmarshal([]byte(`{
		"id": "JT650", "carrier": {"name": "Lion Air", "iata": "JT"},
		"route": {"from": {"code": "CGK", "city": "Jakarta"}, "to": {"code": "DPS", "city": "Denpasar"}},
		"schedule": {"departure": "2025-12-15T16:20:00", "departure_timezone": "Asia/Jakarta",
		             "arrival": "2025-12-15T21:10:00", "arrival_timezone": "Asia/Makassar"},
		"flight_time": 230, "is_direct": false, "stop_count": 1,
		"layovers": [{"airport": "SUB", "duration_minutes": 75}],
		"pricing": {"total": 780000, "currency": "IDR", "fare_type": "ECONOMY"},
		"seats_left": 52, "plane_type": "Boeing 737-800",
		"services": {"wifi_available": false, "meals_included": false, "baggage_allowance": {"cabin": "7 kg", "hold": "20 kg"}}
	}`), &f)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLionAir_Maps(t *testing.T) {
	f, err := testMapper.LionAir(lionAirRecord(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "JT650_LionAir" || f.Provider != "Lion Air" || f.Airline != (domain.Airline{Name: "Lion Air", Code: "JT"}) {
		t.Errorf("identity = %s, %s, %+v", f.ID, f.Provider, f.Airline)
	}
	if got := f.Departure.Time.Format(time.RFC3339); got != "2025-12-15T16:20:00+07:00" {
		t.Errorf("departure = %s", got)
	}
	if got := f.Arrival.Time.Format(time.RFC3339); got != "2025-12-15T21:10:00+08:00" {
		t.Errorf("arrival = %s", got)
	}
	if f.Departure.Time.Unix() != 1765790400 || f.Arrival.Time.Unix() != 1765804200 {
		t.Errorf("timestamps = %d, %d", f.Departure.Time.Unix(), f.Arrival.Time.Unix())
	}
	if f.Duration() != 230*time.Minute || len(f.Warnings) != 0 {
		t.Errorf("Duration() = %v, warnings %v; want 230m, none", f.Duration(), f.Warnings)
	}
	if want := []domain.Layover{{Airport: "SUB", Duration: 75 * time.Minute}}; !slices.Equal(f.Layovers, want) {
		t.Errorf("Layovers = %v, want %v", f.Layovers, want)
	}
	if f.Baggage != (domain.Baggage{CarryOn: "7 kg", Checked: "20 kg"}) {
		t.Errorf("Baggage = %+v", f.Baggage)
	}
	if f.Fare != (domain.Fare{Amount: "780000", Currency: "IDR"}) || f.Cabin != domain.CabinEconomy {
		t.Errorf("Fare = %+v, Cabin = %q", f.Fare, f.Cabin)
	}
	if f.Aircraft == nil || *f.Aircraft != "Boeing 737-800" || f.AvailableSeats != 52 {
		t.Errorf("aircraft %v, seats %d", f.Aircraft, f.AvailableSeats)
	}
	if f.Departure.City == nil || *f.Departure.City != "Jakarta" {
		t.Errorf("departure airport = %+v", f.Departure)
	}
}

func TestLionAir_WITZone(t *testing.T) {
	r := lionAirRecord(t)
	r.Schedule.Arrival, r.Schedule.ArrivalTimezone = "2025-12-15T23:20:00", "Asia/Jayapura"
	f, err := testMapper.LionAir(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Arrival.Time.Format(time.RFC3339); got != "2025-12-15T23:20:00+09:00" {
		t.Errorf("arrival = %s", got)
	}
}

func TestLionAir_DurationMismatch(t *testing.T) {
	r := lionAirRecord(t)
	r.FlightTime = 170
	f, err := testMapper.LionAir(r)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Warnings, []domain.Warning{domain.WarningDurationMismatch}) {
		t.Errorf("Warnings = %v, want duration_mismatch", f.Warnings)
	}
}

func TestLionAir_Amenities(t *testing.T) {
	tests := []struct {
		wifi, meals bool
		want        []string
	}{
		{false, false, nil},
		{true, false, []string{"wifi"}},
		{false, true, []string{"meal"}},
		{true, true, []string{"wifi", "meal"}},
	}
	for _, tt := range tests {
		r := lionAirRecord(t)
		r.Services.WifiAvailable, r.Services.MealsIncluded = tt.wifi, tt.meals
		f, err := testMapper.LionAir(r)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(f.Amenities, tt.want) {
			t.Errorf("wifi %v meals %v: Amenities = %v, want %v", tt.wifi, tt.meals, f.Amenities, tt.want)
		}
	}
}

func TestLionAir_Cabins(t *testing.T) {
	tests := []struct {
		fareType string
		want     domain.Cabin
		wantErr  bool
	}{
		{"ECONOMY", domain.CabinEconomy, false},
		{"BUSINESS", domain.CabinBusiness, false},
		{"FIRST", domain.CabinFirst, false},
		{"economy", "", true},
		{"PREMIUM", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		r := lionAirRecord(t)
		r.Pricing.FareType = tt.fareType
		f, err := testMapper.LionAir(r)
		if (err != nil) != tt.wantErr || f.Cabin != tt.want {
			t.Errorf("fare type %q: cabin %q, err %v; want %q, error %v", tt.fareType, f.Cabin, err, tt.want, tt.wantErr)
		}
	}
}

func TestLionAir_Rejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*lionair.Flight)
	}{
		{"missing id", func(f *lionair.Flight) { f.ID = "" }},
		{"missing amount", func(f *lionair.Flight) { f.Pricing.Total = "" }},
		{"missing currency", func(f *lionair.Flight) { f.Pricing.Currency = "" }},
		{"naive time without zone", func(f *lionair.Flight) { f.Schedule.DepartureTimezone = "" }},
		{"unknown zone", func(f *lionair.Flight) { f.Schedule.ArrivalTimezone = "Asia/Atlantis" }},
		{"offset in naive time", func(f *lionair.Flight) { f.Schedule.Departure = "2025-12-15T16:20:00+07:00" }},
		{"missing airport", func(f *lionair.Flight) { f.Route.To.Code = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := lionAirRecord(t)
			tt.mutate(&r)
			if _, err := testMapper.LionAir(r); err == nil {
				t.Error("LionAir() succeeded, want error")
			}
		})
	}
}

func TestLionAir_StopCountMismatch(t *testing.T) {
	for _, tt := range []struct {
		name      string
		stopCount int
		want      []domain.Warning
	}{
		{"matches layovers", 1, nil},
		{"claims more stops than layovers", 2, []domain.Warning{domain.WarningSegmentInconsistent}},
		{"claims direct with a layover", 0, []domain.Warning{domain.WarningSegmentInconsistent}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := lionAirRecord(t)
			r.StopCount = tt.stopCount
			f, err := testMapper.LionAir(r)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(f.Warnings, tt.want) || f.Stops() != 1 {
				t.Errorf("warnings %v, stops %d; want %v, 1 (from layovers)", f.Warnings, f.Stops(), tt.want)
			}
		})
	}
}

func TestLionAir_LayoverMissingAirportRejected(t *testing.T) {
	r := lionAirRecord(t)
	r.Layovers[0].Airport = ""
	if _, err := testMapper.LionAir(r); err == nil {
		t.Error("LionAir() succeeded, want error for a layover without airport")
	}
}
