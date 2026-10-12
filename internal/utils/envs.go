package utils

import (
	"os"
	"strconv"
)

type Settings struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	RedisAddr  string
	RedisPass  string
	EspxURL    string

	VideoBucket string
	AdsStreamId string

	TMPDir string
}

// PlannerSettings holds the planner's tunables. Some fields are sourced from
// environment variables (MaxPrepareRetries, PrepareRetryBaseMs,
// PrepareRetryMaxMs); the rest keep their existing hard-coded defaults so
// today's behavior is preserved while centralising the configuration surface
// for future env wiring. Defaults mirror the constants previously defined in
// internal/planner/planner.go.
type PlannerSettings struct {
	// MaxPrepareRetries is the number of attempts
	// ProcessEPGToTimelineWithRetry will make before giving up.
	// Env: MAX_PREPARE_RETRIES (default 3).
	MaxPrepareRetries int
	// PrepareRetryBaseMs is the base delay for the exponential backoff
	// (delay = base * 2^(i-1)). Env: PREPARE_RETRY_BASE_MS (default 500).
	PrepareRetryBaseMs int
	// PrepareRetryMaxMs caps the per-attempt sleep.
	// Env: PREPARE_RETRY_MAX_MS (default 8000).
	PrepareRetryMaxMs int
	// RetentionSeconds is how far back the cleanup cursor keeps segments.
	// Default 7200 (2h) — matches the historical RETENTION_SECONDS const.
	RetentionSeconds int64
	// LookaheadSeconds is how far ahead of `now` the planner walks per tick.
	// Default 7200 (2h) — matches the historical LOOKAHEAD_SECONDS const.
	LookaheadSeconds int64
	// FailureInactivateThreshold is the consecutive-failure count at which the
	FailureInactivateThreshold int64
	// FailureCounterKey is the Redis key template for the per-channel
	// failure counter (use with fmt.Sprintf + channelID). Default
	// "planner:failures:%s".
	FailureCounterKey string
	// WindowItemSize is the cadence at which the planner flushes the
	// in-memory segment batch into Redis while walking the EPG.
	// Default 50.
	WindowItemSize int
	// AdInsertionInterval is the minimum gap (in seconds) between
	// successive ad-breaks inserted into the timeline. Default 300.
	AdInsertionInterval int
	// RedisExpiryHours sets the TTL on the timeline ZSET so an
	// inactive channel's data ages out automatically. Default 26.
	RedisExpiryHours int
	// WindowBufferSeconds is the slack (in seconds) the planner adds
	// around the playhead so the engine always has a small lookahead
	// in Redis. Default 120.
	WindowBufferSeconds int
}

// GetPlannerSettings returns the planner's tunables resolved from the
// environment. Re-evaluating on every call keeps behavior identical to the
// previous per-call helpers; callers that want to cache the value can store
// the returned pointer.
func GetPlannerSettings() *PlannerSettings {
	return &PlannerSettings{
		MaxPrepareRetries:          GetEnvInt("MAX_PREPARE_RETRIES", 3),
		PrepareRetryBaseMs:         GetEnvInt("PREPARE_RETRY_BASE_MS", 500),
		PrepareRetryMaxMs:          GetEnvInt("PREPARE_RETRY_MAX_MS", 8000),
		RetentionSeconds:           7200,
		LookaheadSeconds:           7200,
		FailureInactivateThreshold: 5,
		FailureCounterKey:          "planner:failures:%s",
		WindowItemSize:             50,
		AdInsertionInterval:        GetEnvInt("AD_INSERTION_INTERVAL", 300),
		RedisExpiryHours:           26,
		WindowBufferSeconds:        120,
	}
}

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

// GetEnvInt returns the integer value of an environment variable, or defaultVal
// when the variable is unset, empty, or not a valid integer.
func GetEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func GetSettings() *Settings {
	dbHost := GetEnv("DB_HOST", "localhost")
	dbPort := GetEnv("DB_PORT", "5432")
	dbUser := GetEnv("DB_USER", "postgres")
	dbPass := GetEnv("DB_PASSWORD", "postgrespassword")
	dbName := GetEnv("DB_NAME", "simlive")

	redisAddr := GetEnv("REDIS_ADDR", "localhost:6379")
	espxURL := GetEnv("ESPX_API_URL", "https://sample.espx.cloud")

	videoBucket := GetEnv("STORAGE_BUCKET", "espx-video-lib:espx-video-lib")

	adsStreamId := "ads-v4"
	return &Settings{
		DBHost:      dbHost,
		DBName:      dbName,
		DBPort:      dbPort,
		DBUser:      dbUser,
		DBPassword:  dbPass,
		RedisAddr:   redisAddr,
		EspxURL:     espxURL,
		VideoBucket: videoBucket,
		AdsStreamId: adsStreamId,
		TMPDir:      GetEnv("TMP_DIR", os.TempDir()),
	}

}
