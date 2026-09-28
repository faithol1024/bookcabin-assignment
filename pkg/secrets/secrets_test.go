package secrets

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type mapSource map[string]string

func (m mapSource) Secret(_ context.Context, path string) (string, error) {
	if v, ok := m[path]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

func render(t *testing.T, doc string, src Source) map[string]any {
	t.Helper()
	out, err := Render(context.Background(), []byte(doc), src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("rendered yaml does not parse: %v\n%s", err, out)
	}
	return got
}

func TestRender_ReplacesPlaceholders(t *testing.T) {
	doc := `
redis:
  addr: ${secret:/secret/redis/addr}
  password: "${secret:/secret/redis/password}"
  op_timeout: 50ms
hosts:
  - ${secret:/secret/hosts/primary}
  - static.example
`
	got := render(t, doc, mapSource{
		"/secret/redis/addr":     "redis:6379",
		"/secret/redis/password": "p@ss",
		"/secret/hosts/primary":  "10.0.0.1",
	})
	want := map[string]any{
		"redis": map[string]any{"addr": "redis:6379", "password": "p@ss", "op_timeout": "50ms"},
		"hosts": []any{"10.0.0.1", "static.example"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestRender_SecretContentCannotChangeStructure(t *testing.T) {
	nasty := "a: b\n# not a comment\n\"quoted\" 'x' ${secret:/other}\ninjected: true"
	got := render(t, "password: ${secret:/secret/p}\n", mapSource{"/secret/p": nasty})

	if got["password"] != nasty {
		t.Errorf("password = %q, want the exact secret", got["password"])
	}
	if len(got) != 1 {
		t.Errorf("rendered document has keys %v, want only password", got)
	}
}

func TestRender_SecretStaysAStringEvenIfItLooksLikeANumber(t *testing.T) {
	got := render(t, "pin: ${secret:/secret/pin}\n", mapSource{"/secret/pin": "0042"})
	if got["pin"] != "0042" {
		t.Errorf("pin = %#v, want string \"0042\"", got["pin"])
	}
}

func TestRender_NoPlaceholdersUnchanged(t *testing.T) {
	doc := "a: 1\nb:\n  c: [x, y]\n"
	var want map[string]any
	_ = yaml.Unmarshal([]byte(doc), &want)
	if got := render(t, doc, mapSource{}); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestRender_EmptyDocument(t *testing.T) {
	out, err := Render(context.Background(), nil, mapSource{})
	if err != nil || len(out) != 0 {
		t.Errorf("Render(nil) = %q, %v; want empty, nil", out, err)
	}
}

func TestRender_Errors(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{"missing secret", "a: ${secret:/secret/missing}\n", []string{"secret /secret/missing: not found"}},
		{"bad path", "a: ${secret:secret/no-leading-slash}\n", []string{"malformed secret placeholder"}},
		{"uppercase path", "a: ${secret:/Secret/X}\n", []string{"malformed secret placeholder"}},
		{"unterminated", "a: ${secret:/secret/x\n", []string{"malformed secret placeholder"}},
		{"embedded in text", "a: redis://${secret:/secret/x}:6379\n", []string{"line 1: malformed secret placeholder"}},
		{"invalid yaml", "a: [unclosed\n", []string{"parse yaml"}},
		{"all problems reported", "a: ${secret:/secret/one}\nb: ${secret:/secret/two}\nc: x${secret:/secret/three}\n",
			[]string{"/secret/one", "/secret/two", "line 3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Render(context.Background(), []byte(tt.doc), mapSource{"/secret/x": "v"})
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestRender_ErrorsNeverContainSecretValues(t *testing.T) {
	doc := "ok: ${secret:/secret/ok}\nbad: ${secret:/secret/missing}\n"
	_, err := Render(context.Background(), []byte(doc), mapSource{"/secret/ok": "TOPSECRET"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "TOPSECRET") {
		t.Errorf("error leaks a secret value: %v", err)
	}
}

func TestEnvName(t *testing.T) {
	tests := map[string]string{
		"/secret/redis/password":   "SECRET_REDIS_PASSWORD",
		"/secret/redis/addr":       "SECRET_REDIS_ADDR",
		"/secret/fx-rates/conn_v2": "SECRET_FX_RATES_CONN_V2",
	}
	for path, want := range tests {
		if got := envName(path); got != want {
			t.Errorf("envName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestEnvSource(t *testing.T) {
	t.Setenv("SECRET_REDIS_ADDR", "localhost:6379")
	t.Setenv("SECRET_EMPTY", "")
	src := EnvSource{}

	if v, err := src.Secret(context.Background(), "/secret/redis/addr"); err != nil || v != "localhost:6379" {
		t.Errorf("Secret(addr) = %q, %v", v, err)
	}
	for _, path := range []string{"/secret/unset", "/secret/empty"} {
		if _, err := src.Secret(context.Background(), path); err == nil || !strings.Contains(err.Error(), envName(path)) {
			t.Errorf("Secret(%s) error = %v, want one naming %s", path, err, envName(path))
		}
	}
}
