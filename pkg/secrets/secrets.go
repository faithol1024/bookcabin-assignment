// Package secrets fills ${secret:/path} placeholders in YAML from a Source.
// Source can be a secret manager, a file, or an environment variable.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Source interface {
	Secret(ctx context.Context, path string) (string, error)
}

const (
	prefix = "${secret:"
	suffix = "}"
)

var pathPattern = regexp.MustCompile(`^/[a-z0-9_-]+(/[a-z0-9_-]+)*$`)

// Render replaces placeholders in the parsed tree, so values can't alter the YAML structure.
func Render(ctx context.Context, doc []byte, src Source) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if errs := fill(ctx, &root, src); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	if root.Kind == 0 { // empty document
		return doc, nil
	}
	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	return out, nil
}

// fill resolves every placeholder scalar under n and returns all failures.
func fill(ctx context.Context, n *yaml.Node, src Source) []error {
	if n.Kind != yaml.ScalarNode {
		var errs []error
		for _, c := range n.Content {
			errs = append(errs, fill(ctx, c, src)...)
		}
		return errs
	}
	if !strings.Contains(n.Value, prefix) {
		return nil
	}
	path, ok := placeholderPath(n.Value)
	if !ok {
		return []error{fmt.Errorf("line %d: malformed secret placeholder %q (want the whole value to be ${secret:/path})", n.Line, n.Value)}
	}
	v, err := src.Secret(ctx, path)
	if err != nil {
		return []error{fmt.Errorf("secret %s: %w", path, err)}
	}
	n.Tag = "!!str"
	n.Style = yaml.DoubleQuotedStyle
	n.Value = v
	return nil
}

func placeholderPath(v string) (string, bool) {
	if !strings.HasPrefix(v, prefix) || !strings.HasSuffix(v, suffix) {
		return "", false
	}
	path := v[len(prefix) : len(v)-len(suffix)]
	return path, pathPattern.MatchString(path)
}

// EnvSource reads /secret/redis/password from SECRET_REDIS_PASSWORD.
type EnvSource struct{}

// Secret implements Source. An unset or empty variable is an error.
func (EnvSource) Secret(_ context.Context, path string) (string, error) {
	name := envName(path)
	if v := os.Getenv(name); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("not set (env %s)", name)
}

var envNameReplacer = strings.NewReplacer("/", "_", "-", "_")

func envName(path string) string {
	return strings.ToUpper(envNameReplacer.Replace(strings.TrimPrefix(path, "/")))
}
