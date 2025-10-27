package ui

import (
	"pixly/internal/i18n"
	"strings"
)

// ShowStatisticsPage 显示转换统计页面
// 统计数据显示应由converter包处理，UI包不应依赖converter类型
func ShowStatisticsPage(statsText string, interactive bool) {
	ClearScreen()

	// 显示横幅
	DisplayBanner(i18n.T(i18n.TextConversionComplete), "success")

	// 显示完成动画
	DisplayCompletionAnimation()

	// 添加分隔线
	Println("")
	SuccessColor.Println("═══════════════════════════════════════════════════════════════")
	SuccessColor.Println("                        📊 转换统计报告                        ")
	SuccessColor.Println("═══════════════════════════════════════════════════════════════")
	Println("")

	// 显示详细统计信息
	displayDetailedStats(statsText)

	// 添加底部分隔线
	Println("")
	SuccessColor.Println("═══════════════════════════════════════════════════════════════")
	Println("")

	// 只在交互模式下显示下一步提示和等待用户按键
	if interactive {
		// 显示下一步提示
		displayNextStepPrompt()

		// 等待用户按键
		WaitForKeyPress("")
	}
}

// displayDetailedStats 显示详细统计信息
func displayDetailedStats(statsText string) {
	// 统计详情显示逻辑应在converter包中完成
	// UI包只负责渲染已格式化的文本

	// 使用更好的格式显示统计信息
	InfoColor.Println("  📈 详细统计信息:")
	Println("")

	// 显示统计文本，每行前添加缩进
	lines := strings.Split(statsText, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			// 使用InfoColor来确保统计信息能够正确显示
			InfoColor.Println("    " + line)
		}
	}
}

// displayNextStepPrompt 显示下一步提示
func displayNextStepPrompt() {
	// 显示操作提示
	InfoColor.Println("  🔄 操作选项:")
	Println("")
	PromptColor.Println("    ✨ 按 Enter 键继续处理更多文件")
	PromptColor.Println("    🏠 或者直接关闭程序返回主菜单")
	Println("")
	PromptColor.Println("  " + i18n.T(i18n.TextPressEnterToContinue))
}
