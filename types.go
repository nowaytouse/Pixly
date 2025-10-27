package main

import (
	"os"
	"time"
)

// Logger interface defines the methods that a logger should implement.
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	With(fields ...interface{}) Logger
}

// FileTask represents a single file that needs to be processed by the pipeline.
// It holds all information required for assessment, routing, and conversion.
type FileTask struct {
	Path     string
	Ext      string
	Size     int64
	MimeType string
	Type     MediaType // Enum: Static, Animated, Video

	// Assessment results
	Quality QualityLevel

	// User/Programmatic Decisions
	BatchDecision UserChoice // User's choice for a batch of low-quality files
	Action        Action     // Final action to take: Convert, Skip, Delete

	// Conversion Parameters
	TargetFormat   TargetFormat   // e.g., "jxl", "avif"
	ConversionType ConversionType // e.g., "Lossless", "Lossy"
	IsStickerMode  bool           // Special flag for sticker mode's aggressive compression

	// Contextual info
	Logger  Logger
	TempDir string
}

// ConversionResult holds the outcome of a conversion task.
type ConversionResult struct {
	OriginalPath string
	FinalPath    string
	OriginalSize int64
	NewSize      int64
	Decision     string // e.g., "SUCCESS", "SKIP_LARGER", "FAIL_CONVERSION"
	Error        error
	Task         *FileTask // Include the original task for context
}

// AppContext holds the application state and counters during execution.
type AppContext struct {
	Config              Config
	Logger              Logger
	TempDir             string
	ResultsDir          string
	LogFile             *os.File
	runStarted          time.Time

	// Counters
	filesFoundCount     Counter
	filesAssessedCount  Counter
	totalFilesToProcess Counter
	processedCount      Counter
	successCount        Counter
	failCount           Counter
	skipCount           Counter
	deleteCount         Counter
	resumedCount        Counter
	retrySuccessCount   Counter
	smartDecisionsCount Counter
	losslessWinsCount   Counter
	totalIncreased      Counter
	totalDecreased      Counter
	extremeHighCount    Counter
	highCount           Counter
	mediumCount         Counter
	lowCount            Counter
	extremeLowCount     Counter
}
