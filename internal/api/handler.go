package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"simlive/internal/models"
)

type ChannelHandler struct {
	db   *gorm.DB
	espx *ESPXClient
}

func NewChannelHandler(db *gorm.DB, espx *ESPXClient) *ChannelHandler {
	return &ChannelHandler{db: db, espx: espx}
}

// POST /api/channels
func (h *ChannelHandler) RegisterChannel(c *gin.Context) {
	var req models.CreateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// 1. Validate Channel ID với ESPX API
	detail, err := h.espx.GetChannelDetailWithEPG(req.ChannelID)
	if err != nil || detail.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Channel ID or not found in ESPX API"})
		return
	}

	// 2. Map dữ liệu chuẩn bị lưu DB
	channel := models.PlayoutChannel{
		ID:              detail.ID,
		Name:            detail.Name,
		BaseName:        detail.BaseName,
		ContentTimezone: detail.ContentTimezone,
		ChannelEpgID:    detail.ChannelEpg.ID,
		UpdatedAt:       time.Now(),
	}

	// 3. Upsert vào Database
	err = h.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "base_name", "content_timezone", "channel_epg_id", "is_active", "updated_at"}),
	}).Create(&channel).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save channel to database"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Channel registered/updated successfully",
		"data":    channel,
	})
}
