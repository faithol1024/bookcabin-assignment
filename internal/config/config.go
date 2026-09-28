// Package config parses and validates the per-environment YAML config.
package config

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/util"
	pkgconfig "github.com/faithol1024/bookcabin-assignment/pkg/config"
	"github.com/faithol1024/bookcabin-assignment/pkg/httpserver"
)

type Config struct {
	HTTPServer   httpserver.Config `yaml:"server"`
	Log          pkgconfig.Log     `yaml:"log"`
	Redis        Redis             `yaml:"redis"`
	FlightSearch FlightSearch      `yaml:"flight_search"`
}

type FlightSearch struct {
	Cache      Cache      `yaml:"cache"`
	Search     Search     `yaml:"search"`
	Validation Validation `yaml:"validation"`
	API        API        `yaml:"api"`
	Providers  Providers  `yaml:"providers"`
	Currency   Currency   `yaml:"currency"`
}

type Redis struct {
	Addr      string           `yaml:"addr"`
	Password  pkgconfig.Secret `yaml:"password,omitempty"` // development has none
	OpTimeout time.Duration    `yaml:"op_timeout"`
	// startup ping; Redis must be up when the service starts
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
}

type Cache struct {
	TTL time.Duration `yaml:"ttl"`
}

type Search struct {
	Deadline time.Duration `yaml:"deadline"`
	Score    Score         `yaml:"score"`
}

type Validation struct {
	DurationMismatchThreshold time.Duration `yaml:"duration_mismatch_threshold"` // provider duration vs timestamps
}

// API bounds what a search request may ask for.
type API struct {
	Page       Page  `yaml:"page"`
	Passengers Range `yaml:"passengers"`
	Legs       Range `yaml:"legs"` // multi-city
}

type Page struct {
	DefaultLimit int `yaml:"default_limit"`
	MaxLimit     int `yaml:"max_limit"`
}

// Range is inclusive.
type Range struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

type Score struct {
	Weights      Weights    `yaml:"weights"`
	MaxStops     int        `yaml:"max_stops"`
	RedeyeWindow TimeWindow `yaml:"redeye_window"`
}

// Weights are normalized by their sum.
type Weights struct {
	Price    float64 `yaml:"price"`
	Duration float64 `yaml:"duration"`
	Stops    float64 `yaml:"stops"`
	Redeye   float64 `yaml:"redeye"`
}

type Providers struct {
	Garuda  Provider `yaml:"garuda"`
	Lion    Provider `yaml:"lion"`
	Batik   Provider `yaml:"batik"`
	AirAsia Provider `yaml:"airasia"`
}

type Provider struct {
	Timeout   time.Duration `yaml:"timeout"` // per attempt
	Retry     Retry         `yaml:"retry"`
	RateLimit RateLimit     `yaml:"rate_limit"`
	Breaker   Breaker       `yaml:"breaker"`
}

// Currency's exponent table is also the supported-currency list.
type Currency struct {
	Exponents map[string]int `yaml:"exponents"` // minor-unit digits; IDR is 0 (whole rupiah)
}

type Retry struct {
	Max        int           `yaml:"max"` // retries after the first attempt
	Base       time.Duration `yaml:"base"`
	MaxBackoff time.Duration `yaml:"max_backoff"`
}

type RateLimit struct {
	RPS   float64 `yaml:"rps"`
	Burst int     `yaml:"burst"`
}

type Breaker struct {
	Failures         int           `yaml:"failures"` // consecutive failures that open it
	Cooldown         time.Duration `yaml:"cooldown"`
	HalfOpenRequests int           `yaml:"half_open_requests"` // probes allowed once the cooldown ends
}

// TimeWindow is minutes after midnight, inclusive; From > To wraps midnight.
type TimeWindow struct {
	From int
	To   int
}

func (w *TimeWindow) UnmarshalText(b []byte) error {
	parsed, err := parseTimeWindow(string(b))
	if err != nil {
		return err
	}
	*w = parsed
	return nil
}

// Parse strictly decodes YAML and validates it; there are no defaults.
func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := pkgconfig.Decode(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func provider(v *pkgconfig.Validator, path string, p Provider) {
	v.Positive(path+".timeout", p.Timeout)
	v.Check(p.Retry.Max >= 0, path+".retry.max", "must be 0 or greater")
	v.Positive(path+".retry.base", p.Retry.Base)
	v.Positive(path+".retry.max_backoff", p.Retry.MaxBackoff)
	v.Check(p.Retry.Base <= p.Retry.MaxBackoff, path+".retry.base", "must not exceed retry.max_backoff")
	v.Check(finite(p.RateLimit.RPS) && p.RateLimit.RPS > 0, path+".rate_limit.rps", "must be a finite number greater than 0")
	v.Check(p.RateLimit.Burst > 0, path+".rate_limit.burst", "must be greater than 0")
	breaker(v, path+".breaker", p.Breaker)
}

func breaker(v *pkgconfig.Validator, path string, b Breaker) {
	v.Check(b.Failures > 0, path+".failures", "must be greater than 0")
	v.Positive(path+".cooldown", b.Cooldown)
	v.Check(b.HalfOpenRequests > 0 && int64(b.HalfOpenRequests) <= math.MaxUint32, path+".half_open_requests", fmt.Sprintf("must be between 1 and %d", uint32(math.MaxUint32)))
}

func apiRange(v *pkgconfig.Validator, key string, r Range, least int) {
	path := "flight_search.api." + key
	v.Check(r.Min >= least, path+".min", fmt.Sprintf("must be %d or greater", least))
	v.Check(r.Min <= r.Max, path+".max", "must not be below api."+key+".min")
}

func (c Config) validate() error {
	v := &pkgconfig.Validator{}
	v.Add(c.HTTPServer.Validate("server")...)
	v.Check(strings.TrimSpace(c.Redis.Addr) != "", "redis.addr", "must not be empty")
	v.Positive("redis.op_timeout", c.Redis.OpTimeout)
	v.Positive("redis.connect_timeout", c.Redis.ConnectTimeout)

	fs := c.FlightSearch
	v.Positive("flight_search.cache.ttl", fs.Cache.TTL)
	v.Positive("flight_search.search.deadline", fs.Search.Deadline)

	score := fs.Search.Score
	v.Check(score.MaxStops > 0, "flight_search.search.score.max_stops", "must be greater than 0")
	weights := []struct {
		name string
		w    float64
	}{
		{"price", score.Weights.Price},
		{"duration", score.Weights.Duration},
		{"stops", score.Weights.Stops},
		{"redeye", score.Weights.Redeye},
	}
	var sum float64
	for _, w := range weights {
		v.Check(finite(w.w) && w.w >= 0, "flight_search.search.score.weights."+w.name, "must be a finite number, 0 or greater")
		sum += w.w
	}
	v.Check(sum > 0, "flight_search.search.score.weights", "must sum to more than 0")
	v.Positive("flight_search.validation.duration_mismatch_threshold", fs.Validation.DurationMismatchThreshold)

	page := fs.API.Page
	v.Check(page.MaxLimit > 0, "flight_search.api.page.max_limit", "must be greater than 0")
	v.Check(page.DefaultLimit > 0 && page.DefaultLimit <= page.MaxLimit, "flight_search.api.page.default_limit", "must be between 1 and api.page.max_limit")
	apiRange(v, "passengers", fs.API.Passengers, 1)
	apiRange(v, "legs", fs.API.Legs, 2)

	providers := []struct {
		name string
		p    Provider
	}{
		{"garuda", fs.Providers.Garuda},
		{"lion", fs.Providers.Lion},
		{"batik", fs.Providers.Batik},
		{"airasia", fs.Providers.AirAsia},
	}
	for _, p := range providers {
		provider(v, "flight_search.providers."+p.name, p.p)
	}

	exps := fs.Currency.Exponents
	idr, ok := exps["IDR"]
	v.Check(ok && idr == 0, "flight_search.currency.exponents.IDR", "must be present and 0")
	for _, code := range slices.Sorted(maps.Keys(exps)) {
		path := "flight_search.currency.exponents." + code
		v.Check(isCurrencyCode(code), path, "must be a 3-letter upper-case ISO 4217 code")
		// 10^exp must fit int64
		v.Check(exps[code] >= 0 && exps[code] <= maxExponent, path, fmt.Sprintf("must be between 0 and %d", maxExponent))
	}
	return v.Err()
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

const maxExponent = 18

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func parseTimeWindow(s string) (TimeWindow, error) {
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return TimeWindow{}, errors.New("must be HH:MM-HH:MM")
	}
	f, err := util.ParseClock(from)
	if err != nil {
		return TimeWindow{}, err
	}
	t, err := util.ParseClock(to)
	if err != nil {
		return TimeWindow{}, err
	}
	return TimeWindow{From: f, To: t}, nil
}
