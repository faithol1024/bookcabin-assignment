package search

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	fxgw "github.com/faithol1024/bookcabin-assignment/internal/gateway/fx"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
)

type legResult struct {
	flights   []domain.Flight
	rejected  int
	providers []string // providers that returned at least one record
}

// retry absorbs AirAsia's simulated 503s; the mock is seeded, so the outcome is still deterministic.
func retry[T any](t *testing.T, call func() ([]T, error)) []T {
	t.Helper()
	for range 5 {
		records, err := call()
		if err == nil {
			return records
		}
		if !errors.Is(err, apperr.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	t.Fatal("provider still unavailable after 5 attempts")
	return nil
}

// searchLeg runs one economy leg through every mock gateway, the mappers, conversion, validation and the city lookup.
func searchLeg(t *testing.T, origin, destination, date string) legResult {
	t.Helper()
	ctx := context.Background()
	m := mapper.New(5 * time.Minute)
	var res legResult
	add := func(provider string, n int, flights []domain.Flight, rejected int) {
		if n > 0 {
			res.providers = append(res.providers, provider)
		}
		res.flights = append(res.flights, flights...)
		res.rejected += rejected
	}

	ga := retry(t, func() ([]garuda.Flight, error) {
		r, err := garuda.NewMock().Search(ctx, garuda.Query{Origin: origin, Destination: destination, Date: date})
		return r.Flights, err
	})
	f, r := mapper.Records(ctx, ga, m.Garuda, domain.CabinEconomy)
	add("Garuda Indonesia", len(ga), f, r)

	jt := retry(t, func() ([]lionair.Flight, error) {
		r, err := lionair.NewMock().Search(ctx, lionair.Query{Origin: origin, Destination: destination, Date: date})
		return r.Flights, err
	})
	f, r = mapper.Records(ctx, jt, m.LionAir, domain.CabinEconomy)
	add("Lion Air", len(jt), f, r)

	id := retry(t, func() ([]batik.Flight, error) {
		r, err := batik.NewMock().Search(ctx, batik.Query{Origin: origin, Destination: destination, Date: date})
		return r.Flights, err
	})
	f, r = mapper.Records(ctx, id, m.Batik, domain.CabinEconomy)
	add("Batik Air", len(id), f, r)

	aa := airasia.NewMock()
	qz := retry(t, func() ([]airasia.Flight, error) {
		r, err := aa.Search(ctx, airasia.Query{Origin: origin, Destination: destination, Date: date})
		return r.Flights, err
	})
	f, r = mapper.Records(ctx, qz, m.AirAsia, domain.CabinEconomy)
	add("AirAsia", len(qz), f, r)

	converted, unconverted := converter{exponents: exponents, fx: fxgw.NewMock()}.convert(ctx, res.flights)
	kept, rejected := validate(ctx, converted)
	res.flights, res.rejected = kept, res.rejected+unconverted+rejected
	fillCities(res.flights)
	return res
}

func (r legResult) ids() []string {
	ids := make([]string, 0, len(r.flights))
	for _, f := range r.flights {
		ids = append(ids, f.ID)
	}
	return ids
}

func (r legResult) flight(t *testing.T, id string) domain.Flight {
	t.Helper()
	i := slices.IndexFunc(r.flights, func(f domain.Flight) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in %v", id, r.ids())
	}
	return r.flights[i]
}

func TestScenario_ReturnRoute(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "DPS", "CGK", "2025-12-18")
	want := []string{"GA401_GarudaIndonesia", "JT31_LionAir", "ID6515_BatikAir", "QZ7511_AirAsia"}
	if !slices.Equal(got.ids(), want) || got.rejected != 0 {
		t.Fatalf("flights %v, rejected %d; want %v, 0", got.ids(), got.rejected, want)
	}
	for _, f := range got.flights {
		if f.Departure.Code != "DPS" || f.Arrival.Code != "CGK" || len(f.Warnings) != 0 {
			t.Errorf("%s: %s→%s, warnings %v", f.ID, f.Departure.Code, f.Arrival.Code, f.Warnings)
		}
	}
}

func TestScenario_NextDayNotInSameDay(t *testing.T) {
	t.Parallel()
	next := searchLeg(t, "CGK", "DPS", "2025-12-16")
	if want := []string{"GA402_GarudaIndonesia", "JT744_LionAir"}; !slices.Equal(next.ids(), want) {
		t.Fatalf("12-16 flights = %v, want %v", next.ids(), want)
	}
	same := searchLeg(t, "CGK", "DPS", "2025-12-15")
	for _, id := range next.ids() {
		if slices.Contains(same.ids(), id) {
			t.Errorf("12-15 search returned 12-16 flight %s", id)
		}
	}
	if len(same.flights) != 13 {
		t.Errorf("12-15 search returned %d flights, want the 13 given", len(same.flights))
	}
}

func TestScenario_WITTimezone(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "DJJ", "2025-12-20")
	if want := []string{"GA652_GarudaIndonesia", "ID6180_BatikAir"}; !slices.Equal(got.ids(), want) {
		t.Fatalf("flights = %v, want %v", got.ids(), want)
	}
	for id, minutes := range map[string]int{"GA652_GarudaIndonesia": 450, "ID6180_BatikAir": 465} {
		f := got.flight(t, id)
		if _, off := f.Arrival.Time.Zone(); off != 9*3600 {
			t.Errorf("%s arrival offset = %ds, want +09:00", id, off)
		}
		if int(f.Duration().Minutes()) != minutes || city(f.Arrival) != "Jayapura" || len(f.Warnings) != 0 {
			t.Errorf("%s: %v, city %s, warnings %v; want %dm, Jayapura, none", id, f.Duration(), city(f.Arrival), f.Warnings, minutes)
		}
	}
}

func TestScenario_Overnight(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "UPG", "2025-12-20")
	if want := []string{"JT790_LionAir", "QZ7800_AirAsia"}; !slices.Equal(got.ids(), want) {
		t.Fatalf("flights = %v, want %v", got.ids(), want)
	}
	for _, f := range got.flights {
		dep, arr := f.Departure.Time.Format(time.RFC3339), f.Arrival.Time.Format(time.RFC3339)
		if dep != "2025-12-20T23:30:00+07:00" || arr != "2025-12-21T02:10:00+08:00" {
			t.Errorf("%s: %s → %s, want 23:30 WIB → 02:10 WITA next day", f.ID, dep, arr)
		}
		if f.Duration() != 100*time.Minute || len(f.Warnings) != 0 {
			t.Errorf("%s: %v, warnings %v; want 100m, none", f.ID, f.Duration(), f.Warnings)
		}
	}
	if next := searchLeg(t, "CGK", "UPG", "2025-12-21"); len(next.flights) != 0 {
		t.Errorf("arrival date search returned %v", next.ids())
	}
}

func TestScenario_EqualPriceAndDuration(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "SUB", "2025-12-21")
	if want := []string{"JT690_LionAir", "ID6580_BatikAir"}; !slices.Equal(got.ids(), want) {
		t.Fatalf("flights = %v, want %v", got.ids(), want)
	}
	a, b := got.flights[0], got.flights[1]
	if a.Fare != b.Fare || a.Duration() != b.Duration() || a.ID == b.ID {
		t.Errorf("fares %+v / %+v, durations %v / %v; want equal, with distinct IDs", a.Fare, b.Fare, a.Duration(), b.Duration())
	}
}

func TestScenario_ArrivalBeforeDeparture(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "KNO", "2025-12-22")
	if len(got.flights) != 0 || got.rejected != 1 || !slices.Equal(got.providers, []string{"Batik Air"}) {
		t.Errorf("flights %v, rejected %d, providers %v; want none, 1, [Batik Air]", got.ids(), got.rejected, got.providers)
	}
}

func TestScenario_UnparseableTime(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "BPN", "2025-12-22")
	if len(got.flights) != 0 || got.rejected != 1 || !slices.Equal(got.providers, []string{"AirAsia"}) {
		t.Errorf("flights %v, rejected %d, providers %v; want none, 1, [AirAsia]", got.ids(), got.rejected, got.providers)
	}
}

func TestScenario_MissingOptionalFields(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "JOG", "2025-12-22")
	f := got.flight(t, "GA208_GarudaIndonesia")
	if f.Aircraft != nil || len(f.Amenities) != 0 || f.Baggage != (domain.Baggage{}) {
		t.Errorf("aircraft %v, amenities %v, baggage %+v; want none", f.Aircraft, f.Amenities, f.Baggage)
	}
	if len(f.Warnings) != 0 || got.rejected != 0 {
		t.Errorf("warnings %v, rejected %d", f.Warnings, got.rejected)
	}
}

func TestScenario_AirportNotInCityMap(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "TTE", "2025-12-22")
	f := got.flight(t, "JT798_LionAir")
	if f.Arrival.City != nil || city(f.Departure) != "Jakarta" {
		t.Errorf("cities %s→%s, want Jakarta→<nil>", city(f.Departure), city(f.Arrival))
	}
	if !slices.Equal(f.Warnings, []domain.Warning{domain.WarningAirportUnknown}) || got.rejected != 0 {
		t.Errorf("warnings %v, rejected %d; want [airport_unknown], 0", f.Warnings, got.rejected)
	}
}

func TestScenario_UnknownBaggageText(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "LOP", "2025-12-22")
	f := got.flight(t, "QZ7650_AirAsia")
	const raw = "20kg checked included, priority boarding"
	if f.Baggage != (domain.Baggage{CarryOn: raw, Checked: raw}) {
		t.Errorf("Baggage = %+v, want raw text in both fields", f.Baggage)
	}
	if !slices.Equal(f.Warnings, []domain.Warning{domain.WarningBaggageUnparsed}) {
		t.Errorf("Warnings = %v, want [baggage_unparsed]", f.Warnings)
	}
}

func TestScenario_TwoSeatsLeft(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "SUB", "2025-12-23")
	if f := got.flight(t, "GA320_GarudaIndonesia"); f.AvailableSeats != 2 {
		t.Errorf("AvailableSeats = %d, want 2", f.AvailableSeats)
	}
}

func TestScenario_ForeignCurrency(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "DPS", "2025-12-24")
	if want := []string{"GA410_GarudaIndonesia", "ID6520_BatikAir"}; !slices.Equal(got.ids(), want) || got.rejected != 1 {
		t.Fatalf("flights %v, rejected %d; want %v, 1 (JT35 in XYZ)", got.ids(), got.rejected, want)
	}
	if !slices.Contains(got.providers, "Lion Air") {
		t.Errorf("providers %v, want Lion Air to have returned its XYZ record", got.providers)
	}
	for id, want := range map[string]struct {
		price    domain.Money
		original domain.ForeignPrice
	}{
		"GA410_GarudaIndonesia": {1064375, domain.ForeignPrice{Amount: 6550, Currency: "USD", Exponent: 2, Rate: 16250}},
		"ID6520_BatikAir":       {1087790, domain.ForeignPrice{Amount: 8990, Currency: "SGD", Exponent: 2, Rate: 12100}},
	} {
		f := got.flight(t, id)
		if f.Price != want.price || f.Original == nil || *f.Original != want.original {
			t.Errorf("%s: price %d, original %+v; want %d, %+v", id, f.Price, f.Original, want.price, want.original)
		}
	}
}

func TestScenario_IDRFaresHaveNoOriginal(t *testing.T) {
	t.Parallel()
	got := searchLeg(t, "CGK", "DPS", "2025-12-15")
	for _, f := range got.flights {
		if f.Price <= 0 || f.Original != nil {
			t.Errorf("%s: price %d, original %+v; want positive IDR, nil", f.ID, f.Price, f.Original)
		}
	}
}

func TestScenario_MultiCityChain(t *testing.T) {
	t.Parallel()
	legs := []struct {
		origin, destination, date string
		minProviders              int
	}{
		{"CGK", "DPS", "2025-12-15", 4},
		{"DPS", "SUB", "2025-12-17", 2},
		{"SUB", "CGK", "2025-12-19", 2},
	}
	for _, l := range legs {
		got := searchLeg(t, l.origin, l.destination, l.date)
		if len(got.providers) < l.minProviders {
			t.Errorf("%s→%s %s: providers %v, want at least %d", l.origin, l.destination, l.date, got.providers, l.minProviders)
		}
		for _, f := range got.flights {
			if f.Departure.Code != l.origin || f.Arrival.Code != l.destination || f.Departure.Time.Format(time.DateOnly) != l.date {
				t.Errorf("%s: %s→%s on %s, want %s→%s on %s", f.ID, f.Departure.Code, f.Arrival.Code, f.Departure.Time.Format(time.DateOnly), l.origin, l.destination, l.date)
			}
		}
	}
}
