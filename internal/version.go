package internal

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"pixly/internal/cmd"
	"pixly/core/deps"
	"pixly/internal/ui"
	"pixly/internal/version"

	"github.com/spf13/cobra"
)

// 版本信息变量已在 root.go 中定义

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "📋 显示版本信息",
	Long:  `显示Pixly的详细版本信息，包括构建时间、Go版本和依赖状态。`,
	Run: func(cmd *cobra.Command, args []string) {
		showVersionInfo()
	},
}

var shortVersionCmd = &cobra.Command{
	Use:   "short",
	Short: "显示简短版本信息",
	Long:  `仅显示版本号，适用于脚本调用。`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.GetVersion())
	},
}

var fullVersionCmd = &cobra.Command{
	Use:   "full",
	Short: "显示完整版本信息",
	Long:  `显示包含依赖状态的完整版本信息。`,
	Run: func(cmd *cobra.Command, args []string) {
		showFullVersionInfo()
	},
}

func init() {
	cmd.AddCommand(versionCmd)
	versionCmd.AddCommand(shortVersionCmd)
	versionCmd.AddCommand(fullVersionCmd)
}

func showVersionInfo() {
	ui.ClearScreen()
	ui.DisplayBanner("📋 Pixly 版本信息", "info")

	fmt.Printf("\n🚀 Pixly 媒体转换引擎\n")
	fmt.Printf("   版本: %s\n", version.GetVersion())
	fmt.Printf("   构建时间: %s\n", version.GetBuildTime())
	fmt.Printf("   Go版本: %s\n", runtime.Version())
	fmt.Printf("   系统架构: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("   CPU核心数: %d\n", runtime.NumCPU())

	fmt.Printf("\n📦 核心依赖状态:\n")
	showDependencyStatus()

	fmt.Printf("\n💡 使用 'pixly version full' 查看完整信息\n")
	fmt.Printf("💡 使用 'pixly deps check' 检查所有依赖\n")
}

func showFullVersionInfo() {
	ui.ClearScreen()
	ui.DisplayBanner("📋 Pixly 完整版本信息", "info")

	fmt.Printf("\n🚀 Pixly 媒体转换引擎\n")
	fmt.Printf("   版本: %s\n", version.GetVersion())
	fmt.Printf("   构建时间: %s\n", version.GetBuildTime())
	fmt.Printf("   Go版本: %s\n", runtime.Version())
	fmt.Printf("   系统架构: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("   CPU核心数: %d\n", runtime.NumCPU())

	// 显示内存信息
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("\n💾 内存信息:\n")
	fmt.Printf("   已分配内存: %.2f MB\n", float64(m.Alloc)/1024/1024)
	fmt.Printf("   系统内存: %.2f MB\n", float64(m.Sys)/1024/1024)
	fmt.Printf("   GC次数: %d\n", m.NumGC)

	fmt.Printf("\n📦 依赖组件状态:\n")
	showDetailedDependencyStatus()

	fmt.Printf("\n🔧 配置信息:\n")
	// 使用 ui.GetGlobalConfig() 获取配置
	cfg := ui.GetGlobalConfig()
	if cfg != nil {
		fmt.Printf("   日志级别: %s\n", cfg.Logging.Level)
		fmt.Printf("   主题模式: %s\n", cfg.Theme.Mode)
		fmt.Printf("   默认转换模式: %s\n", cfg.Conversion.DefaultMode)
		fmt.Printf("   并发工作数: %d\n", cfg.Concurrency.ConversionWorkers)
	} else {
		fmt.Printf("   配置未加载\n")
	}

	fmt.Printf("\n⏰ 运行时信息:\n")
	fmt.Printf("   启动时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("   Goroutine数量: %d\n", runtime.NumGoroutine())
}

func showDependencyStatus() {
	dm := deps.NewDependencyManager()
	err := dm.CheckDependencies()
	if err != nil {
		fmt.Printf("   ⚠️ 检查依赖时出错: %v\n", err)
		return
	}

	tools := dm.GetAllTools()
	for _, tool := range tools {
		if tool.Installed {
			fmt.Printf("   ✅ %s: %s\n", tool.Name, tool.Version)
		} else {
			fmt.Printf("   ❌ %s: 未安装\n", tool.Name)
		}
	}
}

func showDetailedDependencyStatus() {
	dm := deps.NewDependencyManager()
	err := dm.CheckDependencies()
	if err != nil {
		fmt.Printf("   ⚠️ 检查依赖时出错: %v\n", err)
		return
	}

	tools := dm.GetAllTools()
	for _, tool := range tools {
		if tool.Installed {
			fmt.Printf("   ✅ %s\n", tool.Name)
			fmt.Printf("      版本: %s\n", tool.Version)
			fmt.Printf("      路径: %s\n", tool.Path)
			if len(tool.Features) > 0 {
				fmt.Printf("      特性: %s\n", strings.Join(tool.Features, ", "))
			}
		} else {
			fmt.Printf("   ❌ %s: 未安装\n", tool.Name)
			if tool.ErrorMessage != "" {
				fmt.Printf("      错误: %s\n", tool.ErrorMessage)
			}
		}
		fmt.Println()
	}
}