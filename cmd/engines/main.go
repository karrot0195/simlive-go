package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

type trackedTask struct {
	cancel context.CancelFunc
	ch     models.PlayoutChannel
}

func PrepareEnv(ctx context.Context, rdb *redis.Client) {
	cleanUp := utils.GetEnv("ENABLE_CLEAN_UP", "") == "true"
	if !cleanUp {
		log.Printf("[Engine] startup: preserving {TMP_DIR} cache (set ENABLE_CLEAN_UP=true for a cold start)")
		return
	}
	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
	log.Printf("[Engine] startup: ENABLE_CLEAN_UP=true, removing %s", tmpDir)
	_ = os.RemoveAll(tmpDir)
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Failed to load .env file: %v", err)
	}

	settings := utils.GetSettings()
	maxWorkers, _ := strconv.Atoi(utils.GetEnv("MAX_WORKERS", "4"))

	log.Printf("[Engine] NewEngine maxWorkers=%d", maxWorkers)

	// 1. Connect Postgres
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		settings.DBHost, settings.DBUser, settings.DBPassword, settings.DBName, settings.DBPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect DB: %v", err)
	}

	if err := db.AutoMigrate(&models.PlayoutChannel{}); err != nil {
		log.Fatalf("Failed to migrate DB: %v", err)
	}

	// 2. Connect Redis
	rdb := redis.NewClient(&redis.Options{Addr: settings.RedisAddr})

	// 3. Init Services
	rcloneSvc := storage.NewRcloneService()
	ctx := context.Background()
	engineSvc := engine.NewEngine(rdb, rcloneSvc, maxWorkers)

	PrepareEnv(ctx, rdb)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		time.Sleep(3 * time.Second)

		activeEngineTasks := make(map[string]trackedTask)
		var tasksMu sync.Mutex

		spawn := func(channelID string, channelCtx context.Context) {
			go engineSvc.StartPlayoutLoop(channelCtx, channelID)
		}

		for {
			var activeChannels []models.PlayoutChannel
			statuses := []models.PlayoutStatus{models.PlayoutInactiveStatus, models.PlayoutFailureStatus}

			db.Where("status NOT IN ?", statuses).Find(&activeChannels)

			reconcileEngines(ctx, activeChannels, &activeEngineTasks, &tasksMu, spawn, settings.TMPDir, engineSvc)
			time.Sleep(5 * time.Second)
		}
	}()

	wg.Wait()
}

func reconcileEngines(
	parent context.Context,
	activeChannels []models.PlayoutChannel,
	tasks *map[string]trackedTask,
	tasksMu *sync.Mutex,
	spawn func(channelID string, channelCtx context.Context),
	tmpDir string,
	engineSvc *engine.Engine,
) {
	activeMap := make(map[string]models.PlayoutChannel, len(activeChannels))
	for _, ch := range activeChannels {
		activeMap[ch.ID] = ch
	}

	var toSpawn []newSpawn

	tasksMu.Lock()
	// 1. Cancel/Prune engine loops for removed or updated channels
	for id, task := range *tasks {
		newCh, stillActive := activeMap[id]
		if !stillActive {
			log.Printf("[Engine] Stopping playout engine for channel: %s (no longer active)", id)
			task.cancel()
			delete(*tasks, id)
			engineSvc.ClearChannelCache(id)
			cleanupChannelDiskDir(tmpDir, id)
		} else if isChannelChanged(task.ch, newCh) {
			log.Printf("[Engine] Channel %s definition changed, restarting playout loop", id)
			task.cancel()
			delete(*tasks, id)
			engineSvc.ClearChannelCache(id)
		}
	}

	// 2. Start new playout loop for newly added or modified channels
	for id, ch := range activeMap {
		if _, running := (*tasks)[id]; !running {
			log.Printf("[Engine] Starting playout engine for channel: %s", id)
			channelCtx, cancel := context.WithCancel(parent)
			(*tasks)[id] = trackedTask{
				cancel: cancel,
				ch:     ch,
			}
			toSpawn = append(toSpawn, newSpawn{id: id, ctx: channelCtx})
		}
	}
	tasksMu.Unlock()

	// 3. Clean up disk folders for inactive/orphaned channels
	cleanupOrphanedChannels(tmpDir, activeMap)

	// 4. Execute tasks outside the mutex lock
	for _, s := range toSpawn {
		spawn(s.id, s.ctx)
	}
}

func isChannelChanged(oldCh, newCh models.PlayoutChannel) bool {
	if oldCh.UpdatedAt != newCh.UpdatedAt {
		return true
	}
	if oldCh.Status != newCh.Status {
		return true
	}
	return false
}

func cleanupChannelDiskDir(tmpDir, channelID string) {
	chDir := filepath.Join(tmpDir, channelID)
	if err := os.RemoveAll(chDir); err != nil {
		log.Printf("[Engine] cleanup disk error for channel %s: %v", channelID, err)
	}
}

func cleanupOrphanedChannels(tmpDir string, activeMap map[string]models.PlayoutChannel) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chID := entry.Name()
		if _, isActive := activeMap[chID]; !isActive {
			log.Printf("[Engine] Pruning orphaned directory on disk: %s", filepath.Join(tmpDir, chID))
			_ = os.RemoveAll(filepath.Join(tmpDir, chID))
		}
	}
}

type newSpawn struct {
	id  string
	ctx context.Context
}
