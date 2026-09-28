package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"

	httphandler "github.com/faithol1024/bookcabin-assignment/internal/handler/http"
	"github.com/faithol1024/bookcabin-assignment/pkg/httpserver"
	"github.com/faithol1024/bookcabin-assignment/pkg/secrets"
)

const sampleRequest = `{
  "origin": "CGK",
  "destination": "DPS",
  "departureDate": "2025-12-15",
  "returnDate": null,
  "passengers": 1,
  "cabinClass": "economy"
}`

// testServer is the service as main builds it from the committed development config, over miniredis.
func testServer(t *testing.T) (http.Handler, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("CONFIG_FILE", "../../files/etc/bookcabin-assignment/development/config.yaml")
	t.Setenv("SECRET_REDIS_ADDR", mr.Addr())
	cfg, err := loadConfig(context.Background(), secrets.EnvSource{})
	if err != nil {
		t.Fatal(err)
	}
	redis, err := connectRedis(context.Background(), cfg.Redis)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redis.Close() })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := newHandler(cfg, redis)
	return httpserver.New(cfg.HTTPServer, newRouter(h, logger), maxBodyBytes, logger).Handler, mr
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	if strings.Contains(rec.Header().Get("Content-Type"), "json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
	}
	return rec, out
}

func readExpected(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../docs/assignment/expected_result.json")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// sameShape fails for every key of want missing from got; got may add keys.
func sameShape(t *testing.T, path string, want, got any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s = %#v, want an object", path, got)
			return
		}
		for k, v := range w {
			if _, ok := g[k]; !ok {
				t.Errorf("%s.%s missing", path, k)
				continue
			}
			sameShape(t, path+"."+k, v, g[k])
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			t.Errorf("%s = %#v, want an array", path, got)
			return
		}
		if len(w) > 0 {
			for i := range g {
				sameShape(t, path, w[0], g[i])
			}
		}
	}
}

// sameValues compares every value want has, except the timestamps the given file gets wrong.
func sameValues(t *testing.T, path string, want, got any) {
	t.Helper()
	w, ok := want.(map[string]any)
	if !ok {
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s = %#v, want %#v", path, got, want)
		}
		return
	}
	g, ok := got.(map[string]any)
	if !ok {
		t.Errorf("%s = %#v, want an object", path, got)
		return
	}
	for k, v := range w {
		if k == "timestamp" {
			continue
		}
		sameValues(t, path+"."+k, v, g[k])
	}
}

func findFlight(flights []any, id string) map[string]any {
	for _, f := range flights {
		if m := f.(map[string]any); m["id"] == id {
			return m
		}
	}
	return nil
}

func TestSearch_SampleRequestMatchesExpectedShape(t *testing.T) {
	srv, _ := testServer(t)
	rec, got := do(t, srv, http.MethodPost, "/v1/flights/search", sampleRequest)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get(httphandler.RequestIDHeader) == "" {
		t.Error("no request ID header")
	}
	want := readExpected(t)
	sameShape(t, "$", want, got)

	flights := got["flights"].([]any)
	if meta := got["metadata"].(map[string]any); meta["providers_succeeded"] != 4.0 || meta["total_results"] != float64(len(flights)) {
		t.Errorf("metadata = %v, want 4 providers and every flight on the page", meta)
	}
	wantQZ := want["flights"].([]any)[0].(map[string]any)
	gotQZ := findFlight(flights, "QZ7250_AirAsia")
	if gotQZ == nil {
		t.Fatalf("QZ7250_AirAsia not among %d flights", len(flights))
	}
	sameValues(t, "QZ7250_AirAsia", wantQZ, gotQZ)
	// the given file's timestamps are a year off; these are the instants its datetimes name
	for side, ts := range map[string]float64{"departure": 1765786500, "arrival": 1765802100} {
		if got := gotQZ[side].(map[string]any)["timestamp"]; got != ts {
			t.Errorf("%s.timestamp = %v, want %v", side, got, ts)
		}
	}
	if gotQZ["aircraft"] != nil || !reflect.DeepEqual(gotQZ["amenities"], []any{}) {
		t.Errorf("aircraft %#v, amenities %#v; want null and []", gotQZ["aircraft"], gotQZ["amenities"])
	}
}

func TestSearch_RoundTripAndMultiCityShapes(t *testing.T) {
	srv, _ := testServer(t)
	tests := []struct {
		name string
		body string
		keys []string
	}{
		{"round trip", strings.Replace(sampleRequest, `"returnDate": null`, `"returnDate": "2025-12-20"`, 1), []string{"inbound", "outbound", "search_criteria"}},
		{"multi-city", `{"legs":[{"origin":"CGK","destination":"DPS","date":"2025-12-15"},{"origin":"DPS","destination":"SUB","date":"2025-12-18"}],
			"passengers":1,"cabinClass":"economy"}`, []string{"legs", "search_criteria"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, got := do(t, srv, http.MethodPost, "/v1/flights/search", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if keys := slices.Sorted(maps.Keys(got)); !reflect.DeepEqual(keys, tt.keys) {
				t.Errorf("keys = %v, want %v", keys, tt.keys)
			}
		})
	}
}

func TestSearch_RedisDownStillAnswers(t *testing.T) {
	srv, mr := testServer(t)
	mr.Close()

	rec, got := do(t, srv, http.MethodPost, "/v1/flights/search", sampleRequest)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d with Redis down: %s", rec.Code, rec.Body)
	}
	if n := len(got["flights"].([]any)); n == 0 {
		t.Error("no flights with Redis down")
	}
}

func TestSearch_BodyTooLarge(t *testing.T) {
	srv, _ := testServer(t)
	body := `{"origin":"` + strings.Repeat("A", maxBodyBytes) + `"}`
	rec, got := do(t, srv, http.MethodPost, "/v1/flights/search", body)
	if rec.Code != http.StatusRequestEntityTooLarge || got["type"] != "payload_too_large" ||
		rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("status %d, %s: %v; want 413 payload_too_large problem+json", rec.Code, rec.Header().Get("Content-Type"), got)
	}
}

func TestRouter_Endpoints(t *testing.T) {
	srv, _ := testServer(t)
	tests := []struct {
		method, path string
		code         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodGet, "/readyz", http.StatusNotFound}, // removed: /healthz covers liveness
		{http.MethodGet, "/docs", http.StatusOK},
		{http.MethodGet, "/openapi.yaml", http.StatusOK},
		{http.MethodGet, "/v1/flights/search", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/flights/search", http.StatusBadRequest}, // empty body
		{http.MethodGet, "/nope", http.StatusNotFound},
	}
	for _, tt := range tests {
		rec, _ := do(t, srv, tt.method, tt.path, "")
		if rec.Code != tt.code {
			t.Errorf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.code)
		}
	}
}
