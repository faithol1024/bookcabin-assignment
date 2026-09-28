package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var discard = slog.New(slog.DiscardHandler)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// freeAddr returns a loopback address with a port that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln := listen(t)
	defer ln.Close()
	return ln.Addr().String()
}

// startServe gives srv a free address, runs Run in the background, waits
// until it accepts connections, and returns a channel with Run's result.
func startServe(ctx context.Context, t *testing.T, srv *http.Server, timeout time.Duration) <-chan error {
	t.Helper()
	srv.Addr = freeAddr(t)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, srv, timeout, discard) }()
	waitReady(t, srv.Addr)
	return done
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server at %s never became ready", addr)
}

func waitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func ok(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }

func TestNew_UsesConfig(t *testing.T) {
	cfg := Config{
		Addr:              ":9999",
		ReadHeaderTimeout: 1 * time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
	}
	srv := New(cfg, http.HandlerFunc(ok), 1<<10, discard)

	if srv.Addr != ":9999" {
		t.Errorf("Addr = %q", srv.Addr)
	}
	if srv.ReadHeaderTimeout != time.Second || srv.ReadTimeout != 2*time.Second || srv.WriteTimeout != 3*time.Second {
		t.Errorf("timeouts = %v/%v/%v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout)
	}
	if srv.ErrorLog == nil {
		t.Error("ErrorLog is nil")
	}
}

func TestNew_LimitsBodySize(t *testing.T) {
	var readErr error
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, readErr = io.ReadAll(r.Body) })
	srv := New(Config{}, h, 8, discard)

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456789")))
	var tooBig *http.MaxBytesError
	if !errors.As(readErr, &tooBig) {
		t.Errorf("reading a 9-byte body with an 8-byte cap: err = %v, want *http.MaxBytesError", readErr)
	}
}

func TestRun_ServesUntilCancelledThenStopsCleanly(t *testing.T) {
	srv := New(Config{}, http.HandlerFunc(ok), 1<<10, discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := startServe(ctx, t, srv, time.Second)

	resp, err := http.Get("http://" + srv.Addr)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if _, err := net.Dial("tcp", srv.Addr); err == nil {
		t.Error("server still accepting connections after shutdown")
	}
}

func TestRun_WaitsForInFlightRequest(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := startServe(ctx, t, srv, 5*time.Second)

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr)
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()

	<-started
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run returned %v before the in-flight request finished", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if got := <-body; got != "done" {
		t.Errorf("in-flight response = %q, want %q", got, "done")
	}
	if err := waitResult(t, done); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
}

func TestRun_ShutdownTimeoutForcesClose(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := startServe(ctx, t, srv, 50*time.Millisecond)

	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr)
		if err == nil {
			_ = resp.Body.Close()
		}
		clientErr <- err
	}()

	<-started
	cancel()
	err := waitResult(t, done)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want a wrapped context.DeadlineExceeded", err)
	}
	if err := <-clientErr; err == nil {
		t.Error("client request succeeded, want its connection dropped")
	}
}

func TestRun_ReturnsServeError(t *testing.T) {
	busy := listen(t) // ListenAndServe fails: the port is taken
	defer busy.Close()
	srv := New(Config{Addr: busy.Addr().String()}, http.HandlerFunc(ok), 1<<10, discard)

	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), srv, time.Second, discard) }()
	err := waitResult(t, done)
	if err == nil || !strings.Contains(err.Error(), "http server") {
		t.Fatalf("Run returned %v, want an http server error", err)
	}
}

func TestConfig_Validate(t *testing.T) {
	valid := Config{Addr: ":8080", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second}
	if errs := valid.Validate("server"); len(errs) != 0 {
		t.Fatalf("valid config: %v", errs)
	}

	errs := Config{Addr: " ", ReadTimeout: -1}.Validate("server")
	got := errors.Join(errs...).Error()
	for _, want := range []string{"server.addr", "server.read_header_timeout", "server.read_timeout", "server.write_timeout", "server.shutdown_timeout"} {
		if !strings.Contains(got, want) {
			t.Errorf("errors %q do not mention %s", got, want)
		}
	}
}
