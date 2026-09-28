package search

import (
	"context"
	"errors"
	"maps"
	"math"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// exponents mirrors flight_search.currency.exponents in the committed config files.
var exponents = map[string]int{"IDR": 0, "USD": 2, "SGD": 2, "MYR": 2, "EUR": 2, "JPY": 0}

// fakeFX quotes rates and counts calls per currency; a currency missing from rates fails.
type fakeFX struct {
	rates map[string]int64
	mu    sync.Mutex
	calls map[string]int
}

func newFakeFX(rates map[string]int64) *fakeFX {
	return &fakeFX{rates: rates, calls: map[string]int{}}
}

func (f *fakeFX) Rate(_ context.Context, currency string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[currency]++
	if rate, ok := f.rates[currency]; ok {
		return rate, nil
	}
	return 0, apperr.ErrUnavailable
}

var mockRates = map[string]int64{"USD": 16250, "SGD": 12100, "MYR": 3500, "EUR": 17600, "JPY": 108}

func fareFlight(id, amount, currency string) domain.Flight {
	return domain.Flight{ID: id, Fare: domain.Fare{Amount: amount, Currency: currency}}
}

func convertOne(t *testing.T, fx FXGateway, amount, currency string) (domain.Flight, bool) {
	t.Helper()
	kept, rejected := converter{exponents: exponents, fx: fx}.convert(context.Background(), []domain.Flight{fareFlight("F1", amount, currency)})
	if len(kept)+rejected != 1 {
		t.Fatalf("kept %d + rejected %d, want 1 flight", len(kept), rejected)
	}
	if rejected == 1 {
		return domain.Flight{}, false
	}
	return kept[0], true
}

func TestConvert_USD(t *testing.T) {
	f, ok := convertOne(t, newFakeFX(mockRates), "65.50", "USD")
	if !ok {
		t.Fatal("USD fare rejected")
	}
	if f.Price != 1064375 {
		t.Errorf("Price = %d, want 1064375 (65.50 × 16250)", f.Price)
	}
	want := domain.ForeignPrice{Amount: 6550, Currency: "USD", Exponent: 2, Rate: 16250}
	if f.Original == nil || *f.Original != want {
		t.Errorf("Original = %+v, want %+v", f.Original, want)
	}
}

func TestConvert_RoundsHalfUp(t *testing.T) {
	tests := []struct {
		amount string
		want   domain.Money
	}{
		{"65.51", 1064538},  // 1 064 537.5
		{"0.01", 163},       // 162.5
		{"0.02", 325},       // exact
		{"0.03", 488},       // 487.5
		{"65.5", 1064375},   // one fraction digit
		{"65.500", 1064375}, // zero beyond the exponent
	}
	for _, tt := range tests {
		t.Run(tt.amount, func(t *testing.T) {
			f, ok := convertOne(t, newFakeFX(mockRates), tt.amount, "USD")
			if !ok || f.Price != tt.want {
				t.Errorf("USD %s = %d (kept %v), want %d", tt.amount, f.Price, ok, tt.want)
			}
		})
	}
}

func TestConvert_JPYHasNoMinorUnits(t *testing.T) {
	f, ok := convertOne(t, newFakeFX(mockRates), "12000", "JPY")
	if !ok || f.Price != 1296000 {
		t.Fatalf("JPY 12000 = %d (kept %v), want 1296000", f.Price, ok)
	}
	if want := (domain.ForeignPrice{Amount: 12000, Currency: "JPY", Exponent: 0, Rate: 108}); *f.Original != want {
		t.Errorf("Original = %+v, want %+v", *f.Original, want)
	}
	if _, ok := convertOne(t, newFakeFX(mockRates), "12000.5", "JPY"); ok {
		t.Error("JPY with a fraction was kept")
	}
}

func TestConvert_IDRUntouched(t *testing.T) {
	fx := newFakeFX(mockRates)
	f, ok := convertOne(t, fx, "485000", "IDR")
	if !ok || f.Price != 485000 || f.Original != nil {
		t.Errorf("IDR 485000 = %d, original %+v (kept %v); want 485000, nil", f.Price, f.Original, ok)
	}
	if len(fx.calls) != 0 {
		t.Errorf("FX calls = %v, want none for IDR", fx.calls)
	}
}

func TestConvert_UnsupportedCurrencyRejected(t *testing.T) {
	fx := newFakeFX(mockRates)
	flights := []domain.Flight{fareFlight("A", "485000", "IDR"), fareFlight("B", "120.00", "XYZ"), fareFlight("C", "10", "usd")}
	kept, rejected := converter{exponents: exponents, fx: fx}.convert(context.Background(), flights)
	if ids := flightIDs(kept); !slices.Equal(ids, []string{"A"}) || rejected != 2 {
		t.Errorf("kept %v, rejected %d; want [A], 2", ids, rejected)
	}
	if len(fx.calls) != 0 {
		t.Errorf("FX calls = %v, want none for unsupported currencies", fx.calls)
	}
}

func TestConvert_FXDownDropsOnlyForeignFares(t *testing.T) {
	flights := []domain.Flight{
		fareFlight("A", "485000", "IDR"),
		fareFlight("B", "65.50", "USD"),
		fareFlight("C", "89.90", "SGD"),
		fareFlight("D", "1250000", "IDR"),
	}
	kept, rejected := converter{exponents: exponents, fx: newFakeFX(nil)}.convert(context.Background(), flights)
	if ids := flightIDs(kept); !slices.Equal(ids, []string{"A", "D"}) || rejected != 2 {
		t.Errorf("kept %v, rejected %d; want [A D], 2", ids, rejected)
	}
}

func TestConvert_FailedCurrencyDoesNotAffectOthers(t *testing.T) {
	fx := newFakeFX(map[string]int64{"SGD": 12100})
	flights := []domain.Flight{fareFlight("A", "65.50", "USD"), fareFlight("B", "89.90", "SGD")}
	kept, rejected := converter{exponents: exponents, fx: fx}.convert(context.Background(), flights)
	if ids := flightIDs(kept); !slices.Equal(ids, []string{"B"}) || rejected != 1 || kept[0].Price != 1087790 {
		t.Errorf("kept %v, rejected %d; want [B] at 1087790, 1", ids, rejected)
	}
}

func TestConvert_OneFXCallPerCurrency(t *testing.T) {
	fx := newFakeFX(mockRates)
	var flights []domain.Flight
	for i, cur := range []string{"USD", "SGD", "USD", "IDR", "SGD", "USD", "JPY", "XYZ"} {
		flights = append(flights, fareFlight(strconv.Itoa(i), "100", cur))
	}
	kept, rejected := converter{exponents: exponents, fx: fx}.convert(context.Background(), flights)
	if len(kept) != 7 || rejected != 1 {
		t.Errorf("kept %d, rejected %d; want 7, 1", len(kept), rejected)
	}
	if want := map[string]int{"USD": 1, "SGD": 1, "JPY": 1}; !maps.Equal(fx.calls, want) {
		t.Errorf("FX calls = %v, want %v", fx.calls, want)
	}
}

func TestConvert_NoFlights(t *testing.T) {
	fx := newFakeFX(mockRates)
	kept, rejected := converter{exponents: exponents, fx: fx}.convert(context.Background(), nil)
	if len(kept) != 0 || rejected != 0 || len(fx.calls) != 0 {
		t.Errorf("kept %d, rejected %d, calls %v; want nothing", len(kept), rejected, fx.calls)
	}
}

func TestConvert_DoesNotModifyInput(t *testing.T) {
	flights := []domain.Flight{fareFlight("A", "65.50", "USD")}
	converter{exponents: exponents, fx: newFakeFX(mockRates)}.convert(context.Background(), flights)
	if flights[0].Price != 0 || flights[0].Original != nil {
		t.Errorf("input changed: %+v", flights[0])
	}
}

func TestParseMinor(t *testing.T) {
	tests := []struct {
		s       string
		exp     int
		want    int64
		wantErr bool
	}{
		{"485000", 0, 485000, false},
		{"485000.00", 0, 485000, false},
		{"65.50", 2, 6550, false},
		{"65.5", 2, 6550, false},
		{"65", 2, 6500, false},
		{"0.01", 2, 1, false},
		{"0", 2, 0, false},
		{"007.10", 2, 710, false},
		{"65.505", 2, 0, true},
		{"485000.5", 0, 0, true},
		{"", 2, 0, true},
		{"-5", 2, 0, true},
		{"+5", 2, 0, true},
		{"abc", 2, 0, true},
		{"1e5", 2, 0, true},
		{"5.", 2, 0, true},
		{".5", 2, 0, true},
		{"1.2.3", 2, 0, true},
		{"1,000", 0, 0, true},
		{" 5", 0, 0, true},
		{"9223372036854775807", 0, math.MaxInt64, false},
		{"9223372036854775808", 0, 0, true},
		{"92233720368547758.07", 2, math.MaxInt64, false},
		{"92233720368547758.08", 2, 0, true},
		{"92233720368547758", 3, 0, true}, // overflows while padding
	}
	for _, tt := range tests {
		t.Run(tt.s+"/"+strconv.Itoa(tt.exp), func(t *testing.T) {
			got, err := parseMinor(tt.s, tt.exp)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("parseMinor(%q, %d) = %d, %v; want %d, err %v", tt.s, tt.exp, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestToIDR_Overflow(t *testing.T) {
	if _, err := toIDR(math.MaxInt64/16250+1, 2, 16250); !errors.Is(err, errAmountRange) {
		t.Errorf("toIDR overflow error = %v, want errAmountRange", err)
	}
	if got, err := toIDR(math.MaxInt64/108, 0, 108); err != nil || int64(got) != math.MaxInt64/108*108 {
		t.Errorf("toIDR near max = %d, %v", got, err)
	}
}

func flightIDs(flights []domain.Flight) []string {
	ids := make([]string, 0, len(flights))
	for _, f := range flights {
		ids = append(ids, f.ID)
	}
	return ids
}
