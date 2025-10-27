package ui

import (
	"pixly/internal/i18n"
	"strings"
)

// SymmetricEmojiLayout 对称emoji布局结构
// 修改：移除数字键显示，改为使用方向键导航
type SymmetricEmojiLayout struct {
	Icon        string
	Text        string
	Description string
	Enabled     bool
	LeftEmoji   string // 添加LeftEmoji字段
	RightEmoji  string // 添加RightEmoji字段
}

// CreateSymmetricEmojiText 创建对称emoji文本
func CreateSymmetricEmojiText(emoji string, text string) string {
	var builder strings.Builder
	builder.WriteString(emoji)
	builder.WriteString(" ")
	builder.WriteString(text)
	builder.WriteString(" ")
	builder.WriteString(emoji)
	return builder.String()
}

// CreateDualEmojiText 创建双emoji文本（左右不同）
func CreateDualEmojiText(leftEmoji, rightEmoji, text string) string {
	var builder strings.Builder
	builder.WriteString(leftEmoji)
	builder.WriteString(" ")
	builder.WriteString(text)
	builder.WriteString(" ")
	builder.WriteString(rightEmoji)
	return builder.String()
}

// DisplaySymmetricMenu 显示对称emoji菜单
// 修改：移除数字键显示，改为使用方向键导航
func DisplaySymmetricMenu(title string, options []SymmetricEmojiLayout) {
	print("\n")

	// 显示标题（使用对称的边框装饰增强美观性）
	titleLen := len(title) + 4
	// 限制边框长度，防止生成过长的字符串
	if titleLen > 100 {
		titleLen = 100
	}
	border := strings.Repeat("═", titleLen)
	HeaderColor.Printf("  ╔%s╗\n", border)
	HeaderColor.Printf("  ║ %s ║\n", title)
	HeaderColor.Printf("  ╚%s╝\n", border)

	// 修改：移除数字键显示，改为使用方向键导航
	for _, option := range options {
		// 显示对称emoji菜单项
		if option.RightEmoji != "" {
			// 对于语言选项，LeftEmoji是国旗，Text是语言名称，RightEmoji是英文名称
			// 修改：移除数字键显示
			getMenuColor().Printf("  ▶ %s %s\n", option.LeftEmoji, option.Text)
			GetColor(ColorMenuDescription).Printf("     %s\n", option.RightEmoji)
		} else {
			// 对于其他选项，使用对称emoji显示
			// 修改：移除数字键显示
			menuText := CreateSymmetricEmojiText(option.Icon, option.Text)
			getMenuColor().Printf("  ▶ %s\n", menuText)
		}
	}
	// 修改：提示使用方向键导航
	getPromptColor().Print(i18n.T(i18n.TextChooseOption) + " (使用方向键 ↑/↓ 选择, Enter 确认): ")
}

// CreateSymmetricBanner 创建对称emoji横幅
func CreateSymmetricBanner(leftEmoji, rightEmoji, text string) string {
	var builder strings.Builder
	builder.WriteString(leftEmoji)
	builder.WriteString(" ")
	builder.WriteString(text)
	builder.WriteString(" ")
	builder.WriteString(rightEmoji)
	return builder.String()
}

// DisplaySymmetricStats 显示对称emoji统计信息
func DisplaySymmetricStats(stats map[string]SymmetricEmojiLayout) {
	print("\n")

	// 显示标题
	title := i18n.T(i18n.TextConversionStats)
	titleLen := len(title) + 4
	// 限制边框长度，防止生成过长的字符串
	if titleLen > 100 {
		titleLen = 100
	}
	border := strings.Repeat("═", titleLen)
	HeaderColor.Printf("  ╔%s╗\n", border)
	HeaderColor.Printf("  ║ %s ║\n", title)
	HeaderColor.Printf("  ╚%s╝\n", border)

	// 显示统计信息
	for _, stat := range stats {
		statText := CreateSymmetricEmojiText(stat.LeftEmoji, stat.Text)
		HeaderColor.Printf("  %s\n", statText)
	}
}
