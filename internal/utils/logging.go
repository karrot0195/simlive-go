package utils

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger = *slog.Logger

// GetLogger returns a configured slog.Logger that outputs to both console and a rotating log file.
func GetLogger(logName string) *slog.Logger {
	logPath := GetSettings().TMPDir + "/" + "logs"
	_ = os.MkdirAll(logPath, 0755)

	channelWriter := &lumberjack.Logger{
		Filename:   filepath.Join(logPath, logName+".log"),
		MaxSize:    500,
		MaxAge:     7,
		MaxBackups: 10,
		Compress:   true,
	}

	verboseWriter := &lumberjack.Logger{
		Filename:   logPath + "/verbose.log",
		MaxSize:    500,
		MaxAge:     7,
		MaxBackups: 10,
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stdout, channelWriter, verboseWriter)

	logger := slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return logger
}
