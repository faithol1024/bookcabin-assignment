package mapper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // tests must not depend on the host zoneinfo

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	"github.com/faithol1024/bookcabin-assignment/pkg/golden"
)

var testMapper = New(5 * time.Minute)

type mappedFixture struct {
	Flights  []domain.Flight
	Rejected int
}

// assertGolden maps every record of each fixture in dir and compares with testdata/<prefix>_<fixture>.golden.json.
func assertGolden[R any, T any](t *testing.T, dir, prefix string, records func(R) []T, toFlight func(T) (domain.Flight, error)) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no fixtures in %s: %v", dir, err)
	}
	for _, name := range names {
		base := strings.TrimSuffix(filepath.Base(name), ".json")
		t.Run(base, func(t *testing.T) {
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			var resp R
			if err := json.Unmarshal(b, &resp); err != nil {
				t.Fatal(err)
			}
			var got mappedFixture
			for _, r := range records(resp) {
				f, err := toFlight(r)
				if err != nil {
					got.Rejected++
					continue
				}
				got.Flights = append(got.Flights, f)
			}
			golden.Assert(t, filepath.Join("testdata", prefix+"_"+base+".golden.json"), got)
		})
	}
}

func TestRecords(t *testing.T) {
	records := []string{"economy", "bad", "business", "economy"}
	toFlight := func(r string) (domain.Flight, error) {
		c, ok := domain.ParseCabin(r)
		if !ok {
			return domain.Flight{}, errors.New("bad cabin")
		}
		return domain.Flight{Cabin: c}, nil
	}
	flights, rejected := Records(context.Background(), records, toFlight, domain.CabinEconomy)
	if len(flights) != 2 || rejected != 1 {
		t.Errorf("got %d flights, %d rejected; want 2, 1", len(flights), rejected)
	}
	for _, f := range flights {
		if f.Cabin != domain.CabinEconomy {
			t.Errorf("kept cabin %q, want economy only", f.Cabin)
		}
	}
}

func TestFlightID(t *testing.T) {
	if got := flightID("GA400", "Garuda Indonesia"); got != "GA400_GarudaIndonesia" {
		t.Errorf("flightID = %q", got)
	}
	if got := flightID("JT650", "Lion Air"); got != "JT650_LionAir" {
		t.Errorf("flightID = %q", got)
	}
}

func TestMismatch(t *testing.T) {
	tests := []struct {
		claimed, actual time.Duration
		want            bool
	}{
		{0, 245 * time.Minute, false},
		{245 * time.Minute, 245 * time.Minute, false},
		{250 * time.Minute, 245 * time.Minute, false},
		{240 * time.Minute, 245 * time.Minute, false},
		{251 * time.Minute, 245 * time.Minute, true},
		{185 * time.Minute, 245 * time.Minute, true},
	}
	for _, tt := range tests {
		if got := testMapper.mismatch(tt.claimed, tt.actual); got != tt.want {
			t.Errorf("mismatch(%v, %v) = %v, want %v", tt.claimed, tt.actual, got, tt.want)
		}
	}
}

func TestRecords_RejectedCountsOnlyRequestedCabin(t *testing.T) {
	eco := garudaRecord(t)
	biz := eco
	biz.FlightID, biz.FareClass = "GA401", "business"
	bizBad := biz
	bizBad.FlightID, bizBad.Arrival.Time = "GA402", "soon"
	ecoBad := eco
	ecoBad.FlightID, ecoBad.Arrival.Time = "GA403", "soon"
	unknownCabin := eco
	unknownCabin.FlightID, unknownCabin.FareClass = "GA404", "premium"

	flights, rejected := Records(context.Background(), []garuda.Flight{eco, biz, bizBad, ecoBad, unknownCabin}, testMapper.Garuda, domain.CabinEconomy)
	if len(flights) != 1 || rejected != 2 {
		t.Errorf("got %d flights, %d rejected; want 1 kept, 2 rejected (bad economy + unknown cabin, not bad business)", len(flights), rejected)
	}
}

// Every mapper reports the cabin with an error found after the cabin is known, so Records can skip other cabins.
func TestMappers_ReportCabinOnError(t *testing.T) {
	tests := []struct {
		name string
		mapF func() (domain.Flight, error)
	}{
		{"garuda", func() (domain.Flight, error) {
			return testMapper.Garuda(garuda.Flight{FlightID: "GA1", FareClass: "business", Price: garuda.Price{Amount: "1", Currency: "IDR"}})
		}},
		{"lionair", func() (domain.Flight, error) {
			return testMapper.LionAir(lionair.Flight{ID: "JT1", Pricing: lionair.Pricing{FareType: "BUSINESS", Total: "1", Currency: "IDR"}})
		}},
		{"batik", func() (domain.Flight, error) {
			return testMapper.Batik(batik.Flight{FlightNumber: "ID1", Fare: batik.Fare{Class: "C", TotalPrice: "1", CurrencyCode: "IDR"}})
		}},
		{"airasia", func() (domain.Flight, error) {
			return testMapper.AirAsia(airasia.Flight{FlightCode: "QZ1", CabinClass: "business", PriceIDR: "1"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := tt.mapF()
			if err == nil || f.Cabin != domain.CabinBusiness {
				t.Errorf("got cabin %q, error %v; want business with an error (no airports)", f.Cabin, err)
			}
		})
	}
}
