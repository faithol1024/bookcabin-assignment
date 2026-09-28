// Package httphandler holds the HTTP handlers, DTOs and problem+json errors.
package httphandler

import (
	"net/http"
)

func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
