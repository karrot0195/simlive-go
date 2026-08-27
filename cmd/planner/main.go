package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

func PrepareEnv(ctx context.Context, rdb *redis.Client) {
	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
	// clean up
	rdb.FlushAll(ctx)
	os.RemoveAll(tmpDir)
}

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
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)

	ctx := context.Background()

	PrepareEnv(ctx, rdb)

	var wg sync.WaitGroup

	wg.Add(1)
	// 4. Planner Cron Loop: CHỈ XỬ LÝ CHANNELS is_active = true
	go func() {
		defer wg.Done()
		// for {
		var activeChannels []models.PlayoutChannel
		db.Where("is_active = ?", true).Find(&activeChannels)
		today := time.Now().Format("2006-01-02")

		for _, ch := range activeChannels {
			log.Printf("[Planner] Processing active channel: %s (%s)", ch.Name, ch.ID)
			plannerSvc.ProcessEPGToTimeline(ctx, ch, today)
		}
		// time.Sleep(30 * time.Minute)
		// }
	}()

	wg.Wait()
}
