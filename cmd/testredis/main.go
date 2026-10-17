package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// TimelineEntry represents a single entry in the timeline
type TimelineEntry struct {
	Sequence    int64    `json:"sequence"`
	StoragePath string   `json:"storage_path"`
	Duration    float64  `json:"duration"`
	StartTime   int64    `json:"start_time"`
	TimeText    string   `json:"time_text"`
	RunningTime float64  `json:"running_time"`
	PtsStart    int64    `json:"pts_start"`
	PtsEnd      int64    `json:"pts_end"`
	MediaStart  float64  `json:"media_start"`
	MediaEnd    float64  `json:"media_end"`
	PtsOffset   float64  `json:"pts_offset"`
	Rendition   []string `json:"rendition"`
	IsAd        bool     `json:"is_ad,omitempty"`
}

func main() {
	// Redis connection settings
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB := 0

	channelId := flag.String("channelId", "cdda7af8-8f9d-4882-a3c4-0b69dd7fce01", "")
	flag.Parse()
	key := "simlive:timeline:" + *channelId

	fmt.Printf("use channelId %s\n", key)
	if len(os.Args) > 1 {
		key = os.Args[1]
	}

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})
	defer rdb.Close()

	ctx := context.Background()

	// Test connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Fetch all entries from the sorted set
	results, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		log.Fatalf("Failed to ZRANGE: %v", err)
	}

	// Get the local timezone name for display
	zoneName, _ := time.Now().Zone()
	fmt.Printf("Total entries: %d (local timezone: %s)\n\n", len(results), zoneName)

	// Parse each entry and display date + local time based on running_time
	for _, raw := range results {
		var entry TimelineEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			log.Printf("Failed to parse entry: %v\nRaw: %s\n", err, raw)
			continue
		}

		// Convert running_time (unix timestamp with fractional seconds) to local time
		sec := int64(entry.RunningTime)
		nsec := int64((entry.RunningTime - float64(sec)) * 1e9)
		t := time.Unix(sec, nsec) // no .UTC() → uses local timezone

		// Print as YYYY-MM-DD HH:MM:SS <TZ>  sequence=XXXXX
		fmt.Printf("%s %s  sequence=%d %s\n",
			t.Format("2006-01-02 15:04:05"),
			zoneName,
			entry.Sequence,
			entry.IsAd,
		)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
