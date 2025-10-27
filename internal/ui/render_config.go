package ui

import (
	"golang.org/x/term"
	"os"
)

// RenderConfig 统一的渲染配置 - 消除所有特殊情况判断
type RenderConfig struct {
	UseSimpleDisplay bool
	GradientEnabled  bool
	IsDarkMode       bool
	TerminalWidth    int
}

// GetRenderConfig 获取统一的渲染配置 - 一次计算，到处使用
func GetRenderConfig() *RenderConfig {
	themeInfo := themeManager.GetThemeInfo()

	return &RenderConfig{
		UseSimpleDisplay: IsHeavyProcessingMode(),
		GradientEnabled:  false, // 简化：禁用渐变效果
		IsDarkMode:       themeInfo["current_mode"] == "dark",
		TerminalWidth:    getTerminalWidth(),
	}
}

// getTerminalWidth 获取终端宽度 - 直接实现，无依赖
func getTerminalWidth() int {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
			// 严格限制终端宽度最大值为120，防止UI排版混乱
			if width > 120 {
				width = 120
			}
			return width
		}
	}
	return 80 // 默认宽度
}

// ShouldUseEnhancedEffect 判断是否使用增强效果
func (rc *RenderConfig) ShouldUseEnhancedEffect() bool {
	return !rc.UseSimpleDisplay && rc.GradientEnabled
}

// GetBannerStyle 获取横幅样式
func (rc *RenderConfig) GetBannerStyle() string {
	if rc.UseSimpleDisplay {
		return "simple"
	}
	if rc.GradientEnabled {
		return "gradient"
	}
	return "matte"
}
