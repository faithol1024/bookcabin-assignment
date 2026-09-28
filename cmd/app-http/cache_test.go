package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/faithol1024/bookcabin-assignment/internal/config"
)

func cacheConfig(addr string) config.Config {
	var cfg config.Config
	cfg.Redis = config.Redis{
		Addr:           addr,
		OpTimeout:      50 * time.Millisecond,
		ConnectTimeout: 2 * time.Second,
	}
	cfg.FlightSearch.Cache = config.Cache{TTL: time.Minute}
	return cfg
}

func TestConnectRedis_Password(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireAuth("pw")
	c := cacheConfig(mr.Addr()).Redis
	c.Password = "pw"
	s, err := connectRedis(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}

func TestConnectRedis_BoundedByConnectTimeout(t *testing.T) {
	// accepts connections and never answers
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	c := cacheConfig(ln.Addr().String()).Redis
	c.ConnectTimeout = 100 * time.Millisecond

	start := time.Now()
	if _, err := connectRedis(context.Background(), c); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("connectRedis() error = %v, want unreachable", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("connectRedis() took %v, want about the 100ms connect timeout", took)
	}
}
