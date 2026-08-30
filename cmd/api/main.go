package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/models"
	"simlive/internal/utils"
)

func main() {
	godotenv.Load()

	settings := utils.GetSettings()

	// 1. Connect Postgres
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		settings.DBHost, settings.DBUser, settings.DBPassword, settings.DBName, settings.DBPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect DB: %v", err)
	}
	db.AutoMigrate(&models.PlayoutChannel{})

	// 2. Connect Redis
	rdb := redis.NewClient(&redis.Options{Addr: settings.RedisAddr})

	// 3. Init Services
	espxClient := api.NewESPXClient(settings.EspxURL)
	channelHandler := api.NewChannelHandler(db, espxClient)

	// 4. HTTP REST API
	r := gin.Default()

	// CORS: allow any browser origin to call the REST + HLS endpoints.
	// This is safe for a local simulated-live server (no auth cookies are
	// issued). We reflect the request Origin and set `Vary: Origin` so the
	// response is cacheable per-origin. Preflight (OPTIONS) is short-
	// circuited with 204 so the API + /live/* routes accept CORS requests
	// from any origin (e.g. the index.html player running on a different
	// port). `gin-contrib/cors` is intentionally NOT used to avoid pulling
	// in another dependency -- the behaviour we need is just a few headers.
	r.Use(corsMiddleware())

	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())

	// Serve the built Vue 3 + Video.js player from client/dist/.
	// The build emits an index.html + hashed assets under /assets/*. We
	// expose both on the API server so the player can be loaded from
	// http://localhost:8080/ during local verification (the dev server
	// is normally on :5173 with a /live proxy, but for "double-click and
	// watch" the API alone should be enough).
	clientDistDir := "./client/dist"

	// /assets/* → hashed JS / CSS emitted by `vite build` (e.g.
	// /assets/index-CbYoz8QZ.js, /assets/index-Dij7FH0h.css). Without
	// this the index.html would reference broken /assets/... URLs.
	r.Static("/assets", filepath.Join(clientDistDir, "assets"))

	// /favicon.ico → 204 (browsers always probe for it; we don't ship one).
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// API routes
	apiGroup := r.Group("/api")
	{
		apiGroup.POST("/channels", channelHandler.RegisterChannel)
	}

	// HLS Live Streaming routes (M1.5: shared segment cache, 3 rung playlists)
	liveGroup := r.Group("/live")
	{
		// M1.5: serve the *master* playlist for the channel. The engine
		// writes the master (with one #EXT-X-STREAM-INF per active rung)
		// to simlive:hls:{ch} on every tick so a player reload never gets
		// a 404 here. Falls back to the on-disk {TMP_DIR}/{ch}/playlist.m3u8
		// when Redis is cold (e.g. immediately after a fresh restart).
		liveGroup.GET("/:channel_id/playlist.m3u8", func(c *gin.Context) {
			chID := c.Param("channel_id")

			if strings.Contains(chID, "..") || strings.Contains(chID, "/") {
				c.String(http.StatusBadRequest, "Invalid channel ID")
				return
			}

			playlist, err := rdb.Get(c, "simlive:hls:"+chID).Result()
			if err == nil && len(playlist) > 0 {
				c.Header("Content-Type", "application/x-mpegURL")
				c.Header("Cache-Control", "no-cache")
				c.String(http.StatusOK, playlist)
				return
			}
			// Redis-cold fallback: serve the on-disk master the engine
			// writes every tick.
			diskPath := filepath.Join(tmpDir, chID, "playlist.m3u8")
			if _, statErr := os.Stat(diskPath); statErr == nil {
				c.Header("Content-Type", "application/x-mpegURL")
				c.File(diskPath)
				return
			}
			c.String(http.StatusNotFound, "Stream not active or channel disabled")
		})

		// M1.5: serve a *rung* media playlist (e.g. /live/{ch}/playlist_360p.m3u8).
		// The engine writes one media playlist per active rung in the channel
		// root (playlist_360p.m3u8, playlist_720p.m3u8, playlist_1080p.m3u8),
		// each referencing the same shared seg_N.ts files.
		// Route order matters in Gin: this MUST be registered BEFORE the
		// 2-param /:channel_id/:segment route or the segment route will
		// shadow it (Gin's radix tree).
		liveGroup.GET("/:channel_id/playlist_:rendition", func(c *gin.Context) {
			chID := c.Param("channel_id")
			rendRaw := c.Param("rendition")

			if strings.Contains(chID, "..") || strings.Contains(chID, "/") ||
				strings.Contains(rendRaw, "..") || strings.Contains(rendRaw, "/") {
				c.String(http.StatusBadRequest, "Invalid path")
				return
			}
			// rendRaw arrives WITHOUT the ".m3u8" suffix (Gin strips it via
			// the `:rendition` wildcard before the literal ".m3u8"). Accept
			// either "360p" or "360p.m3u8" defensively.
			rend := strings.TrimSuffix(rendRaw, ".m3u8")
			// Must be a known ladder rung.
			switch rend {
			case "360p", "720p", "1080p":
			default:
				c.String(http.StatusNotFound, "Unknown rendition")
				return
			}

			playlist, err := rdb.Get(c, "simlive:hls:"+chID+":rendition:"+rend).Result()
			if err == nil && len(playlist) > 0 {
				c.Header("Content-Type", "application/x-mpegURL")
				c.Header("Cache-Control", "no-cache")
				c.String(http.StatusOK, playlist)
				return
			}
			// Fallback: serve from disk if the engine has written it locally.
			diskPath := filepath.Join(tmpDir, chID, "playlist_"+rend+".m3u8")
			if _, statErr := os.Stat(diskPath); statErr == nil {
				c.Header("Content-Type", "application/x-mpegURL")
				c.File(diskPath)
				return
			}
			c.String(http.StatusNotFound, "Rendition not active or channel disabled")
		})

		// M1.5: serve a shared-cache .ts segment. The engine writes a
		// SINGLE flat set of seg_N.ts files in {TMP_DIR}/{ch}/ -- all
		// rung playlists (360p/720p/1080p) point to the SAME files.
		liveGroup.GET("/:channel_id/:segment", func(c *gin.Context) {
			channelID := c.Param("channel_id")
			segmentFile := c.Param("segment")

			if strings.Contains(channelID, "..") || strings.Contains(channelID, "/") ||
				strings.Contains(segmentFile, "..") || strings.Contains(segmentFile, "/") {
				c.String(http.StatusBadRequest, "Invalid path")
				return
			}

			if !strings.HasSuffix(segmentFile, ".ts") {
				c.String(http.StatusNotFound, "Invalid segment format")
				return
			}

			filePath := filepath.Join(tmpDir, channelID, segmentFile)
			c.File(filePath)
		})
	}

	r.GET("/legacy/:segment", func(c *gin.Context) {
		segmentFile := c.Param("segment")

		if strings.Contains(segmentFile, "..") {
			c.String(http.StatusBadRequest, "Invalid segment path")
			return
		}

		if !strings.HasSuffix(segmentFile, ".ts") {
			c.String(http.StatusNotFound, "Invalid segment format")
			return
		}

		c.File(filepath.Join(tmpDir, segmentFile))
	})

	r.GET("/", func(c *gin.Context) {
		// Serve the production Vue build. Built by `npm run build`
		// inside ./client; emits ./client/dist/index.html + ./client/dist/assets/*.
		// The matching /assets/* route is registered above so the
		// hashed JS / CSS referenced by this HTML resolve correctly.
		c.File(filepath.Join(clientDistDir, "index.html"))
	})

	log.Println("🚀 SimLive Engine Server running on http://localhost:8080")
	r.Run(":8080")
}

// corsMiddleware returns a Gin middleware that enables CORS for any
// origin. It writes the standard CORS response headers and short-circuits
// preflight (OPTIONS) requests with 204. We intentionally do NOT use
// `gin-contrib/cors` to avoid pulling in another dependency -- the
// behaviour we need (allow any origin, common methods, no credentials) is
// just a few headers.
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			// Non-browser request (curl, server-to-server) -- nothing to do.
			c.Next()
			return
		}

		headers := c.Writer.Header()
		headers.Set("Access-Control-Allow-Origin", origin)
		headers.Set("Vary", "Origin")
		headers.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		headers.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With")
		headers.Set("Access-Control-Expose-Headers", "Content-Length, Content-Type")
		headers.Set("Access-Control-Max-Age", "600")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
