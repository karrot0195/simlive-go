package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"simlive/internal/utils"
)

func prepVersionValue() string {
	return "v1;audio-language=" + utils.GetPmtAudioLanguage()
}

func (e *Engine) ensurePreparationVersion(ctx context.Context, logger *utils.Logger, channelID string) {
	expected := prepVersionValue()
	stampPath := filepath.Join(e.tmpDir, channelID, ".prep_version")

	data, err := os.ReadFile(stampPath)
	current := ""
	if err == nil {
		current = string(data)
	}

	if current == expected {
		return
	}

	// Purge rendition directories.
	for _, ren := range RENDITIONMAP {
		renDir := filepath.Join(e.tmpDir, channelID, ren)
		_ = os.RemoveAll(renDir)
		_ = os.MkdirAll(renDir, 0755)
	}

	// Remove master playlist.
	_ = os.Remove(filepath.Join(e.tmpDir, channelID, "playlist.m3u8"))

	// Delete Redis HLS keys.
	_ = e.rdb.Del(ctx, fmt.Sprintf("simlive:hls:%s", channelID)).Err()
	for _, ren := range RENDITIONMAP {
		_ = e.rdb.Del(ctx, fmt.Sprintf("simlive:hls:%s:rendition:%s", channelID, ren)).Err()
	}

	// Write stamp atomically.
	tmpPath := stampPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(expected), 0644); err != nil {
		logger.Error("prep version stamp write failed", "channel", channelID, "error", err)
		return
	}
	if err := os.Rename(tmpPath, stampPath); err != nil {
		logger.Error("prep version stamp rename failed", "channel", channelID, "error", err)
		return
	}

	logger.Info("[PMT] event=prep_version_invalidated", "channel", channelID, "previous", current, "current", expected)
}
