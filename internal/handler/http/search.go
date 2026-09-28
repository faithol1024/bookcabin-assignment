package httphandler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

type Searcher interface {
	Search(ctx context.Context, c search.Criteria) (search.Response, error)
}

type Handler struct {
	search Searcher
	limits Limits
}

func New(s Searcher, limits Limits) *Handler {
	return &Handler{search: s, limits: limits}
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	req, err := decodeSearch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	c, err := req.criteria(h.limits)
	if err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.search.Search(r.Context(), c)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, l := range res.Legs {
		for _, e := range l.ProviderErrors {
			slog.WarnContext(r.Context(), "provider failed",
				"provider", e.Provider, "error", e.Code, "attempts", e.Attempts, "origin", l.Origin, "destination", l.Destination, "date", l.Date)
		}
	}
	writeJSON(w, http.StatusOK, newResponse(c, res))
}
