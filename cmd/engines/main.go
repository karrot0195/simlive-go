package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/engine"
	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

func PrepareEnv(ctx context.Context, rdb *redis.Client) {
	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
	os.RemoveAll(tmpDir)
}

func main() {
	godotenv.Load()

	settings := utils.GetSettings()
	maxWorkers, _ := strconv.Atoi(utils.GetEnv("MAX_WORKERS", "10"))

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
	rcloneSvc := storage.NewRcloneService()

	ctx := context.Background()

	engineSvc := engine.NewEngine(rdb, rcloneSvc, maxWorkers)

	PrepareEnv(ctx, rdb)
	var wg sync.WaitGroup

	wg.Add(1)
	// 5. Engine Loop: ONLY STREAM FOR CHANNELS WITH is_active = true
	go func() {
		time.Sleep(3 * time.Second)
		activeEngineTasks := make(map[string]context.CancelFunc)
		defer wg.Done()
		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ?", true).Find(&activeChannels)

			// Start loops for newly activated active channels
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

	wg.Wait()
}
