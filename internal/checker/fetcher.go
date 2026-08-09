package checker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MaxBody caps the response the fetcher will read. Price pages are usually
// <500 KB; 5 MB is plenty of headroom and prevents a hostile server from
// exhausting memory.
const MaxBody = 5 * 1024 * 1024

// DefaultUserAgent identifies the service to remote sites. Some refuse
// requests without a UA; sites that block headless bots typically block
// common UA strings, so this one is distinct.
const DefaultUserAgent = "price-checker/0.1 (+https://github.com/simons-agent-space/price-checker)"

// Fetcher wraps net/http with a per-request timeout, a User-Agent, and a
// body size cap. One struct per service; reuse across checks.
type Fetcher struct {
	client *http.Client
	ua     string
}

// NewFetcher returns a Fetcher with a 10s timeout and the default UA.
func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{Timeout: 10 * time.Second},
		ua:     DefaultUserAgent,
	}
}

// Fetch issues a GET and returns the body. ctx bounds the caller's intent
// (e.g. the scheduler's per-iteration timeout). Returns an error on any
// non-2xx status; the body is not read on error.
func (f *Fetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", f.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}
