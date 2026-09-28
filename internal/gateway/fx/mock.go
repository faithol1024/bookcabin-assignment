package fx

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/faithol1024/bookcabin-assignment/pkg/simulate"
)

// Latency and failures of a real rates API are simulated here; client.go holds the rate behaviour.
const (
	minLatency  = 30 * time.Millisecond
	maxLatency  = 80 * time.Millisecond
	failureRate = 0.0
	mockSeed    = 42
	mockBaseURL = "http://fx.mock"
)

var profile = simulate.Profile{LatencyMin: minLatency, LatencyMax: maxLatency, FailureRate: failureRate}

// rates are whole rupiah per 1 unit.
var rates = map[string]int64{
	"USD": 16250,
	"SGD": 12100,
	"MYR": 3500,
	"EUR": 17600,
	"JPY": 108,
}

// NewMock returns a client whose requests the mock rates API serves in-process.
func NewMock() *Client {
	return mockClient(simulate.NewSeeded(profile, mockSeed))
}

func mockClient(sim *simulate.Simulator) *Client {
	return New(&http.Client{Transport: simulate.NewTransport(sim, mockHandler())}, mockBaseURL)
}

// mockHandler stands in for the rates API server: 404 for a currency it does not quote.
func mockHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+ratesPath, func(w http.ResponseWriter, r *http.Request) {
		currency := r.URL.Query().Get("currency")
		if currency == "" {
			http.Error(w, "currency is required", http.StatusBadRequest)
			return
		}
		rate, ok := rates[currency]
		if !ok {
			http.Error(w, "unknown currency", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rateResponse{Currency: currency, Rate: rate})
	})
	return mux
}
