package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
)

// UserChoice represents the user's decision for batch operations.
type UserChoice int

const (
	ChoiceNotApplicable UserChoice = iota
	ChoiceSkip
	ChoiceDelete
	ChoiceForceConvert
)

// Use color functions from logger.go

// checkDependencies verifies that all required command-line tools are available.
func checkDependencies() ToolCheckResults {
	fmt.Println("\n" + bold("🔍 正在检查依赖工具..."))
	var tools ToolCheckResults

	if _, err := exec.LookPath("cjxl"); err == nil {
		tools.HasCjxl = true
		fmt.Println(green("  ✅ cjxl: 已找到 (用于 JXL 转换)"))
	} else {
		fmt.Println(red("  ❌ cjxl: 未找到 (JXL 转换将不可用)"))
		fmt.Println(subtle("     请通过 Homebrew 安装: brew install jpeg-xl"))
	}

	if ffmpegPath, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Println(green("  ✅ ffmpeg: 已找到"))
		out, err := exec.Command(ffmpegPath, "-codecs").Output()
		if err == nil {
			if strings.Contains(string(out), "libsvtav1") {
				tools.HasLibSvtAv1 = true
				fmt.Println(green("    ✅ libsvtav1: 已找到 (用于 AVIF 动图高质量编码)"))
			} else {
				fmt.Println(yellow("    ⚠️ libsvtav1: 未找到 (AVIF 动图编码质量可能下降)"))
			}
			if strings.Contains(string(out), "videotoolbox") {
				tools.HasVToolbox = true
				fmt.Println(green("    ✅ VideoToolbox: 已找到 (支持 macOS 硬件加速)"))
			} else {
				fmt.Println(yellow("    ⚠️ VideoToolbox: 未找到 (无法使用硬件加速)"))
			}
		}
	} else {
		fmt.Println(red("  ❌ ffmpeg: 未找到 (视频和动图转换将不可用)"))
		fmt.Println(subtle("     请通过 Homebrew 安装: brew install ffmpeg"))
	}

	if _, err := exec.LookPath("exiftool"); err == nil {
		fmt.Println(green("  ✅ exiftool: 已找到 (用于元数据迁移)"))
	} else {
		fmt.Println(red("  ❌ exiftool: 未找到 (元数据将不会被保留)"))
		fmt.Println(subtle("     请通过 Homebrew 安装: brew install exiftool"))
	}

	if _, err := exec.LookPath("avifenc"); err == nil {
		fmt.Println(green("  ✅ avifenc: 已找到 (用于 AVIF 静图转换)"))
	} else {
		fmt.Println(red("  ❌ avifenc: 未找到 (AVIF 静图转换将不可用)"))
		fmt.Println(subtle("     请通过 Homebrew 安装: brew install libavif"))
	}

	fmt.Println(bold("-----------------------------------"))
	return tools
}

// executeStreamingPipeline is the main pipeline function that orchestrates the conversion process.
func executeStreamingPipeline(ctx context.Context, config Config, tools ToolCheckResults) error {
	if err := validateConfig(&config); err != nil {
		return fmt.Errorf("配置验证失败: %w", err)
	}

	// Create temporary directory for processing
	tempDir, err := os.MkdirTemp("", "pixly_*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create results directory for resume functionality
	resultsDir := filepath.Join(config.TargetDir, ".pixly_results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("创建结果目录失败: %w", err)
	}

	// Initialize application context
	app := &AppContext{
		Config:     config,
		TempDir:    tempDir,
		ResultsDir: resultsDir,
		runStarted: time.Now(), // Initialize run start time
	}

	// Setup logger
	logFile, err := os.Create(filepath.Join(tempDir, "pixly.log"))
	if err != nil {
		return fmt.Errorf("创建日志文件失败: %w", err)
	}
	defer logFile.Close()
	
	app.LogFile = logFile
	app.Logger = newStructuredLogger(logFile, parseLogLevel(config.LogLevel))

	// Create channels for the pipeline
	pathChan := make(chan string, 100)
	preAssessedTaskChan := make(chan *FileTask, 100)  // From assessment stage
	lowQualityChan := make(chan *FileTask, 100)
	assessedTaskChan := make(chan *FileTask, 100)     // Combined channel for all assessed tasks
	routedTaskChan := make(chan *FileTask, 100)
	resultChan := make(chan *ConversionResult, 100)

	// Start background goroutines for UI updates
	scanCtx, scanCancel := context.WithCancel(ctx)
	defer scanCancel()
	go showScanProgress(scanCtx, app)

	convCtx, convCancel := context.WithCancel(ctx)
	defer convCancel()
	go showConversionProgress(convCtx, app)

	// Start pipeline stages as goroutines
	var wg sync.WaitGroup

	// Stage 1: File discovery
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(pathChan)
		discoverFiles(ctx, app, pathChan)
	}()

	// Stage 2: Assessment and classification
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(preAssessedTaskChan)
		defer close(lowQualityChan)
		assessmentStage(ctx, app, pathChan, preAssessedTaskChan, lowQualityChan)
	}()

	// Stage 3: Handle low quality files (batch interaction) and combine streams
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(assessedTaskChan) // Close the combined channel when done
		// First, collect and process low quality files
		handleLowQualityFiles(ctx, app, lowQualityChan, assessedTaskChan)
		
		// Then, pass through all pre-assessed tasks
		for task := range preAssessedTaskChan {
			select {
			case assessedTaskChan <- task:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Stage 4: Route tasks based on mode
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(routedTaskChan)
		app.Logger.Info("开始路由任务", "mode", app.Config.Mode)
		routeTasks(ctx, app, assessedTaskChan, routedTaskChan)
		app.Logger.Info("路由任务完成")
	}()

	// Stage 5: Conversion
	wg.Add(1)
	go func() {
		defer wg.Done()
		app.Logger.Info("开始转换任务")
		conversionStage(ctx, app, routedTaskChan, resultChan)
		app.Logger.Info("转换任务完成")
		close(resultChan) // Close the result channel when conversion is done
	}()

	// Stage 6: Result processing
	wg.Add(1)
	go func() {
		defer wg.Done()
		resultProcessingStage(ctx, app, resultChan)
	}()

	// Wait for all stages to complete
	wg.Wait()

	// Generate and display final report
	scanCancel()   // Stop scan progress
	convCancel()   // Stop conversion progress
	time.Sleep(100 * time.Millisecond) // Allow progress indicators to finish

	report := app.generateReport(true)
	fmt.Print("\n\n" + report)

	return nil
}

// handleLowQualityFiles handles batch interaction for very low quality files.
func handleLowQualityFiles(ctx context.Context, app *AppContext, lowQualityChan <-chan *FileTask, assessedTaskChan chan<- *FileTask) {
	var lowQualityFiles []*FileTask
	for task := range lowQualityChan {
		lowQualityFiles = append(lowQualityFiles, task)
	}

	if len(lowQualityFiles) > 0 && app.Config.Mode == "auto" {
		choice, err := handleBatchLowQualityInteraction(lowQualityFiles, app)
		if err != nil {
			app.Logger.Error("处理极低质量文件时出错", "error", err)
			// Even on error, we need to send the files through
			for _, task := range lowQualityFiles {
				task.Action = ActionSkip // Default to skip on error
				select {
				case assessedTaskChan <- task:
				case <-ctx.Done():
					return
				}
			}
			return
		}

		// Apply the user's choice to all low quality files and send them downstream
		for _, task := range lowQualityFiles {
			switch choice {
			case ChoiceDelete:
				task.Action = ActionDelete
			case ChoiceForceConvert:
				task.Action = ActionConvert
			case ChoiceSkip:
				task.Action = ActionSkip
			}
			
			// Send the task to the next stage
			select {
			case assessedTaskChan <- task:
			case <-ctx.Done():
				return
			}
		}
	} else {
		// For non-auto modes or when there are no low quality files,
		// just pass them through with default action (skip)
		for _, task := range lowQualityFiles {
			if app.Config.Mode != "auto" {
				task.Action = ActionSkip // Skip low quality files in non-auto modes
			} else {
				task.Action = ActionConvert // Or convert them if needed
			}
			
			// Send the task to the next stage
			select {
			case assessedTaskChan <- task:
			case <-ctx.Done():
				return
			}
		}
	}
}

// routeTasks routes file tasks based on the selected mode.
func routeTasks(ctx context.Context, app *AppContext, inTaskChan <-chan *FileTask, outTaskChan chan<- *FileTask) {
	taskCount := 0
	for task := range inTaskChan {
		taskCount++
		app.Logger.Info("路由任务", "file", filepath.Base(task.Path), "mode", app.Config.Mode, "file_type", task.Type)
		
		// Set target format and conversion type based on mode
		switch app.Config.Mode {
		case "quality":
			// Quality mode: JXL for static, AVIF for animated, MOV for video
			switch task.Type {
			case Static:
				task.TargetFormat = TargetFormatJXL
				task.ConversionType = ConversionTypeLossless
			case Animated:
				task.TargetFormat = TargetFormatAVIF
				task.ConversionType = ConversionTypeLossless
			case Video:
				task.TargetFormat = TargetFormatMOV
				task.ConversionType = ConversionTypeLossless
			}
			task.Action = ActionConvert

		case "efficiency":
			// Efficiency mode: JXL for static, AVIF for animated, MOV for video (same as quality mode for formats)
			switch task.Type {
			case Static:
				task.TargetFormat = TargetFormatJXL
				task.ConversionType = ConversionTypeLossy
			case Animated:
				task.TargetFormat = TargetFormatAVIF
				task.ConversionType = ConversionTypeLossy
			case Video:
				task.TargetFormat = TargetFormatMOV
				task.ConversionType = ConversionTypeLossy
			}
			task.Action = ActionConvert

		case "sticker":
			// Sticker mode: Aggressive compression
			task.TargetFormat = TargetFormatAVIF
			task.ConversionType = ConversionTypeLossy
			task.IsStickerMode = true
			task.Action = ActionConvert

		default: // auto mode
			// Route based on quality assessment
			switch task.Quality {
			case QualityExtremeHigh, QualityHigh:
				// Use quality mode for high quality files
				if task.Type == Static {
					task.TargetFormat = TargetFormatJXL
					task.ConversionType = ConversionTypeLossless
				} else if task.Type == Animated {
					task.TargetFormat = TargetFormatAVIF
					task.ConversionType = ConversionTypeLossless
				} else {
					task.TargetFormat = TargetFormatMOV
					task.ConversionType = ConversionTypeLossless
				}
				task.Action = ActionConvert

			case QualityMedium, QualityLow, QualityExtremeLow:
				// Use efficiency mode for lower quality files
				if task.Type == Static {
					task.TargetFormat = TargetFormatJXL
					task.ConversionType = ConversionTypeLossy
				} else if task.Type == Animated {
					task.TargetFormat = TargetFormatAVIF
					task.ConversionType = ConversionTypeLossy
				} else {
					task.TargetFormat = TargetFormatMOV
					task.ConversionType = ConversionTypeLossy
				}
				task.Action = ActionConvert

			default:
				task.Action = ActionSkip
			}
		}
		
		app.Logger.Info("路由决策", "file", filepath.Base(task.Path), "target_format", task.TargetFormat, "conversion_type", task.ConversionType, "action", task.Action)

		// Send the task to the next stage
		select {
		case outTaskChan <- task:
			app.Logger.Info("任务发送成功", "file", filepath.Base(task.Path))
		case <-ctx.Done():
			app.Logger.Warn("上下文取消，任务发送失败", "file", filepath.Base(task.Path))
			return
		}
	}
	
	app.Logger.Info("路由阶段完成", "processed_tasks", taskCount)
}

// discoverFiles walks the directory and sends file paths to the path channel.
func discoverFiles(ctx context.Context, app *AppContext, pathChan chan<- string) {
	err := filepath.Walk(app.Config.TargetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			app.Logger.Warn("访问文件时出错", "path", path, "error", err)
			return nil
		}

		if info.IsDir() {
			// Skip hidden directories and backup directories
			if strings.HasPrefix(info.Name(), ".") || info.Name() == ".backups" || info.Name() == ".pixly_results" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files and result files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		app.filesFoundCount.Add(1)
		select {
		case pathChan <- path:
		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})

	if err != nil {
		app.Logger.Error("文件发现阶段出错", "error", err)
	}
}

func main() {
	color.NoColor = false // Ensure color output is enabled

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan // First signal
		fmt.Println("\n" + yellow("⏳ 正在尝试强制关闭, 请稍候..."))
		cancel() // Signal all goroutines to stop
		time.Sleep(2 * time.Second) // Give some time for graceful shutdown

		<-sigChan // Second signal
		fmt.Println(red("🚨 接收到第二次中断信号, 强制退出."))
		os.Exit(1) // Force exit
	}()

	// --- Application Startup ---
	fmt.Printf("🚀 Pixly v%s 启动中...\n", AppVersion)
	fmt.Printf("💻 系统信息: %s, 架构: %s\n", runtime.GOOS, runtime.GOARCH)

	if runtime.GOOS != "darwin" {
		fmt.Println(red("❌ 错误: 此程序专为 macOS 设计."))
		os.Exit(1)
	}
	if !strings.Contains(runtime.GOARCH, "arm") {
		fmt.Println(yellow("⚠️ 警告: 非 ARM 架构 (%s), 程序可能无法正常工作.", runtime.GOARCH))
	} else {
		fmt.Println(green("✅ 检测到 ARM 架构: %s", runtime.GOARCH))
	}

	tools := checkDependencies()

	// --- Mode Selection ---
	if len(os.Args) > 1 {
		config := parseFlags()
		if config.TargetDir != "" {
			fmt.Println("📌 检测到命令行参数，进入非交互模式")
			if err := executeStreamingPipeline(ctx, config, tools); err != nil {
				log.Fatalf("FATAL: %v", err)
			}
		} else {
			fmt.Println("✅ 未提供 -dir, 进入交互模式")
			interactiveSessionLoop(ctx, tools)
		}
	} else {
		fmt.Println("✅ 进入交互模式")
		interactiveSessionLoop(ctx, tools)
	}

	fmt.Println("\n" + green("✅ 程序执行完成."))
}

// parseFlags defines and parses command-line flags for non-interactive mode.
func parseFlags() Config {
	var c Config
	var disableBackup bool

	fs := flag.NewFlagSet("pixly", flag.ExitOnError)

	fs.StringVar(&c.Mode, "mode", "auto", "转换模式: 'quality', 'efficiency', 'auto', or 'sticker'")
	fs.StringVar(&c.TargetDir, "dir", "", "目标目录路径")
	fs.IntVar(&c.ConcurrentJobs, "jobs", 0, "并行任务数 (0 for auto)")
	fs.BoolVar(&disableBackup, "no-backup", false, "禁用备份")
	fs.BoolVar(&c.HwAccel, "hwaccel", true, "启用硬件加速")
	fs.StringVar(&c.SortOrder, "sort-by", "quality", "处理顺序: 'quality', 'size', 'default'")
	fs.IntVar(&c.MaxRetries, "retry", 2, "失败后最大重试次数")
	fs.BoolVar(&c.Overwrite, "overwrite", false, "强制重新处理所有文件")
	fs.StringVar(&c.LogLevel, "log-level", "info", "日志级别: 'debug', 'info', 'warn', 'error'")
	fs.IntVar(&c.CRF, "crf", 28, "效率模式CRF值")
	fs.StringVar(&c.StickerTargetFormat, "sticker-format", "avif", "表情包模式的目标格式")

	fs.Parse(os.Args[1:])

	c.EnableBackups = !disableBackup
	c.QualityConfig = getDefaultQualityConfig()
	return c
}