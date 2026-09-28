package cache

import (
	"context"
	"time"
)

// Store misses report false with a nil error.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}
