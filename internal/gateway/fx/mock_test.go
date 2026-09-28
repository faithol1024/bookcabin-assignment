package fx

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/pkg/simulate"
)

func newMock(p simulate.Profile, rng *rand.Rand) *Client {
	return mockClient(simulate.New(p, rng))
}

func instant() *Client {
	return newMock(simulate.Profile{}, rand.New(rand.NewPCG(1, 1)))
}

func TestRate_Table(t *testing.T) {
	want := map[string]int64{"USD": 16250, "SGD": 12100, "MYR": 3500, "EUR": 17600, "JPY": 108}
	for currency, rate := range want {
		got, err := instant().Rate(context.Background(), currency)
		if err != nil || got != rate {
			t.Errorf("Rate(%s) = %d, %v; want %d", currency, got, err, rate)
		}
	}
}

func TestRate_UnknownCurrency(t *testing.T) {
	for _, currency := range []string{"XYZ", "IDR", "usd"} {
		if _, err := instant().Rate(context.Background(), currency); !errors.Is(err, apperr.ErrMalformedResponse) {
			t.Errorf("Rate(%s) error = %v, want ErrMalformedResponse", currency, err)
		}
	}
}

func TestMockHandler_RequiresCurrency(t *testing.T) {
	rec := httptest.NewRecorder()
	mockHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rates", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("GET /rates = %d, want 400", rec.Code)
	}
}

func TestRate_HonoursCancelledContext(t *testing.T) {
	c := newMock(simulate.Profile{LatencyMin: time.Hour, LatencyMax: time.Hour}, rand.New(rand.NewPCG(1, 1)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Rate(ctx, "USD"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rate() error = %v, want context.Canceled", err)
	}
}

func TestRate_SimulatedFailureIsUnavailable(t *testing.T) {
	c := newMock(simulate.Profile{FailureRate: 1}, rand.New(rand.NewPCG(1, 1)))
	if _, err := c.Rate(context.Background(), "USD"); !errors.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("Rate() error = %v, want ErrUnavailable", err)
	}
}

func TestNewMock_UsesMockLatency(t *testing.T) {
	start := time.Now()
	if _, err := NewMock().Rate(context.Background(), "USD"); err != nil {
		t.Fatal(err)
	}
	if got := time.Since(start); got < minLatency {
		t.Errorf("Rate() took %v, want at least %v", got, minLatency)
	}
}
