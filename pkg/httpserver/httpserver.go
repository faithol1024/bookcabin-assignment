// Package httpserver builds an http.Server from config and runs it with graceful shutdown.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	Addr              string        `yaml:"addr"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout"`
	ShutdownTimeout   time.Duration `yaml:"shutdown_timeout"`
}

// Validate names each problem as prefix.<yaml key>.
func (c Config) Validate(prefix string) []error {
	var errs []error
	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, fmt.Errorf("%s.addr: must not be empty", prefix))
	}
	durations := []struct {
		key string
		d   time.Duration
	}{
		{"read_header_timeout", c.ReadHeaderTimeout},
		{"read_timeout", c.ReadTimeout},
		{"write_timeout", c.WriteTimeout},
		{"shutdown_timeout", c.ShutdownTimeout},
	}
	for _, d := range durations {
		if d.d <= 0 {
			errs = append(errs, fmt.Errorf("%s.%s: must be greater than 0", prefix, d.key))
		}
	}
	return errs
}

// New caps request bodies at maxBody bytes and logs server errors to logger.
func New(cfg Config, h http.Handler, maxBody int64, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           http.MaxBytesHandler(h, maxBody),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

// Run serves until ctx is cancelled, then shuts down gracefully within shutdownTimeout.
func Run(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration, logger *slog.Logger) error {
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	return shutdown(ctx, srv, serveErr, shutdownTimeout, logger)
}

func shutdown(ctx context.Context, srv *http.Server, serveErr <-chan error, timeout time.Duration, logger *slog.Logger) error {
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close() // deadline passed: drop remaining connections
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	logger.Info("server stopped")
	return nil
}
