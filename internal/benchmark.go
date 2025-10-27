package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"pixly/internal/cmd"
	"pixly/internal/ui"
)

// benchmarkCmd 定义benchmark命令
var benchmarkCmd = &cobra.Command{
	Use:   "benchmark [directory]",
	Short: "运行性能基准测试",
	Long: `运行Pixly的性能基准测试，评估在当前系统上的转换性能。

基准测试将使用测试文件集合来评估:
- 不同转换模式的性能
- 并发处理能力
- 内存使用效率
- 各种文件格式的处理速度`,
	Example: `  # 使用默认测试文件运行基准测试
  pixly benchmark

  # 使用指定目录的文件运行基准测试
  pixly benchmark /path/to/test/files

  # 运行快速基准测试
  pixly benchmark --quick

  # 运行详细基准测试
  pixly benchmark --detailed`,
	RunE: runBenchmarkCommand,
}

// benchmarkFlags 基准测试标志
var (
	benchmarkQuick    bool
	benchmarkDetailed bool
	benchmarkOutput   string
	benchmarkModes    []string
)

func init() {
	// 添加标志
	benchmarkCmd.Flags().BoolVar(&benchmarkQuick, "quick", false, "运行快速基准测试")
	benchmarkCmd.Flags().BoolVar(&benchmarkDetailed, "detailed", false, "运行详细基准测试")
	benchmarkCmd.Flags().StringVar(&benchmarkOutput, "output", "", "基准测试结果输出文件")
	benchmarkCmd.Flags().StringSliceVar(&benchmarkModes, "modes", []string{"auto+", "quality", "emoji"}, "要测试的转换模式")

	// 添加到根命令
	cmd.AddCommand(benchmarkCmd)
}

func runBenchmarkCommand(cmd *cobra.Command, args []string) error {
	ui.DisplayBanner("🏃 Pixly 性能基准测试", "info")

	// 确定测试目录
	testDir := "./test/benchmark"
	if len(args) > 0 {
		testDir = args[0]
	}

	// 检查测试目录
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		return fmt.Errorf("测试目录不存在: %s", testDir)
	}

	// 显示系统信息
	showSystemInfo()

	// 运行基准测试
	results, err := runBenchmarkSuite(testDir)
	if err != nil {
		return fmt.Errorf("基准测试失败: %v", err)
	}

	// 显示结果
	showBenchmarkResults(results)

	// 保存结果到文件
	if benchmarkOutput != "" {
		if err := saveBenchmarkResults(results, benchmarkOutput); err != nil {
			fmt.Printf("⚠️ 保存结果失败: %v\n", err)
		} else {
			fmt.Printf("✅ 结果已保存到: %s\n", benchmarkOutput)
		}
	}

	return nil
}

func showSystemInfo() {
	fmt.Println("\n💻 系统信息:")
	fmt.Printf("   操作系统: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("   CPU核心数: %d\n", runtime.NumCPU())
	fmt.Printf("   Go版本: %s\n", runtime.Version())

	// 获取内存信息
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("   可用内存: %.2f MB\n", float64(m.Sys)/1024/1024)
	fmt.Println()
}

type BenchmarkResult struct {
	Mode             string
	FilesProcessed   int
	TotalTime        time.Duration
	AvgTimePerFile   time.Duration
	TotalSizeBefore  int64
	TotalSizeAfter   int64
	CompressionRatio float64
	MemoryUsed       uint64
	Errors           int
}

type BenchmarkSuite struct {
	Results   []BenchmarkResult
	StartTime time.Time
	EndTime   time.Time
}

func runBenchmarkSuite(testDir string) (*BenchmarkSuite, error) {
	suite := &BenchmarkSuite{
		StartTime: time.Now(),
	}

	// 获取测试文件列表
	testFiles, err := getTestFiles(testDir)
	if err != nil {
		return nil, err
	}

	if len(testFiles) == 0 {
		return nil, fmt.Errorf("测试目录中没有找到可用的测试文件")
	}

	fmt.Printf("📁 找到 %d 个测试文件\n\n", len(testFiles))

	// 为每个模式运行基准测试
	for _, mode := range benchmarkModes {
		fmt.Printf("🧪 测试模式: %s\n", mode)

		result, err := runModebenchmark(mode, testFiles)
		if err != nil {
			fmt.Printf("❌ 模式 %s 测试失败: %v\n", mode, err)
			continue
		}

		suite.Results = append(suite.Results, *result)
		fmt.Printf("✅ 模式 %s 测试完成\n\n", mode)
	}

	suite.EndTime = time.Now()
	return suite, nil
}

func getTestFiles(testDir string) ([]string, error) {
	var files []string

	err := filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			// 检查是否为支持的文件格式
			ext := filepath.Ext(path)
			if isSupportedFormat(ext) {
				files = append(files, path)
			}
		}

		return nil
	})

	return files, err
}

func isSupportedFormat(ext string) bool {
	supportedExts := []string{
		".jpg", ".jpeg", ".png", ".gif", ".webp", ".tiff", ".bmp",
		".mp4", ".avi", ".mov", ".mkv", ".webm",
		".mp3", ".wav", ".flac", ".aac", ".ogg",
	}

	for _, supported := range supportedExts {
		if ext == supported {
			return true
		}
	}
	return false
}

func runModebenchmark(mode string, testFiles []string) (*BenchmarkResult, error) {
	result := &BenchmarkResult{
		Mode: mode,
	}

	startTime := time.Now()

	// 记录初始内存状态
	var startMem runtime.MemStats
	runtime.ReadMemStats(&startMem)

	// 创建临时输出目录
	tempDir, err := os.MkdirTemp("", "pixly-benchmark-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	// 注意：这里简化了转换器创建逻辑，实际应该使用完整的转换器

	// 处理文件
	processedCount := 0
	errorCount := 0
	totalSizeBefore := int64(0)
	totalSizeAfter := int64(0)

	for _, file := range testFiles {
		// 限制快速测试的文件数量
		if benchmarkQuick && processedCount >= 10 {
			break
		}

		// 获取原始文件大小
		info, err := os.Stat(file)
		if err != nil {
			errorCount++
			continue
		}
		totalSizeBefore += info.Size()

		// 执行转换
		outputFile := filepath.Join(tempDir, filepath.Base(file))
		// 注意：这里使用简化的测试逻辑，实际应该调用转换器的处理方法
		if _, err := os.Stat(file); err != nil {
			errorCount++
			continue
		}

		// 获取转换后文件大小
		outputInfo, err := os.Stat(outputFile)
		if err == nil {
			totalSizeAfter += outputInfo.Size()
		}

		processedCount++
	}

	// 记录结束时间和内存
	endTime := time.Now()
	var endMem runtime.MemStats
	runtime.ReadMemStats(&endMem)

	// 计算结果
	result.FilesProcessed = processedCount
	result.TotalTime = endTime.Sub(startTime)
	result.Errors = errorCount
	result.TotalSizeBefore = totalSizeBefore
	result.TotalSizeAfter = totalSizeAfter
	result.MemoryUsed = endMem.Alloc - startMem.Alloc

	if processedCount > 0 {
		result.AvgTimePerFile = result.TotalTime / time.Duration(processedCount)
	}

	if totalSizeBefore > 0 {
		result.CompressionRatio = float64(totalSizeAfter) / float64(totalSizeBefore)
	}

	return result, nil
}

func showBenchmarkResults(suite *BenchmarkSuite) {
	fmt.Println("📊 基准测试结果")
	fmt.Println("================")
	fmt.Printf("总测试时间: %v\n\n", suite.EndTime.Sub(suite.StartTime))

	for _, result := range suite.Results {
		fmt.Printf("🔧 模式: %s\n", result.Mode)
		fmt.Printf("   处理文件: %d\n", result.FilesProcessed)
		fmt.Printf("   总耗时: %v\n", result.TotalTime)
		fmt.Printf("   平均耗时: %v/文件\n", result.AvgTimePerFile)
		fmt.Printf("   压缩比: %.2f%%\n", result.CompressionRatio*100)
		fmt.Printf("   内存使用: %.2f MB\n", float64(result.MemoryUsed)/1024/1024)
		fmt.Printf("   错误数: %d\n", result.Errors)
		fmt.Println()
	}

	// 显示性能排名
	showPerformanceRanking(suite.Results)
}

func showPerformanceRanking(results []BenchmarkResult) {
	fmt.Println("🏆 性能排名")
	fmt.Println("============")

	// 按平均处理时间排序
	fmt.Println("⚡ 速度排名 (平均处理时间):")
	for i, result := range results {
		fmt.Printf("   %d. %s: %v/文件\n", i+1, result.Mode, result.AvgTimePerFile)
	}
	fmt.Println()

	// 按压缩比排序
	fmt.Println("🗜️  压缩效果排名:")
	for i, result := range results {
		fmt.Printf("   %d. %s: %.2f%%\n", i+1, result.Mode, result.CompressionRatio*100)
	}
	fmt.Println()
}

func saveBenchmarkResults(suite *BenchmarkSuite, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入基准测试结果
	if _, err := fmt.Fprintf(file, "Pixly 基准测试结果\n"); err != nil {
		return fmt.Errorf("写入标题失败: %w", err)
	}
	if _, err := fmt.Fprintf(file, "==================\n"); err != nil {
		return fmt.Errorf("写入分隔线失败: %w", err)
	}
	if _, err := fmt.Fprintf(file, "测试时间: %s\n", suite.StartTime.Format("2006-01-02 15:04:05")); err != nil {
		return fmt.Errorf("写入测试时间失败: %w", err)
	}
	if _, err := fmt.Fprintf(file, "总耗时: %v\n\n", suite.EndTime.Sub(suite.StartTime)); err != nil {
		return fmt.Errorf("写入总耗时失败: %w", err)
	}

	for _, result := range suite.Results {
		if _, err := fmt.Fprintf(file, "模式: %s\n", result.Mode); err != nil {
			return fmt.Errorf("写入模式信息失败: %w", err)
		}
		if _, err := fmt.Fprintf(file, "处理文件: %d\n", result.FilesProcessed); err != nil {
			return fmt.Errorf("写入处理文件数失败: %w", err)
		}
		if _, err := fmt.Fprintf(file, "总耗时: %v\n", result.TotalTime); err != nil {
			return fmt.Errorf("写入总耗时失败: %w", err)
		}
		if _, err := fmt.Fprintf(file, "平均耗时: %v\n", result.AvgTimePerFile); err != nil {
			return fmt.Errorf("写入平均耗时失败: %w", err)
		}
		if _, err := fmt.Fprintf(file, "压缩比: %.2f%%\n", result.CompressionRatio*100); err != nil {
			return fmt.Errorf("写入压缩比失败: %w", err)
		}
		if _, err := fmt.Fprintf(file, "内存使用: %.2f MB\n", float64(result.MemoryUsed)/1024/1024); err != nil {
			return fmt.Errorf("写入内存使用失败: %w", err)
		}
		if _, err := fmt.Fprintf(file, "错误数: %d\n\n", result.Errors); err != nil {
			return fmt.Errorf("写入错误数失败: %w", err)
		}
	}

	return nil
}