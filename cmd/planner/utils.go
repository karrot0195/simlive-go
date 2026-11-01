package main

import (
	"context"
	"os"
	"path/filepath"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/utils"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func prepareEnv(logger *utils.Logger, ctx context.Context, db *gorm.DB, rdb *redis.Client) {
	cleanUp := utils.GetEnv("ENABLE_CLEAN_UP", "") == "true"
	if cleanUp {
		logger.Info("[Planner] startup: ENABLE_CLEAN_UP=true, flushing all Redis keys + removing TMP_DIR (EPG cache preserved)")
		if rdb != nil {
			rdb.FlushAll(ctx)
		}
		wipeTmpDirExcept(logger, utils.GetEnv("TMP_DIR", os.TempDir()), utils.GetSettings().EpgCacheDir)
		if db != nil {
			db.Model(&models.PlayoutChannel{}).
				Where("status = ?", models.PlayoutLIVEStatus).
				Update("status", models.PlayoutPrepareStatus)
		}
		return
	}
	logger.Info("[Planner] startup: preserving all Redis keys + TMP_DIR cache")
}

// wipeTmpDirExcept removes every child of tmpDir except keepDir, so the
// retained EPG schedule survives a cold start. keepDir may live outside tmpDir.
func wipeTmpDirExcept(logger *utils.Logger, tmpDir, keepDir string) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Info("[Planner] startup: cannot read TMP_DIR", "dir", tmpDir, "error", err)
		}
		return
	}

	keep := filepath.Clean(keepDir)
	for _, entry := range entries {
		target := filepath.Clean(filepath.Join(tmpDir, entry.Name()))
		// Skip the cache itself and any ancestor directory that contains it.
		if keep != "" && (target == keep || strings.HasPrefix(keep, target+string(os.PathSeparator))) {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			logger.Info("[Planner] startup: failed to remove", "path", target, "error", err)
		}
	}
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
		logger.Info("[METRIC]", "channel", ch.ID, "name", ch.Name, "zset_size", size, "cursor", cursorStr, "live_segment", cursor, "download_ahead", 5)
	}
}
