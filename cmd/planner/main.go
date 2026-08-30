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

// plannerInterval returns the cron sleep between planner ticks.
// Defaults to 5m; override via PLANNER_INTERVAL (e.g. "1m", "30s", "1h").
// A zero/negative duration falls back to 5m.
func plannerInterval() time.Duration {
	d, err := time.ParseDuration(utils.GetEnv("PLANNER_INTERVAL", "5m"))
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// prepareDate runs EPG -> timeline for one (channel, date) pair unless
// HasDateInTimeline reports the date is already prepared. "today" and
// "tomorrow" are computed in the channel's content timezone, not the
// server's local zone, so a channel in Asia/Saigon (UTC+7) doesn't
// accidentally query tomorrow's EPG when the server is in UTC.
func prepareDate(ctx context.Context, p *planner.Planner, ch models.PlayoutChannel, date time.Time) {
	loc := utils.GetLocFor(ch.ContentTimezone)
	day := date.In(loc)
	dayStart := utils.StartOfDay(day)
	dayEnd := utils.EndOfDay(day)

	has, err := p.HasDateInTimeline(ctx, ch.ID, dayStart, dayEnd)
	if err != nil {
		log.Printf("[Planner] HasDateInTimeline(%s, %s) error: %v (will re-prepare)", ch.ID, dayStart.Format("2006-01-02"), err)
	} else if has {
		log.Printf("[Planner] channel=%s date=%s already prepared, skipping", ch.ID, dayStart.Format("2006-01-02"))
		return
	}

	if err := p.ProcessEPGToTimeline(ctx, ch, dayStart.Format("2006-01-02")); err != nil {
		log.Printf("[Planner] ProcessEPGToTimeline(%s, %s) error: %v", ch.ID, dayStart.Format("2006-01-02"), err)
	}
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
	// 4. Planner Cron Loop: today + tomorrow, per-channel content timezone.
	//    Each tick short-circuits dates that HasDateInTimeline already covers.
	go func() {
		defer wg.Done()
		interval := plannerInterval()
		log.Printf("[Planner] cron started, interval=%s", interval)

		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ?", true).Find(&activeChannels)

			for _, ch := range activeChannels {
				loc := utils.GetLocFor(ch.ContentTimezone)
				nowInCh := time.Now().In(loc)
				today := utils.StartOfDay(nowInCh)
				tomorrow := today.Add(24 * time.Hour)

				log.Printf("[Planner] Processing active channel: %s (%s), today=%s tomorrow=%s",
					ch.Name, ch.ID, today.Format("2006-01-02"), tomorrow.Format("2006-01-02"))

				prepareDate(ctx, plannerSvc, ch, today)
				prepareDate(ctx, plannerSvc, ch, tomorrow)

				// Bump the per-channel cursor to the engine's lookahead horizon
				// and trim the ZSET to RETENTION_SECONDS.
				if err := plannerSvc.ProcessWindow(ctx, ch); err != nil {
					log.Printf("[Planner] ProcessWindow(%s) error: %v", ch.ID, err)
				}
				plannerSvc.CleanupOldSegments(ctx, ch.ID)
			}

			time.Sleep(interval)
		}
	}()

	wg.Wait()
}
