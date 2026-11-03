package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/partner"
	"simlive/internal/utils"
)

// PartnerClient is the admin-facing wrapper over the upstream partner API.
// Base URL, timeout, request headers, and endpoint paths are delegated to
// internal/partner so the same configuration drives both the admin routes and
// the partner-facing common XML source.
type PartnerClient struct {
	baseURL    string
	profile    *partner.PartnerProfile
	httpClient *http.Client
}

func NewPartnerClient(baseURL string) *PartnerClient {
	profile := partner.ProfileFromBaseURL(baseURL)
	if cfg, err := partner.LoadConfig(); err == nil {
		if p, err := cfg.ActiveProfile(); err == nil {
			if p.API.BaseURL == "" {
				p.API.BaseURL = profile.API.BaseURL
			}
			profile = &p
		}
	}
	return &PartnerClient{
		baseURL:    profile.API.BaseURL,
		profile:    profile,
		httpClient: &http.Client{Timeout: profile.API.TimeoutDuration()},
	}
}

// applyHeaders sets the profile's configured headers on the request.
func (c *PartnerClient) applyHeaders(req *http.Request) {
	for k, v := range c.profile.API.Headers {
		req.Header.Set(k, v)
	}
}

// do performs the request with profile headers applied.
func (c *PartnerClient) do(req *http.Request) (*http.Response, error) {
	c.applyHeaders(req)
	return c.httpClient.Do(req)
}

// API 1: Fetch Channel List
func (c *PartnerClient) GetChannels() (*models.PartnerChannelListResp, error) {
	u, err := url.Parse(c.baseURL + c.profile.API.Endpoints.ChannelList)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("$limit", "50")
	q.Set("is_playout_assigned", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result models.PartnerChannelListResp
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

// ListPartnerChannels is a thin wrapper over GetChannels that returns the
// items directly (rather than the wrapped response).
func (c *PartnerClient) ListPartnerChannels() ([]models.PartnerChannelListItem, error) {
	resp, err := c.GetChannels()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []models.PartnerChannelListItem{}, nil
	}
	return resp.Data, nil
}

// API 2: Get Playout Detail to extract channelEpg.id
func (c *PartnerClient) GetChannelEPGID(playoutID string) (string, error) {
	endpoint := strings.ReplaceAll(c.profile.API.Endpoints.ChannelDetail, "{channel_id}", url.PathEscape(playoutID))
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("$eager", "[channelEpg]")
	q.Set("$limit", "50")
	q.Set("is_playout_assigned", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result models.PartnerChannelDetailResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ChannelEpg.ID, nil
}

// API 3b: Get Schedules from the upstream XML EPG endpoint.
func (c *PartnerClient) GetEPGScheduleItemsFromXML(channelID, date, tz string) (*models.PartnerSchedulesResponse, error) {
	u, err := url.Parse(c.baseURL + "/common/channel/epg/" + url.PathEscape(channelID))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("$include_metadata", "true")
	if date != "" {
		q.Set("date", date)
	}
	if tz != "" {
		q.Set("tz", tz)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	type xmlAd struct {
		Path     string `xml:"path,attr"`
		Duration string `xml:"duration,attr"`
		Start    string `xml:"start,attr"`
		Value    string `xml:",chardata"`
	}
	type xmlStream struct {
		StreamID string `xml:"stream_id,attr"`
	}
	type xmlProgramme struct {
		Start   string      `xml:"start,attr"`
		Stop    string      `xml:"stop,attr"`
		IsAds   string      `xml:"is_ads,attr"`
		Ads     []xmlAd     `xml:"ads"`
		Streams []xmlStream `xml:"stream"`
	}
	type xmlTV struct {
		Programmes []xmlProgramme `xml:"programme"`
	}

	var tv xmlTV
	if err := xml.NewDecoder(resp.Body).Decode(&tv); err != nil {
		return nil, err
	}

	// Every timestamp (programme start/stop and ad markers) is normalized to the
	// server's local clock so the planner walks one timeline frame.
	loc := utils.LocalLocation()

	resp2 := &models.PartnerSchedulesResponse{Date: date, TotalItems: len(tv.Programmes)}
	for _, p := range tv.Programmes {
		start, err := time.Parse(configs.PARTNER_XML_TIME_LAYOUT, p.Start)
		if err != nil {
			continue
		}
		stop, err := time.Parse(configs.PARTNER_XML_TIME_LAYOUT, p.Stop)
		if err != nil {
			continue
		}
		startL := start.In(loc)
		stopL := stop.In(loc)

		var streams []models.PartnerProgrammeStream
		for _, s := range p.Streams {
			// stream_id is the bucket-inclusive storage path (rclone
			// remote:bucket/...); the planner uses it verbatim.
			streams = append(streams, models.PartnerProgrammeStream{
				StreamID:      s.StreamID,
				StreamPath:    "",
				SchemaVersion: 3,
				IsVod:         true,
				Name:          "Default Stream",
			})
		}

		var ads []models.PartnerAdBreak
		isAds := p.IsAds == "true" || p.IsAds == "1"
		for _, a := range p.Ads {
			// Text-only <ads>true</ads> designates the whole programme as an ad.
			if strings.TrimSpace(a.Value) == "true" {
				isAds = true
				continue
			}
			dur, _ := strconv.Atoi(a.Duration)
			start := a.Start
			// Re-stamp the ad marker on the same clock as the programme window;
			// the upstream offset is only meaningful next to its own frame.
			if adStart, err := time.Parse(configs.PARTNER_XML_TIME_LAYOUT, a.Start); err == nil {
				start = adStart.In(loc).Format(configs.PARTNER_XML_TIME_LAYOUT)
			} else if a.Start == "" {
				// no start provided; skip malformed marker
				continue
			}
			ads = append(ads, models.PartnerAdBreak{Path: a.Path, Duration: dur, Start: start})
		}

		item := models.PartnerScheduleItem{
			ID:             "",
			LocalDateStart: startL.Format("2006-01-02 15:04:05.000"),
			LocalDateEnd:   stopL.Format("2006-01-02 15:04:05.000"),
		}
		item.Programme.Name = ""
		item.Programme.ProgrammeStreams = streams
		item.Programme.Ads = ads
		item.Programme.IsAds = isAds
		if len(ads) > 0 {
			log.Printf("[partner] ads=%d", len(ads))
		}
		resp2.Data = append(resp2.Data, item)
	}
	return resp2, nil
}

// API 3: Get Schedules
func (c *PartnerClient) GetEPGScheduleItems(epgID, date, tz string) (*models.PartnerSchedulesResponse, error) {
	endpoint := strings.ReplaceAll(c.profile.API.Endpoints.Schedule, "{epg_id}", url.PathEscape(epgID))
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	for k, v := range c.profile.API.ScheduleQuery {
		q.Set(k, v)
	}
	q.Set("$date", date)
	q.Set("tz", tz)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result models.PartnerSchedulesResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

type PartnerChannelDetailFullResp struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	BaseName        string `json:"base_name"`
	ContentTimezone string `json:"content_timezone"`
	ChannelEpg      struct {
		ID string `json:"id"`
	} `json:"channelEpg"`
}

func (c *PartnerClient) GetChannelDetailWithEPG(channelID string) (*PartnerChannelDetailFullResp, error) {
	endpoint := strings.ReplaceAll(c.profile.API.Endpoints.ChannelDetail, "{channel_id}", url.PathEscape(channelID))
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("$eager", "[channelEpg]")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("channel not found, status: %d", resp.StatusCode)
	}

	var result PartnerChannelDetailFullResp
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}
