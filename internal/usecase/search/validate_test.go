package search

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

func validFlight(id string) domain.Flight {
	dep := time.Date(2025, 12, 15, 7, 0, 0, 0, time.UTC)
	return domain.Flight{
		ID:        id,
		Departure: domain.Airport{Code: "CGK", Time: dep},
		Arrival:   domain.Airport{Code: "DPS", Time: dep.Add(2 * time.Hour)},
		Price:     485000,
	}
}

func TestValidate_Rules(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*domain.Flight)
		wantReject bool
	}{
		{"valid", func(*domain.Flight) {}, false},
		{"arrival equals departure", func(f *domain.Flight) { f.Arrival.Time = f.Departure.Time }, true},
		{"arrival before departure", func(f *domain.Flight) { f.Arrival.Time = f.Departure.Time.Add(-time.Minute) }, true},
		{"smallest price", func(f *domain.Flight) { f.Price = 1 }, false},
		{"zero price", func(f *domain.Flight) { f.Price = 0 }, true},
		{"negative price", func(f *domain.Flight) { f.Price = -5 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := validFlight("F1")
			tt.mutate(&f)
			kept, rejected := validate(context.Background(), []domain.Flight{f})
			if got := rejected == 1; got != tt.wantReject || len(kept)+rejected != 1 {
				t.Errorf("kept %d, rejected %d; want reject = %v", len(kept), rejected, tt.wantReject)
			}
		})
	}
}

func TestValidate_KeepsOrderAndCounts(t *testing.T) {
	bad := validFlight("B")
	bad.Price = 0
	late := validFlight("C")
	late.Arrival.Time = late.Departure.Time
	flights := []domain.Flight{validFlight("A"), bad, late, validFlight("D")}

	kept, rejected := validate(context.Background(), flights)

	var ids []string
	for _, f := range kept {
		ids = append(ids, f.ID)
	}
	if !slices.Equal(ids, []string{"A", "D"}) || rejected != 2 {
		t.Errorf("kept %v, rejected %d; want [A D], 2", ids, rejected)
	}
	if flights[1].ID != "B" {
		t.Error("validate must not modify the input slice")
	}
}
