// Package golden compares test output with JSON golden files; go test -update rewrites them.
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Assert compares got, encoded as indented JSON, with the file at path.
func Assert(t testing.TB, path string, got any) {
	t.Helper()
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("golden: encode: %v", err)
	}
	b = append(b, '\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden: %v", err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: %v (run go test -update to create it)", err)
	}
	if !bytes.Equal(b, want) {
		t.Errorf("golden: %s differs (run go test -update to accept)\n got:\n%s\nwant:\n%s", path, b, want)
	}
}
