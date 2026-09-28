package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type inner struct {
	Timeout time.Duration `yaml:"timeout"`
	Count   int           `yaml:"count"`
}

type window struct{ from, to string }

func (w *window) UnmarshalText(b []byte) error {
	w.from, w.to, _ = strings.Cut(string(b), "-")
	return nil
}

type sample struct {
	Name   string  `yaml:"name"`
	Max    int     `yaml:"max"`
	Seed   int64   `yaml:"seed"`
	Rate   float64 `yaml:"rate"`
	Inline inner   `yaml:",inline"`
	Nested inner   `yaml:"nested"`
	Window window  `yaml:"window"`
	Log    Log     `yaml:"log"`
	Token  Secret  `yaml:"token,omitempty"`
}

// full sets every required key of sample.
const full = `name: n
max: 5
seed: 0
rate: 0.5
timeout: 2s
count: 1
nested: {timeout: 1s, count: 7}
window: "22:00-02:00"
log: {level: DEBUG}
`

func TestDecode_AllFields(t *testing.T) {
	var got sample
	if err := Decode([]byte(full+"token: pw\n"), &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := sample{
		Name: "n", Max: 5, Rate: 0.5,
		Inline: inner{Timeout: 2 * time.Second, Count: 1},
		Nested: inner{Timeout: time.Second, Count: 7},
		Window: window{"22:00", "02:00"},
		Log:    Log{Level: slog.LevelDebug},
		Token:  "pw",
	}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestDecode_OmitemptyIsOptional(t *testing.T) {
	var got sample
	if err := Decode([]byte(full), &got); err != nil || got.Token != "" {
		t.Errorf("Decode without token = %+v, %v; want no error", got, err)
	}
}

func TestDecode_MissingKeys(t *testing.T) {
	tests := []struct {
		name, drop string
		want       []string
	}{
		{"top-level key", "max: 5\n", []string{"max: missing"}},
		{"zero-valued key still required", "seed: 0\n", []string{"seed: missing"}},
		{"inline key", "timeout: 2s\n", []string{"timeout: missing"}},
		{"nested key", "nested: {timeout: 1s, count: 7}\n", []string{"nested: missing"}},
		{"text-unmarshaled struct", "window: \"22:00-02:00\"\n", []string{"window: missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s sample
			err := Decode([]byte(strings.Replace(full, tt.drop, "", 1)), &s)
			for _, w := range tt.want {
				if err == nil || !strings.Contains(err.Error(), w) {
					t.Errorf("error = %v, want it to contain %q", err, w)
				}
			}
		})
	}

	var s sample
	err := Decode([]byte(strings.Replace(full, "nested: {timeout: 1s, count: 7}", "nested: {count: 7}", 1)), &s)
	if err == nil || !strings.Contains(err.Error(), "nested.timeout: missing") {
		t.Errorf("missing nested field: error = %v", err)
	}
}

func TestDecode_EmptyDocumentReportsEverything(t *testing.T) {
	var s sample
	err := Decode(nil, &s)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"name", "max", "seed", "rate", "timeout", "count", "nested", "window", "log"} {
		if !strings.Contains(err.Error(), key+": missing") {
			t.Errorf("error does not report %s missing: %v", key, err)
		}
	}
	if strings.Contains(err.Error(), "token") {
		t.Errorf("omitempty token reported missing: %v", err)
	}
}

func TestDecode_Errors(t *testing.T) {
	tests := []struct {
		name, doc, want string
	}{
		{"unknown key", full + "nmae: x", "field nmae not found"},
		{"duplicate key", full + "max: 2", "already defined"},
		{"malformed", "max: [1", "did not find"},
		{"float for int", strings.Replace(full, "max: 5", "max: 2.5", 1), `max: invalid value "2.5": must be an integer`},
		{"float for int64", strings.Replace(full, "seed: 0", "seed: 1.0", 1), "seed: invalid value"},
		{"float for inline int", strings.Replace(full, "count: 1\n", "count: 1.5\n", 1), "count: invalid value"},
		{"float for nested int", strings.Replace(full, "count: 7", "count: 1.5", 1), "nested.count: invalid value"},
		{"int for duration", strings.Replace(full, "timeout: 2s", "timeout: 5", 1), "time.Duration"},
		{"unknown log level", strings.Replace(full, "DEBUG", "loud", 1), "unknown name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s sample
			err := Decode([]byte(tt.doc), &s)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Decode error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestDecode_FloatInIntMap(t *testing.T) {
	var s struct {
		Sizes map[string]int `yaml:"sizes"`
	}
	if err := Decode([]byte("sizes: {a: 1, b: 0}"), &s); err != nil || s.Sizes["a"] != 1 {
		t.Fatalf("Decode = %v, %v; want sizes decoded", s.Sizes, err)
	}
	err := Decode([]byte("sizes: {a: 1, b: 0.5}"), &s)
	if want := `sizes.b: invalid value "0.5": must be an integer`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Decode error = %v, want it to contain %q", err, want)
	}
}

func TestPath(t *testing.T) {
	tests := []struct {
		env, override, want string
	}{
		{"", "", "files/etc/app/development/config.yaml"},
		{"development", "", "files/etc/app/development/config.yaml"},
		{"staging", "", "/etc/app/staging/config.yaml"},
		{"production", "", "/etc/app/production/config.yaml"},
		{"production", "/config/app.yaml", "/config/app.yaml"},
		{"development", " /tmp/dev.yaml ", "/tmp/dev.yaml"},
	}
	for _, tt := range tests {
		got, err := Path("app", tt.env, tt.override)
		if err != nil || got != tt.want {
			t.Errorf("Path(%q, %q) = %q, %v; want %q", tt.env, tt.override, got, err, tt.want)
		}
	}

	if _, err := Path("app", "qa", "/config/app.yaml"); err == nil || !strings.Contains(err.Error(), `APP_ENV "qa"`) {
		t.Errorf("Path(qa) error = %v, want an APP_ENV error even with an override", err)
	}
}

func TestValidator(t *testing.T) {
	v := &Validator{}
	if v.Err() != nil {
		t.Fatal("empty validator should have no error")
	}
	v.Check(true, "a", "fine")
	v.Check(false, "b", "must be set")
	v.Positive("c", 0)
	v.Positive("d", time.Second)
	v.Add(fmt.Errorf("e: custom"))

	got := v.Err().Error()
	for _, want := range []string{"b: must be set", "c: must be greater than 0", "e: custom"} {
		if !strings.Contains(got, want) {
			t.Errorf("errors %q missing %q", got, want)
		}
	}
	for _, unwanted := range []string{"a:", "d:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("errors %q should not contain %q", got, unwanted)
		}
	}
}

func TestSecret_NeverPrintsValue(t *testing.T) {
	s := Secret("hunter2")
	if s.Value() != "hunter2" {
		t.Errorf("Value() = %q", s.Value())
	}
	r := struct{ Password Secret }{s}

	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("cfg", "password", s, "conn", r)
	for name, out := range map[string]string{
		"%v":     fmt.Sprintf("%v", s),
		"Sprint": fmt.Sprint(s),
		"%+v":    fmt.Sprintf("%+v", r),
		"slog":   logs.String(),
	} {
		if strings.Contains(out, "hunter2") {
			t.Errorf("%s leaks the secret: %s", name, out)
		}
	}
	if Secret("").String() != "" {
		t.Error("empty secret should print empty")
	}
}
