package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"simlive/internal/storage"
	"simlive/internal/utils"
)

// renditionMetadataKey is the per-channel Redis key holding the resolved
// ladder metadata as a JSON array of RenditionMeta. The engine reads it on
// startup (and on the first encounter of a new channel) to build the master
// playlist without probing at runtime. See docs/300826-multi-rendition-abr-ladder.md
// §3 / §4 / §5.
const renditionMetadataKey = "simlive:renditions:%s"

// RenditionMeta is the JSON shape written to simlive:renditions:{ch}. One
// entry per active rung (M1: exactly one). Fields are the inputs the engine
// needs to render `#EXT-X-STREAM-INF` lines without probing.
type RenditionMeta struct {
	Rendition string `json:"rendition"` // "360p" | "720p" | "1080p"
	VodDir    string `json:"vod_dir"`   // e.g. "L_0"
	AdsDir    string `json:"ads_dir"`   // e.g. "L2"
	Width     int    `json:"width"`     // 0 == unknown, skipped from master
	Height    int    `json:"height"`    // 0 == unknown, skipped from master
	Bandwidth int64  `json:"bandwidth"` // bits/sec; 0 == unknown, skipped from master
}

// DefaultRendition is the rescue rung used by the engine when a SegmentItem
// has no Rendition field (legacy timeline members pre-M1). Matches the
// active rung in M1's default config (360p). Mirrors engine.DefaultRendition.
const DefaultRendition = "360p"

// activeRenditionsOnce guards the "WARN + use first" log line so it fires
// once per process, not per tick × per channel.
var (
	activeRenditionsOnce sync.Once
	activeRenditionsList []string
)

// activeRenditions reads SIMLIVE_RENDITIONS (comma list) and returns the
// list of active rung labels for M1. Semantics:
//   - Unset / empty -> single rung derived from SIMLIVE_QUALITY (fallback "360p").
//   - "360p,720p"  -> WARN + use ["360p"] (M1 only allows exactly 1 rung).
//   - " 720p "      -> ["720p"] (whitespace trimmed, empty entries dropped).
//
// The parsed result is cached behind a sync.Once so the WARN log fires once.
func (p *Planner) activeRenditions() []string {
	activeRenditionsOnce.Do(func() {
		raw := utils.GetEnv("SIMLIVE_RENDITIONS", "")
		if raw == "" {
			tier, _ := p.resolveQualityTier()
			label := tier.label
			if label == "" {
				label = DefaultRendition
			}
			activeRenditionsList = []string{label}
			return
		}
		parts := strings.Split(raw, ",")
		var cleaned []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			cleaned = append(cleaned, part)
		}
		if len(cleaned) == 0 {
			activeRenditionsList = []string{DefaultRendition}
			return
		}
		if len(cleaned) > 1 {
			log.Printf("[Planner] WARN: SIMLIVE_RENDITIONS=%q has %d rungs; M1 only supports 1, using %q",
				raw, len(cleaned), cleaned[0])
		}
		activeRenditionsList = cleaned[:1]
	})
	return activeRenditionsList
}

// probeBitrate reads format=bit_rate (bits/sec) from the first packet of the
// remote segment via ffprobe. Mirrors probeResolution() but for bandwidth.
// Times out at 15s (same failure semantics as probeResolution). Used by
// writeRenditionMetadata to populate RenditionMeta.Bandwidth.
func (p *Planner) probeBitrate(remotePath string) (int64, error) {
	tmp, err := writeRemoteToTemp(p.rclone, remotePath)
	if err != nil {
		return 0, err
	}
	defer removeTemp(tmp)

	probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := ffprobeCSV(probeCtx, tmp, "format=bit_rate")
	if err != nil {
		return 0, fmt.Errorf("ffprobe bit_rate: %w", err)
	}
	return parseBitrateStdout(out)
}

// parseBitrateStdout parses a single-line bit_rate value (bits/sec) emitted by
// ffprobe `-of csv=p=0`. Tolerates trailing whitespace + commas. Returns
// (0, error) on empty / non-numeric input. Pure function -> unit-testable.
func parseBitrateStdout(b []byte) (int64, error) {
	s := strings.TrimSpace(string(b))
	// ffprobe csv may emit "1234,\n" -> strip trailing comma
	s = strings.TrimRight(s, ",")
	if s == "" {
		return 0, fmt.Errorf("empty bit_rate output")
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse bit_rate %q: %w", s, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("non-positive bit_rate %d", v)
	}
	return v, nil
}

// probeBitrateFallback returns a sane bandwidth estimate (bits/sec) when the
// ffprobe path fails. Formula: w*h*0.08 (matches the v1 estimate) clamped to
// [100_000, 20_000_000] so corrupt probe values can't poison the master.
// Pure function -> unit-testable.
func probeBitrateFallback(w, h int) int64 {
	const lo, hi int64 = 100_000, 20_000_000
	if w <= 0 || h <= 0 {
		return lo
	}
	v := int64(float64(w) * float64(h) * 0.08)
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// bandwidthForRendition returns the nominal bandwidth hint for a ladder rung.
// Used to keep the master playlist's STREAM-INF lines consistent even when
// the bitrate probe fails. The actual streamed bandwidth equals the active
// rung's VOD dir (one physical .ts set is shared across all rungs).
func bandwidthForRendition(label string, w, h int) int64 {
	switch label {
	case "360p":
		return 800_000
	case "720p":
		return 2_500_000
	case "1080p":
		return 5_000_000
	}
	return probeBitrateFallback(w, h)
}

// probeAnyCachedBitrate probes the bitrate of the first segment of the first
// cached stream's VOD rendition. Used by writeRenditionMetadata to get a
// "real" bandwidth number for the middle rung when possible. Returns 0 on
// any failure (no cached streams, probe error) so the caller can fall back
// to the nominal ladder default.
func (p *Planner) probeAnyCachedBitrate() int64 {
	p.rendMu.Lock()
	var (
		anyStreamID string
		anyPair     renditionPair
	)
	for sid, pair := range p.rendCache {
		anyStreamID = sid
		anyPair = pair
		break
	}
	p.rendMu.Unlock()
	if anyStreamID == "" || anyPair.vod == "" {
		return 0
	}
	remote := fmt.Sprintf("%s/%s/%s/vod_000.ts", p.bucketName, anyStreamID, anyPair.vod)
	bw, err := p.probeBitrate(remote)
	if err != nil {
		log.Printf("[Planner] probeAnyCachedBitrate(%s) failed: %v", remote, err)
		return 0
	}
	return bw
}

// writeRenditionMetadata persists the active rung(s) as JSON to
// simlive:renditions:{channelID} with a 24h TTL. M1 writes a single entry
// derived from the active ladder rung + (re-)probed bandwidth. Called from
// ProcessEPGToTimeline as the last step before the pipeline flush. Best-effort
// (logs and continues on failure -- the engine has a fallback default).
// writeRenditionMetadata persists the FULL ladder (360p / 720p / 1080p) as
// JSON to simlive:renditions:{channelID} with a 24h TTL. The engine reads it
// to build the master playlist. M1.5: all 3 rungs are always written so the
// master advertises the full ladder regardless of SIMLIVE_QUALITY. The
// engine serves the SAME seg_N.ts files for all rungs (shared segment cache);
// the per-rung metadata is purely informational (bandwidth, resolution, label).
func (p *Planner) writeRenditionMetadata(ctx context.Context, channelID string) error {
	if p.rdb == nil {
		return nil
	}

	// Build one entry per known tier. Pull the VOD/ads dirs from the per-tier
	// fallback map; the per-stream rendCache is irrelevant here because the
	// engine downloads ONE set of segments (from the active tier's VOD dir)
	// and shares them across all 3 playlists.
	ladder := []RenditionMeta{}
	for _, tierLabel := range []string{"360p", "720p", "1080p"} {
		tier := qualityTierMap[tierLabel]
		if tier.w == 0 || tier.h == 0 {
			continue
		}
		fb, ok := fallbackRenditionForTier[tierLabel]
		if !ok {
			continue
		}
		ladder = append(ladder, RenditionMeta{
			Rendition: tierLabel,
			VodDir:    fb.vod,
			AdsDir:    fb.ads,
			Width:     tier.w,
			Height:    tier.h,
			// Nominal bandwidths -- the engine serves the same files for
			// all rungs, so the bandwidth hint is informational only and
			// does not change the actual download / disk cost. Player
			// ladders stay consistent across hls.js / Safari.
			Bandwidth: bandwidthForRendition(tierLabel, tier.w, tier.h),
		})
	}

	// Probe a single representative bitrate from any cached stream's
	// active-tier VOD dir and use it as a "best effort" override for the
	// 720p rung. The other rungs keep the nominal ladder defaults so the
	// master advertises a consistent bandwidth progression even when the
	// probe fails.
	if probed := p.probeAnyCachedBitrate(); probed > 0 && len(ladder) >= 2 {
		// Use the probed value for the middle rung (720p) so the master
		// shows a real bitrate instead of a constant ladder default.
		ladder[1].Bandwidth = probed
	}

	bytes, err := json.Marshal(ladder)
	if err != nil {
		return fmt.Errorf("marshal RenditionMeta: %w", err)
	}
	if err := p.rdb.Set(ctx, fmt.Sprintf(renditionMetadataKey, channelID), bytes, 24*time.Hour).Err(); err != nil {
		log.Printf("[Planner] SET simlive:renditions:%s error: %v", channelID, err)
		return err
	}
	return nil
}

// hasLegacyTimeline reports whether the ZSET simlive:timeline:{ch} contains
// at least one member whose JSON has no "rendition" field. Used on the first
// planner tick after M1 ships to detect pre-upgrade timelines and DEL the
// key so ProcessEPGToTimeline repopulates with tags. Best-effort: an empty
// ZSET (no members at all) returns false so cold starts aren't flushed.
func (p *Planner) hasLegacyTimeline(ctx context.Context, channelID string) (bool, error) {
	if p.rdb == nil {
		return false, nil
	}
	key := fmt.Sprintf("simlive:timeline:%s", channelID)
	items, err := p.rdb.ZRange(ctx, key, 0, 0).Result()
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil
	}
	// Probe the first member for the rendition field. Use a minimal struct
	// instead of models.SegmentItem so we don't drag the full model import
	// (also makes the field-check explicit).
	var probe struct {
		Rendition string `json:"rendition"`
	}
	if err := json.Unmarshal([]byte(items[0]), &probe); err != nil {
		// Unparseable -- treat as legacy so we don't serve garbage.
		return true, nil
	}
	return probe.Rendition == "", nil
}

// --- small helpers shared by probeBitrate / probeResolution ---------------

// writeRemoteToTemp downloads a remote rclone path to a temp file and returns
// the local path. Used by probeResolution / probeBitrate to give ffprobe a
// local file. Caller is responsible for removeTemp.
func writeRemoteToTemp(rclone *storage.RcloneService, remotePath string) (string, error) {
	if rclone == nil {
		return "", fmt.Errorf("rclone service is nil")
	}
	tmp, err := os.CreateTemp("", "simlive-probe-*.ts")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	if err := rclone.CopyTo(remotePath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// removeTemp best-effort deletes a probe temp file. Failure is non-fatal
// (the OS cleans /tmp eventually) so we don't surface it.
func removeTemp(path string) {
	_ = os.Remove(path)
}

// ffprobeCSV runs `ffprobe -v error -select_streams v:0 -show_entries {entries}
// -of csv=p=0 <path>` and returns the combined stdout. `entries` is something
// like "stream=width,height" or "format=bit_rate". The select_streams=v:0
// matches probeResolution() (video stream #0) which is fine for bit_rate
// because format-level entries ignore stream selection.
func ffprobeCSV(ctx context.Context, path, entries string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", entries,
		"-of", "csv=p=0", path)
	return cmd.Output()
}
