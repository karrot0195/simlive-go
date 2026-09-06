package utils

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger = *slog.Logger

// GetLogger returns a configured slog.Logger that outputs to both console and a rotating log file.
func GetLogger(logName string) *slog.Logger {
	// 1. Configure rotating file writer
	fileWriter := &lumberjack.Logger{
		Filename:   "/tmp/logs/" + logName + ".log",
		MaxSize:    500,  // Megabytes before rotating
		MaxAge:     3,    // Days to retain old log files
		MaxBackups: 10,   // Maximum number of old log files to retain
		Compress:   true, // Compress rotated files with gzip
	}

	// 2. Combine stdout (console) and file into a single MultiWriter
	multiWriter := io.MultiWriter(os.Stdout, fileWriter)

	// 3. Pass the combined multiWriter to slog JSONHandler
	logger := slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return logger
}
