// Package apperr holds sentinel errors shared across the codebase. It imports only the standard library.
package apperr

import "errors"

var (
	ErrUnavailable        = errors.New("unavailable")
	ErrRateLimited        = errors.New("rate limited")
	ErrMalformedResponse  = errors.New("malformed response")
	ErrInvalidInput       = errors.New("invalid input")
	ErrAllProvidersFailed = errors.New("all providers failed")
)
