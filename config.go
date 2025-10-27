package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

type QualityConfig struct {
	ExtremeHighThreshold float64
	HighThreshold        float64
	MediumThreshold      float64
	LowThreshold         float64
}

type ToolCheckResults struct {
	HasCjxl      bool
	HasLibSvtAv1 bool
	HasVToolbox  bool
}

type Config struct {
	Mode           string
	TargetDir      string
	BackupDir      string
	ConcurrentJobs int
	MaxRetries     int
	CRF            int
	EnableBackups  bool
	HwAccel        bool
	Overwrite      bool
	LogLevel       string
	SortOrder      string
	QualityConfig  QualityConfig
}

func getDefaultQualityConfig() QualityConfig {
	return QualityConfig{
		ExtremeHighThreshold: 0.25,
		HighThreshold:        0.15,
		MediumThreshold:      0.08,
		LowThreshold:         0.03,
	}
}

func validateConfig(c *Config) error {
	if c.TargetDir == "" {
		return errors.New("目标目录不能为空")
	}
	absPath, err := filepath.Abs(c.TargetDir)
	if err != nil {
		return fmt.Errorf("无法解析目标目录路径: %w", err)
	}
	c.TargetDir = absPath
	if _, err := os.Stat(c.TargetDir); os.IsNotExist(err) {
		return fmt.Errorf("目标目录不存在: %s", c.TargetDir)
	}
	if c.ConcurrentJobs <= 0 {
		cpuCount := runtime.NumCPU()
		c.ConcurrentJobs = int(math.Max(1.0, float64(cpuCount)*0.75))
		if c.ConcurrentJobs > 7 {
			c.ConcurrentJobs = 7
		}
	}
	if c.BackupDir == "" {
		c.BackupDir = filepath.Join(c.TargetDir, ".backups")
	}
	if c.CRF == 0 {
		c.CRF = 28
	}
	return nil
}
