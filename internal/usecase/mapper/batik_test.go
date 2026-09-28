package mapper

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
)

func TestBatik_Golden(t *testing.T) {
	assertGolden(t, "../../gateway/batik/mockdata", "batik",
		func(r batik.Response) []batik.Flight { return r.Results }, testMapper.Batik)
}

// ID7042 claims 3h 5m, but its timestamps span 18:45 WIB → 23:50 WITA = 245 minutes.
func id7042(t *testing.T) batik.Flight {
	t.Helper()
	var f batik.Flight
	err := json.Unmarshal([]byte(`{
		"flightNumber": "ID7042", "airlineName": "Batik Air", "airlineIATA": "ID",
		"origin": "CGK", "destination": "DPS",
		"departureDateTime": "2025-12-15T18:45:00+0700", "arrivalDateTime": "2025-12-15T23:50:00+0800",
		"travelTime": "3h 5m", "numberOfStops": 1,
		"connections": [{"stopAirport": "UPG", "stopDuration": "55m"}],
		"fare": {"basePrice": 850000, "taxes": 100000, "totalPrice": 950000, "currencyCode": "IDR", "class": "Y"},
		"seatsAvailable": 41, "aircraftModel": "Airbus A320",
		"baggageInfo": "7kg cabin, 20kg checked", "onboardServices": ["Snack"]
	}`), &f)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBatik_ID7042TrustsTimestamps(t *testing.T) {
	f, err := testMapper.Batik(id7042(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Duration() != 245*time.Minute {
		t.Errorf("Duration() = %v, want 245m", f.Duration())
	}
	if !slices.Equal(f.Warnings, []domain.Warning{domain.WarningDurationMismatch}) {
		t.Errorf("Warnings = %v, want duration_mismatch", f.Warnings)
	}
	if got := f.Departure.Time.Format(time.RFC3339); got != "2025-12-15T18:45:00+07:00" {
		t.Errorf("departure = %s", got)
	}
	if got := f.Arrival.Time.Format(time.RFC3339); got != "2025-12-15T23:50:00+08:00" {
		t.Errorf("arrival = %s", got)
	}
	if want := []domain.Layover{{Airport: "UPG", Duration: 55 * time.Minute}}; !slices.Equal(f.Layovers, want) {
		t.Errorf("Layovers = %v, want %v", f.Layovers, want)
	}
}

func TestBatik_Maps(t *testing.T) {
	r := id7042(t)
	r.TravelTime = "4h 5m"
	f, err := testMapper.Batik(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "ID7042_BatikAir" || f.Provider != "Batik Air" || f.Airline != (domain.Airline{Name: "Batik Air", Code: "ID"}) {
		t.Errorf("identity = %s, %s, %+v", f.ID, f.Provider, f.Airline)
	}
	if len(f.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", f.Warnings)
	}
	if f.Fare != (domain.Fare{Amount: "950000", Currency: "IDR"}) || f.Price != 0 || f.Cabin != domain.CabinEconomy {
		t.Errorf("Fare = %+v, Price = %d, Cabin = %q", f.Fare, f.Price, f.Cabin)
	}
	if f.Baggage != (domain.Baggage{CarryOn: "7kg", Checked: "20kg"}) {
		t.Errorf("Baggage = %+v", f.Baggage)
	}
	if !slices.Equal(f.Amenities, []string{"snack"}) {
		t.Errorf("Amenities = %v, want lower-case", f.Amenities)
	}
	if f.Aircraft == nil || *f.Aircraft != "Airbus A320" || f.AvailableSeats != 41 {
		t.Errorf("aircraft %v, seats %d", f.Aircraft, f.AvailableSeats)
	}
	if f.Departure.City != nil || f.Arrival.City != nil {
		t.Error("Batik gives no cities; they are filled per leg")
	}
}

func TestBatik_Cabins(t *testing.T) {
	tests := []struct {
		class   string
		want    domain.Cabin
		wantErr bool
	}{
		{"Y", domain.CabinEconomy, false},
		{"C", domain.CabinBusiness, false},
		{"J", domain.CabinBusiness, false},
		{"y", "", true},
		{"Q", "", true},
		{"economy", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		r := id7042(t)
		r.Fare.Class = tt.class
		f, err := testMapper.Batik(r)
		if (err != nil) != tt.wantErr || f.Cabin != tt.want {
			t.Errorf("class %q: cabin %q, err %v; want %q, error %v", tt.class, f.Cabin, err, tt.want, tt.wantErr)
		}
	}
}

func TestBatik_Baggage(t *testing.T) {
	tests := []struct {
		in           string
		want         domain.Baggage
		wantUnparsed bool
	}{
		{"7kg cabin, 20kg checked", domain.Baggage{CarryOn: "7kg", Checked: "20kg"}, false},
		{"20kg checked, 7kg cabin", domain.Baggage{CarryOn: "7kg", Checked: "20kg"}, false},
		{" 7kg  Cabin ,20kg CHECKED ", domain.Baggage{CarryOn: "7kg", Checked: "20kg"}, false},
		{"7kg cabin", domain.Baggage{CarryOn: "7kg"}, false},
		{"", domain.Baggage{}, false},
		{"7kg cabin, 10kg cabin", domain.Baggage{CarryOn: "7kg cabin, 10kg cabin", Checked: "7kg cabin, 10kg cabin"}, true},
		{"7kg cabin, 20kg hold", domain.Baggage{CarryOn: "7kg cabin, 20kg hold", Checked: "7kg cabin, 20kg hold"}, true},
		{"No free baggage", domain.Baggage{CarryOn: "No free baggage", Checked: "No free baggage"}, true},
		{"7kg cabin,", domain.Baggage{CarryOn: "7kg cabin,", Checked: "7kg cabin,"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r := id7042(t)
			r.BaggageInfo = tt.in
			f, err := testMapper.Batik(r)
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

func TestBatik_NoTravelTimeIsNoClaim(t *testing.T) {
	r := id7042(t)
	r.TravelTime = ""
	f, err := testMapper.Batik(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", f.Warnings)
	}
}

func TestBatik_Rejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*batik.Flight)
	}{
		{"missing flight number", func(f *batik.Flight) { f.FlightNumber = "" }},
		{"missing amount", func(f *batik.Flight) { f.Fare.TotalPrice = "" }},
		{"missing currency", func(f *batik.Flight) { f.Fare.CurrencyCode = "" }},
		{"offset with colon", func(f *batik.Flight) { f.DepartureDateTime = "2025-12-15T18:45:00+07:00" }},
		{"time without offset", func(f *batik.Flight) { f.ArrivalDateTime = "2025-12-15T23:50:00" }},
		{"missing airport", func(f *batik.Flight) { f.Destination = "" }},
		{"bad travel time", func(f *batik.Flight) { f.TravelTime = "3 hours" }},
		{"bad stop duration", func(f *batik.Flight) { f.Connections[0].StopDuration = "55" }},
		{"missing stop airport", func(f *batik.Flight) { f.Connections[0].StopAirport = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := id7042(t)
			tt.mutate(&r)
			if _, err := testMapper.Batik(r); err == nil {
				t.Error("Batik() succeeded, want error")
			}
		})
	}
}

func TestParseBatikTime(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "2025-12-15T07:15:00+0700", want: "2025-12-15T07:15:00+07:00"},
		{in: "2025-12-15T23:50:00+0800", want: "2025-12-15T23:50:00+08:00"},
		{in: "2025-12-15T10:00:00+0900", want: "2025-12-15T10:00:00+09:00"},
		{in: "2025-12-15T07:15:00+07:00", wantErr: true},
		{in: "2025-12-15T07:15:00Z", wantErr: true},
		{in: "2025-12-15T07:15:00", wantErr: true},
		{in: "2025-13-15T07:15:00+0700", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseBatikTime(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := got.Format(time.RFC3339); s != tt.want {
				t.Errorf("got %s, want %s", s, tt.want)
			}
		})
	}
}

func FuzzParseBatikTime(f *testing.F) {
	for _, s := range []string{"2025-12-15T07:15:00+0700", "2025-12-15T23:50:00+0800", "2025-12-15T07:15:00+07:00", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := parseBatikTime(s)
		if err != nil {
			return
		}
		again, err := parseBatikTime(got.Format(batikTimeLayout))
		if err != nil || !again.Equal(got) {
			t.Errorf("%q: round trip gave %v, %v", s, again, err)
		}
	})
}

func TestBatik_StopCountMismatch(t *testing.T) {
	for _, tt := range []struct {
		name  string
		stops int
		want  bool
	}{
		{"matches connections", 1, false},
		{"claims direct with a connection", 0, true},
		{"claims more stops than connections", 2, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := id7042(t)
			r.NumberOfStops = tt.stops
			f, err := testMapper.Batik(r)
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(f.Warnings, domain.WarningSegmentInconsistent); got != tt.want || f.Stops() != 1 {
				t.Errorf("segment_inconsistent = %v, stops %d; want %v, 1", got, f.Stops(), tt.want)
			}
		})
	}
}
