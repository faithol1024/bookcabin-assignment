package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/faithol1024/bookcabin-assignment/pkg/secrets"
)

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

// useConfig writes the development config with addr as server.addr and quiet
// logging, applies extra old→new replacements, and points run at it.
func useConfig(t *testing.T, addr string, replace ...string) {
	t.Helper()
	raw, err := os.ReadFile("../../files/etc/bookcabin-assignment/development/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pairs := append([]string{`addr: ":8080"`, fmt.Sprintf("addr: %q", addr), "level: debug", "level: error"}, replace...)
	doc := strings.NewReplacer(pairs...).Replace(string(raw))
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "")
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("SECRET_REDIS_ADDR", miniredis.RunT(t).Addr())
}

func TestRun_InvalidConfig(t *testing.T) {
	useConfig(t, ":0", "write_timeout: 5s", "write_timeout: 0s")

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "server.write_timeout") {
		t.Fatalf("run returned %v, want a config error naming server.write_timeout", err)
	}
}

func TestRun_MissingSecret(t *testing.T) {
	useConfig(t, ":0")
	t.Setenv("SECRET_REDIS_ADDR", "")

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/secret/redis/addr") {
		t.Fatalf("run returned %v, want an error naming /secret/redis/addr", err)
	}
}

func TestRun_IncompleteConfig(t *testing.T) {
	useConfig(t, ":0", "    ttl: 120s\n", "")

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "flight_search.cache.ttl: missing") {
		t.Fatalf("run returned %v, want flight_search.cache.ttl: missing", err)
	}
}

func TestRun_MissingConfigFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	t.Setenv("CONFIG_FILE", missing)

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("run returned %v, want an error naming %s", err, missing)
	}
}

func TestRun_UnknownAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "qa")

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("run returned %v, want an APP_ENV error", err)
	}
}

func TestRun_AddressInUse(t *testing.T) {
	busy := listen(t)
	defer busy.Close()
	useConfig(t, busy.Addr().String())

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("run returned %v, want an address-in-use error", err)
	}
}

func TestRun_RedisUnreachable(t *testing.T) {
	useConfig(t, ":0")
	down := freeAddr(t)
	t.Setenv("SECRET_REDIS_ADDR", down)

	start := time.Now()
	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "redis at "+down+" unreachable") {
		t.Fatalf("run returned %v, want a redis unreachable error", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("run took %v to give up, want at most the 2s connect timeout", took)
	}
}

func TestRun_StartsAndStopsOnCancel(t *testing.T) {
	useConfig(t, freeAddr(t))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel) // stop shortly after startup

	if err := run(ctx); err != nil {
		t.Fatalf("run returned %v, want nil", err)
	}
}

func TestRun_DevelopmentConfigFromRepo(t *testing.T) {
	// The committed development file must load via the default path. Tests
	// run in cmd/app-http, so point CONFIG_FILE at the repo copy.
	t.Setenv("APP_ENV", "development")
	t.Setenv("CONFIG_FILE", "../../files/etc/bookcabin-assignment/development/config.yaml")
	t.Setenv("SECRET_REDIS_ADDR", "localhost:6379")

	cfg, err := loadConfig(context.Background(), secrets.EnvSource{})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Redis.Addr != "localhost:6379" || cfg.HTTPServer.Addr != ":8080" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}
