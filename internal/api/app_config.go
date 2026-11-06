package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"simlive/internal/partner"
)

// GetAppConfig serves app-level configuration sourced from {ROOT}/partner.yaml.
func GetAppConfig(c *gin.Context) {
	adminID := ""
	if cfg, err := partner.LoadConfig(); err == nil && cfg != nil {
		adminID = strings.TrimSpace(cfg.AdminID)
	}
	c.JSON(200, gin.H{"admin_id": adminID})
}