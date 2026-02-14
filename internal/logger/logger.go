package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Component string `json:"component"`
	Message   string `json:"message"`
}

type Logger struct {
	level      Level
	file       *os.File
	fileLogger *log.Logger

	entries   []LogEntry
	entriesMu sync.RWMutex
	maxBuffer int

	OnNewEntry func(LogEntry)
	mu         sync.RWMutex
}

func New(logDir string, level string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	logPath := filepath.Join(logDir, "govault.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return &Logger{
		level:      ParseLevel(level),
		file:       f,
		fileLogger: log.New(f, "", 0),
		entries:    make([]LogEntry, 0, 1000),
		maxBuffer:  1000,
	}, nil
}

func (l *Logger) SetLevel(level string) {
	l.mu.Lock()
	l.level = ParseLevel(level)
	l.mu.Unlock()
}

func (l *Logger) log(lvl Level, component, msg string) {
	l.mu.RLock()
	minLevel := l.level
	l.mu.RUnlock()

	if lvl < minLevel {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Level:     lvl.String(),
		Component: component,
		Message:   msg,
	}

	line := fmt.Sprintf("[%s] [%s] [%s] %s", entry.Timestamp, entry.Level, entry.Component, entry.Message)
	l.fileLogger.Println(line)

	l.entriesMu.Lock()
	if len(l.entries) >= l.maxBuffer {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, entry)
	l.entriesMu.Unlock()

	if l.OnNewEntry != nil {
		l.OnNewEntry(entry)
	}
}

func (l *Logger) Debug(component, msg string)                 { l.log(LevelDebug, component, msg) }
func (l *Logger) Info(component, msg string)                  { l.log(LevelInfo, component, msg) }
func (l *Logger) Warn(component, msg string)                  { l.log(LevelWarn, component, msg) }
func (l *Logger) Error(component, msg string)                 { l.log(LevelError, component, msg) }
func (l *Logger) Debugf(component, format string, a ...any)   { l.log(LevelDebug, component, fmt.Sprintf(format, a...)) }
func (l *Logger) Infof(component, format string, a ...any)    { l.log(LevelInfo, component, fmt.Sprintf(format, a...)) }
func (l *Logger) Warnf(component, format string, a ...any)    { l.log(LevelWarn, component, fmt.Sprintf(format, a...)) }
func (l *Logger) Errorf(component, format string, a ...any)   { l.log(LevelError, component, fmt.Sprintf(format, a...)) }

func (l *Logger) GetEntries(count int) []LogEntry {
	l.entriesMu.RLock()
	defer l.entriesMu.RUnlock()

	total := len(l.entries)
	if count <= 0 || count > total {
		count = total
	}
	start := total - count
	result := make([]LogEntry, count)
	copy(result, l.entries[start:])
	return result
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}
