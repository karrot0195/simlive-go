package utils

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger struct {
	*slog.Logger
	name string
}

var (
	logDirOnce   sync.Once
	logDirPath   string
	verboseW     io.Writer
	loggerCache  = make(map[string]*Logger)
	loggerCacheMu sync.Mutex
)

func initLogDir() {
	dir := GetSettings().TMPDir + "/" + "logs"
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Error ensuring directory %s exists: %v\n", dir, err)
		dir = os.TempDir()
		_ = os.MkdirAll(dir, 0755)
	}
	logDirPath = dir
	verboseW = &lumberjack.Logger{
		Filename:   filepath.Join(dir, "verbose.log"),
		MaxSize:    500,
		MaxAge:     7,
		MaxBackups: 10,
		Compress:   true,
	}
}

func parseLogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

// GetLogger returns a configured slog.Logger that outputs to both console and a
// rotating log file. Loggers are cached by name so repeated calls reuse the
// same instance (and underlying file handles).
func GetLogger(logName string) *Logger {
	loggerCacheMu.Lock()
	defer loggerCacheMu.Unlock()

	if l, ok := loggerCache[logName]; ok {
		return l
	}

	logDirOnce.Do(initLogDir)

	channelWriter := &lumberjack.Logger{
		Filename:   filepath.Join(logDirPath, logName+".log"),
		MaxSize:    500,
		MaxAge:     7,
		MaxBackups: 10,
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stdout, channelWriter, verboseW)

	level := parseLogLevel()
	logger := slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: level,
	}))

	l := &Logger{Logger: logger, name: logName}
	loggerCache[logName] = l
	return l
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