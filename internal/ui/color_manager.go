package ui

import (
	"pixly/internal/theme"

	"github.com/fatih/color"
)

// ColorType 颜色类型枚举 - 消除switch特殊情况
type ColorType int

const (
	ColorBrand ColorType = iota
	ColorAccent
	ColorSuccess
	ColorWarning
	ColorError
	ColorInfo
	ColorProgress
	ColorHighlight
	ColorMuted
	ColorMenu
	ColorMenuDisabled
	ColorMenuSelected
	ColorMenuDescription
	ColorPrompt
	ColorInput
	ColorConfirm
	ColorStatsSuccessful
	ColorStatsFailed
	ColorStatsSkipped
	ColorStatsDuration
	ColorStatsSpaceSaved
	// 新增扩展颜色类型
	ColorAccent1
	ColorAccent2
	ColorAccent3
	ColorBackground
	ColorForeground
	ColorBorder
	ColorShadow
)

// ColorConfig 颜色配置映射 - 单一数据结构，无特殊情况
type ColorConfig struct {
	themeColor   func(*theme.ColorScheme) *color.Color
	defaultColor *color.Color
}

// colorConfigs 颜色配置表 - 消除所有重复的颜色获取函数
var colorConfigs = map[ColorType]ColorConfig{
	ColorBrand: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Primary },
		defaultColor: color.New(color.FgCyan, color.Bold),
	},
	ColorAccent: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Secondary },
		defaultColor: color.New(color.FgMagenta, color.Bold),
	},
	ColorSuccess: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Success },
		defaultColor: color.New(color.FgGreen, color.Bold),
	},
	ColorWarning: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Warning },
		defaultColor: color.New(color.FgYellow, color.Bold),
	},
	ColorError: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Error },
		defaultColor: color.New(color.FgRed, color.Bold),
	},
	ColorInfo: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Info },
		defaultColor: color.New(color.FgBlue),
	},
	ColorProgress: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Progress },
		defaultColor: color.New(color.FgCyan, color.Bold),
	},
	ColorHighlight: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Highlight },
		defaultColor: color.New(color.FgWhite, color.Bold, color.BgBlue),
	},
	ColorMuted: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Muted },
		defaultColor: color.New(color.FgBlack, color.Faint),
	},
	ColorMenu: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Menu.Item },
		defaultColor: color.New(color.FgCyan, color.Bold),
	},
	ColorMenuDisabled: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Menu.Disabled },
		defaultColor: color.New(color.FgBlack, color.Faint),
	},
	ColorMenuSelected: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Menu.Selected },
		defaultColor: color.New(color.FgMagenta, color.Bold),
	},
	ColorMenuDescription: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Menu.Description },
		defaultColor: color.New(color.FgBlue, color.Faint),
	},
	ColorPrompt: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Prompt.Question },
		defaultColor: color.New(color.FgMagenta, color.Bold),
	},
	ColorInput: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Prompt.Input },
		defaultColor: color.New(color.FgCyan, color.Bold),
	},
	ColorConfirm: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Prompt.Confirm },
		defaultColor: color.New(color.FgYellow, color.Bold),
	},
	ColorStatsSuccessful: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Stats.Successful },
		defaultColor: color.New(color.FgGreen, color.Bold),
	},
	ColorStatsFailed: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Stats.Failed },
		defaultColor: color.New(color.FgRed, color.Bold),
	},
	ColorStatsSkipped: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Stats.Skipped },
		defaultColor: color.New(color.FgYellow),
	},
	ColorStatsDuration: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Stats.Duration },
		defaultColor: color.New(color.FgBlue),
	},
	ColorStatsSpaceSaved: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Stats.SpaceSaved },
		defaultColor: color.New(color.FgCyan),
	},
	// 新增扩展颜色配置
	ColorAccent1: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Accent1 },
		defaultColor: color.New(color.FgHiBlue),
	},
	ColorAccent2: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Accent2 },
		defaultColor: color.New(color.FgHiMagenta),
	},
	ColorAccent3: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Accent3 },
		defaultColor: color.New(color.FgHiCyan),
	},
	ColorBackground: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Background },
		defaultColor: color.New(color.FgWhite),
	},
	ColorForeground: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Foreground },
		defaultColor: color.New(color.FgBlack),
	},
	ColorBorder: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Border },
		defaultColor: color.New(color.FgBlack, color.Faint),
	},
	ColorShadow: {
		themeColor:   func(cs *theme.ColorScheme) *color.Color { return cs.Shadow },
		defaultColor: color.New(color.FgBlack, color.Faint),
	},
}

// GetColor 获取颜色 - 单一函数替代所有重复的颜色获取函数
// 这就是"好品味" - 消除特殊情况，用数据结构解决问题
func GetColor(colorType ColorType) *color.Color {
	config, exists := colorConfigs[colorType]
	if !exists {
		return color.New(color.FgWhite) // 默认白色
	}

	// 检查主题管理器和颜色方案
	if themeManager != nil {
		if colorScheme := themeManager.GetColorScheme(); colorScheme != nil {
			if themeColor := config.themeColor(colorScheme); themeColor != nil {
				return themeColor
			}
		}
	}

	// 返回默认颜色
	return config.defaultColor
}

// 颜色变量已在ui.go中声明，这里只提供初始化函数

// InitColors 初始化颜色变量 - 替代initColorVars
func InitColors() {
	BrandColor = GetColor(ColorBrand)
	AccentColor = GetColor(ColorAccent)
	SuccessColor = GetColor(ColorSuccess)
	WarningColor = GetColor(ColorWarning)
	ErrorColor = GetColor(ColorError)
	InfoColor = GetColor(ColorInfo)
	HeaderColor = GetColor(ColorHighlight)
	MenuColor = GetColor(ColorMenu)
	PromptColor = GetColor(ColorPrompt)
}

// GetAccent1Color 获取第一强调色
func GetAccent1Color() *color.Color {
	return GetColor(ColorAccent1)
}

// GetAccent2Color 获取第二强调色
func GetAccent2Color() *color.Color {
	return GetColor(ColorAccent2)
}

// GetAccent3Color 获取第三强调色
func GetAccent3Color() *color.Color {
	return GetColor(ColorAccent3)
}

// GetBackgroundColor 获取背景色
func GetBackgroundColor() *color.Color {
	return GetColor(ColorBackground)
}

// GetForegroundColor 获取前景色
func GetForegroundColor() *color.Color {
	return GetColor(ColorForeground)
}

// GetBorderColor 获取边框色
func GetBorderColor() *color.Color {
	return GetColor(ColorBorder)
}

// GetShadowColor 获取阴影色
func GetShadowColor() *color.Color {
	return GetColor(ColorShadow)
}

// GetThemeAwareColor 根据主题获取颜色，支持渐变效果
func GetThemeAwareColor(colorType ColorType, enableGradient bool) *color.Color {
	baseColor := GetColor(colorType)

	if !enableGradient {
		return baseColor
	}

	// 如果启用渐变，尝试从主题管理器获取渐变效果
	if themeManager != nil {
		var gradientType string
		switch colorType {
		case ColorBrand:
			gradientType = "primary"
		case ColorAccent:
			gradientType = "secondary"
		case ColorSuccess:
			gradientType = "success"
		default:
			return baseColor
		}

		if gradientColor := themeManager.GetGradientEffect(gradientType); gradientColor != nil {
			return gradientColor
		}
	}

	return baseColor
}

// GetColorWithCustomization 获取带自定义设置的颜色
func GetColorWithCustomization(colorType ColorType, brightness, contrast float64) *color.Color {
	baseColor := GetColor(colorType)

	// 简化的亮度和对比度调整（实际实现可能需要更复杂的颜色空间转换）
	if brightness != 1.0 || contrast != 1.0 {
		// 这里可以添加更复杂的颜色调整逻辑
		// 目前返回基础颜色
		return baseColor
	}

	return baseColor
}
