// Command app-http runs the flight search HTTP service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // zone data must not depend on the host

	"github.com/faithol1024/bookcabin-assignment/internal/config"
	httphandler "github.com/faithol1024/bookcabin-assignment/internal/handler/http"
	pkgconfig "github.com/faithol1024/bookcabin-assignment/pkg/config"
	"github.com/faithol1024/bookcabin-assignment/pkg/httpserver"
	"github.com/faithol1024/bookcabin-assignment/pkg/secrets"
)

const (
	appName = "bookcabin-assignment"
	// maxBodyBytes caps request bodies; a fixed security limit, not a tunable.
	maxBodyBytes = 64 << 10
)

func main() {
	// for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := loadConfig(ctx, secrets.EnvSource{})
	if err != nil {
		return err
	}

	logger := slog.New(httphandler.LogHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Log.Level})))
	slog.SetDefault(logger)

	if _, err := time.LoadLocation("Asia/Jakarta"); err != nil {
		return fmt.Errorf("load time zone data: %w", err)
	}

	redis, err := connectRedis(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer redis.Close()
	h := newHandler(cfg, redis)

	srv := httpserver.New(cfg.HTTPServer, newRouter(h, logger), maxBodyBytes, logger)
	return httpserver.Run(ctx, srv, cfg.HTTPServer.ShutdownTimeout, logger)
}

func loadConfig(ctx context.Context, src secrets.Source) (config.Config, error) {
	path, err := pkgconfig.Path(appName, os.Getenv("APP_ENV"), os.Getenv("CONFIG_FILE"))
	if err != nil {
		return config.Config{}, fmt.Errorf("config path: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return config.Config{}, fmt.Errorf("read config: %w", err)
	}
	rendered, err := secrets.Render(ctx, raw, src)
	if err != nil {
		return config.Config{}, fmt.Errorf("render secrets in %s: %w", path, err)
	}
	cfg, err := config.Parse(rendered)
	if err != nil {
		return config.Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}
