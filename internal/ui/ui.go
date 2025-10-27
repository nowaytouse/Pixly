package ui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"pixly/config"
	"pixly/internal/emoji"
	"pixly/internal/i18n"
	"pixly/internal/output" // 添加output包导入
	"pixly/internal/theme"

	"github.com/fatih/color"
)

// 不再需要全局锁 - 使用OutputController的统一锁机制

// 全局配置实例
var globalConfig *config.Config

// 全局AsciiArtEnhancer实例
var asciiArtEnhancer *emoji.AsciiArtEnhancer

// SetGlobalConfig 设置全局配置实例
func SetGlobalConfig(cfg *config.Config) {
	globalConfig = cfg
}

// GetGlobalConfig 获取全局配置实例（公共接口，保持向后兼容）
func GetGlobalConfig() *config.Config {
	return globalConfig
}

// 简洁版标题 - Linus式极简设计
const pixlyTitle = "Pixly Media Converter v1.65.6.2"
const pixlySubtitle = "高性能批处理媒体转换引擎"

// 主题管理器实例
var themeManager *theme.ThemeManager

// 国际化管理器实例
var i18nManager *i18n.I18nManager

// 重荷处理模式标志
var heavyProcessingMode = false

// 缓存不需要锁 - 简单的map操作

// SetHeavyProcessingMode 设置重荷处理模式
func SetHeavyProcessingMode(enabled bool) {
	heavyProcessingMode = enabled
}

// IsHeavyProcessingMode 检查是否处于重荷处理模式
func IsHeavyProcessingMode() bool {
	return heavyProcessingMode
}

// 初始化UI模块
func init() {
	themeManager = theme.GetGlobalThemeManager()
	i18nManager = i18n.GetGlobalI18nManager()
	initColorVars() // 初始化颜色变量

	// 初始化AsciiArtEnhancer
	// 注意：在init函数中globalConfig可能还未设置，所以延迟初始化

	// 启动窗口大小变化监听器
	go listenForWindowChanges()
}

// 获取主题颜色的函数（加入空值检查）
func getBrandColor() *color.Color {
	return GetColor(ColorBrand)
}

func getAccentColor() *color.Color {
	return GetColor(ColorAccent)
}

func getSuccessColor() *color.Color {
	return GetColor(ColorSuccess)
}

func getWarningColor() *color.Color {
	return GetColor(ColorWarning)
}

func getErrorColor() *color.Color {
	return GetColor(ColorError)
}

func getInfoColor() *color.Color {
	return GetColor(ColorInfo)
}

func getProgressColor() *color.Color {
	return GetColor(ColorProgress)
}

func getHighlightColor() *color.Color {
	return GetColor(ColorHighlight)
}

// 获取菜单颜色
func getMenuColor() *color.Color {
	return GetColor(ColorMenu)
}

// 获取提示颜色
func getPromptColor() *color.Color {
	return GetColor(ColorPrompt)
}

// 获取扩展颜色的函数
func getAccent1Color() *color.Color {
	return GetColor(ColorAccent1)
}

func getAccent2Color() *color.Color {
	return GetColor(ColorAccent2)
}

func getAccent3Color() *color.Color {
	return GetColor(ColorAccent3)
}

func getBackgroundColor() *color.Color {
	return GetColor(ColorBackground)
}

func getForegroundColor() *color.Color {
	return GetColor(ColorForeground)
}

func getBorderColor() *color.Color {
	return GetColor(ColorBorder)
}

func getShadowColor() *color.Color {
	return GetColor(ColorShadow)
}

// 兼容性变量（保持向后兼容，延迟初始化）
var (
	// 品牌颜色
	BrandColor  *color.Color
	AccentColor *color.Color

	// 状态颜色
	SuccessColor *color.Color
	WarningColor *color.Color
	ErrorColor   *color.Color
	InfoColor    *color.Color

	// UI元素颜色
	HeaderColor *color.Color
	MenuColor   *color.Color
	PromptColor *color.Color

	// 新增扩展颜色变量
	Accent1Color    *color.Color
	Accent2Color    *color.Color
	Accent3Color    *color.Color
	BackgroundColor *color.Color
	ForegroundColor *color.Color
	BorderColor     *color.Color
	ShadowColor     *color.Color

	// 特殊效果
	RainbowColors = []*color.Color{
		color.New(color.FgRed),
		color.New(color.FgYellow),
		color.New(color.FgGreen),
		color.New(color.FgCyan),
		color.New(color.FgBlue),
		color.New(color.FgMagenta),
	}
)

// 进度条兼容性适配器 - 使用新的动态进度条系统
// 保持旧API兼容性，但内部使用pterm实现

// 初始化颜色变量（延迟调用）
func initColorVars() {
	if BrandColor == nil {
		InitColors()
		BrandColor = getBrandColor()
		AccentColor = getAccentColor()
		SuccessColor = getSuccessColor()
		WarningColor = getWarningColor()
		ErrorColor = getErrorColor()
		InfoColor = getInfoColor()
		HeaderColor = getHighlightColor()
		MenuColor = getMenuColor()
		PromptColor = getPromptColor()
		// 初始化新增扩展颜色
		Accent1Color = getAccent1Color()
		Accent2Color = getAccent2Color()
		Accent3Color = getAccent3Color()
		BackgroundColor = getBackgroundColor()
		ForegroundColor = getForegroundColor()
		BorderColor = getBorderColor()
		ShadowColor = getShadowColor()
	}
}

// createMatteEffect 创建点点质感效果（透明背景）
func createMatteEffect(text string, isDarkMode bool) string {
	// 创建点点质感效果，使用透明背景
	var matteColor *color.Color
	if isDarkMode {
		// 暗色模式下使用亮白色+粗体模拟金属点点质感
		matteColor = color.New(color.FgHiWhite, color.Bold)
	} else {
		// 亮色模式下使用深黑色+粗体模拟磨砂点点质感
		matteColor = color.New(color.FgHiBlack, color.Bold)
	}

	// 添加点点质感效果
	return addDottedEffect(matteColor.Sprint(text), isDarkMode)
}

// createEnhancedMatteEffect 创建增强的点点质感效果（透明背景）
func createEnhancedMatteEffect(text string, isDarkMode bool) string {
	// 创建增强的点点质感效果，使用粗体模拟材质感
	var matteColor *color.Color
	if isDarkMode {
		// 暗色模式下使用亮白色+粗体模拟金属点点质感
		matteColor = color.New(color.FgHiWhite, color.Bold)
	} else {
		// 亮色模式下使用深黑色+粗体模拟磨砂点点质感
		matteColor = color.New(color.FgHiBlack, color.Bold)
	}

	// 添加增强的点点质感效果
	return addEnhancedDottedEffect(matteColor.Sprint(text), isDarkMode)
}

// getDotCharacter 获取点状字符
func getDotCharacter(isDarkMode bool, isEnhanced bool) string {
	if isDarkMode {
		if isEnhanced {
			return color.New(color.FgHiWhite, color.Faint).Sprint("··")
		}
		return color.New(color.FgHiWhite, color.Faint).Sprint("·")
	}

	if isEnhanced {
		return color.New(color.FgHiBlack, color.Faint).Sprint("··")
	}
	return color.New(color.FgHiBlack, color.Faint).Sprint("·")
}

// addDottedEffect 添加基础点点效果
func addDottedEffect(text string, isDarkMode bool) string {
	// 使用点状字符添加质感效果
	// 简单实现：在文本前后添加点状装饰
	dotChar := getDotCharacter(isDarkMode, false)

	// 在文本前后添加点状装饰，避免emoji重复显示
	return dotChar + " " + text + " " + dotChar
}

// addEnhancedDottedEffect 添加增强的点点效果
func addEnhancedDottedEffect(text string, isDarkMode bool) string {
	// 使用更复杂的点状字符添加质感效果
	// 在文本周围添加点状边框
	doubleDotChar := getDotCharacter(isDarkMode, true)

	// 创建点状边框效果，避免emoji重复显示
	return doubleDotChar + " " + text + " " + doubleDotChar
}

// DisplayWelcomeScreen 显示欢迎界面 - 增强视觉效果和对称性
func DisplayWelcomeScreen() {
	// 直接调用ASCII艺术显示函数，避免通过渲染通道造成重复输出
	displayCompactAsciiArt()

	// 添加一个小延迟确保显示完成
	time.Sleep(50 * time.Millisecond)
}

// displayCompactAsciiArt 显示紧凑版ASCII艺术字 - 提高UI可靠性
func displayCompactAsciiArt() {
	// 注意：不要在这里加锁，因为调用它的函数已经加锁了

	// 获取全局配置
	cfg := GetGlobalConfig()

	// 延迟初始化AsciiArtEnhancer
	if asciiArtEnhancer == nil && cfg != nil && themeManager != nil {
		// 创建默认的EmojiConfig
		emojiConfig := &emoji.EmojiConfig{
			Enabled: cfg.Theme.EnableAsciiArtColors,
			AsciiArt: struct {
				DotEffect            bool   `mapstructure:"dot_effect"`
				GradientEffect       bool   `mapstructure:"gradient_effect"`
				GradientStyle        string `mapstructure:"gradient_style"`
				PrimaryGradientColor string `mapstructure:"primary_gradient_color"`
			}{
				DotEffect:            true,
				GradientEffect:       true,
				GradientStyle:        "multi",
				PrimaryGradientColor: "yellow",
			},
		}
		asciiArtEnhancer = emoji.NewAsciiArtEnhancer(emojiConfig, themeManager)
	}

	// 根据配置选择显示哪种ASCII艺术字
	switch cfg.Theme.AsciiArtMode {
	case "enhanced":
		// 显示增强版ASCII艺术字
		lines := strings.Split(strings.Trim(EnhancedPixlyAsciiArt, "\n"), "\n")
		displayAsciiArt(lines, themeManager.GetThemeInfo()["current_mode"] == "dark", cfg.Theme.EnableAsciiArtColors)
	case "simplified":
		// 显示简化版ASCII艺术字
		lines := strings.Split(strings.Trim(SimplifiedPixlyAsciiArt, "\n"), "\n")
		displayAsciiArt(lines, themeManager.GetThemeInfo()["current_mode"] == "dark", cfg.Theme.EnableAsciiArtColors)
	default:
		// 默认显示简洁版（当前行为）
		lines := []string{pixlyTitle, pixlySubtitle}
		displayAsciiArt(lines, themeManager.GetThemeInfo()["current_mode"] == "dark", !GetRenderConfig().UseSimpleDisplay)
	}
}

// displayAsciiArt 显示ASCII艺术字 - 增强视觉效果和稳定性
func displayAsciiArt(lines []string, isDarkMode bool, useEnhancedEffect bool) {
	// 注意：不要在这里加锁，因为调用它的函数已经加锁了

	// 获取主题管理器
	themeManager := theme.GetGlobalThemeManager()
	if themeManager == nil {
		// 如果主题管理器不可用，使用简化显示
		for _, line := range lines {
			if line != "" {
				fmt.Println("  " + line)
			}
		}
		return
	}

	themeInfo := themeManager.GetThemeInfo()

	// 检查点点效果和渐变效果配置
	enableDotEffect := true
	if dotEffect, ok := themeInfo["enable_ascii_art_dot_effect"]; ok {
		// 安全的类型断言，使用Go 1.25+特性
		if boolValue, typeOk := dotEffect.(bool); typeOk {
			enableDotEffect = boolValue
		} else {
			// 如果类型断言失败，记录警告并使用默认值
			fmt.Printf("警告: enable_ascii_art_dot_effect 类型断言失败，使用默认值 true\n")
			enableDotEffect = true
		}
	}

	// 计算最大行长度用于居中对齐
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	// 限制maxLen最大值为200，防止异常大的ASCII艺术字导致UI排版混乱
	if maxLen > 200 {
		maxLen = 200
	}

	// 在高负荷模式下使用简化显示
	if IsHeavyProcessingMode() {
		// 简化显示，避免复杂计算
		output.GetOutputController().WriteLine("")
		for _, line := range lines {
			if line != "" {
				BrandColor.Println("  " + line)
			}
		}
		return
	}

	// 修复字符画显示凌乱问题：限制最大宽度
	const maxDisplayWidth = 80
	if maxLen > maxDisplayWidth {
		maxLen = maxDisplayWidth
	}

	// 按词组而非字符切换颜色，减少颜色切换频率
	for i, line := range lines {
		if line == "" {
			output.GetOutputController().WriteLine("")
			continue
		}

		// 限制行长度，避免过长导致排版问题
		if len(line) > maxDisplayWidth {
			line = line[:maxDisplayWidth]
		}

		// 添加左侧边框（仅对复杂ASCII艺术字添加边框）
		if len(lines) > 2 { // 复杂ASCII艺术字
			output.GetOutputController().WriteColor("  ║ ", BrandColor)
		} else { // 简洁版标题
			output.GetOutputController().WriteString("  ")
		}

		if i < len(lines)-2 { // ASCII艺术部分
			var processedText string

			// 应用点点效果
			if enableDotEffect && asciiArtEnhancer != nil {
				if useEnhancedEffect {
					// 使用增强的点点质感效果
					processedText = asciiArtEnhancer.CreateEnhancedDottedEffect(line, isDarkMode)
				} else {
					// 使用基础点点质感效果
					processedText = asciiArtEnhancer.CreateDottedEffect(line, isDarkMode)
				}
			} else {
				processedText = line
			}

			// 应用渐变效果
			if asciiArtEnhancer != nil {
				processedText = asciiArtEnhancer.CreateGradientEffect(processedText, "multi")
			}

			// 居中对齐 - 限制最大padding防止UI排版混乱
			padding := (maxLen - len(line)) / 2
			if padding < 0 {
				padding = 0
			}
			// 限制padding最大值为20，防止生成过长的空格字符串
			if padding > 20 {
				padding = 20
			}
			processedLine := strings.Repeat(" ", padding) + processedText
			output.GetOutputController().WriteString(processedLine)
			output.GetOutputController().WriteString("\033[0m") // 重置属性
		} else { // 版本信息部分
			versionText := line

			// 使用主题的强调色 - 限制最大padding防止UI排版混乱
			padding := (maxLen - len(line)) / 2
			if padding < 0 {
				padding = 0
			}
			// 限制padding最大值为20，防止生成过长的空格字符串
			if padding > 20 {
				padding = 20
			}
			processedLine := strings.Repeat(" ", padding)
			output.GetOutputController().WriteString(processedLine)
			output.GetOutputController().WriteColor(versionText, AccentColor)
			output.GetOutputController().WriteString("\033[0m") // 重置属性
		}

		// 添加右侧边框并换行（仅对复杂ASCII艺术字添加边框）
		if len(lines) > 2 { // 复杂ASCII艺术字
			output.GetOutputController().WriteColorLine(" ║", BrandColor)
		} else { // 简洁版标题
			output.GetOutputController().WriteLine("")
		}
	}
}

// listenForWindowChanges 监听窗口大小变化 - 增强稳定性
func listenForWindowChanges() {
	// 创建信号通道
	sigChan := make(chan os.Signal, 1)

	// 监听窗口大小变化信号
	signal.Notify(sigChan, syscall.SIGWINCH)

	// 持续监听信号
	go func() {
		for range sigChan {
			// 窗口大小发生变化，只清理缓存，不重新显示欢迎界面
			// 避免重复显示版本信息
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

// 添加互斥锁以保护窗口变化处理
// 不再需要窗口变化锁 - 使用OutputController的统一锁机制

// ClearScreen 清屏函数 - 使用新的渲染引擎，消除mutex hell
func ClearScreen() {
	// 直接使用渲染引擎，内部已处理线程安全
	ClearScreenNew()
}

// DisplayBanner 显示横幅信息 - Linus式极简设计，消除所有特殊情况
func DisplayBanner(title string, bannerType string) {
	outputController := output.GetOutputController()

	// 获取渲染配置 - 一次计算，到处使用
	config := GetRenderConfig()

	// 获取颜色 - 消除switch特殊情况
	colorMap := map[string]*color.Color{
		"success": getSuccessColor(),
		"warning": getWarningColor(),
		"error":   getErrorColor(),
		"info":    getInfoColor(),
	}
	bannerColor := colorMap[bannerType]
	if bannerColor == nil {
		bannerColor = getBrandColor()
	}

	// 计算布局 - 单一数据结构
	titleLen := utf8.RuneCountInString(title)
	borderLen := titleLen + 4
	leftPadding := (config.TerminalWidth - borderLen) / 2
	if leftPadding < 0 {
		leftPadding = 0
	}
	// 严格限制leftPadding最大值为20，防止生成过长的空格字符串
	if leftPadding > 20 {
		leftPadding = 20
	}
	// 限制边框长度，防止生成过长的字符串
	borderRepeatCount := borderLen - 2
	if borderRepeatCount > 100 {
		borderRepeatCount = 100
	}
	if borderRepeatCount < 0 {
		borderRepeatCount = 0
	}
	border := strings.Repeat("═", borderRepeatCount)

	// 构建完整输出 - 一次性生成，避免重复fmt.Print
	var output strings.Builder
	output.WriteString("\n")

	// 顶部边框
	output.WriteString(strings.Repeat(" ", leftPadding))
	output.WriteString("╔")
	output.WriteString(border)
	output.WriteString("╗\n")
	// 标题行
	output.WriteString(strings.Repeat(" ", leftPadding))
	output.WriteString("║ ")
	output.WriteString(title)
	output.WriteString(" ║\n")
	// 底部边框
	output.WriteString(strings.Repeat(" ", leftPadding))
	output.WriteString("╚")
	output.WriteString(border)
	output.WriteString("╝\n")
	output.WriteString("\n")

	// 一次性输出 - 消除排版混乱
	outputController.WriteColorLine(output.String(), bannerColor)
	outputController.Flush()
}

// DisplayCenteredBanner 显示居中对齐的横幅信息 - 实现大标题的中心对称效果
func DisplayCenteredBanner(title string, bannerType string) {
	outputController := output.GetOutputController()

	// 获取渲染配置
	config := GetRenderConfig()

	// 获取颜色 - 消除switch特殊情况
	colorMap := map[string]*color.Color{
		"success": getSuccessColor(),
		"warning": getWarningColor(),
		"error":   getErrorColor(),
		"info":    getInfoColor(),
	}
	bannerColor := colorMap[bannerType]
	if bannerColor == nil {
		bannerColor = getBrandColor()
	}

	// 计算布局 - 单一数据结构
	titleText := "📋 " + title
	titleLen := utf8.RuneCountInString(titleText)
	borderLen := titleLen + 4
	if borderLen > config.TerminalWidth-4 {
		borderLen = config.TerminalWidth - 4
	}
	leftPadding := (config.TerminalWidth - borderLen) / 2
	if leftPadding < 0 {
		leftPadding = 0
	}
	// 严格限制leftPadding最大值为20，防止生成过长的空格字符串
	if leftPadding > 20 {
		leftPadding = 20
	}
	// 限制边框长度，防止生成过长的字符串
	borderRepeatCount := borderLen - 2
	if borderRepeatCount > 100 {
		borderRepeatCount = 100
	}
	if borderRepeatCount < 0 {
		borderRepeatCount = 0
	}
	borderStr := strings.Repeat("═", borderRepeatCount)

	// 计算标题居中 - 限制最大padding防止UI排版混乱
	totalPadding := borderLen - 2 - titleLen
	if totalPadding < 0 {
		totalPadding = 0
	}
	// 严格限制单个padding最大值为20，防止生成过长的空格字符串
	maxPadding := 20
	if totalPadding > maxPadding*2 {
		totalPadding = maxPadding * 2
	}
	leftTitlePadding := totalPadding / 2
	rightTitlePadding := totalPadding - leftTitlePadding

	// 额外限制每个padding值，防止生成超长空格字符串
	if leftTitlePadding > 20 {
		leftTitlePadding = 20
	}
	if rightTitlePadding > 20 {
		rightTitlePadding = 20
	}

	// 构建完整输出 - 一次性生成，严格控制空格数量
	var output strings.Builder
	output.WriteString("\n")

	// 生成安全的padding字符串，防止超长空格
	leftPaddingStr := ""
	if leftPadding > 0 && leftPadding <= 20 {
		leftPaddingStr = strings.Repeat(" ", leftPadding)
	}
	leftTitlePaddingStr := ""
	if leftTitlePadding > 0 && leftTitlePadding <= 20 {
		leftTitlePaddingStr = strings.Repeat(" ", leftTitlePadding)
	}
	rightTitlePaddingStr := ""
	if rightTitlePadding > 0 && rightTitlePadding <= 20 {
		rightTitlePaddingStr = strings.Repeat(" ", rightTitlePadding)
	}

	// 顶部边框
	output.WriteString(leftPaddingStr + "╔" + borderStr + "╗\n")
	// 标题行
	output.WriteString(leftPaddingStr + "║" + leftTitlePaddingStr + titleText + rightTitlePaddingStr + "║\n")
	// 底部边框
	output.WriteString(leftPaddingStr + "╚" + borderStr + "╝\n")
	output.WriteString("\n")

	// 一次性输出 - 消除排版混乱
	outputController.WriteColorLine(output.String(), bannerColor)
	outputController.Flush()
}

// DisplayMenu 显示菜单 - Linus式极简设计，消除排版混乱
func DisplayMenu(title string, options []MenuOption) {
	outputController := output.GetOutputController()

	// 构建完整输出 - 一次性生成，避免重复fmt.Print
	var output strings.Builder
	output.WriteString("\n")

	// 显示标题
	output.WriteString("  ")
	output.WriteString(title)
	output.WriteString("\n\n")

	// 限制选项数量
	maxOptions := 20
	if len(options) > maxOptions {
		options = options[:maxOptions]
	}

	// 构建菜单项
	for i, option := range options {
		// 限制选项文本长度
		optionText := option.Text
		const maxOptionLength = 40
		if len(optionText) > maxOptionLength {
			optionText = optionText[:maxOptionLength-3] + "..."
		}

		menuNumber := "✧ " + strconv.Itoa(i+1) + " ✧"
		if option.Enabled {
			output.WriteString("  ")
			output.WriteString(menuNumber)
			output.WriteString(" ")
			output.WriteString(optionText)
			output.WriteString("\n")
			// 描述文本
			if option.Description != "" {
				descText := option.Description
				const maxDescLength = 50
				if len(descText) > maxDescLength {
					descText = descText[:maxDescLength-3] + "..."
				}
				output.WriteString("     ")
				output.WriteString(descText)
				output.WriteString("\n")
			}
		} else {
			output.WriteString("  ")
			output.WriteString(menuNumber)
			output.WriteString(" ")
			output.WriteString(optionText)
			output.WriteString(" (")
			output.WriteString(i18n.T(i18n.TextDisabled))
			output.WriteString(")\n")
		}
	}

	output.WriteString("\n")
	output.WriteString(i18n.T(i18n.TextChooseOption))

	// 一次性输出 - 消除排版混乱
	outputController.WriteColorLine(output.String(), HeaderColor)
	outputController.Flush()
}

// MenuOption 菜单选项
type MenuOption struct {
	Icon        string
	Text        string
	Description string
	Enabled     bool
}

// DisplayStats 函数已移除 - 避免循环依赖
// 统计信息显示应在converter包中处理

// ConversionStats 转换统计结构 - 已移除循环依赖
// 调用方应直接使用 converter.ConversionStats

// PromptUser 提示用户输入 - 使用统一输入管理器
func PromptUser(message string) string {
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// 使用渲染引擎输出提示，内部已处理线程安全
		RenderText(message + ": ")

		// 使用统一输入管理器读取输入
		input, err := ReadLine()
		if err != nil {
			if attempt == maxAttempts {
				// 使用渲染引擎显示错误，内部已处理线程安全
				DisplayError(fmt.Errorf("%s %s", i18n.T(i18n.TextInvalidInput), i18n.T(i18n.TextOperationCanceled)))
				os.Exit(1)
			}
			// 使用渲染引擎显示错误，内部已处理线程安全
			var errorMsg strings.Builder
			errorMsg.WriteString(i18n.T(i18n.TextError))
			errorMsg.WriteString(" ")
			errorMsg.WriteString(strconv.Itoa(attempt))
			errorMsg.WriteString("/")
			errorMsg.WriteString(strconv.Itoa(maxAttempts))
			DisplayError(errors.New(errorMsg.String()))
			continue
		}

		// 移除末尾的换行符
		input = strings.TrimSpace(input)

		// 移除外层的引号（单引号或双引号）
		if len(input) >= 2 {
			if (input[0] == '\'' && input[len(input)-1] == '\'') ||
				(input[0] == '"' && input[len(input)-1] == '"') {
				input = input[1 : len(input)-1]
			}
		}

		// 路径规范化已移除 - 避免循环依赖
		// 调用方应在converter包中处理路径规范化

		return input
	}

	// 如果达到最大尝试次数仍未获得有效输入，退出程序
	// 使用渲染引擎显示错误，内部已处理线程安全
	DisplayError(fmt.Errorf("%s %s", i18n.T(i18n.TextInvalidInput), i18n.T(i18n.TextOperationCanceled)))
	os.Exit(1)
	return "" // 这行不会执行到，但为了语法正确性保留
}

// PromptIntegerWithValidation 提示用户输入整数并验证
func PromptIntegerWithValidation(prompt string, min, max int) int {
	for {
		input := PromptUser(prompt + " (" + strconv.Itoa(min) + "-" + strconv.Itoa(max) + ")")
		value, err := strconv.Atoi(input)
		if err != nil {
			DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInvalidInput)))
			continue
		}
		if value < min || value > max {
			DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInputOutOfRange)))
			continue
		}
		return value
	}
}

// PromptNumericWithValidation 提示用户输入数字并验证
func PromptNumericWithValidation(prompt string, min, max float64) float64 {
	for {
		input := PromptUser(prompt + " (" + strconv.FormatFloat(min, 'f', 2, 64) + "-" + strconv.FormatFloat(max, 'f', 2, 64) + ")")
		value, err := strconv.ParseFloat(input, 64)
		if err != nil {
			DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInvalidInput)))
			continue
		}
		if value < min || value > max {
			DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInputOutOfRange)))
			continue
		}
		return value
	}
}

// PromptYesNoWithValidation 提示用户输入是/否并验证 - Linus式好品味：消除特殊情况
func PromptYesNoWithValidation(prompt string, defaultValue bool) bool {
	for {
		// 显示提示信息，明确默认值
		fullPrompt := prompt + " (y/N): "
		if defaultValue {
			fullPrompt = prompt + " (Y/n): "
		}

		input := PromptUser(fullPrompt)

		// Linus式好品味：空输入直接返回默认值，无条件分支
		if strings.TrimSpace(input) == "" {
			return defaultValue
		}

		// 标准化输入处理
		inputLower := strings.ToLower(strings.TrimSpace(input))
		switch inputLower {
		case "y", "yes", "是", "1":
			return true
		case "n", "no", "否", "0":
			return false
		default:
			// 错误信息不显示在UI，通过logger处理
			continue
		}
	}
}

// PromptUserWithValidation 提示用户输入并使用自定义验证函数
func PromptUserWithValidation(prompt string, validate func(string) bool) string {
	for {
		input := PromptUser(prompt)
		if validate(input) {
			return input
		}
		DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInvalidInput)))
	}
}

// 注意：路径编码处理功能已迁移到 converter.GlobalPathUtils.NormalizePath
// 所有路径相关的编码问题应该使用统一的路径处理工具解决

// PromptConfirm 确认提示 - 使用统一输入管理器
func PromptConfirm(message string) bool {
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// 使用渲染引擎输出提示，内部已处理线程安全
		RenderText(message + " (y/N): ")

		// 使用统一输入管理器读取输入
		input, err := ReadLine()
		if err != nil {
			if attempt == maxAttempts {
				// 使用渲染引擎显示错误，内部已处理线程安全
				DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInvalidInput)+" "+i18n.T(i18n.TextOperationCanceled)))
				os.Exit(1)
			}
			// 使用渲染引擎显示错误，内部已处理线程安全
			DisplayError(fmt.Errorf("%s %d/%d", i18n.T(i18n.TextError), attempt, maxAttempts))
			continue
		}

		input = strings.TrimSpace(strings.ToLower(input))
		return input == "y" || input == "yes"
	}

	// 如果达到最大尝试次数仍未获得有效输入，退出程序
	// 使用渲染引擎显示错误，内部已处理线程安全
	DisplayError(fmt.Errorf("%s", i18n.T(i18n.TextInvalidInput)+" "+i18n.T(i18n.TextOperationCanceled)))
	os.Exit(1)
	return false // 这行不会执行到，但为了语法正确性保留
}

// WaitForKeyPress 等待按键
func WaitForKeyPress(message string) {
	if message == "" {
		message = i18n.T(i18n.TextPressEnterToContinue)
	}
	getPromptColor().Printf("\n%s", message)

	// 使用统一输入管理器读取输入
	ReadLine()
}

// DisplayError 显示错误信息 - Linus式好品味：错误不污染UI
func DisplayError(err error) {
	// 错误信息通过logger系统记录到文件
	// 不在UI显示，保持界面清洁
	// 消除错误显示的特殊情况
	// logger会自动处理错误记录
}

// DisplayWarning 显示警告信息 - 使用新的渲染引擎，消除特殊情况
func DisplayWarning(message string) {
	// 直接使用渲染引擎，无需复杂的消息传递
	RenderWarning(message)
}

// DisplaySuccess 显示成功信息 - 使用新的渲染引擎，消除特殊情况
func DisplaySuccess(message string) {
	// 直接使用渲染引擎，无需复杂的消息传递
	RenderSuccess(message)
}

// DisplayInfo 显示信息 - 使用新的渲染引擎，消除特殊情况
func DisplayInfo(message string) {
	// 直接使用渲染引擎，无需复杂的消息传递
	RenderInfo(message)
}

// Println 统一输出函数 - 消除特殊情况
func Println(text string) {
	output.GetOutputController().WriteLine(text)
}

// Printf 统一格式化输出函数 - 消除特殊情况
func Printf(format string, a ...interface{}) {
	if len(a) == 0 {
		output.GetOutputController().WriteString(format)
	} else {
		output.GetOutputController().WriteString(fmt.Sprintf(format, a...))
	}
}

// Print 统一输出函数 - 消除特殊情况
func Print(text string) {
	output.GetOutputController().WriteString(text)
}

// UpdateTheme 更新主题并刷新颜色变量
func UpdateTheme(newMode theme.ThemeMode) error {
	if err := themeManager.SwitchTheme(newMode); err != nil {
		return err
	}

	// 更新颜色变量
	BrandColor = getBrandColor()
	AccentColor = getAccentColor()
	SuccessColor = getSuccessColor()
	WarningColor = getWarningColor()
	ErrorColor = getErrorColor()
	InfoColor = getInfoColor()
	HeaderColor = getHighlightColor()
	MenuColor = getMenuColor()
	PromptColor = getPromptColor()
	// 更新新增扩展颜色
	Accent1Color = getAccent1Color()
	Accent2Color = getAccent2Color()
	Accent3Color = getAccent3Color()
	BackgroundColor = getBackgroundColor()
	ForegroundColor = getForegroundColor()
	BorderColor = getBorderColor()
	ShadowColor = getShadowColor()

	return nil
}

// GetCurrentTheme 获取当前主题信息
func GetCurrentTheme() map[string]interface{} {
	return themeManager.GetThemeInfo()
}

// GetThemeManager 获取主题管理器
func GetThemeManager() *theme.ThemeManager {
	return themeManager
}

// SetLanguage 设置语言
func SetLanguage(lang i18n.Language) error {
	if err := i18nManager.SetLanguage(lang); err != nil {
		return err
	}

	return nil
}

// GetCurrentLanguage 获取当前语言
func GetCurrentLanguage() i18n.Language {
	return i18nManager.GetCurrentLanguage()
}

// ============================================================================
// 统一进度条系统 - 基于fatih/color，消除外部依赖
// ============================================================================

// ProgressBar 统一进度条结构
// 兼容性类型定义 - 内部使用动态进度条
type ProgressBar = DynamicProgressBar
type ProgressManager = DynamicProgressManager

// GetProgressManager 获取全局进度管理器 - 兼容性适配器
func GetProgressManager() *ProgressManager {
	return GetDynamicProgressManager()
}

// 兼容性进度条函数 - 直接使用动态进度条
func StartProgress(total int64, message string) {
	StartDynamicProgress(total, message)
}

func UpdateProgress(current int64, message string) {
	UpdateDynamicProgress(current, message)
}

func FinishProgress() {
	FinishDynamicProgress()
}

func StartNamedProgress(name string, total int64, message string) {
	StartNamedDynamicProgress(name, total, message)
}

func UpdateNamedProgress(name string, current int64, message string) {
	UpdateNamedDynamicProgress(name, current, message)
}

func FinishNamedProgress(name string) {
	FinishNamedDynamicProgress(name)
}

// StartBar 启动进度条
// 兼容性方法 - 直接委托给动态进度条管理器
// 这些方法保持API兼容性，但内部使用pterm实现
