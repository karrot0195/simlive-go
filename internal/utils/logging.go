package utils

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger struct {
	*slog.Logger
	name string
}

// GetLogger returns a configured slog.Logger that outputs to both console and a rotating log file.
func GetLogger(logName string) *Logger {
	logPath := GetSettings().TMPDir + "/" + "logs"
	if err := os.MkdirAll(logPath, 0755); err != nil {
		fmt.Printf("Error ensuring directory %s exists: %v\n", logPath, err)
		return nil
	}

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

	return &Logger{Logger: logger, name: logName}
}

func (logger *Logger) format(msg string, args ...interface{}) string {
	return fmt.Sprintf("[%s]: %s", logger.name, fmt.Sprintf(msg, args...))
}

func (logger *Logger) VInfo(msg string, args ...interface{}) {
	logger.Info(logger.format(msg, args...))
}

func (logger *Logger) VWarning(msg string, args ...interface{}) {
	logger.Warn(logger.format(msg, args...))
}

func (logger *Logger) VError(msg string, args ...interface{}) {
	logger.Error(logger.format(msg, args...))
}

func (logger *Logger) VErrorWithError(msg string, args ...interface{}) error {
	textError := logger.format(msg, args...)
	logger.Error(textError)
	return errors.New(textError)
}

func (logger *Logger) VFatalf(msg string, args ...interface{}) {
	logger.VError(msg, args...)
	os.Exit(1)
}
