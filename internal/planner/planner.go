package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

const WINDOW_ITEM_SIZE = 50

type Planner struct {
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESPXClient
	rclone     *storage.RcloneService
	bucketName string
	adsBucket  string
}

func NewPlanner(db *gorm.DB, rdb *redis.Client, espx *api.ESPXClient, rclone *storage.RcloneService) *Planner {
	return &Planner{
		db:         db,
		rdb:        rdb,
		espx:       espx,
		rclone:     rclone,
		bucketName: "espx-video-lib:espx-video-lib",
		adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
	}
}

type AdSegment struct {
	Duration    float64
	StoragePath string
}

func (p *Planner) loadAdSegments() ([]AdSegment, error) {
	adsM3u8Path := fmt.Sprintf("%s/L0/vod.m3u8", p.adsBucket)
	m3u8Content, err := p.rclone.Cat(adsM3u8Path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var adSegments []AdSegment
	var currentDuration float64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			currentDuration, _ = strconv.ParseFloat(durStr, 64)
		} else if strings.HasSuffix(line, ".ts") {
			adSegments = append(adSegments, AdSegment{
				Duration:    currentDuration,
				StoragePath: fmt.Sprintf("%s/L0/%s", p.adsBucket, line),
			})
		}
	}

	if len(adSegments) == 0 {
		return nil, fmt.Errorf("no ad segments found in %s", adsM3u8Path)
	}

	return adSegments, nil
}

// just running into 24:00:00, so we need to adjust the duration by 1 second
func (p *Planner) CheckEndOfTimeline(processTime time.Time, globalRunningTime int) bool {
	endOfDay := processTime.AddDate(0, 0, 1)
	currentRunningTime := processTime.Add(time.Duration(globalRunningTime))
	if endOfDay.Before(currentRunningTime) {
		return true
	}
	return false
}

func (p *Planner) ProcessEPGToTimeline(ctx context.Context, channel models.PlayoutChannel, dateStr string) error {
	loc := utils.GetLoc()
	tz := configs.TZ_NUM

	nowLocal := time.Now().In(loc)
	nowUnix := nowLocal.Unix()

	log.Printf("[Planner] Fetching schedules for channel %s (%s) date: %s tz: %s | Now Local: %s",
		channel.Name, channel.ID, dateStr, tz, nowLocal.Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS))

	// get schedule items
	schedules, err := p.espx.GetSchedules(channel.ChannelEpgID, dateStr, tz)
	if err != nil {
		log.Printf("[Planner ERROR] Failed to fetch schedules: %v", err)
		return err
	}

	if len(schedules.Data) == 0 {
		log.Printf("[Planner WARNING] No schedule items returned from ESPX API for channel %s", channel.ID)
		return nil
	}

	adSegments, adErr := p.loadAdSegments()
	if adErr != nil {
		log.Printf("[Planner WARN] Could not load ads, proceeding without ads: %v", adErr)
	}

	redisKey := fmt.Sprintf("simlive:timeline:%s", channel.ID)
	var sequenceCounter int64 = 0

	var adOffset int64 = 0
	var lastAdTime int64 = 0
	var globalRunningTime int64 = 0

	// urgentWindowMax := nowUnix + 300
	// urgentFlushed := false

	pipe := p.rdb.Pipeline()
	totalSegmentsAdded := 0
	totalAdsAdded := 0

	for _, item := range schedules.Data {
		if len(item.Programme.ProgrammeStreams) == 0 {
			continue
		}

		streamID := item.Programme.ProgrammeStreams[0].StreamID
		if streamID == "" {
			continue
		}

		startTime, err := utils.ParseESPXTime(item.LocalDateStart, channel.ContentTimezone)
		endTime, err := utils.ParseESPXTime(item.LocalDateEnd, channel.ContentTimezone)
		if err != nil || endTime.Before(nowLocal) {
			continue
		}

		epgStartTimeUnix := startTime.Unix()
		epgWithOffset := epgStartTimeUnix + adOffset
		if epgWithOffset > globalRunningTime {
			globalRunningTime = epgWithOffset
		}

		if lastAdTime == 0 {
			lastAdTime = globalRunningTime
		}

		m3u8Path := fmt.Sprintf("%s/%s/L_0/vod.m3u8", p.bucketName, streamID)
		m3u8Content, err := p.rclone.Cat(m3u8Path)
		if err != nil {
			log.Printf("[Planner ERROR] Failed rclone cat %s: %v", m3u8Path, err)
			continue
		}

		lines := strings.Split(m3u8Content, "\n")
		var currentDuration float64

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#EXTINF:") {
				durStr := strings.TrimPrefix(line, "#EXTINF:")
				durStr = strings.Split(durStr, ",")[0]
				currentDuration, _ = strconv.ParseFloat(durStr, 64)
			} else if strings.HasSuffix(line, ".ts") {
				sequenceCounter++
				segmentPath := fmt.Sprintf("%s/%s/L_0/%s", p.bucketName, streamID, line)
				actualStartTime := globalRunningTime

				seg := models.SegmentItem{
					Sequence:    sequenceCounter,
					StoragePath: segmentPath,
					Duration:    currentDuration,
					StartTime:   actualStartTime,
					TimeText:    utils.TimeNumToText(actualStartTime),
				}

				segBytes, _ := json.Marshal(seg)

				if actualStartTime >= (nowUnix - 120) {
					pipe.ZAdd(ctx, redisKey, redis.Z{
						Score:  float64(actualStartTime),
						Member: string(segBytes),
					})
					totalSegmentsAdded++
				}

				globalRunningTime += int64(currentDuration)

				// Chèn Ads định kỳ
				if len(adSegments) > 0 && actualStartTime >= nowUnix && (actualStartTime-lastAdTime) >= 120 {
					for _, adSeg := range adSegments {
						sequenceCounter++
						adItem := models.SegmentItem{
							Sequence:    sequenceCounter,
							StoragePath: adSeg.StoragePath,
							Duration:    adSeg.Duration,
							StartTime:   globalRunningTime,
							TimeText:    utils.TimeNumToText(globalRunningTime),
						}
						adBytes, _ := json.Marshal(adItem)

						pipe.ZAdd(ctx, redisKey, redis.Z{
							Score:  float64(globalRunningTime),
							Member: string(adBytes),
						})

						adDurationInt := int64(adSeg.Duration)
						globalRunningTime += adDurationInt
						adOffset += adDurationInt

						totalSegmentsAdded++
						totalAdsAdded++

						if p.CheckEndOfTimeline(nowLocal, int(globalRunningTime)) {
							return nil
						}

					}
					lastAdTime = globalRunningTime
				}
				// if totalSegmentsAdded / 10 -> push to Redis every 10 segments
				if totalSegmentsAdded%WINDOW_ITEM_SIZE == 0 {
					_, err := pipe.Exec(ctx)
					if err != nil {
						log.Printf("[Planner ERROR] Fast-path flush failed: %v", err)
					} else {
						log.Printf("[Planner FAST-PATH] Added %d items into redis (total=%d, runnning-time=%s)", WINDOW_ITEM_SIZE, totalSegmentsAdded, utils.TimeNumToText(globalRunningTime))
					}
					pipe = p.rdb.Pipeline()
				}

				if p.CheckEndOfTimeline(nowLocal, int(globalRunningTime)) {
					return nil
				}

			}
		}
	}

	pipe.Expire(ctx, redisKey, 26*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[Planner ERROR] Final pipeline execution failed: %v", err)
		return err
	}

	log.Printf("[Planner SUCCESS] Completed schedule processing. Total %d segments (%d ads) in Redis key: %s",
		totalSegmentsAdded, totalAdsAdded, redisKey)

	return nil
}
