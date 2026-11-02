package partner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrChannelNotFound means the upstream does not know the requested channel.
var ErrChannelNotFound = errors.New("channel not found upstream")

// Client is a config-driven upstream partner API client. Every request derives
// its base URL, timeout, headers, endpoint path, and query parameters from the
// profile so a new partner needs configuration only.
type Client struct {
	profile    *PartnerProfile
	httpClient *http.Client
}

// NewClient builds a Client for the given profile.
func NewClient(profile *PartnerProfile) *Client {
	hc := &http.Client{}
	if profile != nil {
		hc.Timeout = profile.API.TimeoutDuration()
	}
	return &Client{profile: profile, httpClient: hc}
}

// FetchChannelList returns the raw channel records from the upstream channel list.
func (c *Client) FetchChannelList(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.getJSON(ctx, c.profile.API.Endpoints.ChannelList, nil, c.profile.API.ChannelListQuery)
	if err != nil {
		return nil, err
	}
	return decodeDataList(raw), nil
}

// FetchSchedule returns the raw schedule items for the given guide id and day.
func (c *Client) FetchSchedule(ctx context.Context, epgID, date, tz string) ([]map[string]any, error) {
	endpoint := strings.ReplaceAll(c.profile.API.Endpoints.Schedule, "{epg_id}", url.PathEscape(epgID))
	extra := map[string]string{"$date": date, "tz": tz}
	raw, err := c.getJSON(ctx, endpoint, nil, c.profile.API.ScheduleQuery, extra)
	if err != nil {
		return nil, err
	}
	return decodeDataList(raw), nil
}

// FetchChannelDetail resolves the guide id for a channel from the upstream
// detail endpoint. It eager-loads channelEpg so the guide id is present, and
// returns ErrChannelNotFound when the upstream rejects the id (404/422).
func (c *Client) FetchChannelDetail(ctx context.Context, channelID string) (string, error) {
	endpoint := strings.ReplaceAll(c.profile.API.Endpoints.ChannelDetail, "{channel_id}", url.PathEscape(channelID))
	raw, err := c.getJSON(ctx, endpoint, nil, map[string]string{"$eager": "[channelEpg]"})
	if err != nil {
		return "", err
	}
	detail, ok := raw.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected channel detail response")
	}
	id, _ := ResolveSourceString(detail, "channelEpg.id")
	return id, nil
}

// getJSON performs a GET request and decodes the JSON body into any.
func (c *Client) getJSON(ctx context.Context, endpoint string, headers map[string]string, querySets ...map[string]string) (any, error) {
	reqURL, err := c.buildURL(endpoint, querySets...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.profile.API.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Treat 404 and 422 as "unknown channel" so the handler can return a
		// 404 rather than a spurious 502 upstream failure.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity {
			return nil, fmt.Errorf("%w: status %d for %s", ErrChannelNotFound, resp.StatusCode, endpoint)
		}
		return nil, fmt.Errorf("upstream returned status %d for %s", resp.StatusCode, endpoint)
	}

	var out any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// buildURL joins the base URL with the endpoint and merges query sets.
func (c *Client) buildURL(endpoint string, querySets ...map[string]string) (string, error) {
	base := c.profile.API.BaseURL
	if base == "" {
		base = defaultAPIBaseURL
	}
	u, err := url.Parse(strings.TrimRight(base, "/") + endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for _, set := range querySets {
		for k, v := range set {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// decodeDataList extracts the `data` array from a wrapped upstream response.
func decodeDataList(raw any) []map[string]any {
	obj, ok := raw.(map[string]any)
	if !ok {
		return []map[string]any{}
	}
	items, ok := obj["data"].([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
