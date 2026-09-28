package fx

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/pkg/simulate"
)

// stub serves the client's requests with h instead of the rates API.
func stub(h http.HandlerFunc) *Client {
	return New(&http.Client{Transport: simulate.NewTransport(simulate.New(simulate.Profile{}, rand.New(rand.NewPCG(1, 1))), h)}, "http://fx.test")
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestRate_SendsQuery(t *testing.T) {
	var got *http.Request
	c := stub(func(w http.ResponseWriter, r *http.Request) {
		got = r
		reply(http.StatusOK, `{"currency": "USD", "rate": 16250}`)(w, r)
	})
	rate, err := c.Rate(context.Background(), "USD")
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodGet || got.URL.Host != "fx.test" || got.URL.Path != "/rates" || got.URL.Query().Get("currency") != "USD" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
	if rate != 16250 {
		t.Errorf("Rate() = %d, want 16250", rate)
	}
}

func TestRate_UpstreamErrors(t *testing.T) {
	tests := []struct {
		name string
		h    http.HandlerFunc
		want error
	}{
		{"503", reply(http.StatusServiceUnavailable, ""), apperr.ErrUnavailable},
		{"500", reply(http.StatusInternalServerError, ""), apperr.ErrUnavailable},
		{"404", reply(http.StatusNotFound, ""), apperr.ErrMalformedResponse},
		{"400", reply(http.StatusBadRequest, ""), apperr.ErrMalformedResponse},
		{"garbage body", reply(http.StatusOK, `{"rate": `), apperr.ErrMalformedResponse},
		{"fractional rate", reply(http.StatusOK, `{"currency": "USD", "rate": 16250.5}`), apperr.ErrMalformedResponse},
		{"zero rate", reply(http.StatusOK, `{"currency": "USD", "rate": 0}`), apperr.ErrMalformedResponse},
		{"negative rate", reply(http.StatusOK, `{"currency": "USD", "rate": -1}`), apperr.ErrMalformedResponse},
		{"other currency", reply(http.StatusOK, `{"currency": "SGD", "rate": 12100}`), apperr.ErrMalformedResponse},
		{"null body", reply(http.StatusOK, `null`), apperr.ErrMalformedResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := stub(tt.h).Rate(context.Background(), "USD"); !errors.Is(err, tt.want) {
				t.Errorf("Rate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestRate_TransportErrorIsUnavailable(t *testing.T) {
	c := New(&http.Client{Transport: failingTransport{}}, "http://fx.test")
	if _, err := c.Rate(context.Background(), "USD"); !errors.Is(err, apperr.ErrUnavailable) {
		t.Errorf("Rate() error = %v, want ErrUnavailable", err)
	}
}
