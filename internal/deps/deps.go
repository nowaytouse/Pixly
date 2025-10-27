package deps

import (
	"fmt"

	"github.com/spf13/cobra"
	"pixly/core/deps"  // 导入core/deps包
)

// DepsCmd represents the deps command
var DepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "📦 管理依赖组件",
	Long: `检查、安装和管理Pixly所需的外部依赖组件。

支持的依赖组件：
- FFmpeg/FFprobe: 视频处理工具
- cjxl: JPEG XL编码器
- avifenc: AVIF编码器
- exiftool: 元数据处理工具`,
}

var checkDepsCmd = &cobra.Command{
	Use:   "check",
	Short: "检查依赖组件状态",
	Long:  `检查所有必需依赖组件是否已正确安装并可访问。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheckDeps()
	},
}

var installDepsCmd = &cobra.Command{
	Use:   "install",
	Short: "安装缺失的依赖组件",
	Long:  `自动检测并安装缺失的依赖组件（需要Homebrew）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInstallDeps()
	},
}

var interactiveInstallCmd = &cobra.Command{
	Use:   "interactive",
	Short: "交互式安装依赖组件",
	Long:  `提供交互式界面来选择和安装依赖组件`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInteractiveInstall()
	},
}

func init() {
	DepsCmd.AddCommand(checkDepsCmd)
	DepsCmd.AddCommand(installDepsCmd)
	DepsCmd.AddCommand(interactiveInstallCmd)
}

func runCheckDeps() error {
	fmt.Println("🔍 检查依赖组件状态...")

	// 创建依赖管理器
	dm := deps.NewDependencyManager()

	// 检查所有依赖
	if err := dm.CheckDependencies(); err != nil {
		return fmt.Errorf("检查依赖失败: %v", err)
	}

	// 显示结果
	fmt.Println("\n📦 依赖组件状态:")
	fmt.Println("==================")

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

		fmt.Printf("%-20s %s%s\n", tool.Name+":", status, required)
		if tool.Installed && tool.Version != "" {
			fmt.Printf("  版本: %s\n", tool.Version)
		}
		if tool.Installed && len(tool.Features) > 0 {
			fmt.Printf("  特性: %s\n", tool.Features)
		}
		if !tool.Installed && tool.ErrorMessage != "" {
			fmt.Printf("  错误: %s\n", tool.ErrorMessage)
		}
		fmt.Println()
	}

	// 总结
	if dm.IsAllRequiredInstalled() {
		fmt.Println("🎉 所有必需依赖组件均已正确安装!")
	} else {
		missing := dm.GetMissingRequiredTools()
		fmt.Printf("⚠️  缺失 %d 个必需依赖组件:\n", len(missing))
		for _, tool := range missing {
			fmt.Printf("  - %s (%s)\n", tool.Name, tool.Path)
		}
		fmt.Println("\n运行 'pixly deps install' 来安装缺失的组件")
	}

	return nil
}

func runInstallDeps() error {
	fmt.Println("🔧 安装依赖组件...")

	// 创建依赖管理器和安装器
	dm := deps.NewDependencyManager()
	installer := deps.NewInstaller(dm)

	// 检查并安装
	return installer.CheckAndInstall()
}

func runInteractiveInstall() error {
	fmt.Println("🔧 交互式安装依赖组件...")

	// 创建依赖管理器和安装器
	dm := deps.NewDependencyManager()
	installer := deps.NewInstaller(dm)

	// 交互式安装
	return installer.InteractiveInstall()
}