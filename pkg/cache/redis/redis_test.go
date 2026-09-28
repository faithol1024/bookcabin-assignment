package redis

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

var ctx = context.Background()

func newStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	s := New(Options{Addr: mr.Addr(), OpTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { _ = s.Close() })
	return s, mr
}

func TestStore_Ping(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStore_PingUnreachable(t *testing.T) {
	s, mr := newStore(t)
	mr.Close()
	if err := s.Ping(ctx); err == nil {
		t.Fatal("Ping() succeeded against a stopped server")
	}
}

func TestStore_Password(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireAuth("pw")
	for _, tt := range []struct {
		password string
		wantErr  bool
	}{{"pw", false}, {"wrong", true}} {
		s := New(Options{Addr: mr.Addr(), Password: tt.password, OpTimeout: 50 * time.Millisecond})
		if err := s.Ping(ctx); (err != nil) != tt.wantErr {
			t.Errorf("Ping() with password %q = %v, want error %v", tt.password, err, tt.wantErr)
		}
		_ = s.Close()
	}
}

func TestStore_Miss(t *testing.T) {
	s, _ := newStore(t)
	v, ok, err := s.Get(ctx, "nope")
	if err != nil || ok || v != nil {
		t.Errorf("Get() = %q, %v, %v; want a clean miss", v, ok, err)
	}
}

func TestStore_SetGetWithTTL(t *testing.T) {
	s, mr := newStore(t)
	if err := s.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := mr.TTL("k"); got != time.Minute {
		t.Errorf("TTL = %v, want 1m", got)
	}
	v, ok, err := s.Get(ctx, "k")
	if err != nil || !ok || string(v) != "v" {
		t.Errorf("Get() = %q, %v, %v; want v, true", v, ok, err)
	}
	mr.FastForward(time.Minute)
	if _, ok, _ := s.Get(ctx, "k"); ok {
		t.Error("hit after the TTL")
	}
}

func TestStore_ServerDownIsError(t *testing.T) {
	s, mr := newStore(t)
	mr.Close()
	if _, _, err := s.Get(ctx, "k"); err == nil {
		t.Error("Get() succeeded against a stopped server")
	}
	if err := s.Set(ctx, "k", []byte("v"), time.Minute); err == nil {
		t.Error("Set() succeeded against a stopped server")
	}
}

func TestStore_OpTimeout(t *testing.T) {
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
	s := New(Options{Addr: ln.Addr().String(), OpTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { _ = s.Close() })

	start := time.Now()
	_, _, err = s.Get(ctx, "k")
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("Get() error = %v, want a timeout", err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("Get() took %v, want about the 50ms op timeout", took)
	}
}
