package ui

import (
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"go.uber.org/zap"
)

// InterruptMenuOption 中断菜单选项
type InterruptMenuOption struct {
	Icon        string
	Title       string
	Description string
	Action      func() error
	Enabled     bool
}

// InterruptMenu 中断菜单系统
type InterruptMenu struct {
	logger  *zap.Logger
	options []ArrowMenuOption
}

// NewInterruptMenu 创建新的中断菜单
func NewInterruptMenu(logger *zap.Logger) *InterruptMenu {
	return &InterruptMenu{
		logger: logger,
		options: []ArrowMenuOption{
			{
				Icon:        "🔄",
				Text:        "恢复上次进度",
				Description: "从上次中断的位置继续转换",
				Enabled:     true,
			},
			{
				Icon:        "📂",
				Text:        "开始新的目录转换",
				Description: "选择新的目录开始转换",
				Enabled:     true,
			},
			{
				Icon:        "🧪",
				Text:        "更新底层测试套件",
				Description: "运行测试套件更新",
				Enabled:     true,
			},
			{
				Icon:        "🚪",
				Text:        "退出程序",
				Description: "完全退出Pixly",
				Enabled:     true,
			},
		},
	}
}

// Show 显示中断菜单并处理用户选择
// 修改：使用方向键导航替代数字键输入
func (im *InterruptMenu) Show() error {
	// 清屏并显示标题
	fmt.Print("\033[2J\033[H") // ANSI清屏序列
	pterm.DefaultHeader.WithFullWidth().WithBackgroundStyle(pterm.NewStyle(pterm.BgRed)).Println("Pixly 转换中断")
	pterm.Println()

	// 显示中断信息
	pterm.Warning.Println("转换过程已被中断，当前状态已保存")
	pterm.Info.Println("请选择下一步操作：")
	pterm.Println()

	// 使用方向键菜单选择选项
	result, err := DisplayArrowMenu("中断菜单选项", im.options)
	if err != nil {
		return fmt.Errorf("显示菜单失败: %w", err)
	}

	if result.Cancelled {
		return nil
	}

	// 执行选择的操作
	if result.SelectedIndex >= 0 && result.SelectedIndex < len(im.options) {
		// 注意：由于我们使用了ArrowMenuOption而不是InterruptMenuOption，
		// 我们需要直接处理选项而不是调用Action函数
		switch result.SelectedIndex {
		case 0: // 恢复上次进度
			// 恢复逻辑已集成到主转换流程中
			return nil
		case 1: // 开始新的目录转换
			// 新转换逻辑已集成到主转换流程中
			return nil
		case 2: // 更新底层测试套件
			// 测试套件更新逻辑已集成到自动化测试框架中
			return nil
		case 3: // 退出程序
			os.Exit(0)
			return nil
		}
	}

	return fmt.Errorf("无效的选择")
}

// ShowQuickMenu 显示快速菜单（无交互）
// 修改：使用方向键导航替代数字键显示
func ShowQuickMenu() {
	pterm.DefaultHeader.WithFullWidth().WithBackgroundStyle(pterm.NewStyle(pterm.BgBlue)).Println("Pixly 图像转换工具")
	pterm.Println()
	pterm.Info.Println("可用选项:")

	// 使用emoji显示菜单项，不使用数字键
	pterm.Printf("%s %s 恢复上次进度\n", pterm.LightBlue("["), "🔄")
	pterm.Printf("%s %s 开始新的目录转换\n", pterm.LightBlue("["), "📂")
	pterm.Printf("%s %s 更新底层测试套件\n", pterm.LightBlue("["), "🧪")
	pterm.Printf("%s %s 退出程序\n", pterm.LightBlue("["), "🚪")
	pterm.Println()
}
