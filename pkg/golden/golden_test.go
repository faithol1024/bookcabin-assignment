package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type recorder struct {
	testing.TB
	failed bool
	msg    string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.msg = fmt.Sprintf(format, args...)
}

func (r *recorder) Fatalf(format string, args ...any) { r.Errorf(format, args...) }

func setUpdate(t *testing.T, v bool) {
	old := *update
	*update = v
	t.Cleanup(func() { *update = old })
}

func TestAssert_WritesThenMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "testdata", "x.golden.json")
	value := map[string]int{"a": 1}

	setUpdate(t, true)
	Assert(t, path, value)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "{\n  \"a\": 1\n}\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}

	setUpdate(t, false)
	r := &recorder{}
	Assert(r, path, value)
	if r.failed {
		t.Errorf("Assert on equal value failed: %s", r.msg)
	}
}

func TestAssert_Mismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.golden.json")
	if err := os.WriteFile(path, []byte("{\n  \"a\": 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setUpdate(t, false)
	r := &recorder{}
	Assert(r, path, map[string]int{"a": 2})
	if !r.failed {
		t.Error("Assert on different value passed")
	}
}

func TestAssert_MissingFile(t *testing.T) {
	setUpdate(t, false)
	r := &recorder{}
	Assert(r, filepath.Join(t.TempDir(), "missing.golden.json"), 1)
	if !r.failed {
		t.Error("Assert on missing file passed")
	}
}
