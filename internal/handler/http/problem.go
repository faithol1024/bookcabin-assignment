package httphandler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError lists every problem found in a request.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 0 {
		return apperr.ErrInvalidInput.Error()
	}
	return apperr.ErrInvalidInput.Error() + ": " + e.Errors[0].Field + " " + e.Errors[0].Message
}

func (e *ValidationError) Unwrap() error { return apperr.ErrInvalidInput }

func (e *ValidationError) add(field, message string) {
	e.Errors = append(e.Errors, FieldError{Field: field, Message: message})
}

// problem is an RFC 9457 problem detail.
type problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// writeError is the one place errors become HTTP statuses; internal error text never reaches the client.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		tooLarge *http.MaxBytesError
		invalid  *ValidationError
		p        problem
	)
	switch {
	case errors.As(err, &tooLarge):
		p = problem{Type: "payload_too_large", Title: "Payload too large", Status: http.StatusRequestEntityTooLarge, Detail: "The request body is too large."}
	case errors.As(err, &invalid):
		p = problem{Type: "validation_error", Title: "Validation failed", Status: http.StatusBadRequest, Detail: "The request is invalid.", Errors: invalid.Errors}
	case errors.Is(err, apperr.ErrInvalidInput):
		p = problem{Type: "validation_error", Title: "Validation failed", Status: http.StatusBadRequest, Detail: "The request is invalid."}
	case errors.Is(err, apperr.ErrAllProvidersFailed):
		p = problem{Type: "all_providers_failed", Title: "All providers failed", Status: http.StatusBadGateway, Detail: "Every provider failed or timed out."}
	default:
		slog.ErrorContext(r.Context(), "request failed", "error", err)
		p = internalError
	}
	writeProblem(w, r, p)
}

var internalError = problem{Type: "internal_error", Title: "Internal server error", Status: http.StatusInternalServerError, Detail: "An unexpected error occurred."}

func writeProblem(w http.ResponseWriter, r *http.Request, p problem) {
	p.RequestID = RequestID(r.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
