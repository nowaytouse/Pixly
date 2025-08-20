package main

import (
	"bufio"
	"context"
	"flag" // 添加flag包导入
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func NewAppContext(c Config, t ToolCheckResults) (*AppContext, error) {
	if err := validateConfig(&c); err != nil {
		return nil, err
	}
	if err := checkArchitecture(); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "media_converter_go_*")
	if err != nil {
		return nil, fmt.Errorf("无法创建主临时目录: %w", err)
	}
	resultsDir := filepath.Join(c.TargetDir, ".media_conversion_results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建结果目录: %w", err)
	}
	logsDir := filepath.Join(c.TargetDir, ".logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建日志目录: %w", err)
	}
	logFileName := filepath.Join(logsDir, fmt.Sprintf("%s_run_%s.log", c.Mode, time.Now().Format("20060102_150405")))
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建日志文件: %w", err)
	}
	logger := newStructuredLogger(logFile, parseLogLevel(c.LogLevel))
	// 初始化清理白名单
	cleanupWhitelist := make(map[string]bool)
	cleanupWhitelist[".backups"] = true
	cleanupWhitelist[".media_conversion_results"] = true
	cleanupWhitelist[".logs"] = true
	// 初始化修复信号量，限制同时修复任务数量
	repairSem := make(chan struct{}, 3) // 最多同时修复3个文件
	app := &AppContext{
		Config:           c,
		Tools:            t,
		Logger:           logger,
		TempDir:          tempDir,
		ResultsDir:       resultsDir,
		LogFile:          logFile,
		cleanupWhitelist: cleanupWhitelist,
		repairSem:        repairSem,
	}
	return app, nil
}

func (app *AppContext) Cleanup() {
	if app.LogFile != nil {
		app.LogFile.Close()
	}
	if app.TempDir != "" {
		os.RemoveAll(app.TempDir)
	}
}

// 添加架构检查，只适配macOS m芯片 arm架构
func checkArchitecture() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("此程序仅支持 macOS 系统")
	}
	// 检查是否为ARM架构（Apple Silicon）
	if runtime.GOARCH != "arm64" {
		return fmt.Errorf("此程序仅支持 Apple Silicon (M1/M2/M3) 芯片")
	}
	return nil
}

func executeStreamingPipeline(config Config, tools ToolCheckResults) error {
	app, err := NewAppContext(config, tools)
	if err != nil {
		return err
	}
	defer app.Cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		printToConsole(red("\n接收到中断信号，正在优雅地关闭...请稍候...\n"))
		cancel()
	}()
	app.runStarted = time.Now()
	pathChan := make(chan string, 1024)         // 减小缓冲区大小，避免内存压力
	taskChan := make(chan *FileTask, 2048)      // 减小缓冲区大小，避免内存压力
	lowQualityChan := make(chan *FileTask, 512) // 减小缓冲区大小，避免内存压力
	resultChan := make(chan *ConversionResult, 1024)

	scanCtx, scanCancel := context.WithCancel(ctx)
	go showScanProgress(scanCtx, app)

	// 启动发现阶段
	go func() {
		if err := discoveryStage(ctx, app, pathChan); err != nil && err != context.Canceled {
			app.Logger.Error("发现阶段出错", "error", err)
			cancel()
		}
	}()

	// 启动评估阶段
	go func() {
		if err := assessmentStage(ctx, app, pathChan, taskChan, lowQualityChan); err != nil && err != context.Canceled {
			app.Logger.Error("评估阶段出错", "error", err)
			cancel()
		}
		close(lowQualityChan)
	}()

	// 收集低质量文件
	var lowQualityFiles []*FileTask
	for task := range lowQualityChan {
		lowQualityFiles = append(lowQualityFiles, task)
		if len(lowQualityFiles) > 10000 {
			break
		}
	}

	// 显示质量分布统计
	fmt.Printf("\n%s\n", bold(cyan("📊 质量分布统计与处理计划")))
	fmt.Printf("  %s 极高质量: %d → 将使用质量模式\n", violet("🌟"), app.extremeHighCount.Load())
	fmt.Printf("  %s 高质量: %d → 将使用质量模式\n", violet("⭐"), app.highCount.Load())
	fmt.Printf("  %s 中质量: %d → 将使用质量模式\n", violet("✨"), app.mediumCount.Load())
	fmt.Printf("  %s 低质量: %d → 将使用效率模式\n", violet("💤"), app.lowCount.Load())
	fmt.Printf("  %s 极低质量: %d → 将跳过或由用户决定\n", violet("⚠️"), app.extremeLowCount.Load())

	// 等待用户确认
	fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回: ")))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if strings.ToLower(input) == "n" {
		return nil
	}

	// 处理低质量文件
	batchChoice, interactionErr := handleBatchLowQualityInteraction(lowQualityFiles, app)
	if interactionErr != nil {
		return fmt.Errorf("批量交互失败: %w", interactionErr)
	}

	go func() {
		for _, task := range lowQualityFiles {
			task.BatchDecision = batchChoice
			taskChan <- task
		}
		close(taskChan)
	}()

	scanCancel()
	time.Sleep(100 * time.Millisecond)

	go showConversionProgress(ctx, app)
	go app.memoryWatchdog(ctx)

	conversionErr := conversionStage(ctx, app, taskChan, resultChan)
	if conversionErr != nil && conversionErr != context.Canceled {
		app.Logger.Error("转换阶段出错", "error", conversionErr)
	}

	resultProcessingErr := resultProcessingStage(ctx, app, resultChan)
	if resultProcessingErr != nil && resultProcessingErr != context.Canceled {
		app.Logger.Error("结果处理阶段出错", "error", resultProcessingErr)
	}

	app.totalFilesToProcess.Store(app.filesAssessedCount.Load() - app.resumedCount.Load())
	report := app.generateReport(true)
	fmt.Println("\n" + report)

	reportPath := filepath.Join(app.Config.TargetDir, fmt.Sprintf("conversion_report_%s.txt", time.Now().Format("20060102_150405")))
	if err := os.WriteFile(reportPath, []byte(app.generateReport(false)), 0644); err != nil {
		app.Logger.Warn("无法保存报告文件", "path", reportPath, "error", err)
	}

	return nil
}

func (app *AppContext) memoryWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.Alloc > 2*1024*1024*1024 && app.Config.ConcurrentJobs > 1 {
				newJobs := app.Config.ConcurrentJobs - 1
				if newJobs < 1 {
					newJobs = 1
				}
				if newJobs != app.Config.ConcurrentJobs {
					app.mu.Lock()
					app.Config.ConcurrentJobs = newJobs
					app.mu.Unlock()
					app.Logger.Warn("检测到高内存使用，动态降低并发数", "new_jobs", newJobs)
				}
			}
		}
	}
}

func parseFlags() Config {
	var c Config
	var disableBackup bool
	flag.StringVar(&c.Mode, "mode", "auto", "转换模式: 'quality', 'efficiency', or 'auto'")
	flag.StringVar(&c.TargetDir, "dir", "", "目标目录路径")
	flag.IntVar(&c.ConcurrentJobs, "jobs", 0, "并行任务数 (0 for auto: 75% of CPU cores, max 7)")
	flag.BoolVar(&disableBackup, "no-backup", false, "禁用备份")
	flag.BoolVar(&c.HwAccel, "hwaccel", true, "启用硬件加速")
	flag.StringVar(&c.SortOrder, "sort-by", "quality", "处理顺序: 'quality', 'size', 'default'")
	flag.IntVar(&c.MaxRetries, "retry", 2, "失败后最大重试次数")
	flag.BoolVar(&c.Overwrite, "overwrite", false, "强制重新处理所有文件")
	flag.StringVar(&c.LogLevel, "log-level", "info", "日志级别: 'debug', 'info', 'warn', 'error'")
	flag.IntVar(&c.CRF, "crf", 28, "效率模式CRF值")
	flag.Parse()
	c.EnableBackups = !disableBackup
	if c.TargetDir == "" && flag.NArg() > 0 {
		c.TargetDir = flag.Arg(0)
	}
	c.QualityConfig = getDefaultQualityConfig()
	return c
}
