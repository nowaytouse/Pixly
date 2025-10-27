package ui

import (
	"strconv"
	"strings"

	"pixly/internal/output"
	"pixly/internal/theme"

	"github.com/fatih/color"
)

// BackgroundStyle 背景样式结构
type BackgroundStyle struct {
	Name        string
	Description string
	Preview     string
}

// CreateStyledBackground 创建带样式的背景
func CreateStyledBackground(content string, bgStyle string) string {
	themeManager := theme.GetGlobalThemeManager()
	if themeManager == nil {
		return content
	}

	switch bgStyle {
	case "dark":
		// 暗色背景
		return "\033[48;5;235m" + content + "\033[0m"
	case "light":
		// 亮色背景
		return "\033[48;5;255m\033[38;5;235m" + content + "\033[0m"
	case "gradient":
		// 渐变背景
		return createGradientBackground(content)
	default:
		return content
	}
}

// createGradientBackground 创建渐变背景
func createGradientBackground(content string) string {
	// 简化的渐变背景实现
	lines := strings.Split(content, "\n")
	var result strings.Builder

	for i, line := range lines {
		// 根据行号计算颜色
		bgColor := 232 + (i % 10)
		result.WriteString("\033[48;5;")
		result.WriteString(strconv.Itoa(bgColor))
		result.WriteString("m")
		result.WriteString(line)
		result.WriteString("\033[0m\n")
	}

	return result.String()
}

// DisplayWithBackground 带背景显示内容
func DisplayWithBackground(title, content, bgStyle string) {
	output.GetOutputController().WriteLine("")

	// 显示标题
	titleLen := len(title) + 4
	// 限制边框长度，防止生成过长的字符串
	if titleLen > 100 {
		titleLen = 100
	}
	border := strings.Repeat("═", titleLen)
	HeaderColor.Printf("  ╔%s╗\n", border)
	HeaderColor.Printf("  ║ %s ║\n", title)
	HeaderColor.Printf("  ╚%s╝\n", border)

	output.GetOutputController().WriteLine("")

	// 显示带背景的内容
	styledContent := CreateStyledBackground(content, bgStyle)
	output.GetOutputController().WriteLine(styledContent)
}

// GetAvailableBackgroundStyles 获取可用的背景样式
func GetAvailableBackgroundStyles() []BackgroundStyle {
	return []BackgroundStyle{
		{Name: "default", Description: "默认背景", Preview: "■"},
		{Name: "dark", Description: "暗色背景", Preview: "■"},
		{Name: "light", Description: "亮色背景", Preview: "■"},
		{Name: "gradient", Description: "渐变背景", Preview: "■■■■■"},
	}
}

// ApplyBackgroundTheme 应用背景主题
func ApplyBackgroundTheme(themeName string) {
	themeManager := theme.GetGlobalThemeManager()
	if themeManager == nil {
		return
	}

	// 根据主题名称应用不同的背景设置
	switch themeName {
	case "dark":
		// 应用暗色主题背景设置
		output.GetOutputController().WriteString("\033[48;5;235m") // 设置暗色背景
	case "light":
		// 应用亮色主题背景设置
		output.GetOutputController().WriteString("\033[48;5;255m") // 设置亮色背景
	default:
		// 重置背景
		output.GetOutputController().WriteString("\033[49m") // 重置为默认背景
	}

	// 确保在程序结束时重置所有属性
	defer output.GetOutputController().WriteString("\033[0m")
}

// CreateSectionWithBackground 创建带背景的区域
func CreateSectionWithBackground(title string, content []string, bgStyle string) {
	output.GetOutputController().WriteLine("")

	// 创建带背景的标题
	titleSection := "  " + title + "  "
	titleLen := len(titleSection)
	// 限制边框长度，防止生成过长的字符串
	if titleLen > 100 {
		titleLen = 100
	}
	border := strings.Repeat("═", titleLen)

	// 根据背景样式设置颜色
	switch bgStyle {
	case "dark":
		color.New(color.BgHiBlack, color.FgHiWhite, color.Bold).Printf("  ╔%s╗\n", border)
		color.New(color.BgHiBlack, color.FgHiWhite, color.Bold).Printf("  ║%s║\n", titleSection)
		color.New(color.BgHiBlack, color.FgHiWhite, color.Bold).Printf("  ╚%s╝\n", border)
	case "light":
		color.New(color.BgHiWhite, color.FgHiBlack, color.Bold).Printf("  ╔%s╗\n", border)
		color.New(color.BgHiWhite, color.FgHiBlack, color.Bold).Printf("  ║%s║\n", titleSection)
		color.New(color.BgHiWhite, color.FgHiBlack, color.Bold).Printf("  ╚%s╝\n", border)
	default:
		HeaderColor.Printf("  ╔%s╗\n", border)
		HeaderColor.Printf("  ║%s║\n", titleSection)
		HeaderColor.Printf("  ╚%s╝\n", border)
	}

	output.GetOutputController().WriteLine("")

	// 显示内容
	for _, line := range content {
		switch bgStyle {
		case "dark":
			color.New(color.BgHiBlack, color.FgHiWhite).Printf("  %s\n", line)
		case "light":
			color.New(color.BgHiWhite, color.FgHiBlack).Printf("  %s\n", line)
		default:
			InfoColor.Printf("  %s\n", line)
		}
	}
}
