package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"simlive/internal/models"
	"time"
)

type ESPXClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewESPXClient(baseURL string) *ESPXClient {
	return &ESPXClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// API 1: Fetch Channel List
func (c *ESPXClient) GetChannels() (*models.ESPXChannelListResp, error) {
	url := fmt.Sprintf("%s/api/v2/common/channel-playouts?$limit=50&is_playout_assigned=true", c.baseURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result models.ESPXChannelListResp
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

// ListESpxChannels is a thin wrapper over GetChannels that returns the
// items directly (rather than the wrapped response). Used by the new
// GET /api/channels/esp REST route — keeps the upstream URL constant
// in one place so a future endpoint rename is a one-line change.
func (c *ESPXClient) ListESpxChannels() ([]models.ESPXChannelListItem, error) {
	resp, err := c.GetChannels()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []models.ESPXChannelListItem{}, nil
	}
	return resp.Data, nil
}

// API 2: Get Playout Detail to extract channelEpg.id
func (c *ESPXClient) GetChannelEPGID(playoutID string) (string, error) {
	url := fmt.Sprintf("%s/api/v2/common/channel-playouts/%s?$eager=[channelEpg]&$limit=50&is_playout_assigned=true", c.baseURL, playoutID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result models.ESPXChannelDetailResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ChannelEpg.ID, nil
}

// API 3: Get Schedules
func (c *ESPXClient) GetSchedules(epgID, date, tz string) (*models.ESPXSchedulesResponse, error) {
	// 1. Parse base URL path
	endpoint := fmt.Sprintf("%s/api/v2/common/channel-epg/%s/schedules", c.baseURL, epgID)
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	// 2. Build Query Parameters safely using url.Values
	q := u.Query()
	q.Set("$eager", "[programme.[programmeStreams]]")
	q.Set("$skip", "0")
	q.Set("$limit", "30000")
	q.Set("$date", date)
	q.Set("tz", tz) // url.Values will safely encode "+07:00" to "%2B07:00"

	u.RawQuery = q.Encode()
	reqURL := u.String()

	log.Printf("[HTTP] Request URL: %s", reqURL)
	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result models.ESPXSchedulesResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

type ESPXChannelDetailFullResp struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	BaseName        string `json:"base_name"`
	ContentTimezone string `json:"content_timezone"`
	ChannelEpg      struct {
		ID string `json:"id"`
	} `json:"channelEpg"`
}

func (c *ESPXClient) GetChannelDetailWithEPG(channelID string) (*ESPXChannelDetailFullResp, error) {
	url := fmt.Sprintf("%s/api/v2/common/channel-playouts/%s?$eager=[channelEpg]", c.baseURL, channelID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("channel not found, status: %d", resp.StatusCode)
	}

	var result ESPXChannelDetailFullResp
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}
