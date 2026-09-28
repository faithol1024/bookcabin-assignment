package search

import (
	"context"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/airasia"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/batik"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/garuda"
	"github.com/faithol1024/bookcabin-assignment/internal/gateway/lionair"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/mapper"
)

type GarudaGateway interface {
	Search(ctx context.Context, q garuda.Query) (garuda.Result, error)
}

type LionAirGateway interface {
	Search(ctx context.Context, q lionair.Query) (lionair.Result, error)
}

type BatikGateway interface {
	Search(ctx context.Context, q batik.Query) (batik.Result, error)
}

type AirAsiaGateway interface {
	Search(ctx context.Context, q airasia.Query) (airasia.Result, error)
}

type FXGateway interface {
	Rate(ctx context.Context, currency string) (int64, error)
}

type Gateways struct {
	Garuda  GarudaGateway
	LionAir LionAirGateway
	Batik   BatikGateway
	AirAsia AirAsiaGateway
	FX      FXGateway
}

type Config struct {
	Deadline  time.Duration  // per leg: fan-out and conversion
	Exponents map[string]int // currency minor-unit digits, also the supported-currency list
	Score     Score
}

type Service struct {
	sources   []source
	converter converter
	deadline  time.Duration
	score     Score
}

func New(gw Gateways, m mapper.Mapper, cfg Config) *Service {
	return &Service{
		sources: []source{
			garudaSource(gw.Garuda, m),
			lionAirSource(gw.LionAir, m),
			batikSource(gw.Batik, m),
			airAsiaSource(gw.AirAsia, m),
		},
		converter: converter{exponents: cfg.Exponents, fx: gw.FX},
		deadline:  cfg.Deadline,
		score:     cfg.Score,
	}
}

func garudaSource(gw GarudaGateway, m mapper.Mapper) source {
	return source{provider: mapper.ProviderGaruda, search: func(ctx context.Context, l leg) (sourceResult, error) {
		r, err := gw.Search(ctx, garuda.Query(l.LegQuery))
		return mapped(ctx, r.Flights, m.Garuda, l.cabin, r.Attempts, r.CacheHit, err)
	}}
}

func lionAirSource(gw LionAirGateway, m mapper.Mapper) source {
	return source{provider: mapper.ProviderLionAir, search: func(ctx context.Context, l leg) (sourceResult, error) {
		r, err := gw.Search(ctx, lionair.Query(l.LegQuery))
		return mapped(ctx, r.Flights, m.LionAir, l.cabin, r.Attempts, r.CacheHit, err)
	}}
}

func batikSource(gw BatikGateway, m mapper.Mapper) source {
	return source{provider: mapper.ProviderBatik, search: func(ctx context.Context, l leg) (sourceResult, error) {
		r, err := gw.Search(ctx, batik.Query(l.LegQuery))
		return mapped(ctx, r.Flights, m.Batik, l.cabin, r.Attempts, r.CacheHit, err)
	}}
}

func airAsiaSource(gw AirAsiaGateway, m mapper.Mapper) source {
	return source{provider: mapper.ProviderAirAsia, search: func(ctx context.Context, l leg) (sourceResult, error) {
		r, err := gw.Search(ctx, airasia.Query(l.LegQuery))
		return mapped(ctx, r.Flights, m.AirAsia, l.cabin, r.Attempts, r.CacheHit, err)
	}}
}

// mapped runs on cache hits too, so rejections are recomputed from the raw records on every search.
func mapped[F any](ctx context.Context, records []F, toFlight func(F) (domain.Flight, error), cabin domain.Cabin, attempts int, cacheHit bool, err error) (sourceResult, error) {
	if err != nil {
		return sourceResult{attempts: attempts}, err
	}
	flights, rejected := mapper.Records(ctx, records, toFlight, cabin)
	return sourceResult{flights: flights, rejected: rejected, attempts: attempts, cacheHit: cacheHit}, nil
}
