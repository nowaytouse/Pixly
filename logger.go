package main

import (
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/fatih/color"
)

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

type StructuredLogger struct {
	logger *log.Logger
	level  LogLevel
	mu     sync.Mutex
}

// 优化配色方案，确保在暗色和亮色模式下都有良好的可读性
var (
	bold             = color.New(color.Bold).SprintFunc()
	cyan             = color.New(color.FgHiCyan).SprintFunc()
	green            = color.New(color.FgHiGreen).SprintFunc()
	yellow           = color.New(color.FgHiYellow).SprintFunc()
	red              = color.New(color.FgHiRed).SprintFunc()
	violet           = color.New(color.FgHiMagenta).SprintFunc()
	subtle           = color.New(color.FgHiBlack).SprintFunc()
	consoleMutex     = &sync.Mutex{}
	isLivePhotoRegex = regexp.MustCompile(`(?i)^IMG_E?[0-9]{4}\.HEIC$`)
)

func newStructuredLogger(w io.Writer, l LogLevel) *StructuredLogger {
	return &StructuredLogger{logger: log.New(w, "", log.Ldate|log.Ltime|log.Lmicroseconds), level: l}
}

func (l *StructuredLogger) log(level LogLevel, msg string, fields ...interface{}) {
	if level < l.level {
		return
	}
	var levelStr string
	switch level {
	case LogLevelDebug:
		levelStr = "DEBUG"
	case LogLevelInfo:
		levelStr = "INFO"
	case LogLevelWarn:
		levelStr = "WARN"
	case LogLevelError:
		levelStr = "ERROR"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("level=%s msg=\"%s\"", levelStr, msg))
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			b.WriteString(fmt.Sprintf(" %v=\"%v\"", fields[i], fields[i+1]))
		}
	}
	l.mu.Lock()
	l.logger.Println(b.String())
	l.mu.Unlock()
}

func (l *StructuredLogger) Debug(msg string, fields ...interface{}) {
	l.log(LogLevelDebug, msg, fields...)
}
func (l *StructuredLogger) Info(msg string, fields ...interface{}) {
	l.log(LogLevelInfo, msg, fields...)
}
func (l *StructuredLogger) Warn(msg string, fields ...interface{}) {
	l.log(LogLevelWarn, msg, fields...)
}
func (l *StructuredLogger) Error(msg string, fields ...interface{}) {
	l.log(LogLevelError, msg, fields...)
}

func parseLogLevel(l string) LogLevel {
	switch strings.ToLower(l) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn":
		return LogLevelWarn
	case "error":
		return LogLevelError
	}
	return LogLevelInfo
}
