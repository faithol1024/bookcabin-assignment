package simulate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func get(ctx context.Context, t *testing.T, rt http.RoundTripper) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://upstream.mock/items?id=7", nil)
	if err != nil {
		t.Fatal(err)
	}
	return (&http.Client{Transport: rt}).Do(req)
}

var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"path":"`+r.URL.Path+`","id":"`+r.URL.Query().Get("id")+`"}`)
})

func TestTransport_ServesHandler(t *testing.T) {
	resp, err := get(context.Background(), t, NewTransport(NewSeeded(Profile{}, 1), echo))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != `{"path":"/items","id":"7"}` {
		t.Errorf("got %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Type") != "application/json" || resp.Request == nil {
		t.Errorf("header %q, request %v", resp.Header.Get("Content-Type"), resp.Request)
	}
}

func TestTransport_FailureIs503(t *testing.T) {
	called := false
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	resp, err := get(context.Background(), t, NewTransport(NewSeeded(Profile{FailureRate: 1}, 1), h))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || called {
		t.Errorf("status %d, handler called %v; want 503 without calling the handler", resp.StatusCode, called)
	}
}

func TestTransport_HonoursCancelledContext(t *testing.T) {
	rt := NewTransport(NewSeeded(Profile{LatencyMin: time.Hour, LatencyMax: time.Hour}, 1), echo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	resp, err := get(ctx, t, rt)
	if err == nil {
		resp.Body.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := time.Since(start); got > time.Second {
		t.Errorf("took %v after cancel", got)
	}
}

func TestTransport_Latency(t *testing.T) {
	p := Profile{LatencyMin: 5 * time.Millisecond, LatencyMax: 10 * time.Millisecond}
	start := time.Now()
	resp, err := get(context.Background(), t, NewTransport(NewSeeded(p, 1), echo))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := time.Since(start); got < p.LatencyMin {
		t.Errorf("took %v, want at least %v", got, p.LatencyMin)
	}
}
