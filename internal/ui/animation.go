package ui

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"pixly/internal/output"
	"pixly/internal/theme"

	"github.com/fatih/color"
	"github.com/spf13/viper"
)

// GetGlobalConfig 获取全局配置实例
// 注意：这个函数在ui.go中已经定义，这里直接调用ui包中的实现

// AnimationConfig 动画配置结构
type AnimationConfig struct {
	MaxDuration time.Duration
	MaxFrames   int
	FrameDelay  time.Duration
	MaxLength   int
}

// DefaultAnimationConfig 返回默认动画配置
func DefaultAnimationConfig() AnimationConfig {
	return AnimationConfig{
		MaxDuration: 2 * time.Second,
		MaxFrames:   20,
		FrameDelay:  100 * time.Millisecond,
		MaxLength:   100,
	}
}

// AnimationContext 动画上下文，使用现代化的并发控制
type AnimationContext struct {
	ctx    context.Context
	cancel context.CancelFunc
	config AnimationConfig
}

// NewAnimationContext 创建新的动画上下文
func NewAnimationContext(config AnimationConfig) *AnimationContext {
	ctx, cancel := context.WithTimeout(context.Background(), config.MaxDuration)
	return &AnimationContext{
		ctx:    ctx,
		cancel: cancel,
		config: config,
	}
}

// Close 关闭动画上下文
func (ac *AnimationContext) Close() {
	ac.cancel()
}

// CreateOptimizedPulsingText 创建优化的脉冲动画文本效果 - 使用现代化并发控制
func CreateOptimizedPulsingText(text string, duration time.Duration) {
	// 检查是否启用动画
	if !isAnimationEnabled() {
		BrandColor.Println(text)
		return
	}

	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	themeManager := theme.GetGlobalThemeManager()
	if themeManager == nil || !themeManager.GetThemeInfo()["gradient_enabled"].(bool) {
		BrandColor.Println(text)
		return
	}

	// 创建动画配置
	animConfig := DefaultAnimationConfig()
	if duration < animConfig.MaxDuration {
		animConfig.MaxDuration = duration
	}

	// 限制文本长度
	if len(text) > animConfig.MaxLength {
		text = text[:animConfig.MaxLength] + "..."
	}

	// 创建动画上下文
	animCtx := NewAnimationContext(animConfig)
	defer animCtx.Close()

	// 使用现代化的并发控制
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		startTime := time.Now()
		frameCount := 0

		for frameCount < animConfig.MaxFrames {
			// 检查上下文是否已取消
			select {
			case <-animCtx.ctx.Done():
				return
			default:
			}

			// 计算脉冲强度 (0.0 - 1.0)
			elapsed := time.Since(startTime).Seconds()
			pulseIntensity := (math.Sin(elapsed*3) + 1) / 2 // 正弦波，范围0-1

			// 创建脉冲效果文本
			pulsingText := themeManager.CreatePulsingEffect(text, pulseIntensity)
			output.GetOutputController().ClearLine() // 清除当前行并回到行首
			output.GetOutputController().WriteString(pulsingText)
			output.GetOutputController().Flush()

			// 添加恢复光标的代码
			output.GetOutputController().ShowCursor() // 显示光标

			// 使用上下文控制的睡眠
			select {
			case <-animCtx.ctx.Done():
				return
			case <-time.After(animConfig.FrameDelay):
			}

			frameCount++
		}
	}()

	// 等待动画完成
	wg.Wait()

	// 显示最终文本
	output.GetOutputController().ClearLine() // 清除当前行并回到行首
	BrandColor.Println(text)
}

// CreateTypewriterText 创建打字机动画文本效果
func CreateTypewriterText(text string, delay time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	// 限制打字机效果的延迟时间，避免过度消耗资源
	if delay < 10*time.Millisecond {
		delay = 10 * time.Millisecond
	}

	// 限制文本长度，避免长时间显示
	maxLength := 100
	if len(text) > maxLength {
		text = text[:maxLength] + "..."
	}

	// 修复死循环问题：添加字符计数器
	charCount := 0
	maxChars := 150 // 限制最大字符数

	// 逐字符显示文本，创建打字机效果
	for _, char := range text {
		BrandColor.Printf("%c", char)
		time.Sleep(delay)
		charCount++

		// 额外的退出条件，防止死循环
		if charCount > maxChars {
			break
		}
	}
	output.GetOutputController().WriteLine("")
}

// CreateRainbowText 创建彩虹动画文本效果
func CreateRainbowText(text string, duration time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	themeManager := theme.GetGlobalThemeManager()
	if themeManager == nil || !themeManager.GetThemeInfo()["gradient_enabled"].(bool) {
		BrandColor.Println(text)
		return
	}

	// 限制动画时长，避免过度消耗资源
	if duration > 1*time.Second {
		duration = 1 * time.Second
	}

	// 彩虹颜色序列
	rainbowColors := []*color.Color{
		color.New(color.FgHiRed),
		color.New(color.FgHiYellow),
		color.New(color.FgHiGreen),
		color.New(color.FgHiCyan),
		color.New(color.FgHiBlue),
		color.New(color.FgHiMagenta),
	}

	startTime := time.Now()
	frame := 0
	maxFrames := int(duration / (150 * time.Millisecond)) // 限制最大帧数

	// 修复死循环问题：添加明确的退出条件
	for time.Since(startTime) < duration && frame < maxFrames {
		var result strings.Builder

		for i, char := range text {
			// 循环使用彩虹颜色
			colorIndex := (i + frame) % len(rainbowColors)
			result.WriteString(rainbowColors[colorIndex].Sprint(string(char)))
		}

		output.GetOutputController().ClearLine() // 清除当前行并回到行首
		output.GetOutputController().WriteString(result.String())
		output.GetOutputController().Flush()
		time.Sleep(150 * time.Millisecond)
		frame++

		// 额外的退出条件，防止死循环
		if frame > 10 {
			break
		}
	}

	// 显示最终文本
	output.GetOutputController().ClearLine() // 清除当前行并回到行首
	BrandColor.Println(text)
}

// CreateBlinkingText 创建闪烁动画文本效果
func CreateBlinkingText(text string, duration time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	// 限制动画时长，避免过度消耗资源
	if duration > 1*time.Second {
		duration = 1 * time.Second
	}

	startTime := time.Now()
	visible := true
	frameCount := 0
	maxFrames := int(duration / (500 * time.Millisecond)) // 限制最大帧数

	// 修复死循环问题：添加明确的退出条件
	for time.Since(startTime) < duration && frameCount < maxFrames {
		output.GetOutputController().ClearLine() // 清除当前行并回到行首
		if visible {
			BrandColor.Print(text)
		}
		time.Sleep(500 * time.Millisecond)
		visible = !visible
		frameCount++

		// 额外的退出条件，防止死循环
		if frameCount > 5 {
			break
		}
	}

	// 显示最终文本
	output.GetOutputController().ClearLine() // 清除当前行并回到行首
	BrandColor.Println(text)
}

// DisplayStartupAnimation 显示启动动画
func DisplayStartupAnimation() {
	// 修复：使用全局配置而不是创建新的配置实例
	// 这样可以避免重复初始化和潜在的卡死问题
	cfg := GetGlobalConfig()

	// 如果全局配置不可用，通过渲染通道显示简单的启动文本
	if cfg == nil {
		startupText := "🚀 Pixly Media Converter 正在启动..."
		renderChannel := GetRenderChannel()
		renderChannel.SendMessage(UIMessage[any]{
			Type: "text",
			Data: startupText,
		})
		// 修复：减少延迟时间，避免卡住
		time.Sleep(100 * time.Millisecond) // 短暂延迟
		return
	}

	// 通过渲染通道发送启动动画消息
	startupText := "🚀 Pixly Media Converter 正在启动..."
	renderChannel := GetRenderChannel()
	renderChannel.SendMessage(UIMessage[any]{
		Type: "animation",
		Data: startupText,
	})

	// 等待动画时间
	time.Sleep(500 * time.Millisecond)

	// 发送清除消息
	renderChannel.SendMessage(UIMessage[any]{
		Type: "clear",
		Data: "",
	})

	// 设置跳过首次运行标志
	viper.Set("theme.skip_first_run", true)

	// 保存配置 - 避免循环依赖，使用简单路径
	// 路径规范化已移除以避免循环依赖
	// 调用方应在converter包中处理配置保存
}

// DisplayCompletionAnimation 显示完成动画
func DisplayCompletionAnimation() {
	// 修复：使用全局配置而不是创建新的配置实例
	cfg := GetGlobalConfig()

	// 如果全局配置不可用，直接返回
	if cfg == nil {
		return
	}

	// 显示多层次完成动画效果
	completionText := "🎉 转换完成!"
	CreateSlideInText(completionText, "left", 600*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	thanksText := "感谢使用 Pixly!"
	CreateBounceText(thanksText, 2, 800*time.Millisecond)

	time.Sleep(300 * time.Millisecond)

	// 最后的渐变效果
	finalText := "✨ 处理完成 ✨"
	CreateGradientFadeText(finalText, 1*time.Second)
}

// DisplayEnhancedStartupAnimation 显示增强的启动动画
func DisplayEnhancedStartupAnimation() {
	cfg := GetGlobalConfig()
	if cfg == nil {
		return
	}

	// 多阶段启动动画
	welcomeText := "🚀 Pixly Media Converter"
	CreateSlideInText(welcomeText, "right", 700*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	startingText := "正在启动..."
	CreateTypewriterText(startingText, 50*time.Millisecond)

	time.Sleep(300 * time.Millisecond)

	readyText := "✨ 准备就绪 ✨"
	CreateGradientFadeText(readyText, 800*time.Millisecond)
}

// CreateWaveText 创建波浪动画文本效果
func CreateWaveText(text string, duration time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	themeManager := theme.GetGlobalThemeManager()
	if themeManager == nil || !themeManager.GetThemeInfo()["gradient_enabled"].(bool) {
		BrandColor.Println(text)
		return
	}

	// 限制动画时长，避免过度消耗资源
	if duration > 1*time.Second {
		duration = 1 * time.Second
	}

	startTime := time.Now()
	frame := 0
	maxFrames := int(duration / (100 * time.Millisecond)) // 限制最大帧数

	// 修复死循环问题：添加明确的退出条件
	for time.Since(startTime) < duration && frame < maxFrames {
		var result strings.Builder

		for i, char := range text {
			// 计算波浪效果
			wave := math.Sin(float64(i)/2.0 + float64(frame)/5.0)
			intensity := (wave + 1) / 2 // 转换为0-1范围

			// 根据强度选择颜色
			var c *color.Color
			if intensity > 0.7 {
				c = themeManager.GetColorScheme().Highlight
			} else if intensity > 0.4 {
				c = themeManager.GetColorScheme().Primary
			} else {
				c = themeManager.GetColorScheme().Info
			}

			result.WriteString(c.Sprint(string(char)))
		}

		output.GetOutputController().ClearLine() // 清除当前行并回到行首
		output.GetOutputController().WriteString(result.String())
		output.GetOutputController().Flush()
		time.Sleep(100 * time.Millisecond)
		frame++

		// 额外的退出条件，防止死循环
		if frame > 15 {
			break
		}
	}

	// 显示最终文本
	output.GetOutputController().ClearLine() // 清除当前行并回到行首
	BrandColor.Println(text)
}

// CreateSlideInText 创建滑入动画文本效果
func CreateSlideInText(text string, direction string, duration time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	// 限制动画时长
	if duration > 800*time.Millisecond {
		duration = 800 * time.Millisecond
	}

	// 限制终端宽度，防止过大的值导致UI混乱
	terminalWidth := config.TerminalWidth
	if terminalWidth > 120 {
		terminalWidth = 120 // 限制最大宽度
	}
	if terminalWidth < 40 {
		terminalWidth = 80 // 设置最小宽度
	}

	textLen := len(text)
	endPos := (terminalWidth - textLen) / 2
	if endPos < 0 {
		endPos = 0
	}

	// 根据方向设置起始位置
	var startPos int
	switch direction {
	case "left":
		startPos = -textLen
	case "right":
		startPos = terminalWidth
	default:
		startPos = -textLen // 默认从左侧滑入
	}

	steps := 20
	stepDuration := duration / time.Duration(steps)

	for i := 0; i <= steps; i++ {
		// 计算当前位置
		currentPos := startPos + (endPos-startPos)*i/steps

		// 限制currentPos的范围，防止生成过长的空白字符串
		if currentPos < 0 {
			currentPos = 0
		}
		if currentPos > 80 { // 限制最大空格数
			currentPos = 80
		}

		// 清除当前行
		output.GetOutputController().ClearLine()

		// 添加空格填充
		if currentPos > 0 {
			output.GetOutputController().WriteString(strings.Repeat(" ", currentPos))
		}

		// 显示文本
		BrandColor.Print(text)

		time.Sleep(stepDuration)
	}
	output.GetOutputController().WriteLine("")
}

// CreateBounceText 创建弹跳动画文本效果
func CreateBounceText(text string, bounces int, duration time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay {
		BrandColor.Println(text)
		return
	}

	if bounces > 3 {
		bounces = 3 // 限制弹跳次数
	}

	bounceDuration := duration / time.Duration(bounces*2)

	for i := 0; i < bounces; i++ {
		// 上升阶段
		output.GetOutputController().ClearLine()
		BrandColor.Print(text + " ↗")
		time.Sleep(bounceDuration)

		// 下降阶段
		output.GetOutputController().ClearLine()
		BrandColor.Print(text + " ↘")
		time.Sleep(bounceDuration)
	}

	// 最终静止状态
	output.GetOutputController().ClearLine()
	BrandColor.Println(text)
}

// CreateGradientFadeText 创建渐变淡入文本效果
func CreateGradientFadeText(text string, duration time.Duration) {
	// 获取统一渲染配置
	config := GetRenderConfig()
	if config.UseSimpleDisplay || !config.GradientEnabled {
		BrandColor.Println(text)
		return
	}

	// 限制动画时长
	if duration > 1*time.Second {
		duration = 1 * time.Second
	}

	steps := 10
	stepDuration := duration / time.Duration(steps)
	colors := []*color.Color{
		color.New(color.FgHiBlack),
		color.New(color.FgBlack),
		color.New(color.FgHiBlue),
		color.New(color.FgBlue),
		color.New(color.FgHiCyan),
		color.New(color.FgCyan),
		color.New(color.FgHiGreen),
		color.New(color.FgGreen),
		color.New(color.FgHiYellow),
		BrandColor, // 最终颜色
	}

	for i := 0; i < steps; i++ {
		output.GetOutputController().ClearLine()
		colors[i].Print(text)
		time.Sleep(stepDuration)
	}
	output.GetOutputController().WriteLine("")
}

// isAnimationEnabled 检查是否启用动画效果
func isAnimationEnabled() bool {
	// 检查环境变量
	if os.Getenv("PIXLY_DISABLE_ANIMATION") != "" {
		return false
	}

	// 检查是否在 CI 环境中
	if os.Getenv("CI") != "" {
		return false
	}

	// 简化动画支持检查
	if os.Getenv("TERM") == "dumb" {
		return false
	}

	return true // 简化：始终启用动画
}
