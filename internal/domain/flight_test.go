package domain

import (
	"testing"
	"time"
)

func TestParseCabin(t *testing.T) {
	tests := []struct {
		in     string
		want   Cabin
		wantOK bool
	}{
		{"economy", CabinEconomy, true},
		{"business", CabinBusiness, true},
		{"first", CabinFirst, true},
		{"ECONOMY", "", false},
		{"Economy", "", false},
		{"Y", "", false},
		{"premium", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		if got, ok := ParseCabin(tt.in); got != tt.want || ok != tt.wantOK {
			t.Errorf("ParseCabin(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestFlight_DurationAcrossZones(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	wita := time.FixedZone("WITA", 8*3600)
	f := Flight{
		Departure: Airport{Time: time.Date(2025, 12, 15, 18, 45, 0, 0, wib)},
		Arrival:   Airport{Time: time.Date(2025, 12, 15, 23, 50, 0, 0, wita)},
	}
	if got, want := f.Duration(), 245*time.Minute; got != want {
		t.Errorf("Duration() = %v, want %v", got, want)
	}
}

func TestFlight_DurationIncludesLayovers(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	wita := time.FixedZone("WITA", 8*3600)
	f := Flight{
		Departure: Airport{Time: time.Date(2025, 12, 15, 14, 0, 0, 0, wib)},
		Arrival:   Airport{Time: time.Date(2025, 12, 15, 18, 45, 0, 0, wita)},
		Layovers:  []Layover{{Airport: "SUB", Duration: 105 * time.Minute}},
	}
	if got, want := f.Duration(), 225*time.Minute; got != want {
		t.Errorf("Duration() = %v, want %v", got, want)
	}
}

func TestFlight_Stops(t *testing.T) {
	tests := []struct {
		name     string
		layovers []Layover
		want     int
	}{
		{"direct", nil, 0},
		{"one stop", []Layover{{Airport: "SUB"}}, 1},
		{"two stops", []Layover{{Airport: "SUB"}, {Airport: "UPG"}}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Flight{Layovers: tt.layovers}).Stops(); got != tt.want {
				t.Errorf("Stops() = %d, want %d", got, tt.want)
			}
		})
	}
}
