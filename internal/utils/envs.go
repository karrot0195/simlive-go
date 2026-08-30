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
	espxURL := GetEnv("ESPX_API_URL", "https://api2.espx.cloud")
	return &Settings{
		DBHost:     dbHost,
		DBName:     dbName,
		DBPort:     dbPort,
		DBUser:     dbUser,
		DBPassword: dbPass,
		RedisAddr:  redisAddr,
		EspxURL:    espxURL,
	}

}
