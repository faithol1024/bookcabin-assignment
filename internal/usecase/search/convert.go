package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

const idr = "IDR"

// converter turns each flight's Fare into an IDR Price; exponents doubles as the supported-currency list.
type converter struct {
	exponents map[string]int
	fx        FXGateway
}

type quote struct {
	rate int64
	err  error
}

// convert sets Price (and Original for foreign fares) and drops the flights it cannot convert,
// calling FX once per distinct foreign currency.
func (c converter) convert(ctx context.Context, flights []domain.Flight) ([]domain.Flight, int) {
	quotes := c.quotes(ctx, flights)
	kept := make([]domain.Flight, 0, len(flights))
	rejected := 0
	for _, f := range flights {
		if err := c.price(&f, quotes); err != nil {
			rejected++
			slog.DebugContext(ctx, "flight rejected", "id", f.ID, "fare", f.Fare.Amount, "currency", f.Fare.Currency, "error", err)
			continue
		}
		kept = append(kept, f)
	}
	return kept, rejected
}

// quotes fetches the rate of every supported foreign currency in parallel.
func (c converter) quotes(ctx context.Context, flights []domain.Flight) map[string]quote {
	var currencies []string
	for _, f := range flights {
		cur := f.Fare.Currency
		if _, ok := c.exponents[cur]; ok && cur != idr && !slices.Contains(currencies, cur) {
			currencies = append(currencies, cur)
		}
	}
	quotes := make(map[string]quote, len(currencies))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, cur := range currencies {
		wg.Go(func() {
			rate, err := c.fx.Rate(ctx, cur)
			if err != nil {
				slog.WarnContext(ctx, "fx rate lookup failed", "currency", cur, "error", err)
			}
			mu.Lock()
			quotes[cur] = quote{rate: rate, err: err}
			mu.Unlock()
		})
	}
	wg.Wait()
	return quotes
}

func (c converter) price(f *domain.Flight, quotes map[string]quote) error {
	cur := f.Fare.Currency
	exp, ok := c.exponents[cur]
	if !ok {
		return fmt.Errorf("unsupported currency %q", cur)
	}
	minor, err := parseMinor(f.Fare.Amount, exp)
	if err != nil {
		return err
	}
	if cur == idr {
		f.Price = domain.Money(minor)
		return nil
	}
	q := quotes[cur]
	if q.err != nil {
		return q.err
	}
	amount, err := toIDR(minor, exp, q.rate)
	if err != nil {
		return err
	}
	f.Price = amount
	f.Original = &domain.ForeignPrice{Amount: minor, Currency: cur, Exponent: exp, Rate: q.rate}
	return nil
}

var errAmountRange = errors.New("amount out of range")

// parseMinor reads a plain decimal ("65.50", "485000") as minor units of a currency with exp fraction digits;
// digits beyond exp must be zeros, so a fare never loses precision.
func parseMinor(s string, exp int) (int64, error) {
	var minor int64
	frac := -1 // fraction digits read; -1 before the dot
	for i, c := range s {
		switch {
		case c >= '0' && c <= '9':
			if frac >= exp {
				if c != '0' {
					return 0, fmt.Errorf("amount %q has more than %d fraction digits", s, exp)
				}
				continue
			}
			if minor > (math.MaxInt64-int64(c-'0'))/10 {
				return 0, fmt.Errorf("%w: %q", errAmountRange, s)
			}
			minor = minor*10 + int64(c-'0')
			if frac >= 0 {
				frac++
			}
		case c == '.' && frac < 0 && i > 0 && i < len(s)-1:
			frac = 0
		default:
			return 0, fmt.Errorf("invalid amount %q", s)
		}
	}
	if s == "" {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	for range exp - max(frac, 0) {
		if minor > math.MaxInt64/10 {
			return 0, fmt.Errorf("%w: %q", errAmountRange, s)
		}
		minor *= 10
	}
	return minor, nil
}

// toIDR computes minor × rate / 10^exp, rounding half up, in integers only.
func toIDR(minor int64, exp int, rate int64) (domain.Money, error) {
	scale := int64(1)
	for range exp {
		scale *= 10
	}
	if rate > 0 && minor > (math.MaxInt64-scale/2)/rate {
		return 0, fmt.Errorf("%w: %d × %d", errAmountRange, minor, rate)
	}
	return domain.Money((minor*rate + scale/2) / scale), nil
}
