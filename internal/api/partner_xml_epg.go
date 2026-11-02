package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"simlive/internal/models"
	"simlive/internal/partner"
	"simlive/internal/utils"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// GetEPGXML serves GET /common/channel/epg/:channelId.
func (h *PartnerXMLHandler) GetEPGXML(c *gin.Context) {
	channelID := c.Param("channelId")
	if strings.Contains(channelID, "..") || strings.Contains(channelID, "/") {
		writeXMLError(c, http.StatusBadRequest, "invalid_channel_id", "Invalid channel id")
		return
	}

	date, ok := resolveEPGDate(c)
	if !ok {
		writeXMLError(c, http.StatusBadRequest, "invalid_date", "Invalid date")
		return
	}

	prof, err := h.activeProfile()
	if err != nil {
		logPartnerError("epg: config", err)
		writeXMLError(c, http.StatusBadGateway, "partner_unavailable", "Partner configuration unavailable")
		return
	}

	client := partner.NewClient(prof)
	epgID, found, err := h.resolveGuideID(client, channelID)
	if err != nil {
		logPartnerError("epg: guide lookup", err)
		writeXMLError(c, http.StatusBadGateway, "partner_unavailable", "Partner API unavailable")
		return
	}
	if !found {
		writeXMLError(c, http.StatusNotFound, "channel_not_found", "Channel not found")
		return
	}

	contentTimezone := h.channelTimezone(channelID)
	raw, err := client.FetchSchedule(context.Background(), epgID, date, utils.LocalTZOffset())
	if err != nil {
		logPartnerError("epg: schedule", err)
		writeXMLError(c, http.StatusBadGateway, "partner_unavailable", "Partner API unavailable")
		return
	}

	includeMetadata := wantsMetadata(c)
	items := make([]partner.EPGItemView, 0, len(raw))
	for _, item := range raw {
		if v, ok := partner.NormalizeEPGItem(item, channelID, contentTimezone); ok {
			items = append(items, v)
		}
	}

	fields, fieldErrs := partner.ValidCustomFields(prof.XML.EPG.CustomFields)
	for _, e := range fieldErrs {
		logPartnerError("epg: custom field", e)
	}

	writeXML(c, http.StatusOK, partner.EPGDocument(channelID, items, fields, includeMetadata))
}

// resolveGuideID resolves the upstream guide id: local playout_channels first,
// then the upstream channel detail. Returns found=false when neither resolves.
func (h *PartnerXMLHandler) resolveGuideID(client *partner.Client, channelID string) (string, bool, error) {
	// Defensive: h.db is nil in upstream-only mode.
	if h.db != nil {
		var ch models.PlayoutChannel
		if err := h.db.Where("id = ?", channelID).First(&ch).Error; err == nil {
			if ch.ChannelEpgID != "" {
				return ch.ChannelEpgID, true, nil
			}
		}
	}

	epgID, err := client.FetchChannelDetail(context.Background(), channelID)
	if err != nil {
		// An upstream 404/422 means "unknown channel", not "partner down".
		if errors.Is(err, partner.ErrChannelNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if epgID == "" {
		return "", false, nil
	}
	return epgID, true, nil
}

// channelTimezone returns the channel's content timezone, or "" for host-local.
func (h *PartnerXMLHandler) channelTimezone(channelID string) string {
	if h.db == nil {
		return ""
	}
	var ch models.PlayoutChannel
	if err := h.db.Where("id = ?", channelID).First(&ch).Error; err != nil {
		return ""
	}
	return ch.ContentTimezone
}

// resolveEPGDate reads date/$date, defaulting to host-local today (YYYY-MM-DD).
func resolveEPGDate(c *gin.Context) (string, bool) {
	value := c.Query("date")
	if value == "" {
		value = c.Query("$date")
	}
	if value == "" {
		return time.Now().In(utils.LocalLocation()).Format("2006-01-02"), true
	}
	if !datePattern.MatchString(value) {
		return "", false
	}
	return value, true
}

// wantsMetadata reports whether $include_metadata requests optional metadata.
// Only "true"/"1" (case-insensitive) enable it; anything else excludes it.
func wantsMetadata(c *gin.Context) bool {
	v := c.Query("$include_metadata")
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1":
		return true
	default:
		return false
	}
}
