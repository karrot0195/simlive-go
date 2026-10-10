package main

import (
	"context"
	"os"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/utils"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func prepareEnv(logger *utils.Logger, ctx context.Context, db *gorm.DB, rdb *redis.Client) {
	cleanUp := utils.GetEnv("ENABLE_CLEAN_UP", "") == "true"
	if cleanUp {
		logger.Info("[Planner] startup: ENABLE_CLEAN_UP=true, flushing all Redis keys + removing TMP_DIR")
		if rdb != nil {
			rdb.FlushAll(ctx)
		}
		tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
		os.RemoveAll(tmpDir)
		if db != nil {
			db.Model(&models.PlayoutChannel{}).
				Where("status = ?", models.PlayoutLIVEStatus).
				Update("status", models.PlayoutPrepareStatus)
		}
		return
	}
	logger.Info("[Planner] startup: preserving all Redis keys + TMP_DIR cache")
}

func plannerInterval() time.Duration {
	d, err := time.ParseDuration(utils.GetEnv("PLANNER_INTERVAL", "5m"))
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

func plannerWorkerCount() int {
	n := utils.GetEnvInt("PLANNER_WORKERS", 4)
	if n < 1 {
		return 1
	}
	return n
}

func logMetrics(logger *utils.Logger, ctx context.Context, p *planner.Planner, channels []models.PlayoutChannel) {
	for _, ch := range channels {
		size := p.TimelineSize(ctx, ch.ID)
		cursor := p.LastProcessedCursor(ctx, ch.ID)
		cursorStr := "none"
		if cursor > 0 {
			cursorStr = time.Unix(cursor, 0).UTC().Format("2006-01-02T15:04:05Z")
		}
		logger.Info("[METRIC] channel=%s name=%q zset_size=%d cursor=%s live_segment=%d download_ahead=%d",
			ch.ID, ch.Name, size, cursorStr, cursor, 5)
	}
}
