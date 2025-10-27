package ui

import (
	"strings"

	"pixly/internal/emoji"
	"pixly/internal/i18n"
	"pixly/internal/output"
)

// ArrowMenuOption 方向键菜单选项
type ArrowMenuOption struct {
	Icon        string
	Text        string
	Description string
	Enabled     bool
}

// ArrowMenuResult 方向键菜单结果
type ArrowMenuResult struct {
	SelectedIndex int
	Cancelled     bool
}

// DisplayArrowMenu 显示真正的方向键导航菜单
// 统一使用方向键+回车，消除数字输入的特殊情况
func DisplayArrowMenu(title string, options []ArrowMenuOption) (*ArrowMenuResult, error) {
	if len(options) == 0 {
		return &ArrowMenuResult{Cancelled: true}, nil
	}

	// 使用统一输出系统
	output := output.GetOutputController()

	// 找到第一个启用的选项作为初始选择
	selectedIndex := 0
	for i, option := range options {
		if option.Enabled {
			selectedIndex = i
			break
		}
	}

	// 首次显示菜单
	displayArrowMenuContent(title, options, selectedIndex)

	// 主循环：处理输入并更新显示
	for {
		// 显示操作提示 - 使用统一输出
		output.WriteLine("\n🎮 操作: ↑/↓ 选择, Enter 确认, q/b 退出")
		output.Flush()

		// 读取输入 - 修复ANSI序列处理
		input := readCleanInput()
		if input == "" {
			continue
		}

		// 处理输入
		switch {
		case strings.ToLower(input) == "q" || strings.ToLower(input) == "b":
			return &ArrowMenuResult{Cancelled: true}, nil

		case input == "up" || strings.ToLower(input) == "w":
			// 向上移动（处理ANSI转义序列）
			oldIndex := selectedIndex
			for i := selectedIndex - 1; i >= 0; i-- {
				if options[i].Enabled {
					selectedIndex = i
					break
				}
			}
			// 只有当选择发生变化时才重新渲染
			if oldIndex != selectedIndex {
				displayArrowMenuContent(title, options, selectedIndex)
			}

		case input == "down" || strings.ToLower(input) == "s":
			// 向下移动（处理ANSI转义序列）
			oldIndex := selectedIndex
			for i := selectedIndex + 1; i < len(options); i++ {
				if options[i].Enabled {
					selectedIndex = i
					break
				}
			}
			// 只有当选择发生变化时才重新渲染
			if oldIndex != selectedIndex {
				displayArrowMenuContent(title, options, selectedIndex)
			}

		case input == "enter":
			// 确认选择
			if selectedIndex >= 0 && selectedIndex < len(options) && options[selectedIndex].Enabled {
				return &ArrowMenuResult{SelectedIndex: selectedIndex}, nil
			}

		default:
			// 忽略无效的ANSI序列和数字输入，不显示错误信息
			// 好品味：消除数字输入特殊情况，统一使用方向键导航
			if !strings.HasPrefix(input, "\033[") {
				output.WriteLine("❌ 无效输入，请使用方向键导航")
				output.Flush()
			}
		}
	}
}

// displayArrowMenuContent 显示方向键菜单内容
func displayArrowMenuContent(title string, options []ArrowMenuOption, selectedIndex int) {
	// 使用统一输出系统 - 消除多重渲染器地狱
	output := output.GetOutputController()

	// 统一清屏 - 消除重复的ANSI序列
	output.Clear()
	output.WriteLine("")

	// 直接使用UnicodeEmoji，避免复杂的依赖
	unicodeEmoji := emoji.NewUnicodeEmoji()

	// 显示标题 - 使用unicode emoji，优化字符串拼接
	var titleBuilder strings.Builder
	titleBuilder.WriteString("📋 ")
	titleBuilder.WriteString(title)
	titleText := unicodeEmoji.Apply(titleBuilder.String())
	// 动态计算边框长度，但限制最大值防止布局混乱
	titleDisplayWidth := calculateDisplayWidth(titleText)
	borderLen := titleDisplayWidth + 4 // 标题宽度 + 左右边距
	if borderLen < 30 {
		borderLen = 30 // 最小边框长度
	}
	if borderLen > 80 {
		borderLen = 80 // 最大边框长度，防止过长
	}
	border := strings.Repeat("═", borderLen)

	// 标题框架 - 统一输出，优化字符串拼接
	output.WriteString("  ╔")
	output.WriteString(border)
	output.WriteString("╗\n")
	output.WriteString("  ║  ")
	output.WriteString(titleText)
	output.WriteString("  ║\n")
	output.WriteString("  ╚")
	output.WriteString(border)
	output.WriteString("╝\n")
	output.WriteLine("")

	// 显示选项 - 真正的emoji包围效果，序列数字装饰
	for i, option := range options {
		if !option.Enabled {
			// 禁用选项：emoji包围灰色效果，优化字符串拼接
			var disabledBuilder strings.Builder
			disabledBuilder.WriteString("⚫ ")
			disabledBuilder.WriteString(option.Icon)
			disabledBuilder.WriteString(" ")
			disabledBuilder.WriteString(option.Text)
			disabledBuilder.WriteString(" ⚫")
			disabledText := unicodeEmoji.Apply(disabledBuilder.String())
			output.WriteString("  ⓪ ")
			output.WriteString(disabledText)
			output.WriteString(" (")
			output.WriteString(i18n.T(i18n.TextDisabled))
			output.WriteString(")\n")
			continue
		}

		if i == selectedIndex {
			// 选中项：双层emoji包围，简洁无数字，优化字符串拼接
			var selectedBuilder strings.Builder
			selectedBuilder.WriteString("✨ ")
			selectedBuilder.WriteString(option.Icon)
			selectedBuilder.WriteString(" ")
			selectedBuilder.WriteString(option.Text)
			selectedBuilder.WriteString(" ✨")
			selectedText := unicodeEmoji.Apply(selectedBuilder.String())
			output.WriteString("  ▶ ")
			output.WriteString(selectedText)
			output.WriteString(" ◀\n")
			if option.Description != "" {
				output.WriteString("     💡 ")
				output.WriteString(option.Description)
				output.WriteString("\n")
			}
		} else {
			// 普通选项：单层emoji包围，简洁无数字，优化字符串拼接
			var normalBuilder strings.Builder
			normalBuilder.WriteString("🔹 ")
			normalBuilder.WriteString(option.Icon)
			normalBuilder.WriteString(" ")
			normalBuilder.WriteString(option.Text)
			normalBuilder.WriteString(" 🔹")
			normalText := unicodeEmoji.Apply(normalBuilder.String())
			output.WriteString("    ")
			output.WriteString(normalText)
			output.WriteString("\n")
			if option.Description != "" {
				output.WriteString("       💭 ")
				output.WriteString(option.Description)
				output.WriteString("\n")
			}
		}
	}

	output.WriteLine("")
	output.Flush()
}

// calculateDisplayWidth 计算字符串的实际显示宽度
// 处理emoji字符的双宽度特性
func calculateDisplayWidth(text string) int {
	width := 0
	for _, r := range text {
		// emoji字符和其他宽字符通常占用2个显示位置
		if isWideCharacter(r) {
			width += 2
		} else {
			width += 1
		}
	}
	return width
}

// isWideCharacter 判断字符是否为宽字符（如emoji）
func isWideCharacter(r rune) bool {
	// 简化的宽字符检测
	// emoji字符通常在这些Unicode范围内
	if r >= 0x1F600 && r <= 0x1F64F { // 表情符号
		return true
	}
	if r >= 0x1F300 && r <= 0x1F5FF { // 杂项符号和象形文字
		return true
	}
	if r >= 0x1F680 && r <= 0x1F6FF { // 交通和地图符号
		return true
	}
	if r >= 0x2600 && r <= 0x26FF { // 杂项符号
		return true
	}
	if r >= 0x2700 && r <= 0x27BF { // 装饰符号
		return true
	}
	if r >= 0xFE00 && r <= 0xFE0F { // 变体选择器
		return true
	}
	// 中文字符等也是宽字符
	if r >= 0x4E00 && r <= 0x9FFF { // CJK统一汉字
		return true
	}
	return false
}

// readCleanInput 使用统一输入管理器读取按键
func readCleanInput() string {
	key, err := ReadKey()
	if err != nil {
		// 如果读取失败，返回退出
		return "q"
	}
	return key
}

// getPreviousEnabledOption 获取上一个启用的选项索引
// getPreviousEnabledOption 和 getNextEnabledOption 已删除 - 不再需要方向键导航

// fallbackToNumberMenu 已删除 - 统一使用DisplayArrowMenu的数字输入机制
// getUserChoiceForArrowMenu 已删除 - 统一使用DisplayArrowMenu的数字输入机制
