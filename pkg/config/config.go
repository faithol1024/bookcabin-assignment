// Package config holds app-agnostic config helpers: file location, strict YAML decoding,
// validation and common sections.
package config

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Path returns the config file of app for appEnv (default development); a non-empty override wins.
func Path(app, appEnv, override string) (string, error) {
	appEnv = strings.TrimSpace(appEnv)
	if appEnv == "" {
		appEnv = EnvDevelopment
	}
	switch appEnv {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		return "", fmt.Errorf("APP_ENV %q: must be %s, %s or %s", appEnv, EnvDevelopment, EnvStaging, EnvProduction)
	}
	if o := strings.TrimSpace(override); o != "" {
		return o, nil
	}
	if appEnv == EnvDevelopment {
		return fmt.Sprintf("files/etc/%s/%s/config.yaml", app, appEnv), nil // repo-relative, for local runs
	}
	return fmt.Sprintf("/etc/%s/%s/config.yaml", app, appEnv), nil
}

// Decode strictly decodes YAML onto out. Every field is required unless its yaml tag has omitempty.
func Decode(data []byte, out any) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	t := reflect.TypeOf(out).Elem()
	errs := append(checkRequired(&root, t, ""), checkIntegers(&root, t, "")...)
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Validator collects every problem instead of stopping at the first.
type Validator struct{ errs []error }

func (v *Validator) Check(ok bool, name, msg string) {
	if !ok {
		v.errs = append(v.errs, fmt.Errorf("%s: %s", name, msg))
	}
}

func (v *Validator) Positive(name string, d time.Duration) {
	v.Check(d > 0, name, "must be greater than 0")
}

func (v *Validator) Add(errs ...error) { v.errs = append(v.errs, errs...) }

func (v *Validator) Err() error { return errors.Join(v.errs...) }

type Log struct {
	Level slog.Level `yaml:"level"`
}

// Secret is a sensitive string that never prints its value.
type Secret string

func (s Secret) Value() string { return string(s) }

func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}

func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalText redacts the value in encoders (JSON, slog structs).
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

var (
	durationType    = reflect.TypeFor[time.Duration]()
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// checkRequired reports every non-omitempty struct field whose key is missing.
func checkRequired(n *yaml.Node, t reflect.Type, path string) []error {
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	if t.Kind() != reflect.Struct || reflect.PointerTo(t).Implements(textUnmarshaler) {
		return nil // scalar-like (e.g. parsed from text)
	}
	if n.Kind != yaml.MappingNode {
		n = &yaml.Node{Kind: yaml.MappingNode} // empty document: everything is missing
	}
	keys := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys[n.Content[i].Value] = n.Content[i+1]
	}

	var errs []error
	for f := range t.Fields() {
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if opts == "inline" {
			errs = append(errs, checkRequired(n, f.Type, path)...)
			continue
		}
		full := strings.TrimPrefix(path+"."+name, ".")
		val, ok := keys[name]
		switch {
		case ok:
			errs = append(errs, checkRequired(val, f.Type, full)...)
		case !strings.Contains(opts, "omitempty"):
			errs = append(errs, fmt.Errorf("%s: missing", full))
		}
	}
	return errs
}

// checkIntegers rejects floats for int fields; yaml.v3 silently truncates them.
func checkIntegers(n *yaml.Node, t reflect.Type, path string) []error {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 1 {
			return checkIntegers(n.Content[0], t, path)
		}
	case yaml.MappingNode:
		if t.Kind() != reflect.Struct && t.Kind() != reflect.Map {
			return nil
		}
		var errs []error
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i].Value, n.Content[i+1]
			if t.Kind() == reflect.Map {
				errs = append(errs, checkIntegers(val, t.Elem(), strings.TrimPrefix(path+"."+key, "."))...)
			} else if f, ok := fieldByKey(t, key); ok {
				errs = append(errs, checkIntegers(val, f.Type, strings.TrimPrefix(path+"."+key, "."))...)
			}
		}
		return errs
	case yaml.ScalarNode:
		isInt := t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64 && t != durationType
		if isInt && n.Tag == "!!float" {
			return []error{fmt.Errorf("%s: invalid value %q: must be an integer", path, n.Value)}
		}
	}
	return nil
}

// fieldByKey finds the field for a yaml key, looking through ",inline" fields.
func fieldByKey(t reflect.Type, key string) (reflect.StructField, bool) {
	for f := range t.Fields() {
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if opts == "inline" {
			if inner, ok := fieldByKey(f.Type, key); ok {
				return inner, true
			}
			continue
		}
		if name == key {
			return f, true
		}
	}
	return reflect.StructField{}, false
}
