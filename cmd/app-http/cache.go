package main

import (
	"context"
	"fmt"

	"github.com/faithol1024/bookcabin-assignment/internal/config"
	rediscache "github.com/faithol1024/bookcabin-assignment/pkg/cache/redis"
)

// connectRedis fails unless Redis answers within redis.connect_timeout: the service needs it to start.
func connectRedis(ctx context.Context, c config.Redis) (*rediscache.Store, error) {
	s := rediscache.New(rediscache.Options{Addr: c.Addr, Password: c.Password.Value(), OpTimeout: c.OpTimeout})
	ctx, cancel := context.WithTimeout(ctx, c.ConnectTimeout)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("redis at %s unreachable: %w", c.Addr, err)
	}
	return s, nil
}
