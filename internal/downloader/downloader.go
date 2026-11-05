package downloader

import (
	"context"
	"fmt"
	"time"

	"simlive/internal/partner"
)

// Downloader obtains partner communication data.
type Downloader interface {
	Fetch(ctx context.Context, source string) ([]byte, error)
}

// New creates a Downloader for the given partner profile.
func New(p *partner.PartnerProfile) (Downloader, error) {
	if p == nil {
		return nil, fmt.Errorf("partner profile is nil")
	}

	provider := p.DataProvider
	if provider == "" {
		provider = "rclone"
	}

	switch provider {
	case "rclone":
		return NewRclone(nil), nil
	case "http":
		if p.Data == nil || p.Data.Endpoint == "" {
			return nil, fmt.Errorf("http data provider requires endpoint")
		}
		timeout := p.API.TimeoutDuration()
		if p.Data != nil && p.Data.Timeout != "" {
			if d, err := time.ParseDuration(p.Data.Timeout); err == nil && d > 0 {
				timeout = d
			}
		}
		headers := map[string]string{}
		if p.Data != nil && p.Data.Headers != nil {
			for k, v := range p.Data.Headers {
				headers[k] = v
			}
		}
		return NewClient(p.Data.Endpoint, headers, timeout), nil
	default:
		return nil, fmt.Errorf("unknown data provider %q", provider)
	}
}
