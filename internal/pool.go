package internal

import (
	"fmt"
	"time"

	"pixly/config"
	"pixly/core/converter"
	"pixly/internal/logger"
	"pixly/internal/ui"

	"github.com/spf13/cobra"
)

// PoolCmd 池监控命令
var PoolCmd = &cobra.Command{
	Use:   "pool",
	Short: "🏊 池监控和管理工具",
	Long: `池监控和管理工具提供以下功能：

• 实时监控 ants goroutine 池状态
• 查看任务队列和执行统计
• 动态调整池大小
• 性能指标分析

这个工具帮助开发者和高级用户了解 Pixly 的并发处理性能。`,
	Run: func(cmd *cobra.Command, args []string) {
		showPoolStatus()
	},
}

// poolStatusCmd 显示池状态
var poolStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "📊 显示当前池状态",
	Long:  "显示 ants goroutine 池的详细状态信息，包括活跃工作者、队列任务、性能指标等。",
	Run: func(cmd *cobra.Command, args []string) {
		showPoolStatus()
	},
}

// poolMonitorCmd 实时监控池状态
var poolMonitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "📈 实时监控池状态",
	Long:  "实时监控 ants goroutine 池的状态变化，每秒更新一次数据。按 Ctrl+C 退出监控。",
	Run: func(cmd *cobra.Command, args []string) {
		monitorPool()
	},
}

// poolTuneCmd 调整池大小
var poolTuneCmd = &cobra.Command{
	Use:   "tune [size]",
	Short: "⚙️ 动态调整池大小",
	Long:  "动态调整 ants goroutine 池的大小。参数为新的池大小（工作者数量）。",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		tunePoolSize(args[0])
	},
}

func init() {
	// 添加子命令
	PoolCmd.AddCommand(poolStatusCmd)
	PoolCmd.AddCommand(poolMonitorCmd)
	PoolCmd.AddCommand(poolTuneCmd)
}

// showPoolStatus 显示池状态
func showPoolStatus() {
	// 创建日志器
	loggerInstance, err := logger.NewLoggerWithConfig(logger.DefaultLoggerConfig())
	if err != nil {
		fmt.Printf("❌ 创建日志器失败: %v\n", err)
		return
	}
	defer func() {
		if err := loggerInstance.Sync(); err != nil {
			fmt.Printf("Warning: failed to sync logger: %v\n", err)
		}
	}()

	// 创建转换器实例
	cfg, err := config.NewConfig("", loggerInstance)
	if err != nil {
		fmt.Printf("❌ 创建配置失败: %v\n", err)
		return
	}
	conv, err := converter.NewConverter(cfg, loggerInstance, "auto+")
	if err != nil {
		fmt.Printf("❌ 创建转换器失败: %v\n", err)
		return
	}
	defer conv.Close()

	// 显示标题
	ui.DisplayCenteredBanner("🏊 Goroutine 池状态监控", "info")
	fmt.Println()

	// 获取池信息
	poolInfo := conv.GetPoolInfo()
	poolMetrics := conv.GetPoolMetrics()

	// 显示基础池信息
	fmt.Println("📋 基础 Ants 池信息:")
	if running, ok := poolInfo["basic_pool_running"].(int); ok {
		fmt.Printf("  • 运行中的工作者: %d\n", running)
	}
	if free, ok := poolInfo["basic_pool_free"].(int); ok {
		fmt.Printf("  • 空闲工作者: %d\n", free)
	}
	if cap, ok := poolInfo["basic_pool_cap"].(int); ok {
		fmt.Printf("  • 池容量: %d\n", cap)
	}
	fmt.Println()

	// 显示高级池信息
	if poolMetrics != nil {
		fmt.Println("🚀 高级池监控指标:")
		fmt.Printf("  • 活跃工作者: %d\n", poolMetrics.ActiveWorkers)
		fmt.Printf("  • 排队任务: %d\n", poolMetrics.QueuedTasks)
		fmt.Printf("  • 已完成任务: %d\n", poolMetrics.CompletedTasks)
		fmt.Printf("  • 失败任务: %d\n", poolMetrics.FailedTasks)
		fmt.Printf("  • 总任务数: %d\n", poolMetrics.TotalTasks)
		fmt.Printf("  • 平均等待时间: %v\n", poolMetrics.AverageWaitTime)
		fmt.Printf("  • 平均执行时间: %v\n", poolMetrics.AverageExecTime)
		fmt.Printf("  • 最后更新: %v\n", poolMetrics.LastUpdate.Format("2006-01-02 15:04:05"))
		fmt.Println()
	} else {
		fmt.Println("⚠️ 高级池未启用或不可用")
		fmt.Println()
	}

	// 显示详细配置信息
	fmt.Println("⚙️ 池配置详情:")
	for key, value := range poolInfo {
		if key != "basic_pool_running" && key != "basic_pool_free" && key != "basic_pool_cap" {
			fmt.Printf("  • %s: %v\n", key, value)
		}
	}
}

// monitorPool 实时监控池状态
func monitorPool() {
	// 创建日志器
	loggerInstance, err := logger.NewLoggerWithConfig(logger.DefaultLoggerConfig())
	if err != nil {
		fmt.Printf("❌ 创建日志器失败: %v\n", err)
		return
	}
	defer loggerInstance.Sync()

	// 创建转换器实例
	cfg, err := config.NewConfig("", loggerInstance)
	if err != nil {
		fmt.Printf("❌ 创建配置失败: %v\n", err)
		return
	}
	conv, err := converter.NewConverter(cfg, loggerInstance, "auto+")
	if err != nil {
		fmt.Printf("❌ 创建转换器失败: %v\n", err)
		return
	}
	defer conv.Close()

	// 显示标题
	ui.DisplayCenteredBanner("📊 实时池监控 (按 Ctrl+C 退出)", "info")
	fmt.Println("按 Ctrl+C 退出监控...")
	fmt.Println()

	// 创建定时器
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// 监控循环
	for range ticker.C {
		// 清屏并显示当前状态
		ui.ClearScreen()
		ui.DisplayCenteredBanner("📊 实时池监控 (按 Ctrl+C 退出)", "info")

		// 显示当前时间
		fmt.Printf("🕐 监控时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

		// 获取并显示池状态
		poolMetrics := conv.GetPoolMetrics()
		if poolMetrics != nil {
			fmt.Println("🚀 高级池实时状态:")
			fmt.Printf("  活跃工作者: %d\n", poolMetrics.ActiveWorkers)
			fmt.Printf("  排队任务: %d\n", poolMetrics.QueuedTasks)
			fmt.Printf("  已完成: %d | 失败: %d | 总计: %d\n",
				poolMetrics.CompletedTasks, poolMetrics.FailedTasks, poolMetrics.TotalTasks)
			fmt.Printf("  平均等待: %v | 平均执行: %v\n",
				poolMetrics.AverageWaitTime, poolMetrics.AverageExecTime)
		} else {
			fmt.Println("⚠️ 高级池监控数据不可用")
		}

		// 显示基础池信息
		poolInfo := conv.GetPoolInfo()
		fmt.Println("\n📋 基础池状态:")
		if running, ok := poolInfo["basic_pool_running"].(int); ok {
			fmt.Printf("  运行中: %d", running)
		}
		if free, ok := poolInfo["basic_pool_free"].(int); ok {
			fmt.Printf(" | 空闲: %d", free)
		}
		if cap, ok := poolInfo["basic_pool_cap"].(int); ok {
			fmt.Printf(" | 容量: %d\n", cap)
		}
	}
}

// tunePoolSize 调整池大小
func tunePoolSize(sizeStr string) {
	// 解析大小参数
	var size int
	if _, err := fmt.Sscanf(sizeStr, "%d", &size); err != nil {
		fmt.Printf("❌ 无效的池大小参数: %s\n", sizeStr)
		return
	}

	if size <= 0 {
		fmt.Printf("❌ 池大小必须大于 0\n")
		return
	}

	// 创建日志器
	loggerInstance, err := logger.NewLoggerWithConfig(logger.DefaultLoggerConfig())
	if err != nil {
		fmt.Printf("❌ 创建日志器失败: %v\n", err)
		return
	}
	defer loggerInstance.Sync()

	// 创建转换器实例
	cfg, err := config.NewConfig("", loggerInstance)
	if err != nil {
		fmt.Printf("❌ 创建配置失败: %v\n", err)
		return
	}
	conv, err := converter.NewConverter(cfg, loggerInstance, "auto+")
	if err != nil {
		fmt.Printf("❌ 创建转换器失败: %v\n", err)
		return
	}
	defer conv.Close()

	// 显示标题
	ui.DisplayCenteredBanner("🔧 池大小调整", "info")

	// 显示调整前状态
	fmt.Printf("🔧 正在调整池大小到 %d...\n", size)
	poolInfo := conv.GetPoolInfo()
	if cap, ok := poolInfo["basic_pool_cap"].(int); ok {
		fmt.Printf("调整前池容量: %d\n", cap)
	}

	// 执行调整
	conv.TunePoolSize(size)

	// 显示调整后状态
	time.Sleep(100 * time.Millisecond) // 等待调整生效
	poolInfo = conv.GetPoolInfo()
	if cap, ok := poolInfo["basic_pool_cap"].(int); ok {
		fmt.Printf("调整后池容量: %d\n", cap)
	}

	fmt.Println("✅ 池大小调整完成")
}
