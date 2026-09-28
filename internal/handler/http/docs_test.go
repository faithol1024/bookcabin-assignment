package httphandler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/api"
)

func TestDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	Docs(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, `url: "/openapi.yaml"`) {
		t.Errorf("page does not load /openapi.yaml: %s", body)
	}
}

func TestOpenAPISpec(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPISpec(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if len(api.OpenAPI) == 0 || !bytes.Equal(rec.Body.Bytes(), api.OpenAPI) {
		t.Error("body is not the embedded spec")
	}
}
