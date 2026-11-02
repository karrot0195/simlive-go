package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"simlive/internal/partner"
)

// PartnerXMLHandler serves the partner-facing common XML endpoints. It reads
// config/partner.yaml per request so operator edits take effect without a
// restart, and resolves the active profile for both endpoints. db may be nil
// for upstream-only mode (used by tests and deployments that resolve guides
// purely upstream).
type PartnerXMLHandler struct {
	db *gorm.DB
}

// NewPartnerXMLHandler returns a handler backed by db (may be nil).
func NewPartnerXMLHandler(db *gorm.DB) *PartnerXMLHandler {
	return &PartnerXMLHandler{db: db}
}

// activeProfile loads partner.yaml and returns the active profile, or an error
// that callers translate into a 502 partner_unavailable response.
func (h *PartnerXMLHandler) activeProfile() (*partner.PartnerProfile, error) {
	cfg, err := partner.LoadConfig()
	if err != nil {
		return nil, err
	}
	prof, err := cfg.ActiveProfile()
	if err != nil {
		return nil, err
	}
	return &prof, nil
}

// GetChannelListXML serves GET /common/channel-playouts-xml.
func (h *PartnerXMLHandler) GetChannelListXML(c *gin.Context) {
	prof, err := h.activeProfile()
	if err != nil {
		logPartnerError("channel list: config", err)
		writeXMLError(c, http.StatusBadGateway, "partner_unavailable", "Partner configuration unavailable")
		return
	}

	client := partner.NewClient(prof)
	raw, err := client.FetchChannelList(context.Background())
	if err != nil {
		logPartnerError("channel list: upstream", err)
		writeXMLError(c, http.StatusBadGateway, "partner_unavailable", "Partner API unavailable")
		return
	}

	views := make([]partner.ChannelView, 0, len(raw))
	for _, item := range raw {
		if v, ok := partner.NormalizeChannel(item, prof); ok {
			views = append(views, v)
		}
	}

	fields, fieldErrs := partner.ValidCustomFields(prof.XML.Channel.CustomFields)
	for _, e := range fieldErrs {
		logPartnerError("channel list: custom field", e)
	}

	writeXML(c, http.StatusOK, partner.ChannelListDocument(views, fields, false))
}
