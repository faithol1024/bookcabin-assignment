package lionair

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/faithol1024/bookcabin-assignment/pkg/simulate"
)

//go:embed mockdata/*.json
var mockdata embed.FS

// Latency and failures of the real Lion Air API are simulated here; client.go holds the provider behaviour.
const (
	minLatency  = 100 * time.Millisecond
	maxLatency  = 200 * time.Millisecond
	failureRate = 0.0
	mockSeed    = 42
	mockBaseURL = "http://lionair.mock"
)

var profile = simulate.Profile{LatencyMin: minLatency, LatencyMax: maxLatency, FailureRate: failureRate}

// NewMock returns a client whose requests the mock Lion Air API serves in-process.
func NewMock() *Client {
	return New(mockDeps(simulate.NewSeeded(profile, mockSeed)))
}

// MockDeps points a client at the mock API; callers add the guard and cache.
func MockDeps() Deps {
	return mockDeps(simulate.NewSeeded(profile, mockSeed))
}

func mockDeps(sim *simulate.Simulator) Deps {
	return Deps{HTTP: &http.Client{Transport: simulate.NewTransport(sim, mockHandler())}, BaseURL: mockBaseURL}
}

// mockHandler stands in for the Lion Air API server: every mockdata file is one upstream dataset.
func mockHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+searchPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		origin, destination, date := q.Get("origin"), q.Get("destination"), q.Get("date")
		if origin == "" || destination == "" || date == "" {
			http.Error(w, "origin, destination and date are required", http.StatusBadRequest)
			return
		}
		datasets, err := mockResponses()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := Response{Success: true}
		out.Data.AvailableFlights = []Flight{}
		for _, d := range datasets {
			for _, f := range d.Data.AvailableFlights {
				// naive local time, so its prefix is the local date
				if f.Route.From.Code == origin && f.Route.To.Code == destination && strings.HasPrefix(f.Schedule.Departure, date+"T") {
					out.Data.AvailableFlights = append(out.Data.AvailableFlights, f)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}

// mockResponses parses the embedded datasets once; requests only read them.
var mockResponses = sync.OnceValues(func() ([]Response, error) {
	names, err := fs.Glob(mockdata, "mockdata/*.json")
	if err != nil {
		return nil, err
	}
	out := make([]Response, 0, len(names))
	for _, name := range names {
		b, err := mockdata.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var r Response
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, r)
	}
	return out, nil
})
