package logger

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/fatih/color"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LoggerConfig 日志配置
type LoggerConfig struct {
	Verbose       bool
	EnableFile    bool
	EnableConsole bool
	LogLevel      zapcore.Level
	MaxSize       int // MB
	MaxBackups    int
	MaxAge        int // days
	Compress      bool
	LogDir        string
	Component     string
}

// DefaultLoggerConfig 默认日志配置
func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Verbose:       false,
		EnableFile:    true,
		EnableConsole: true,
		LogLevel:      zapcore.InfoLevel,
		MaxSize:       100,
		MaxBackups:    3,
		MaxAge:        7,
		Compress:      true,
		LogDir:        "./output/logs",
		Component:     "pixly",
	}
}

// PerformanceLogger 性能日志记录器
type PerformanceLogger struct {
	logger *zap.Logger
	mutex  sync.RWMutex
}

// NewPerformanceLogger 创建性能日志记录器
func NewPerformanceLogger(logger *zap.Logger) *PerformanceLogger {
	return &PerformanceLogger{
		logger: logger.Named("performance"),
	}
}

// LogOperation 记录操作性能
func (pl *PerformanceLogger) LogOperation(operation string, duration time.Duration, fields ...zap.Field) {
	pl.mutex.RLock()
	defer pl.mutex.RUnlock()

	// 操作性能记录
}

// LogMemoryUsage 记录内存使用情况
func (pl *PerformanceLogger) LogMemoryUsage(operation string) {
	pl.mutex.RLock()
	defer pl.mutex.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 内存使用情况记录
}

// ErrorTracker 错误追踪器
type ErrorTracker struct {
	logger     *zap.Logger
	errorCount map[string]int64
	mutex      sync.RWMutex
}

// NewErrorTracker 创建错误追踪器
func NewErrorTracker(logger *zap.Logger) *ErrorTracker {
	return &ErrorTracker{
		logger:     logger.Named("error_tracker"),
		errorCount: make(map[string]int64),
	}
}

// TrackError 追踪错误
func (et *ErrorTracker) TrackError(errorType string, err error, fields ...zap.Field) {
	et.mutex.Lock()
	et.errorCount[errorType]++
	count := et.errorCount[errorType]
	et.mutex.Unlock()

	allFields := append([]zap.Field{
		zap.String("error_type", errorType),
		zap.Error(err),
		zap.Int64("occurrence_count", count),
		zap.String("stack_trace", getStackTrace()),
	}, fields...)

	et.logger.Error("Error tracked", allFields...)
}

// GetErrorStats 获取错误统计
func (et *ErrorTracker) GetErrorStats() map[string]int64 {
	et.mutex.RLock()
	defer et.mutex.RUnlock()

	stats := make(map[string]int64)
	for k, v := range et.errorCount {
		stats[k] = v
	}
	return stats
}

// getStackTrace 获取堆栈跟踪
func getStackTrace() string {
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// NewLogger 创建新的日志实例
func NewLogger(verbose bool) (*zap.Logger, error) {
	config := DefaultLoggerConfig()
	config.Verbose = verbose
	return NewLoggerWithConfig(config)
}

// NewLoggerWithConfig 使用配置创建日志实例
func NewLoggerWithConfig(config *LoggerConfig) (*zap.Logger, error) {
	// 配置控制台日志级别 - 非verbose模式只显示ERROR级别，减少日志污染
	consoleLevel := zapcore.ErrorLevel
	if config.Verbose {
		consoleLevel = zapcore.DebugLevel
	}

	// 如果配置了特定日志级别，使用配置的级别
	if config.LogLevel != zapcore.InfoLevel {
		consoleLevel = config.LogLevel
	}

	// 创建控制台编码器配置
	consoleConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    colorLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("15:04:05"),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 创建文件编码器配置
	fileConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 创建控制台编码器
	consoleEncoder := zapcore.NewConsoleEncoder(consoleConfig)

	// 创建文件编码器
	fileEncoder := zapcore.NewJSONEncoder(fileConfig)

	// 创建文件写入器
	logFile := getLogFilePathWithConfig(config)
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	// 创建核心 - 控制台只显示WARN及以上级别，文件记录所有级别
	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stderr), consoleLevel),
		zapcore.NewCore(fileEncoder, zapcore.AddSync(file), zapcore.DebugLevel),
	)

	// 创建日志器
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}

// colorLevelEncoder 彩色级别编码器
func colorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	var coloredLevel string
	switch level {
	case zapcore.DebugLevel:
		coloredLevel = color.CyanString("[DEBUG]")
	case zapcore.InfoLevel:
		coloredLevel = color.GreenString("[INFO] ")
	case zapcore.WarnLevel:
		coloredLevel = color.YellowString("[WARN] ")
	case zapcore.ErrorLevel:
		coloredLevel = color.RedString("[ERROR]")
	case zapcore.DPanicLevel:
		coloredLevel = color.MagentaString("[DPANIC]")
	case zapcore.PanicLevel:
		coloredLevel = color.MagentaString("[PANIC]")
	case zapcore.FatalLevel:
		coloredLevel = color.RedString("[FATAL]")
	default:
		coloredLevel = level.CapitalString()
	}
	enc.AppendString(coloredLevel)
}

// getLogFilePathWithConfig 使用配置获取日志文件路径
func getLogFilePathWithConfig(config *LoggerConfig) string {
	// 创建日志目录
	logDir := config.LogDir
	if logDir == "" {
		logDir = "./output/logs"
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// 如果无法创建目录，使用当前目录
		logDir = "."
	}

	// 生成日志文件名
	timestamp := time.Now().Format("20060102")
	component := config.Component
	if component == "" {
		component = "pixly"
	}
	logFileName := filepath.Join(logDir, component+"_"+timestamp+".log")

	return logFileName
}

// CreateComponentLogger 为组件创建子日志器
func CreateComponentLogger(parent *zap.Logger, component string) *zap.Logger {
	return parent.Named(component)
}
