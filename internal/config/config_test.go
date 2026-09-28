package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	pkgconfig "github.com/faithol1024/bookcabin-assignment/pkg/config"
	"github.com/faithol1024/bookcabin-assignment/pkg/httpserver"
	"github.com/faithol1024/bookcabin-assignment/pkg/secrets"
)

// expected is the development file's content; other environments differ only
// in log level and the Redis password.
func expected() Config {
	retry := Retry{Max: 2, Base: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond}
	provider := func(timeout time.Duration) Provider {
		return Provider{
			Timeout:   timeout,
			Retry:     retry,
			RateLimit: RateLimit{RPS: 20, Burst: 20},
			Breaker:   Breaker{Failures: 5, Cooldown: 10 * time.Second, HalfOpenRequests: 1},
		}
	}
	return Config{
		HTTPServer: httpserver.Config{
			Addr:              ":8080",
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      5 * time.Second,
			ShutdownTimeout:   10 * time.Second,
		},
		Log: pkgconfig.Log{Level: slog.LevelDebug},
		Redis: Redis{
			Addr:           "localhost:6379",
			OpTimeout:      50 * time.Millisecond,
			ConnectTimeout: 2 * time.Second,
		},
		FlightSearch: FlightSearch{
			Cache: Cache{TTL: 120 * time.Second},
			Search: Search{
				Deadline: 800 * time.Millisecond,
				Score: Score{
					Weights:      Weights{Price: 0.5, Duration: 0.3, Stops: 0.15, Redeye: 0.05},
					MaxStops:     2,
					RedeyeWindow: TimeWindow{From: 0, To: 5*60 + 59},
				},
			},
			Validation: Validation{DurationMismatchThreshold: 5 * time.Minute},
			API: API{
				Page:       Page{DefaultLimit: 20, MaxLimit: 100},
				Passengers: Range{Min: 1, Max: 9},
				Legs:       Range{Min: 2, Max: 5},
			},
			Providers: Providers{
				Garuda:  provider(200 * time.Millisecond),
				Lion:    provider(300 * time.Millisecond),
				Batik:   provider(500 * time.Millisecond),
				AirAsia: provider(200 * time.Millisecond),
			},
			Currency: Currency{Exponents: map[string]int{"IDR": 0, "USD": 2, "SGD": 2, "MYR": 2, "EUR": 2, "JPY": 0}},
		},
	}
}

// base is the committed development config with its secret filled in.
func base(t *testing.T) *yaml.Node {
	t.Helper()
	raw, err := os.ReadFile("../../files/etc/bookcabin-assignment/development/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := secrets.Render(context.Background(), raw, fixedSecret("localhost:6379"))
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(rendered, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

type fixedSecret string

func (f fixedSecret) Secret(context.Context, string) (string, error) { return string(f), nil }

// withOverride deep-merges override into the base document.
func withOverride(t *testing.T, override string) string {
	t.Helper()
	b := base(t)
	var o yaml.Node
	if err := yaml.Unmarshal([]byte(override), &o); err != nil {
		t.Fatalf("override: %v", err)
	}
	if len(o.Content) == 1 {
		merge(b, o.Content[0])
	}
	return encode(t, b)
}

func merge(dst, src *yaml.Node) {
	for i := 0; i+1 < len(src.Content); i += 2 {
		key, val := src.Content[i], src.Content[i+1]
		if cur := lookup(dst, key.Value); cur != nil && cur.Kind == yaml.MappingNode && val.Kind == yaml.MappingNode {
			merge(cur, val)
		} else if cur != nil {
			*cur = *val
		} else {
			dst.Content = append(dst.Content, key, val)
		}
	}
}

func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// without removes the dotted key path from the base document.
func without(t *testing.T, path string) string {
	t.Helper()
	b := base(t)
	keys := strings.Split(path, ".")
	m := b
	for _, k := range keys[:len(keys)-1] {
		if m = lookup(m, k); m == nil {
			t.Fatalf("no %s in base", path)
		}
	}
	last := keys[len(keys)-1]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == last {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return encode(t, b)
		}
	}
	t.Fatalf("no %s in base", path)
	return ""
}

func encode(t *testing.T, n *yaml.Node) string {
	t.Helper()
	out, err := yaml.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCommittedEnvironmentFiles(t *testing.T) {
	t.Setenv("SECRET_REDIS_ADDR", "redis:6379")
	t.Setenv("SECRET_REDIS_PASSWORD", "pw")
	tests := []struct {
		env          string
		level        slog.Level
		wantPassword string
	}{
		{pkgconfig.EnvDevelopment, slog.LevelDebug, ""},
		{pkgconfig.EnvStaging, slog.LevelInfo, "pw"},
		{pkgconfig.EnvProduction, slog.LevelInfo, "pw"},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../files/etc/bookcabin-assignment", tt.env, "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := secrets.Render(context.Background(), raw, secrets.EnvSource{})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			cfg, err := Parse(rendered)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			want := expected()
			want.Log.Level = tt.level
			want.Redis.Addr = "redis:6379"
			want.Redis.Password = pkgconfig.Secret(tt.wantPassword)
			if !reflect.DeepEqual(cfg, want) {
				t.Errorf("config mismatch\n got: %+v\nwant: %+v", cfg, want)
			}
		})
	}
}

func TestParse_EmptyDocumentFails(t *testing.T) {
	_, err := Parse(nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"server: missing", "log: missing", "redis: missing", "flight_search: missing"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not contain %q: %v", key, err)
		}
	}
}

func TestParse_MissingKeys(t *testing.T) {
	for _, path := range []string{
		"server.write_timeout",
		"redis.addr",
		"redis.op_timeout",
		"redis.connect_timeout",
		"flight_search.cache.ttl",
		"flight_search.search.score.weights.redeye",
		"flight_search.validation.duration_mismatch_threshold",
		"flight_search.api.page.default_limit",
		"flight_search.api.passengers.max",
		"flight_search.api.legs",
		"flight_search.providers.garuda.timeout",
		"flight_search.providers.batik.rate_limit.burst",
		"flight_search.providers.airasia.breaker.cooldown",
		"flight_search.providers.lion.breaker.half_open_requests",
		"flight_search.providers.lion",
		"flight_search.currency.exponents",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := Parse([]byte(without(t, path)))
			if err == nil || !strings.Contains(err.Error(), path+": missing") {
				t.Errorf("error = %v, want %s: missing", err, path)
			}
		})
	}
}

func TestParse_Overrides(t *testing.T) {
	doc := withOverride(t, `
server:
  addr: ":9090"
  write_timeout: 1s
log:
  level: INFO
flight_search:
  search:
    score:
      weights: { redeye: 0 }
      redeye_window: "22:00-02:00"
  validation: { duration_mismatch_threshold: 10m }
  providers:
    batik:
      timeout: 1s
      rate_limit: { rps: 2.5 }
`)
	cfg, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fs := cfg.FlightSearch
	def := expected().FlightSearch
	checks := []struct {
		name      string
		got, want any
	}{
		{"server.addr", cfg.HTTPServer.Addr, ":9090"},
		{"server.write_timeout", cfg.HTTPServer.WriteTimeout, time.Second},
		{"server.read_timeout (kept)", cfg.HTTPServer.ReadTimeout, 5 * time.Second},
		{"log.level", cfg.Log.Level, slog.LevelInfo},
		{"score.weights.redeye", fs.Search.Score.Weights.Redeye, 0.0},
		{"score.weights.price (kept)", fs.Search.Score.Weights.Price, 0.5},
		{"score.redeye_window", fs.Search.Score.RedeyeWindow, TimeWindow{From: 22 * 60, To: 2 * 60}},
		{"validation.duration_mismatch_threshold", fs.Validation.DurationMismatchThreshold, 10 * time.Minute},
		{"batik.timeout", fs.Providers.Batik.Timeout, time.Second},
		{"batik.rate_limit.rps", fs.Providers.Batik.RateLimit.RPS, 2.5},
		{"batik.rate_limit.burst (kept)", fs.Providers.Batik.RateLimit.Burst, 20},
		{"garuda untouched", fs.Providers.Garuda, def.Providers.Garuda},
		{"lion untouched", fs.Providers.Lion, def.Providers.Lion},
		{"airasia untouched", fs.Providers.AirAsia, def.Providers.AirAsia},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestParse_InvalidValues(t *testing.T) {
	const (
		fs = "flight_search"
		pr = "flight_search.providers"
	)
	tests := []struct {
		name string
		doc  string // merged over the development file unless it starts with "!"
		want string
	}{
		{"empty server addr", "server: {addr: \"\"}", "server.addr"},
		{"duration as int", "server: {write_timeout: 5}", "time.Duration"},
		{"unparseable duration", "server: {read_timeout: soon}", "time.Duration"},
		{"zero read header timeout", "server: {read_header_timeout: 0s}", "server.read_header_timeout"},
		{"negative shutdown timeout", "server: {shutdown_timeout: -1s}", "server.shutdown_timeout"},
		{"empty redis addr", "redis: {addr: \" \"}", "redis.addr: must not be empty"},
		{"zero redis op timeout", "redis: {op_timeout: 0s}", "redis.op_timeout"},
		{"zero redis connect timeout", "redis: {connect_timeout: 0s}", "redis.connect_timeout"},
		{"redis inside flight_search", "flight_search: {redis: {addr: x}}", "field redis not found"},
		{"unknown log level", "log: {level: verbose}", "unknown name"},

		{"zero cache ttl", fs + ": {cache: {ttl: 0s}}", "flight_search.cache.ttl"},
		{"zero search deadline", fs + ": {search: {deadline: 0s}}", "flight_search.search.deadline"},
		{"zero max stops", fs + ": {search: {score: {max_stops: 0}}}", "flight_search.search.score.max_stops"},
		{"negative price weight", fs + ": {search: {score: {weights: {price: -0.5}}}}", "flight_search.search.score.weights.price"},
		{"negative duration weight", fs + ": {search: {score: {weights: {duration: -1}}}}", "weights.duration"},
		{"negative stops weight", fs + ": {search: {score: {weights: {stops: -1}}}}", "weights.stops"},
		{"negative redeye weight", fs + ": {search: {score: {weights: {redeye: -0.01}}}}", "weights.redeye"},
		{"weights sum to zero", fs + ": {search: {score: {weights: {price: 0, duration: 0, stops: 0, redeye: 0}}}}", "score.weights: must sum to more than 0"},
		{"window without dash", fs + ": {search: {score: {redeye_window: \"00:00\"}}}", "HH:MM-HH:MM"},
		{"window hour out of range", fs + ": {search: {score: {redeye_window: \"24:00-05:59\"}}}", "between 00:00 and 23:59"},
		{"window short form", fs + ": {search: {score: {redeye_window: \"0:00-5:59\"}}}", "must be HH:MM"},
		{"zero duration mismatch threshold", fs + ": {validation: {duration_mismatch_threshold: 0s}}", "flight_search.validation.duration_mismatch_threshold"},
		{"negative duration mismatch threshold", fs + ": {validation: {duration_mismatch_threshold: -5m}}", "flight_search.validation.duration_mismatch_threshold"},
		{"unknown validation key", fs + ": {validation: {max_duration: 1h}}", "field max_duration not found"},

		{"zero max limit", fs + ": {api: {page: {default_limit: 0, max_limit: 0}}}", "flight_search.api.page.max_limit: must be greater than 0"},
		{"zero default limit", fs + ": {api: {page: {default_limit: 0}}}", "flight_search.api.page.default_limit: must be between 1 and api.page.max_limit"},
		{"default limit above max", fs + ": {api: {page: {default_limit: 101}}}", "flight_search.api.page.default_limit: must be between 1 and api.page.max_limit"},
		{"fractional default limit", fs + ": {api: {page: {default_limit: 2.5}}}", "flight_search.api.page.default_limit: invalid value \"2.5\": must be an integer"},
		{"zero min passengers", fs + ": {api: {passengers: {min: 0}}}", "flight_search.api.passengers.min: must be 1 or greater"},
		{"passengers max below min", fs + ": {api: {passengers: {min: 3, max: 2}}}", "flight_search.api.passengers.max: must not be below api.passengers.min"},
		{"one leg", fs + ": {api: {legs: {min: 1}}}", "flight_search.api.legs.min: must be 2 or greater"},
		{"legs max below min", fs + ": {api: {legs: {min: 4, max: 3}}}", "flight_search.api.legs.max: must not be below api.legs.min"},
		{"unknown api key", fs + ": {api: {sort: {default: price}}}", "field sort not found"},

		{"garuda zero timeout", fs + ": {providers: {garuda: {timeout: 0s}}}", pr + ".garuda.timeout"},
		{"lion negative retry max", fs + ": {providers: {lion: {retry: {max: -1}}}}", pr + ".lion.retry.max"},
		{"lion fractional retry max", fs + ": {providers: {lion: {retry: {max: 2.5}}}}", pr + ".lion.retry.max: invalid value \"2.5\": must be an integer"},
		{"batik zero retry base", fs + ": {providers: {batik: {retry: {base: 0s}}}}", pr + ".batik.retry.base"},
		{"batik base above max backoff", fs + ": {providers: {batik: {retry: {base: 1s}}}}", pr + ".batik.retry.base: must not exceed retry.max_backoff"},
		{"airasia zero max backoff", fs + ": {providers: {airasia: {retry: {base: 0s, max_backoff: 0s}}}}", pr + ".airasia.retry.max_backoff"},
		{"garuda zero rps", fs + ": {providers: {garuda: {rate_limit: {rps: 0}}}}", pr + ".garuda.rate_limit.rps"},
		{"lion nan rps", fs + ": {providers: {lion: {rate_limit: {rps: .nan}}}}", pr + ".lion.rate_limit.rps"},
		{"batik zero burst", fs + ": {providers: {batik: {rate_limit: {burst: 0}}}}", pr + ".batik.rate_limit.burst"},
		{"batik fractional burst", fs + ": {providers: {batik: {rate_limit: {burst: 20.0}}}}", pr + ".batik.rate_limit.burst: invalid value"},
		{"airasia zero breaker failures", fs + ": {providers: {airasia: {breaker: {failures: 0}}}}", pr + ".airasia.breaker.failures"},
		{"garuda zero breaker cooldown", fs + ": {providers: {garuda: {breaker: {cooldown: 0s}}}}", pr + ".garuda.breaker.cooldown"},
		{"batik zero half-open requests", fs + ": {providers: {batik: {breaker: {half_open_requests: 0}}}}", pr + ".batik.breaker.half_open_requests: must be between 1 and 4294967295"},
		{"lion half-open requests overflow uint32", fs + ": {providers: {lion: {breaker: {half_open_requests: 4294967296}}}}", pr + ".lion.breaker.half_open_requests"},
		{"airasia fractional half-open requests", fs + ": {providers: {airasia: {breaker: {half_open_requests: 1.5}}}}", pr + ".airasia.breaker.half_open_requests: invalid value \"1.5\": must be an integer"},
		{"provider mock is not config", fs + ": {providers: {airasia: {mock: {failure_rate: 0.1}}}}", "field mock not found"},

		{"idr exponent not zero", fs + ": {currency: {exponents: {IDR: 2}}}", "flight_search.currency.exponents.IDR: must be present and 0"},
		{"negative exponent", fs + ": {currency: {exponents: {JPY: -1}}}", "flight_search.currency.exponents.JPY: must be between 0 and 18"},
		{"exponent overflows int64", fs + ": {currency: {exponents: {USD: 19}}}", "flight_search.currency.exponents.USD: must be between 0 and 18"},
		{"fractional exponent", fs + ": {currency: {exponents: {USD: 2.5}}}", "flight_search.currency.exponents.USD: invalid value \"2.5\": must be an integer"},
		{"lower-case currency", fs + ": {currency: {exponents: {usd: 2}}}", "flight_search.currency.exponents.usd: must be a 3-letter"},
		{"long currency code", fs + ": {currency: {exponents: {USDT: 2}}}", "flight_search.currency.exponents.USDT: must be a 3-letter"},
		{"fx is not config", fs + ": {currency: {fx: {timeout: 150ms}}}", "field fx not found"},

		{"misspelled provider", fs + ": {providers: {garudaa: {timeout: 1s}}}", "field garudaa not found"},
		{"providers outside flight_search", "providers: {garuda: {timeout: 1s}}", "field providers not found"},
		{"unknown key", "server: {adress: \":1\"}", "field adress not found"},
		{"unknown section", "servers: {}", "field servers not found"},
		{"duplicate key", "!server: {addr: a}\nserver: {addr: b}", "already defined"},
		{"malformed yaml", "!server: [unclosed", "invalid configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, raw := strings.CutPrefix(tt.doc, "!")
			if !raw {
				doc = withOverride(t, tt.doc)
			}
			_, err := Parse([]byte(doc))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestParse_IDRExponentRequired(t *testing.T) {
	_, err := Parse([]byte(without(t, "flight_search.currency.exponents.IDR")))
	if want := "flight_search.currency.exponents.IDR: must be present and 0"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want %s", err, want)
	}
}

func TestParse_ExponentOverrideAddsCurrency(t *testing.T) {
	cfg, err := Parse([]byte(withOverride(t, "flight_search: {currency: {exponents: {KWD: 3}}}")))
	if err != nil {
		t.Fatal(err)
	}
	want := expected().FlightSearch.Currency.Exponents
	want["KWD"] = 3
	if !reflect.DeepEqual(cfg.FlightSearch.Currency.Exponents, want) {
		t.Errorf("exponents = %v, want %v", cfg.FlightSearch.Currency.Exponents, want)
	}
}

func TestParse_ReportsAllProblems(t *testing.T) {
	doc := withOverride(t, "flight_search: {cache: {ttl: 0s}, providers: {lion: {rate_limit: {burst: 0}}}, search: {score: {weights: {price: -1}}}}\n")
	_, err := Parse([]byte(doc))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, name := range []string{"flight_search.cache.ttl", "providers.lion.rate_limit.burst", "score.weights.price"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
}
