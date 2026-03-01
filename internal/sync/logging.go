package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stonespren/helium-sync/internal/config"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "debug",
	LevelInfo:  "info",
	LevelWarn:  "warn",
	LevelError: "error",
}

var levelFromString = map[string]Level{
	"debug": LevelDebug,
	"info":  LevelInfo,
	"warn":  LevelWarn,
	"error": LevelError,
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Component string `json:"component,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Logger struct {
	mu        sync.Mutex
	writer    io.Writer
	level     Level
	component string
	logFile   *os.File
}

const (
	maxLogSize  = 10 * 1024 * 1024
	logFileName = "helium-sync.log"
)

var defaultLogger *Logger

func Init(background bool) error {
	stateDir := config.StateDir()
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	logPath := filepath.Join(stateDir, logFileName)

	if info, err := os.Stat(logPath); err == nil && info.Size() > maxLogSize {
		rotated := logPath + ".1"
		os.Remove(rotated)
		os.Rename(logPath, rotated)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	level := LevelInfo
	cfg, cfgErr := config.ForceLoad()
	if cfgErr == nil && cfg.LogLevel != "" {
		if l, ok := levelFromString[cfg.LogLevel]; ok {
			level = l
		}
	}

	var writer io.Writer
	if background {
		writer = f
	} else {
		writer = io.MultiWriter(f, os.Stderr)
	}

	defaultLogger = &Logger{
		writer:  writer,
		level:   level,
		logFile: f,
	}
	return nil
}

func Close() {
	if defaultLogger != nil && defaultLogger.logFile != nil {
		defaultLogger.logFile.Close()
	}
}

func WithComponent(component string) *Logger {
	if defaultLogger == nil {
		return &Logger{
			writer:    os.Stderr,
			level:     LevelInfo,
			component: component,
		}
	}
	return &Logger{
		writer:    defaultLogger.writer,
		level:     defaultLogger.level,
		component: component,
		logFile:   defaultLogger.logFile,
	}
}

func (l *Logger) log(level Level, msg string, errStr string) {
	if level < l.level {
		return
	}
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     levelNames[level],
		Message:   msg,
		Component: l.component,
		Error:     errStr,
	}
	data, _ := json.Marshal(entry)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.writer, string(data))
}

func (l *Logger) Debug(msg string) { l.log(LevelDebug, msg, "") }

func (l *Logger) Info(msg string) { l.log(LevelInfo, msg, "") }

func (l *Logger) Warn(msg string) { l.log(LevelWarn, msg, "") }

func (l *Logger) Error(msg string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	l.log(LevelError, msg, errStr)
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LevelInfo, fmt.Sprintf(format, args...), "")
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(LevelDebug, fmt.Sprintf(format, args...), "")
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LevelWarn, fmt.Sprintf(format, args...), "")
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(LevelError, fmt.Sprintf(format, args...), "")
}

func LogFilePath() string {
	return filepath.Join(config.StateDir(), logFileName)
}
