package main

import (
	"github.com/faithol1024/bookcabin-assignment/internal/config"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/fx"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	httphandler "github.com/faithol1024/bookcabin-assignment/internal/handler/http"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
	"github.com/faithol1024/bookcabin-assignment/pkg/cache"
	"github.com/faithol1024/bookcabin-assignment/pkg/resilience"
)

const cacheKeyPrefix = "flights:v1"

// newHandler wires the mock gateways, each airline behind store and its own guard, into the search API.
func newHandler(cfg config.Config, store cache.Store) *httphandler.Handler {
	fs := cfg.FlightSearch
	p := fs.Providers
	ttl, fetch := fs.Cache.TTL, fs.Search.Deadline

	g := garuda.MockDeps()
	g.Guard, g.FetchTimeout, g.Cache, g.CacheTTL, g.KeyPrefix = guard(p.Garuda), fetch, store, ttl, cacheKeyPrefix
	l := lionair.MockDeps()
	l.Guard, l.FetchTimeout, l.Cache, l.CacheTTL, l.KeyPrefix = guard(p.Lion), fetch, store, ttl, cacheKeyPrefix
	b := batik.MockDeps()
	b.Guard, b.FetchTimeout, b.Cache, b.CacheTTL, b.KeyPrefix = guard(p.Batik), fetch, store, ttl, cacheKeyPrefix
	a := airasia.MockDeps()
	a.Guard, a.FetchTimeout, a.Cache, a.CacheTTL, a.KeyPrefix = guard(p.AirAsia), fetch, store, ttl, cacheKeyPrefix

	score := fs.Search.Score
	svc := search.New(search.Gateways{
		Garuda:  garuda.New(g),
		LionAir: lionair.New(l),
		Batik:   batik.New(b),
		AirAsia: airasia.New(a),
		FX:      fx.NewMock(),
	}, mapper.New(fs.Validation.DurationMismatchThreshold), search.Config{
		Deadline:  fs.Search.Deadline,
		Exponents: fs.Currency.Exponents,
		Score: search.Score{
			Weights:      search.Weights(score.Weights),
			MaxStops:     score.MaxStops,
			RedeyeWindow: search.Window(score.RedeyeWindow),
		},
	})

	api := fs.API
	return httphandler.New(svc, httphandler.Limits{
		DefaultLimit: api.Page.DefaultLimit,
		MaxLimit:     api.Page.MaxLimit,
		Passengers:   httphandler.Bounds(api.Passengers),
		Legs:         httphandler.Bounds(api.Legs),
	})
}

func guard(p config.Provider) *resilience.GuardSettings {
	return &resilience.GuardSettings{
		RateLimit: resilience.RateLimit{RPS: p.RateLimit.RPS, Burst: p.RateLimit.Burst},
		Breaker:   resilience.Breaker(p.Breaker),
		Retry: resilience.Policy{
			Timeout:    p.Timeout,
			MaxRetries: p.Retry.Max,
			Base:       p.Retry.Base,
			MaxBackoff: p.Retry.MaxBackoff,
		},
	}
}
