package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

// showScanProgress displays a progress indicator for the file scanning phase.
// It now uses the modular ProgressBar system with hang detection.
func showScanProgress(ctx context.Context, app *AppContext) {
	// Create a progress bar for scanning
	pb := NewProgressBar(ctx, ProgressTypeScan, "扫描中", 0)
	defer pb.Complete()
	
	// Set force exit function
	pb.SetForceExitFunc(func() {
		printToConsole(red("❌ 30秒内无文件扫描完成,疑似卡死. 强制退出."))
		app.Logger.Error("错误: 30秒内无文件扫描完成,疑似卡死. 强制退出.")
		os.Exit(1)
	})
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			found := app.filesFoundCount.Load()
			assessed := app.filesAssessedCount.Load()
			printToConsole("🔍 扫描完成. [已发现: %d | 已评估: %d]", found, assessed)
			return
		case <-ticker.C:
			found := app.filesFoundCount.Load()
			assessed := app.filesAssessedCount.Load()
			// Update progress bar with current counts
			pb.Update(found + assessed)
		}
	}
}

// showConversionProgress displays a progress bar for the conversion phase.
// It now uses the modular ProgressBar system with hang detection.
func showConversionProgress(ctx context.Context, app *AppContext) {
	// Create a progress bar for conversion
	pb := NewProgressBar(ctx, ProgressTypeConvert, "转换中", 0)
	defer pb.Complete()
	
	// Set force exit function
	pb.SetForceExitFunc(func() {
		cc := app.processedCount.Load()
		app.Logger.Error("错误: 30秒内无文件处理完成,疑似卡死. 强制退出.", "processedCount", cc)
		printToConsole(red("❌ 30秒内无文件处理完成,疑似卡死. 强制退出."))
		os.Exit(1)
	})
	
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cc := app.processedCount.Load()
			tt := app.totalFilesToProcess.Load()
			
			// Update progress bar
			if tt > 0 {
				pb.total = tt
				pb.Update(cc)
			} else {
				pb.Update(cc)
			}
			
			// Set run start time if not already set
			if tt > 0 && app.runStarted.IsZero() {
				app.runStarted = time.Now()
			}
		}
	}
}

// generateReport creates the final summary report after the conversion process.
func (app *AppContext) generateReport(useColor bool) string {
	b, c, g, r, v, s := bold, cyan, green, red, violet, subtle
	if !useColor {
		noColor := func(a ...interface{}) string { return fmt.Sprint(a...) }
		b, c, g, r, v, s = noColor, noColor, noColor, noColor, noColor, noColor
	}

	var report strings.Builder
	increased := formatBytes(app.totalIncreased.Load())
	decreased := formatBytes(app.totalDecreased.Load())
	if increased == "" {
		increased = "0 B"
	}
	if decreased == "" {
		decreased = "0 B"
	}

	// Sticker Mode Report
	if app.Config.Mode == "sticker" {
		report.WriteString(fmt.Sprintf("%s\n", b(c("📊 ================= 表情包模式转换报告 =================="))))
		report.WriteString(fmt.Sprintf("%s %s\n", s("📁 目录:"), app.Config.TargetDir))
		report.WriteString(fmt.Sprintf("%s %s    %s %s\n", s("⚙️ 模式:"), "表情包模式", s("🚀 版本:"), AppVersion))
		
		// Fix time calculation
		var durationStr string
		if !app.runStarted.IsZero() {
			durationStr = time.Since(app.runStarted).Round(time.Second).String()
		} else {
			durationStr = "0s"
		}
		report.WriteString(fmt.Sprintf("%s %s\n", s("⏰ 耗时:"), durationStr))
		report.WriteString(fmt.Sprintf("  %s 成功转换: %d\n", g("✅"), app.successCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 主动跳过: %d\n", s("⏭️"), app.skipCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 转换失败: %d\n", r("❌"), app.failCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 空间变化: ⬇️ %s\n", g("💰"), b(g(decreased))))
		report.WriteString("--------------------------------------------------------\n")
		report.WriteString(fmt.Sprintf("%s %s\n", s("📄 详细日志:"), app.LogFile.Name()))
		return report.String()
	}

	// Set run start time if not already set
	if app.runStarted.IsZero() {
		app.runStarted = time.Now()
	}
	
	// Standard Report for other modes
	report.WriteString(fmt.Sprintf("%s\n", b(c("📊 ================= 媒体转换最终报告 =================="))))
	report.WriteString(fmt.Sprintf("%s %s\n", s("📁 目录:"), app.Config.TargetDir))
	report.WriteString(fmt.Sprintf("%s %s    %s %s\n", s("⚙️ 模式:"), app.Config.Mode, s("🚀 版本:"), AppVersion))
	
	// Fix time calculation
	var durationStr string
	if !app.runStarted.IsZero() {
		durationStr = time.Since(app.runStarted).Round(time.Second).String()
	} else {
		durationStr = "0s"
	}
	report.WriteString(fmt.Sprintf("%s %s\n", s("⏰ 耗时:"), durationStr))
	report.WriteString(fmt.Sprintf("%s\n", b(c("---" + " 📋 概览 (本次运行) " + "---"))))
	totalScanned := app.filesFoundCount.Load()
	report.WriteString(fmt.Sprintf("  %s 总计发现: %d 文件\n", v("🗂️"), totalScanned))
	report.WriteString(fmt.Sprintf("  %s 成功转换: %d\n", g("✅"), app.successCount.Load()))
	if app.retrySuccessCount.Load() > 0 {
		report.WriteString(fmt.Sprintf("    %s (其中 %d 个是在重试后成功的)\n", s(""), app.retrySuccessCount.Load()))
	}
	report.WriteString(fmt.Sprintf("  %s 转换失败: %d\n", r("❌"), app.failCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 主动跳过: %d\n", s("⏭️"), app.skipCount.Load()))
	if app.deleteCount.Load() > 0 {
		report.WriteString(fmt.Sprintf("  %s 用户删除: %d\n", r("🗑️"), app.deleteCount.Load()))
	}
	report.WriteString(fmt.Sprintf("  %s 断点续传: %d (之前已处理)\n", c("🔄"), app.resumedCount.Load()))
	report.WriteString(fmt.Sprintf("%s\n", b(c("---" + " 💾 大小变化统计 (本次运行) " + "---"))))
	report.WriteString(fmt.Sprintf("  %s 空间变化: ⬆️ %s ⬇️ %s\n", g("💰"), b(r(increased)), b(g(decreased))))

	// Conditional Statistics
	if app.Config.Mode == "auto" {
		// Quality statistics only in auto mode
		if app.successCount.Load() > 0 {
			smartPct := int(float64(app.smartDecisionsCount.Load()) / float64(app.successCount.Load()) * 100)
			report.WriteString(fmt.Sprintf("%s\n", b(c("---" + " 🧠 智能效率优化统计 " + "---"))))
			report.WriteString(fmt.Sprintf("  %s 智能决策文件: %d (%d%% of 成功)\n", v("🧠"), app.smartDecisionsCount.Load(), smartPct))
			report.WriteString(fmt.Sprintf("  %s 无损优势识别: %d\n", v("💎"), app.losslessWinsCount.Load()))
		}
		
		// Quality level statistics only in auto mode
		report.WriteString(fmt.Sprintf("%s\n", b(c("---" + " 🔍 质量级别统计 " + "---"))))
		report.WriteString(fmt.Sprintf("  %s 极高质量: %d\n", v("🌟"), app.extremeHighCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 高质量: %d\n", v("⭐"), app.highCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 中质量: %d\n", v("✨"), app.mediumCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 低质量: %d\n", v("💤"), app.lowCount.Load()))
		report.WriteString(fmt.Sprintf("  %s 极低质量: %d\n", v("⚠️"), app.extremeLowCount.Load()))
	} else if app.Config.Mode == "efficiency" {
		// Efficiency mode statistics
		if app.successCount.Load() > 0 {
			smartPct := int(float64(app.smartDecisionsCount.Load()) / float64(app.successCount.Load()) * 100)
			report.WriteString(fmt.Sprintf("%s\n", b(c("---" + " 🧠 效率模式统计 " + "---"))))
			report.WriteString(fmt.Sprintf("  %s 智能决策文件: %d (%d%% of 成功)\n", v("🧠"), app.smartDecisionsCount.Load(), smartPct))
		}
	}

	report.WriteString("--------------------------------------------------------\n")
	report.WriteString(fmt.Sprintf("%s %s\n", s("📄 详细日志:"), app.LogFile.Name()))
	return report.String()
}

func showBanner() {
	color.Cyan(`
    ____  _ __  _      __ 
   / __ \ (_) /_(_)____/ /_ 
  / /_/ / / __/ / ___/ __/ 
 / ____/ / /_/ / /__/ /_ 
/_/   /_/\__/_/\___/\__/ 
`)
	fmt.Printf(bold(violet("              ✨ 欢迎使用 Pixly 媒体转换工具 v%s ✨\n")), AppVersion)
	fmt.Println(subtle("                  专为macOS设计, 稳定、海量、智能"))
	fmt.Println(subtle("                  随时按 Ctrl+C 安全退出脚本"))
	fmt.Println("=====================================================================================")
}

func printToConsole(f string, a ...interface{}) {
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	// Clears the line and returns cursor to the start
	fmt.Printf("\033[2K\r"+f, a...)
}

// handleBatchLowQualityInteraction prompts the user for how to handle very low quality files.
func handleBatchLowQualityInteraction(lowQualityFiles []*FileTask, app *AppContext) (UserChoice, error) {
	if len(lowQualityFiles) == 0 {
		return ChoiceNotApplicable, nil
	}
	consoleMutex.Lock()
	defer consoleMutex.Unlock()

	app.Logger.Warn("检测到极低质量文件", "count", len(lowQualityFiles))
	fmt.Printf("\n%s\n", yellow("------------------------- 批量处理请求 -------------------------"))
	fmt.Printf("%s: %s\n", yellow(fmt.Sprintf("检测到 %d 个极低质量文件。", len(lowQualityFiles))), bold(fmt.Sprintf("%d", len(lowQualityFiles))))
	fmt.Println(subtle("示例文件 (最多显示10个):"))
	for i, f := range lowQualityFiles {
		if i >= 10 {
			break
		}
		fmt.Printf("  - %s (%s)\n", filepath.Base(f.Path), formatBytes(f.Size))
	}
	if len(lowQualityFiles) > 10 {
		fmt.Println(subtle("  ...等更多文件。"))
	}
	fmt.Println(yellow("\n请选择如何处理所有这些文件:"))
	fmt.Printf("  %s\n", bold("[1] 全部跳过 (默认, 5秒后自动选择)"))
	fmt.Printf("  %s\n", bold("[2] 全部强制转换"))
	fmt.Printf("  %s\n", red("[3] 全部直接删除"))
	fmt.Print(yellow("请输入您的选择 [1, 2, 3]: "))

	inputChan := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		inputChan <- strings.TrimSpace(input)
	}()

	select {
	case input := <-inputChan:
		switch input {
		case "2":
			fmt.Println(green("\n已选择 [全部强制转换]"))
			return ChoiceForceConvert, nil
		case "3":
			fmt.Println(red("\n已选择 [全部直接删除]"))
			return ChoiceDelete, nil
		default:
			fmt.Println(green("\n已选择 [全部跳过]"))
			return ChoiceSkip, nil
		}
	case <-time.After(5 * time.Second):
		fmt.Println(green("\n超时，已选择 [全部跳过]"))
		return ChoiceSkip, nil
	}
}

// interactiveSessionLoop guides the user through setting up a conversion task.
func interactiveSessionLoop(ctx context.Context, t ToolCheckResults) {
	reader := bufio.NewReader(os.Stdin)
	var input string

	for {
		var c Config
		// Default settings
		c.EnableBackups = true
		c.MaxRetries = 2
		c.HwAccel = true
		c.LogLevel = "info"
		c.CRF = 28
		c.SortOrder = "quality"
		c.ConcurrentJobs = 7
		c.QualityConfig = getDefaultQualityConfig()

		showBanner()

		// Get target directory with retry limit
		failures := 0
		for {
			if failures >= 3 {
				fmt.Println(red("\n❌ 连续3次提供无效目录，程序将退出。"))
				os.Exit(1)
			}
			fmt.Print(bold(cyan("\n📂 请拖入目标文件夹，然后按 Enter: ")))
			input, _ = reader.ReadString('\n')
			trimmedInput := strings.TrimSpace(input)
			if trimmedInput == "" {
				fmt.Println(red("⚠️ 目录不能为空，请重新输入。"))
				failures++
				continue
			}
			cleanedInput := cleanPath(trimmedInput)
			info, err := os.Stat(cleanedInput)
			if err == nil {
				if !info.IsDir() {
					fmt.Println(red("⚠️ 提供的路径不是一个文件夹，请重新输入。"))
					failures++
					continue
				}
				c.TargetDir = cleanedInput
				break // Success
			}
			fmt.Println(red("⚠️ 无效的目录或路径不存在，请检查后重试。"))
			failures++
		}

		// Get conversion mode
		fmt.Println("\n" + bold(cyan("⚙️ 请选择转换模式: ")))
		fmt.Printf("  %s %s - 追求极致画质与无损，适合存档。\n", green("[1]"), bold("质量模式 (Quality)"))
		fmt.Printf("  %s %s - 智能平衡画质与体积，适合日常使用。\n", yellow("[2]"), bold("效率模式 (Efficiency)"))
		fmt.Printf("  %s %s - 程序自动为每个文件选择最佳模式。\n", violet("[3]"), bold("自动模式 (Auto)"))
		fmt.Printf("  %s %s - 极限压缩动/静图, 适合表情包收藏。\n", red("[4]"), bold("表情包模式 (Sticker)"))

		for {
			fmt.Print(bold(cyan("👉 请输入您的选择 (1/2/3/4) [回车默认 3]: ")))
			input, _ = reader.ReadString('\n')
			input = strings.TrimSpace(input)
			switch input {
			case "1":
				c.Mode = "quality"
			case "2":
				c.Mode = "efficiency"
			case "", "3":
				c.Mode = "auto"
			case "4":
				c.Mode = "sticker"
			default:
				fmt.Println(red("无效输入，请重新选择。"))
				continue
			}
			break
		}

		// Quality parameter configuration (optional)
		if c.Mode != "sticker" {
			fmt.Println(subtle("\n-------------------------------------------------"))
			fmt.Printf("  %-18s %s\n", "🌟 极高质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.ExtremeHighThreshold)))
			fmt.Printf("  %-18s %s\n", "⭐ 高质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.HighThreshold)))
			fmt.Printf("  %-18s %s\n", "✨ 中质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.MediumThreshold)))
			fmt.Printf("  %-18s %s\n", "💤 低质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.LowThreshold)))

			fmt.Print(bold(cyan("\n👉 是否调整质量参数? (y/N): ")))
			input, _ = reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(input)) == "y" {
				adjustQualityParameters(&c)
			}
		}

		// Confirm and run
		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(c.TargetDir))
		fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(c.Mode))
		fmt.Printf("  %-12s %s\n", "⚡ 并发数:", cyan(fmt.Sprintf("%d", c.ConcurrentJobs)))
		if c.Mode != "sticker" {
			fmt.Printf("  %-12s %s\n", "🌟 质量参数:", cyan("已配置"))
		}

		fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回: ")))
		input, _ = reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			continue
		}

		if err := executeStreamingPipeline(ctx, c, t); err != nil {
			printToConsole(red("❌ 任务执行出错: %v\n", err))
		}

		fmt.Print(bold(cyan("\n✨ 本轮任务已完成。是否开始新的转换? (Y/n): ")))
		input, _ = reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			fmt.Println(green("感谢使用！👋"))
			break
		}
	}
}

func adjustQualityParameters(c *Config) {
	reader := bufio.NewReader(os.Stdin)
	var input string

	fmt.Print(bold(cyan("🌟 输入极高质量阈值 (默认 0.25): ")))
	input, _ = reader.ReadString('\n')
	if val, err := strconv.ParseFloat(strings.TrimSpace(input), 64); err == nil {
		c.QualityConfig.ExtremeHighThreshold = val
	}

	fmt.Print(bold(cyan("⭐ 输入高质量阈值 (默认 0.15): ")))
	input, _ = reader.ReadString('\n')
	if val, err := strconv.ParseFloat(strings.TrimSpace(input), 64); err == nil {
		c.QualityConfig.HighThreshold = val
	}

	fmt.Print(bold(cyan("✨ 输入中质量阈值 (默认 0.08): ")))
	input, _ = reader.ReadString('\n')
	if val, err := strconv.ParseFloat(strings.TrimSpace(input), 64); err == nil {
		c.QualityConfig.MediumThreshold = val
	}

	fmt.Print(bold(cyan("💤 输入低质量阈值 (默认 0.03): ")))
	input, _ = reader.ReadString('\n')
	if val, err := strconv.ParseFloat(strings.TrimSpace(input), 64); err == nil {
		c.QualityConfig.LowThreshold = val
	}
	fmt.Println(green("✅ 质量参数已更新。"))
}