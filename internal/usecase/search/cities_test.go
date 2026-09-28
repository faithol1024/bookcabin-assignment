package search

import (
	"slices"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

func route(dep, arr string) domain.Flight {
	return domain.Flight{Departure: domain.Airport{Code: dep}, Arrival: domain.Airport{Code: arr}}
}

func city(a domain.Airport) string {
	if a.City == nil {
		return "<nil>"
	}
	return *a.City
}

func TestFillCities(t *testing.T) {
	jakarta := "Jakarta (provider)"
	withCity := route("CGK", "DPS")
	withCity.Departure.City = &jakarta
	flights := []domain.Flight{route("CGK", "DPS"), route("CGK", "TTE"), withCity}

	fillCities(flights)

	tests := []struct {
		dep, arr    string
		wantUnknown bool
	}{
		{"Jakarta", "Denpasar", false},
		{"Jakarta", "<nil>", true},
		{"Jakarta (provider)", "Denpasar", false},
	}
	for i, tt := range tests {
		f := flights[i]
		if city(f.Departure) != tt.dep || city(f.Arrival) != tt.arr {
			t.Errorf("flight %d: cities %s→%s, want %s→%s", i, city(f.Departure), city(f.Arrival), tt.dep, tt.arr)
		}
		if got := slices.Contains(f.Warnings, domain.WarningAirportUnknown); got != tt.wantUnknown {
			t.Errorf("flight %d: airport_unknown = %v, want %v", i, got, tt.wantUnknown)
		}
	}
}

func TestFillCities_BothUnknownWarnsOnce(t *testing.T) {
	flights := []domain.Flight{route("XXX", "YYY")}
	fillCities(flights)
	if !slices.Equal(flights[0].Warnings, []domain.Warning{domain.WarningAirportUnknown}) {
		t.Errorf("Warnings = %v, want one airport_unknown", flights[0].Warnings)
	}
}
