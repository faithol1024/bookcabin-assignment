package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Options struct {
	Addr      string
	Password  string
	OpTimeout time.Duration // bounds every Get and Set
}

type Store struct {
	client    *goredis.Client
	opTimeout time.Duration
}

func New(o Options) *Store {
	return &Store{
		client: goredis.NewClient(&goredis.Options{
			Addr:     o.Addr,
			Password: o.Password,
			// ctx deadlines, not go-redis's own socket timeouts, bound each op
			ContextTimeoutEnabled: true,
			// the caller decides what a failure means; retries would outlive the op timeout
			MaxRetries:    -1,
			DialerRetries: 1,
		}),
		opTimeout: o.OpTimeout,
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()
	v, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *Store) Close() error {
	return s.client.Close()
}
