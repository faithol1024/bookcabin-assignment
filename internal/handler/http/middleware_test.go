package httphandler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

func quiet() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var p problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem %s: %v", rec.Body, err)
	}
	if p.Status != rec.Code {
		t.Errorf("problem status %d, response status %d", p.Status, rec.Code)
	}
	return p
}

func TestMiddleware_KeepsClientRequestID(t *testing.T) {
	var seen string
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = RequestID(r.Context()) }), quiet())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "abc-123" || rec.Header().Get(RequestIDHeader) != "abc-123" {
		t.Errorf("context ID %q, header %q; want abc-123", seen, rec.Header().Get(RequestIDHeader))
	}
}

func TestMiddleware_GeneratesRequestID(t *testing.T) {
	for _, given := range []string{"", "has space", "line\nbreak", strings.Repeat("a", 129)} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(RequestIDHeader, given)
		Middleware(http.NotFoundHandler(), quiet()).ServeHTTP(rec, req)
		if id := rec.Header().Get(RequestIDHeader); len(id) != 32 {
			t.Errorf("given %q: ID %q, want a generated 32-char hex ID", given, id)
		}
	}
}

func TestMiddleware_AccessLogCarriesRequestID(t *testing.T) {
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }),
		slog.New(slog.NewJSONHandler(&buf, nil)))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(RequestIDHeader, "rid-1")
	h.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line %q: %v", buf.String(), err)
	}
	if line["request_id"] != "rid-1" || line["status"] != float64(http.StatusTeapot) || line["path"] != "/x" {
		t.Errorf("access log = %v", line)
	}
}

func TestMiddleware_PanicIsInternalError(t *testing.T) {
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret detail") }),
		slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/flights/search", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Type != "internal_error" || p.RequestID == "" || strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("problem = %+v", p)
	}
	if !strings.Contains(buf.String(), "secret detail") || !strings.Contains(buf.String(), p.RequestID) {
		t.Errorf("panic not logged with its request ID: %s", buf.String())
	}
}

func TestMiddleware_AbortHandlerPropagates(t *testing.T) {
	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }), quiet())
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestWriteError_Statuses(t *testing.T) {
	invalid := &ValidationError{}
	invalid.add("origin", "must be a 3-letter IATA airport code")
	tests := []struct {
		name     string
		err      error
		status   int
		typ      string
		noDetail string // must not leak into the body
	}{
		{"validation", invalid, http.StatusBadRequest, "validation_error", ""},
		{"service guard", fmt.Errorf("limit 0: %w", apperr.ErrInvalidInput), http.StatusBadRequest, "validation_error", "limit 0"},
		{"body too large", fmt.Errorf("decode: %w", &http.MaxBytesError{Limit: 10}), http.StatusRequestEntityTooLarge, "payload_too_large", "decode"},
		{"all providers failed", fmt.Errorf("search 2 legs: %w", apperr.ErrAllProvidersFailed), http.StatusBadGateway, "all_providers_failed", "2 legs"},
		{"unexpected", errors.New("db password wrong"), http.StatusInternalServerError, "internal_error", "password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, httptest.NewRequest(http.MethodGet, "/", nil), tt.err)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if p := decodeProblem(t, rec); p.Type != tt.typ || p.Title == "" || p.Detail == "" {
				t.Errorf("problem = %+v", p)
			}
			if tt.noDetail != "" && strings.Contains(rec.Body.String(), tt.noDetail) {
				t.Errorf("body leaks %q: %s", tt.noDetail, rec.Body)
			}
		})
	}
}

func TestWriteError_ValidationListsFields(t *testing.T) {
	invalid := &ValidationError{}
	invalid.add("origin", "a")
	invalid.add("passengers", "b")
	rec := httptest.NewRecorder()
	writeError(rec, httptest.NewRequest(http.MethodGet, "/", nil), invalid)

	want := []FieldError{{"origin", "a"}, {"passengers", "b"}}
	if p := decodeProblem(t, rec); fmt.Sprint(p.Errors) != fmt.Sprint(want) {
		t.Errorf("errors = %v, want %v", p.Errors, want)
	}
}

// logLines decodes every JSON log line in buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.Lines(buf.String()) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestLogHandler_AddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	// wrapping twice must not duplicate the attribute
	logger := slog.New(LogHandler(LogHandler(slog.NewJSONHandler(&buf, nil))))
	logger.InfoContext(context.WithValue(context.Background(), requestIDKey, "rid-1"), "with")
	logger.With("k", "v").InfoContext(context.WithValue(context.Background(), requestIDKey, "rid-2"), "derived")
	logger.InfoContext(context.Background(), "without")

	lines := logLines(t, &buf)
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3: %s", len(lines), buf.String())
	}
	if lines[0]["request_id"] != "rid-1" || strings.Count(buf.String(), "rid-1") != 1 {
		t.Errorf("line with ID = %s", buf.String())
	}
	if lines[1]["request_id"] != "rid-2" || lines[1]["k"] != "v" {
		t.Errorf("derived logger line = %v", lines[1])
	}
	if _, ok := lines[2]["request_id"]; ok {
		t.Errorf("line without ID = %v", lines[2])
	}
}

// ctxLogSearcher logs through the default logger with the request context, as the usecase does.
type ctxLogSearcher struct{ fakeSearcher }

func (s *ctxLogSearcher) Search(ctx context.Context, c search.Criteria) (search.Response, error) {
	slog.WarnContext(ctx, "fx rate lookup failed", "currency", "USD")
	return s.fakeSearcher.Search(ctx, c)
}

func TestSearch_LogsCarryRequestID(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(LogHandler(slog.NewJSONHandler(&buf, nil))))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := Middleware(http.HandlerFunc(newTestHandler(&ctxLogSearcher{fakeSearcher{legs: oneFlightLeg}}).Search), quiet())
	req := httptest.NewRequest(http.MethodPost, "/v1/flights/search", strings.NewReader(oneWayBody))
	req.Header.Set(RequestIDHeader, "rid-7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	seen := map[string]bool{}
	for _, l := range logLines(t, &buf) {
		if l["request_id"] != "rid-7" {
			t.Errorf("log line without the request ID: %v", l)
		}
		seen[l["msg"].(string)] = true
	}
	if !seen["fx rate lookup failed"] || !seen["provider failed"] {
		t.Errorf("logged %v, want the usecase and handler lines", seen)
	}
}

func TestMiddleware_PanicAfterWriteKeepsResponse(t *testing.T) {
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("late failure")
	}), slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "partial" {
		t.Errorf("status %d, body %q; want the partial 200 response untouched", rec.Code, rec.Body)
	}
	if !strings.Contains(buf.String(), "late failure") {
		t.Errorf("panic not logged: %s", buf.String())
	}
}
