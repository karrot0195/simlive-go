package api

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
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

// LoadAuthConfig reads and parses the server-side auth.yaml (or AUTH_CONFIG_PATH).
func LoadAuthConfig() (map[string]any, error) {
	path := os.Getenv("AUTH_CONFIG_PATH")
	if path == "" {
		path = "./config/auth.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultAuthConfig, nil
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse auth config: %w", err)
	}
	if _, ok := cfg["steps"]; !ok {
		return defaultAuthConfig, nil
	}
	return cfg, nil
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
