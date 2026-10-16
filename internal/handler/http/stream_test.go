package httphandler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
)

type sseEvent struct {
	name string
	data map[string]any
}

func postStream(t *testing.T, h *Handler, body string) (*httptest.ResponseRecorder, []sseEvent) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.SearchStream(rec, httptest.NewRequest(http.MethodPost, "/v1/flights/search/stream", strings.NewReader(body)))
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		return rec, nil
	}
	var events []sseEvent
	for block := range strings.SplitSeq(strings.TrimSuffix(rec.Body.String(), "\n\n"), "\n\n") {
		name, data, ok := strings.Cut(block, "\n")
		if !ok || !strings.HasPrefix(name, "event: ") || !strings.HasPrefix(data, "data: ") {
			t.Fatalf("malformed event %q", block)
		}
		e := sseEvent{name: strings.TrimPrefix(name, "event: ")}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &e.data); err != nil {
			t.Fatalf("decode %q: %v", data, err)
		}
		events = append(events, e)
	}
	return rec, events
}

func names(events []sseEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.name
	}
	return out
}

func TestSearch_StreamsEvents(t *testing.T) {
	rec, events := postStream(t, newTestHandler(&fakeSearcher{legs: oneFlightLeg}), withFields(`"returnDate":"2025-12-18"`))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	if want := []string{"flights", "leg", "flights", "leg", "done"}; !reflect.DeepEqual(names(events), want) {
		t.Fatalf("events = %v, want %v", names(events), want)
	}
	batch := events[2].data
	if batch["leg"] != 1.0 || batch["provider"] != "Garuda Indonesia" {
		t.Errorf("second batch = leg %v, provider %v; want 1, Garuda Indonesia", batch["leg"], batch["provider"])
	}
	if f := batch["flights"].([]any)[0].(map[string]any); f["id"] != "GA400_GarudaIndonesia" || f["departure"].(map[string]any)["airport"] != "DPS" {
		t.Errorf("flight = %v", f)
	}
	leg := events[3].data
	if want := []string{"date", "destination", "leg", "metadata", "origin"}; !reflect.DeepEqual(keys(leg), want) {
		t.Errorf("leg keys = %v, want %v", keys(leg), want)
	}
	if meta := leg["metadata"].(map[string]any); meta["providers_failed"] != 1.0 || len(meta["provider_errors"].([]any)) != 1 {
		t.Errorf("metadata = %v", meta)
	}
}

func TestSearch_StreamReportsAllProvidersFailed(t *testing.T) {
	h := newTestHandler(&fakeSearcher{err: fmt.Errorf("stream 1 legs: %w", apperr.ErrAllProvidersFailed)})
	rec, events := postStream(t, h, oneWayBody)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(names(events), []string{"error"}) {
		t.Fatalf("status %d, events %v; want 200 and one error event", rec.Code, names(events))
	}
	if p := events[0].data; p["type"] != "all_providers_failed" || p["status"] != 502.0 {
		t.Errorf("error = %v", p)
	}
}

func TestSearch_StreamInvalidRequestIsPlainProblem(t *testing.T) {
	rec, _ := postStream(t, newTestHandler(&fakeSearcher{legs: oneFlightLeg}), `{}`)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("status %d, content type %q; want 400 problem JSON", rec.Code, rec.Header().Get("Content-Type"))
	}
}
