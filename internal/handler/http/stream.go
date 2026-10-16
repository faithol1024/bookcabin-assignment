package httphandler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

type flightsEvent struct {
	Leg      int              `json:"leg"`
	Provider string           `json:"provider"`
	Flights  []flightResponse `json:"flights"`
}

type legEvent struct {
	Leg         int              `json:"leg"`
	Origin      string           `json:"origin"`
	Destination string           `json:"destination"`
	Date        string           `json:"date"`
	Metadata    metadataResponse `json:"metadata"`
}

// SearchStream sends each provider's flights as a server-sent event the moment they arrive, then a leg
// event per leg and a closing done, or error when every provider failed. Invalid requests get the same
// problem responses as Search; once streaming, the status is 200.
func (h *Handler) SearchStream(w http.ResponseWriter, r *http.Request) {
	c, ok := h.criteria(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // keep reverse proxies from buffering the stream
	w.WriteHeader(http.StatusOK)

	sse := &sseWriter{w: w, rc: http.NewResponseController(w), r: r, criteria: c}
	if err := h.search.Stream(r.Context(), c, sse.event); err != nil {
		p := problemFor(r, err)
		p.RequestID = RequestID(r.Context())
		sse.send("error", p)
		return
	}
	sse.send("done", struct{}{})
}

// sseWriter turns search events into server-sent events, flushing each one.
type sseWriter struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	r        *http.Request
	criteria search.Criteria
}

func (s *sseWriter) event(e search.Event) {
	if e.Summary != nil {
		logProviderErrors(s.r.Context(), *e.Summary)
		l := newLeg(*e.Summary, s.criteria)
		s.send("leg", legEvent{Leg: e.Leg, Origin: l.Origin, Destination: l.Destination, Date: l.Date, Metadata: l.Metadata})
		return
	}
	flights := make([]flightResponse, len(e.Flights))
	for i, f := range e.Flights {
		flights[i] = newFlight(f)
	}
	s.send("flights", flightsEvent{Leg: e.Leg, Provider: e.Provider, Flights: flights})
}

func (s *sseWriter) send(event string, v any) {
	b, _ := json.Marshal(v) // response types always marshal
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b)
	_ = s.rc.Flush()
}
