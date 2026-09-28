package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
)

const (
	ratesPath = "/rates"
	// maxResponseBytes bounds what a misbehaving upstream can make us buffer.
	maxResponseBytes = 1 << 20
)

type Client struct {
	http    *http.Client
	baseURL string
}

// New calls the rates API at baseURL; httpClient must not set Timeout, deadlines come from ctx.
func New(httpClient *http.Client, baseURL string) *Client {
	return &Client{http: httpClient, baseURL: baseURL}
}

type rateResponse struct {
	Currency string `json:"currency"`
	Rate     int64  `json:"rate"`
}

// Rate returns how many whole rupiah one unit of currency buys.
func (c *Client) Rate(ctx context.Context, currency string) (int64, error) {
	params := url.Values{"currency": {currency}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+ratesPath+"?"+params.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("fx: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, fmt.Errorf("fx: %w", ctxErr)
		}
		return 0, fmt.Errorf("fx: %w: %w", apperr.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		return 0, fmt.Errorf("fx: %w: status %d", apperr.ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("fx: %w: status %d", apperr.ErrMalformedResponse, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, fmt.Errorf("fx: %w: %w", apperr.ErrUnavailable, err)
	}
	var out rateResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, fmt.Errorf("fx: %w: %w", apperr.ErrMalformedResponse, err)
	}
	if out.Currency != currency || out.Rate <= 0 {
		return 0, fmt.Errorf("fx: %w: rate %d for %q, asked %q", apperr.ErrMalformedResponse, out.Rate, out.Currency, currency)
	}
	return out.Rate, nil
}
