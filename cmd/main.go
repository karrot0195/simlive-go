package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/engine"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/storage"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func PrepareEnv(ctx context.Context, rdb *redis.Client) {
	tmpDir := getEnv("TMP_DIR", os.TempDir())
	// clean up
	rdb.FlushAll(ctx)
	os.RemoveAll(tmpDir)
}

func main() {
	godotenv.Load()

	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPass := getEnv("DB_PASSWORD", "postgrespassword")
	dbName := getEnv("DB_NAME", "simlive")

	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	espxURL := getEnv("ESPX_API_URL", "https://api2.espx.cloud")
	maxWorkers, _ := strconv.Atoi(getEnv("MAX_WORKERS", "10"))

	// 1. Connect Postgres
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		dbHost, dbUser, dbPass, dbName, dbPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect DB: %v", err)
	}
	db.AutoMigrate(&models.PlayoutChannel{})

	// 2. Connect Redis
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})

	// 3. Init Services
	espxClient := api.NewESPXClient(espxURL)
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)
	engineSvc := engine.NewEngine(rdb, rcloneSvc, maxWorkers)
	channelHandler := api.NewChannelHandler(db, espxClient)

	ctx := context.Background()

	PrepareEnv(ctx, rdb)

	// 4. Planner Cron Loop: CHỈ XỬ LÝ CHANNELS is_active = true
	go func() {
		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ?", true).Find(&activeChannels)
			today := time.Now().Format("2006-01-02")

			for _, ch := range activeChannels {
				log.Printf("[Planner] Processing active channel: %s (%s)", ch.Name, ch.ID)
				plannerSvc.ProcessEPGToTimeline(ctx, ch, today)
			}
			time.Sleep(30 * time.Minute)
		}
	}()

	// 5. Engine Loop: CHỈ CHẠY STREAM CHO CHANNELS is_active = true
	go func() {
		time.Sleep(3 * time.Second)
		activeEngineTasks := make(map[string]context.CancelFunc)

		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ?", true).Find(&activeChannels)

			// Khởi chạy loop cho các kênh active mới kích hoạt
			for _, ch := range activeChannels {
				if _, running := activeEngineTasks[ch.ID]; !running {
					log.Printf("[Engine] Starting playout engine for channel: %s", ch.ID)
					channelCtx, cancel := context.WithCancel(ctx)
					activeEngineTasks[ch.ID] = cancel
					go engineSvc.StartPlayoutLoop(channelCtx, ch.ID)
				}
			}

			time.Sleep(10 * time.Second)
		}
	}()

	// 6. HTTP REST API
	r := gin.Default()

	// API để User POST ChannelID và isActive
	r.POST("/api/channels", channelHandler.RegisterChannel)

	// API Lấy Live HLS Playlist
	r.GET("/live/:channel_id/playlist.m3u8", func(c *gin.Context) {
		chID := c.Param("channel_id")
		playlist, err := rdb.Get(c, "simlive:hls:"+chID).Result()
		if err != nil {
			c.String(http.StatusNotFound, "Stream not active or channel disabled")
			return
		}
		c.Header("Content-Type", "application/x-mpegURL")
		c.String(http.StatusOK, playlist)
	})

	log.Println("🚀 SimLive Engine Server running on :8080")
	r.Run(":8080")
}
