package simulate

import (
	"errors"
	"net/http"
	"net/http/httptest"
)

type transport struct {
	sim     *Simulator
	handler http.Handler
}

// NewTransport serves requests with h in-process after the simulated latency; a simulated failure answers 503.
func NewTransport(sim *Simulator, h http.Handler) http.RoundTripper {
	return transport{sim: sim, handler: h}
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.sim.Call(req.Context()); err != nil {
		if !errors.Is(err, ErrFailure) {
			return nil, err
		}
		rec := httptest.NewRecorder()
		http.Error(rec, ErrFailure.Error(), http.StatusServiceUnavailable)
		return response(rec, req), nil
	}
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	return response(rec, req), nil
}

func response(rec *httptest.ResponseRecorder, req *http.Request) *http.Response {
	resp := rec.Result()
	resp.Request = req
	return resp
}
