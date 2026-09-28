package garuda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/pkg/cache"
	"github.com/faithol1024/bookcabin-assignment/pkg/resilience"
)

const (
	name       = "garuda"
	searchPath = "/flights"
	// maxResponseBytes bounds what a misbehaving upstream can make us buffer.
	maxResponseBytes = 4 << 20
)

type Deps struct {
	HTTP         *http.Client // must not set Timeout, deadlines come from ctx
	BaseURL      string
	Guard        *resilience.GuardSettings // nil calls upstream once, unguarded
	FetchTimeout time.Duration             // bounds a fetch shared by identical searches; 0 means none
	Cache        cache.Store               // nil caches nothing
	CacheTTL     time.Duration
	KeyPrefix    string // keys are {KeyPrefix}:garuda:{origin}:{destination}:{date}
}

type Client struct {
	http      *http.Client
	baseURL   string
	guard     *resilience.Guard
	loader    *cache.Loader[fetched]
	keyPrefix string
}

type Result struct {
	Flights  []Flight // shared by concurrent identical searches: read only
	Attempts int      // upstream requests made; 0 on a cache hit, when rate limited or the breaker is open
	CacheHit bool
}

type fetched struct {
	flights  []Flight
	attempts int
}

func New(d Deps) *Client {
	c := &Client{
		http:      d.HTTP,
		baseURL:   d.BaseURL,
		loader:    cache.NewLoader[fetched](d.Cache, d.CacheTTL, d.FetchTimeout),
		keyPrefix: d.KeyPrefix,
	}
	if d.Guard != nil {
		s := *d.Guard
		s.Name = name
		s.Retry.IsRetryable = func(err error) bool { return errors.Is(err, apperr.ErrUnavailable) }
		c.guard = resilience.NewGuard(s)
	}
	return c
}

// Search serves the cached records or joins an identical in-flight search; the shared fetch outlives
// callers that give up.
func (c *Client) Search(ctx context.Context, q Query) (Result, error) {
	key := c.keyPrefix + ":" + name + ":" + q.Origin + ":" + q.Destination + ":" + q.Date
	got, err := c.loader.Load(ctx, key, func(ctx context.Context) ([]byte, fetched, error) {
		return c.fetch(ctx, q)
	})
	switch {
	case ctx.Err() != nil:
		return Result{}, fmt.Errorf("%s: %w", name, ctx.Err())
	case err != nil:
		return Result{Attempts: got.Fetched.attempts}, err
	case !got.Hit:
		return Result{Flights: got.Fetched.flights, Attempts: got.Fetched.attempts}, nil
	}
	var flights []Flight
	if err := json.Unmarshal(got.Value, &flights); err != nil {
		return Result{}, fmt.Errorf("%s: %w: cached records: %w", name, apperr.ErrMalformedResponse, err)
	}
	return Result{Flights: flights, CacheHit: true}, nil
}

// fetch returns the records JSON-encoded for the cache as well as decoded.
func (c *Client) fetch(ctx context.Context, q Query) ([]byte, fetched, error) {
	var flights []Flight
	attempts, err := c.guard.Do(ctx, func(ctx context.Context) error {
		var err error
		flights, err = c.request(ctx, q)
		return err
	})
	if err != nil {
		return nil, fetched{attempts: attempts}, classify(err)
	}
	b, err := json.Marshal(flights)
	if err != nil {
		return nil, fetched{attempts: attempts}, fmt.Errorf("%s: encode records: %w", name, err)
	}
	return b, fetched{flights: flights, attempts: attempts}, nil
}

// classify maps guard rejections to shared sentinels; request errors already carry theirs.
func classify(err error) error {
	switch {
	case errors.Is(err, resilience.ErrRateLimited):
		return fmt.Errorf("garuda: %w", apperr.ErrRateLimited)
	case errors.Is(err, resilience.ErrOpen):
		return fmt.Errorf("garuda: %w: %w", apperr.ErrUnavailable, err)
	case errors.Is(err, resilience.ErrAttemptTimeout):
		return fmt.Errorf("%s: %w: %w", name, apperr.ErrUnavailable, err)
	}
	return err
}

func (c *Client) request(ctx context.Context, q Query) ([]Flight, error) {
	params := url.Values{"origin": {q.Origin}, "destination": {q.Destination}, "date": {q.Date}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+searchPath+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("garuda: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("garuda: %w", ctxErr)
		}
		return nil, fmt.Errorf("garuda: %w: %w", apperr.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		return nil, fmt.Errorf("garuda: %w: status %d", apperr.ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("garuda: %w: status %d", apperr.ErrMalformedResponse, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("garuda: %w: %w", apperr.ErrUnavailable, err)
	}
	r, err := decode(b)
	if err != nil {
		return nil, fmt.Errorf("garuda: %w: %w", apperr.ErrMalformedResponse, err)
	}
	return r.Flights, nil
}

func decode(b []byte) (Response, error) {
	var r Response
	if err := json.Unmarshal(b, &r); err != nil {
		return Response{}, err
	}
	if r.Status != "success" {
		return Response{}, fmt.Errorf("status %q", r.Status)
	}
	return r, nil
}
