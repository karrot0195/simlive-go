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

	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())

	// API routes
	apiGroup := r.Group("/api")
	{
		apiGroup.POST("/channels", channelHandler.RegisterChannel)
	}

	// HLS Live Streaming routes
	liveGroup := r.Group("/live")
	{
		// Serve playlist for specific channel
		liveGroup.GET("/:channel_id/playlist.m3u8", func(c *gin.Context) {
			chID := c.Param("channel_id")

			if strings.Contains(chID, "..") || strings.Contains(chID, "/") {
				c.String(http.StatusBadRequest, "Invalid channel ID")
				return
			}

			playlist, err := rdb.Get(c, "simlive:hls:"+chID).Result()
			if err != nil || len(playlist) == 0 {
				c.String(http.StatusNotFound, "Stream not active or channel disabled")
				return
			}
			c.Header("Content-Type", "application/x-mpegURL")
			c.Header("Cache-Control", "no-cache")
			c.String(http.StatusOK, playlist)
		})

		// Serve TS segments for a specific channel
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
		c.File("./index.html")
	})

	log.Println("🚀 SimLive Engine Server running on http://localhost:8080")
	r.Run(":8080")
}
