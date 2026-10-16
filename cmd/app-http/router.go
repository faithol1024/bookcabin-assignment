package main

import (
	"log/slog"
	"net/http"

	httphandler "github.com/faithol1024/bookcabin-assignment/internal/handler/http"
)

func newRouter(h *httphandler.Handler, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httphandler.Healthz)
	mux.HandleFunc("GET /docs", httphandler.Docs)
	mux.HandleFunc("GET /openapi.yaml", httphandler.OpenAPISpec)
	mux.HandleFunc("POST /v1/flights/search", h.Search)
	mux.HandleFunc("POST /v1/flights/search/stream", h.SearchStream)
	return httphandler.Middleware(mux, logger)
}
