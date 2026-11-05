package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Port      string `json:"port"`
	DataDir   string `json:"data_dir"`
	AuthToken string `json:"auth_token"`
}

func loadConfig() *Config {
	cfg := &Config{
		Port:      getEnv("PARTNER_PORT", "3030"),
		DataDir:   getEnv("PARTNER_DATA_DIR", "./data/partner"),
		AuthToken: getEnv("PARTNER_AUTH_TOKEN", "demo-token"),
	}
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var validTokens = map[string]bool{}

func main() {
	cfg := loadConfig()
	validTokens[cfg.AuthToken] = true

	mux := http.NewServeMux()

	cors := func(w http.ResponseWriter) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
	}

	// Auth endpoints
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Still return token for demo
		}
		// Simple token auth - accept any credentials for demo
		resp := map[string]string{"token": cfg.AuthToken, "id": "demo", "name": "Demo User"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		token := r.Header.Get("Authorization")
		if token == "" {
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			token = req["token"]
		}
		token = strings.TrimPrefix(token, "Bearer ")
		valid := validTokens[token]
		resp := map[string]any{"valid": valid, "id": "demo", "name": "Demo User"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Minimal fixed paths for channels
	mux.HandleFunc("/channels", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		serveFile(w, r, filepath.Join(cfg.DataDir, "channels.json"))
	})
	mux.HandleFunc("/api/v2/common/channel-playouts", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		serveFile(w, r, filepath.Join(cfg.DataDir, "channels.json"))
	})
	mux.HandleFunc("/api/v2/common/channel-playouts/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Return channel detail for any channel ID
		channelID := strings.TrimPrefix(r.URL.Path, "/api/v2/common/channel-playouts/")
		resp := map[string]interface{}{
			"id":               channelID,
			"name":             "Demo Channel " + channelID,
			"base_name":        "demo-" + channelID,
			"content_timezone": "UTC",
			"channelEpg":       map[string]string{"id": channelID + "-epg"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Minimal fixed paths for EPG
	mux.HandleFunc("/epg", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		serveFile(w, r, filepath.Join(cfg.DataDir, "epg.json"))
	})
	mux.HandleFunc("/api/v2/common/channel-epg/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Return dummy schedule data for full day
		epgID := strings.TrimPrefix(r.URL.Path, "/api/v2/common/channel-epg/")
		epgID = strings.TrimSuffix(epgID, "/schedules")
		now := time.Now()
		start := now.Format("2006-01-02 15:04:05.000")
		stop := now.Add(24 * time.Hour).Format("2006-01-02 15:04:05.000")
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":               epgID + "-sched-1",
					"local_date_start": start,
					"local_date_end":   stop,
					"start":            now.Format(time.RFC3339),
					"stop":             now.Add(24 * time.Hour).Format(time.RFC3339),
					"programme": map[string]interface{}{
						"name":    "Demo Programme",
						"episode": "Demo Episode",
						"is_ads":  false,
						"programmeStreams": []map[string]interface{}{
							{"id": "stream-1", "url": "file://" + cfg.DataDir + "/vod.ts"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	// Also support XML EPG endpoint format
	mux.HandleFunc("/common/channel/epg/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Return XML format
		loc := time.Local
		if tz := os.Getenv("TZ"); tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				loc = l
			}
		}
		if loc == time.UTC {
			if l, err := time.LoadLocation("Asia/Saigon"); err == nil {
				loc = l
			}
		}
		now := time.Now().In(loc)
		start := now.Format("20060102150405 -0700")
		stop := now.Add(4 * time.Second).Format("20060102150405 -0700")
		absDataDir, _ := filepath.Abs(cfg.DataDir)
		xml := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <programme start="` + start + `" stop="` + stop + `" is_ads="false">
    <stream stream_id="` + absDataDir + `"/>
    <name>Demo Programme</name>
  </programme>
</tv>`
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(xml))
	})
	// Support JSON schedule endpoint
	mux.HandleFunc("/api/v2/common/channel-epg", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		epgID := strings.TrimPrefix(r.URL.Path, "/api/v2/common/channel-epg/")
		epgID = strings.TrimSuffix(epgID, "/schedules")
		now := time.Now()
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":    epgID + "-sched-1",
					"start": now.Format(time.RFC3339),
					"stop":  now.Add(24 * time.Hour).Format(time.RFC3339),
					"programme": map[string]interface{}{
						"name":    "Demo Programme",
						"episode": "Demo Episode",
						"programmeStreams": []map[string]interface{}{
							{"id": "stream-1", "url": "file://" + cfg.DataDir + "/vod.ts"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Data download endpoint for communication data
	mux.HandleFunc("/data/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		filename := strings.TrimPrefix(r.URL.Path, "/data/")
		if filename == "" || strings.Contains(filename, "..") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		serveFile(w, r, filepath.Join(cfg.DataDir, filename))
	})

	addr := ":" + cfg.Port
	log.Printf("Partner demo server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
