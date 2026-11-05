package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ClientDownload obtains data over HTTP.
type ClientDownload struct {
	endpoint string
	headers  map[string]string
	client   *http.Client
}

// NewClient creates a new ClientDownload.
func NewClient(endpoint string, headers map[string]string, timeout time.Duration) *ClientDownload {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	h := make(map[string]string, len(headers))
	for k, v := range headers {
		h[k] = v
	}
	return &ClientDownload{
		endpoint: endpoint,
		headers:  h,
		client:   &http.Client{Timeout: timeout},
	}
}

// Fetch retrieves data from the configured endpoint.
func (c *ClientDownload) Fetch(ctx context.Context, source string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("http downloader not initialized")
	}
	endpoint := c.endpoint
	if source != "" {
		// If source is provided, prefer it; endpoint may be base
		endpoint = source
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http downloader received status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
