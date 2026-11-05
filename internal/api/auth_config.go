package api

import (
	"os"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"simlive/internal/partner"
	"simlive/internal/utils"
)

// defaultAuthConfig is served when the auth config file is missing or invalid.
var defaultAuthConfig = map[string]any{
	"steps": []map[string]any{
		{
			"title":          "Login",
			"fields":         []map[string]any{{"name": "username", "type": "text", "label": "Username", "required": true}, {"name": "password", "type": "password", "label": "Password", "required": true}},
			"api":            "http://localhost:3030/api/v2/sso/auth/login",
			"method":         "POST",
			"headers":        map[string]string{"Content-Type": "application/json"},
			"body":           map[string]string{"username": "{{username}}", "password": "{{password}}"},
			"response_field": "access_token",
		},
	},
}

// legacyAuthPath is the pre-migration auth file, read-only fallback.
const legacyAuthPath = "./config/auth.yaml"

// LoadAuthConfig returns the auth flow served by GET /api/auth/config.
//
// Resolution order:
//  1. AUTH_CONFIG_PATH override (if set and readable).
//  2. The active profile's `auth` block from {ROOT}/partner.yaml.
//  3. Legacy config/auth.yaml (read-only).
//  4. The built-in default.
//
// Every degradation from (2) is logged for operators.
func LoadAuthConfig() (map[string]any, error) {
	if path := os.Getenv("AUTH_CONFIG_PATH"); path != "" {
		if cfg, ok := readAuthFile(path); ok {
			return cfg, nil
		}
	}

	if cfg, ok := authFromPartnerConfig(); ok {
		return cfg, nil
	}

	if cfg, ok := readAuthFile(legacyAuthPath); ok {
		utils.GetLogger("AUTH").Warn("auth: serving legacy config/auth.yaml; migrate to {ROOT}/partner.yaml")
		return cfg, nil
	}

	return defaultAuthConfig, nil
}

// GetAuthConfig serves the parsed auth config as JSON.
func GetAuthConfig(c *gin.Context) {
	cfg, err := LoadAuthConfig()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, cfg)
}

// authFromPartnerConfig returns the active profile's auth block when it is usable.
func authFromPartnerConfig() (map[string]any, bool) {
	cfg, err := partner.LoadConfig()
	if err != nil {
		utils.GetLogger("AUTH").Warn("auth: partner.yaml unreadable, falling back", "error", err.Error())
		return nil, false
	}
	prof, err := cfg.ActiveProfile()
	if err != nil {
		utils.GetLogger("AUTH").Warn("auth: active profile unresolved, falling back", "error", err.Error())
		return nil, false
	}
	if _, ok := prof.Auth["steps"]; !ok {
		utils.GetLogger("AUTH").Warn("auth: partner.yaml auth block has no steps, falling back")
		return nil, false
	}
	return prof.Auth, true
}

// readAuthFile reads and parses an auth YAML file; ok is false when it is
// missing, unparseable, or has no usable steps.
func readAuthFile(path string) (map[string]any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		utils.GetLogger("AUTH").Warn("auth: parse failed", "path", path, "error", err.Error())
		return nil, false
	}
	if _, ok := cfg["steps"]; !ok {
		return nil, false
	}
	return cfg, true
}
