package deps

import (
	"fmt"
	"time"

	"pixly/core/deps"  // 导入core/deps包
	"pixly/internal/ui"
)

// CheckDependenciesOnStartup 启动时检查依赖状态
// 根据Linus的"好品味"原则：消除特殊情况，提前发现问题
func CheckDependenciesOnStartup() error {
	// 创建依赖管理器
	dm := deps.NewDependencyManager()

	// 检查所有依赖
	if err := dm.CheckDependencies(); err != nil {
		return fmt.Errorf("依赖检查失败: %v", err)
	}

	// 获取缺失的必需工具
	missingTools := dm.GetMissingRequiredTools()

	// 如果有缺失的必需工具，显示警告
	if len(missingTools) > 0 {
		displayDependencyWarning(missingTools)
		return fmt.Errorf("缺失 %d 个必需依赖", len(missingTools))
	}

	// 显示简洁的成功提示
	ui.Printf("✅ 依赖检查完成\n")

	return nil
}

// displayDependencyWarning 显示依赖警告
func displayDependencyWarning(missingTools []*deps.ToolInfo) {
	ui.Printf("\n⚠️  依赖检查警告\n")
	ui.Printf("==================\n")
	ui.Printf("缺失以下必需工具:\n")

	for _, tool := range missingTools {
		ui.Printf("  ❌ %s\n", tool.Name)
	}

	ui.Printf("\n💡 解决方案:\n")
	ui.Printf("  运行: pixly deps install\n")
	ui.Printf("  或者: pixly deps interactive\n")
	ui.Printf("\n按任意键继续...\n")

	// 等待用户确认
	ui.WaitForKeyPress("")
}

// displayDetailedDependencyStatus 显示详细依赖状态
func displayDetailedDependencyStatus(dm *deps.DependencyManager) {
	ui.Printf("\n📦 依赖状态详情\n")
	ui.Printf("==================\n")

	allTools := dm.GetAllTools()
	for _, tool := range allTools {
		status := "❌ 未安装"
		if tool.Installed {
			status = "✅ 已安装"
		}

		required := ""
		if tool.Required {
			required = " (必需)"
		}

		ui.Printf("  %-15s %s%s\n", tool.Name+":", status, required)
		if tool.Installed && tool.Version != "" {
			ui.Printf("    版本: %s\n", tool.Version)
		}
	}

	time.Sleep(100 * time.Millisecond)
}