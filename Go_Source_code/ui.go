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
	"golang.org/x/term"
)

func showScanProgress(ctx context.Context, app *AppContext) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	spinner := []string{"/", "-", "\\", "|"}
	i := 0
	for {
		select {
		case <-ctx.Done():
			found := app.filesFoundCount.Load()
			assessed := app.filesAssessedCount.Load()
			printToConsole("🔍 扫描完成. [已发现: %d | 已评估: %d]", found, assessed)
			return
		case <-ticker.C:
			i = (i + 1) % len(spinner)
			found := app.filesFoundCount.Load()
			assessed := app.filesAssessedCount.Load()
			progressStr := fmt.Sprintf("🔍 %s 扫描中... [已发现: %d | 已评估: %d]", spinner[i], found, assessed)
			printToConsole(progressStr)
		}
	}
}

func showConversionProgress(ctx context.Context, app *AppContext) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cc := app.processedCount.Load()
			tt := app.totalFilesToProcess.Load()
			if tt == 0 {
				continue
			}
			pct := float64(cc) / float64(tt)
			if pct > 1.0 {
				pct = 1.0
			}

			// 获取终端宽度
			width, _, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil || width < 40 {
				width = 80 // 默认宽度
			}

			// 计算进度条宽度，适应不同终端大小
			barWidth := int(float64(width-30) * pct)
			if barWidth < 1 {
				barWidth = 1
			} else if barWidth > width-30 {
				barWidth = width - 30
			}

			bar := strings.Repeat("█", barWidth) + strings.Repeat("░", width-30-barWidth)
			var etaStr string
			if cc > 5 {
				elapsed := time.Since(app.runStarted)
				rate := float64(cc) / elapsed.Seconds()
				remaining := float64(tt - cc)
				if rate > 0 {
					eta := time.Duration(remaining/rate) * time.Second
					etaStr = eta.Round(time.Second).String()
				}
			} else {
				etaStr = "计算中..."
			}
			// 确保进度条显示清晰，避免字符交叉
			progressStr := fmt.Sprintf("🔄 处理进度 [%s] %.1f%% (%d/%d) ETA: %s", cyan(bar), pct*100, cc, tt, etaStr)
			printToConsole(progressStr)
		case <-ctx.Done():
			return
		}
	}
}

func (app *AppContext) generateReport(useColor bool) string {
	b, c, g, r, v, s := bold, cyan, green, red, violet, subtle
	if !useColor {
		noColor := func(a ...interface{}) string { return fmt.Sprint(a...) }
		b, c, g, r, v, s = noColor, noColor, noColor, noColor, noColor, noColor
	}
	var report strings.Builder
	report.WriteString(fmt.Sprintf("%s\n", b(c("📊 ================= 媒体转换最终报告 =================="))))
	report.WriteString(fmt.Sprintf("%s %s\n", s("📁 目录:"), app.Config.TargetDir))
	report.WriteString(fmt.Sprintf("%s %s    %s %s\n", s("⚙️ 模式:"), app.Config.Mode, s("🚀 版本:"), Version))
	report.WriteString(fmt.Sprintf("%s %s\n", s("⏰ 耗时:"), time.Since(app.runStarted).Round(time.Second)))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 📋 概览 (本次运行) ---"))))
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
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 💾 大小变化统计 (本次运行) ---"))))

	// 修复空间变化显示问题，确保图标含义正确
	// ⬆️ 表示文件大小增加（空间占用变多）
	// ⬇️ 表示文件大小减少（空间节省）
	increased := formatBytes(app.totalIncreased.Load())
	decreased := formatBytes(app.totalDecreased.Load())

	// 确保即使为0也显示"0 B"，避免显示错乱
	if increased == "" {
		increased = "0 B"
	}
	if decreased == "" {
		decreased = "0 B"
	}

	report.WriteString(fmt.Sprintf("  %s 空间变化: ⬆️ %s ⬇️ %s\n", g("💰"), b(g(increased)), b(g(decreased))))

	if app.Config.Mode != "quality" && app.successCount.Load() > 0 {
		smartPct := int(float64(app.smartDecisionsCount.Load()) / float64(app.successCount.Load()) * 100)
		report.WriteString(fmt.Sprintf("%s\n", b(c("--- 🧠 智能效率优化统计 ---"))))
		report.WriteString(fmt.Sprintf("  %s 智能决策文件: %d (%d%% of 成功)\n", v("🧠"), app.smartDecisionsCount.Load(), smartPct))
		report.WriteString(fmt.Sprintf("  %s 无损优势识别: %d\n", v("💎"), app.losslessWinsCount.Load()))
	}
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 🔍 质量级别统计 ---"))))
	report.WriteString(fmt.Sprintf("  %s 极高质量: %d\n", v("🌟"), app.extremeHighCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 高质量: %d\n", v("⭐"), app.highCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 中质量: %d\n", v("✨"), app.mediumCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 低质量: %d\n", v("💤"), app.lowCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 极低质量: %d\n", v("⚠️"), app.extremeLowCount.Load()))
	report.WriteString("--------------------------------------------------------\n")
	report.WriteString(fmt.Sprintf("%s %s\n", s("📄 详细日志:"), app.LogFile.Name()))
	return report.String()
}

func showBanner() {
	color.Cyan(`
    __  ___ __  __ ____   ____ _   _    _    _   _ _____ ____ _____ ____  
   |  \/  |  \/  | __ ) / ___| | | |  / \  | \ | |_   _/ ___|_   _|  _ \ 
   | |\/| | |\/| |  _ \| |   | |_| | / _ \ |  \| | | || |     | | | |_) |
   | |  | | |  | | |_) | |___|  _  |/ ___ \| |\  | | || |___  | | |  _ < 
   |_|  |_|_|  |_|____/ \____|_| |_/_/   \_\_| \_| |_| \____| |_| |_| \_\
	`)
	fmt.Printf(bold(violet("              ✨ 欢迎使用媒体批量转换脚本 v%s ✨\n")), Version)
	fmt.Println(subtle("                  钛金流式版 - 稳定、海量、智能"))
	fmt.Println(subtle("                  随时按 Ctrl+C 安全退出脚本"))
	fmt.Println("================================================================================")
}

func printToConsole(f string, a ...interface{}) {
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	fmt.Printf("\033[2K\r"+f, a...)
}

// 修改超时时间为5秒，符合要求"同时设置5秒后自动跳过所有"极低质量"选项"
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
	fmt.Printf("  %s\n", bold("[2] 全部尝试修复并转换"))
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
			fmt.Println(green("\n已选择 [全部尝试修复]"))
			return ChoiceRepair, nil
		case "3":
			fmt.Println(red("\n已选择 [全部直接删除]"))
			return ChoiceDelete, nil
		default:
			fmt.Println(green("\n已选择 [全部跳过]"))
			return ChoiceSkip, nil
		}
	case <-time.After(5 * time.Second): // 从30秒改为5秒
		fmt.Println(green("\n超时，已选择 [全部跳过]"))
		return ChoiceSkip, nil
	}
}

func interactiveSessionLoop(t ToolCheckResults) {
	reader := bufio.NewReader(os.Stdin)
	var input string // 统一在函数开头定义input变量
	for {
		var c Config
		c.EnableBackups = true
		c.MaxRetries = 2
		c.HwAccel = true
		c.LogLevel = "info"
		c.CRF = 28
		c.SortOrder = "quality"
		c.ConcurrentJobs = 7
		// 设置默认质量配置
		c.QualityConfig = getDefaultQualityConfig()

		showBanner()

		for {
			fmt.Print(bold(cyan("\n📂 请拖入目标文件夹，然后按 Enter: ")))
			input, _ = reader.ReadString('\n')
			trimmedInput := strings.TrimSpace(input)
			if trimmedInput == "" {
				fmt.Println(red("⚠️ 目录不能为空，请重新输入。"))
				continue
			}
			cleanedInput := cleanPath(trimmedInput)
			info, err := os.Stat(cleanedInput)
			if err == nil {
				if !info.IsDir() {
					fmt.Println(red("⚠️ 提供的路径不是一个文件夹，请重新输入。"))
					continue
				}
				c.TargetDir = cleanedInput
				break
			}
			fmt.Println(red("⚠️ 无效的目录或路径不存在，请检查后重试。"))
		}

		fmt.Println("\n" + bold(cyan("⚙️ 请选择转换模式: ")))
		fmt.Printf("  %s %s - 追求极致画质与无损，适合存档。\n", green("[1]"), bold("质量模式 (Quality)"))
		fmt.Printf("  %s %s - 智能平衡画质与体积，适合日常使用。\n", yellow("[2]"), bold("效率模式 (Efficiency)"))
		fmt.Printf("  %s %s - 程序自动为每个文件选择最佳模式。\n", violet("[3]"), bold("自动模式 (Auto)"))

		for {
			fmt.Print(bold(cyan("👉 请输入您的选择 (1/2/3) [回车默认 3]: ")))
			input, _ = reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" || input == "3" {
				c.Mode = "auto"
				break
			} else if input == "2" {
				c.Mode = "efficiency"
				break
			} else if input == "1" {
				c.Mode = "quality"
				break
			}
		}

		// 质量参数配置
		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "🌟 极高质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.ExtremeHighThreshold)))
		fmt.Printf("  %-12s %s\n", "⭐ 高质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.HighThreshold)))
		fmt.Printf("  %-12s %s\n", "✨ 中质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.MediumThreshold)))
		fmt.Printf("  %-12s %s\n", "💤 低质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.LowThreshold)))

		fmt.Print(bold(cyan("\n👉 是否调整质量参数? (y/N): ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.ToLower(input) == "y" {
			adjustQualityParameters(&c)
		}

		fmt.Print(bold(cyan("\n👉 是否恢复质量参数默认值? (y/N): ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.ToLower(input) == "y" {
			c.QualityConfig = getDefaultQualityConfig()
			fmt.Println(green("已恢复质量参数默认值"))
		}

		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(c.TargetDir))
		fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(c.Mode))
		fmt.Printf("  %-12s %s\n", "⚡ 并发数:", cyan(fmt.Sprintf("%d", c.ConcurrentJobs)))
		fmt.Printf("  %-12s %s\n", "🌟 质量参数:", cyan("已配置"))

		fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回: ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			continue
		}

		if err := executeStreamingPipeline(c, t); err != nil {
			printToConsole(red("任务执行出错: %v\n", err))
		}

		fmt.Print(bold(cyan("\n✨ 本轮任务已完成。是否开始新的转换? (Y/n): ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			fmt.Println(green("感谢使用！👋"))
			break
		}
	}
}

func adjustQualityParameters(c *Config) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print(bold(cyan("🌟 输入极高质量阈值 (默认 0.25): ")))
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.ExtremeHighThreshold = val
		}
	}

	fmt.Print(bold(cyan("⭐ 输入高质量阈值 (默认 0.15): ")))
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.HighThreshold = val
		}
	}

	fmt.Print(bold(cyan("✨ 输入中质量阈值 (默认 0.08): ")))
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.MediumThreshold = val
		}
	}

	fmt.Print(bold(cyan("💤 输入低质量阈值 (默认 0.03): ")))
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.LowThreshold = val
		}
	}
}
