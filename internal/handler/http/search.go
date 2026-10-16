package httphandler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

type Searcher interface {
	Search(ctx context.Context, c search.Criteria) (search.Response, error)
	Stream(ctx context.Context, c search.Criteria, emit func(search.Event)) error
}

type Handler struct {
	search Searcher
	limits Limits
}

func New(s Searcher, limits Limits) *Handler {
	return &Handler{search: s, limits: limits}
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	c, ok := h.criteria(w, r)
	if !ok {
		return
	}
	res, err := h.search.Search(r.Context(), c)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, l := range res.Legs {
		logProviderErrors(r.Context(), l)
	}
	writeJSON(w, http.StatusOK, newResponse(c, res))
}

// criteria decodes and validates the request body, writing the problem response when it fails.
func (h *Handler) criteria(w http.ResponseWriter, r *http.Request) (search.Criteria, bool) {
	req, err := decodeSearch(r)
	if err != nil {
		writeError(w, r, err)
		return search.Criteria{}, false
	}
	c, err := req.criteria(h.limits)
	if err != nil {
		writeError(w, r, err)
		return search.Criteria{}, false
	}
	return c, true
}

func logProviderErrors(ctx context.Context, l search.Leg) {
	for _, e := range l.ProviderErrors {
		slog.WarnContext(ctx, "provider failed",
			"provider", e.Provider, "error", e.Code, "attempts", e.Attempts, "origin", l.Origin, "destination", l.Destination, "date", l.Date)
	}
}
