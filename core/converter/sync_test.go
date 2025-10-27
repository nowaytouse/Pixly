package converter

import (
	"os"
	"pixly/config"
	"testing"

	"go.uber.org/zap"
)

// TestMainFunctionSync 测试主功能更新同步机制
func TestMainFunctionSync(t *testing.T) {
	// 创建临时目录用于测试
	tempDir := t.TempDir()

	// 创建测试配置
	cfg := &config.Config{
		Conversion: config.ConversionConfig{
			DefaultMode: "auto+",
			Quality: config.QualityConfig{
				JPEGQuality: 85,
				WebPQuality: 85,
				AVIFQuality: 75,
				JXLQuality:  85,
			},
		},
		Concurrency: config.ConcurrencyConfig{
			ConversionWorkers: 4,
			ScanWorkers:       2,
			MemoryLimit:       4096,
			AutoAdjust:        true,
		},
		Tools: config.ToolsConfig{
			FFmpegPath:  "ffmpeg",
			FFprobePath: "ffprobe",
			CjxlPath:    "cjxl",
			AvifencPath: "avifenc",
		},
		Security: config.SecurityConfig{
			AllowedDirectories:   []string{tempDir},
			ForbiddenDirectories: []string{},
			MaxFileSize:          10240,
			CheckDiskSpace:       true,
		},
	}

	// 创建测试logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("创建日志记录器失败: %v", err)
	}

	// 创建测试文件
	testFile, err := GlobalPathUtils.JoinPath(tempDir, "test.jpg")
	if err != nil {
		t.Fatalf("构建测试文件路径失败: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("fake jpeg content"), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	// 测试不同模式下的功能同步
	modes := []string{"auto+", "quality", "emoji"}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			// 创建转换器
			converter, err := NewConverter(cfg, logger, mode)
			if err != nil {
				t.Fatalf("Failed to create converter for mode %s: %v", mode, err)
			}
			defer func() {
				if err := converter.Close(); err != nil {
					t.Logf("关闭转换器失败: %v", err)
				}
			}()

			// 验证所有核心功能是否正确初始化和同步
			validateCoreFunctionality(t, converter, mode)
		})
	}
}

// validateCoreFunctionality 验证核心功能是否正确同步
func validateCoreFunctionality(t *testing.T, converter *Converter, mode string) {
	// 1. 验证转换器是否正确初始化
	if converter == nil {
		t.Errorf("Converter not initialized for mode %s", mode)
		return
	}

	// 2. 验证看门狗是否启动
	if converter.watchdog == nil {
		t.Errorf("Watchdog not initialized for mode %s", mode)
	}

	// 3. 验证原子操作是否初始化
	if converter.atomicOps == nil {
		t.Errorf("Atomic operations not initialized for mode %s", mode)
	}

	// 4. 验证元数据管理器是否初始化
	if converter.metadataManager == nil {
		t.Errorf("Metadata manager not initialized for mode %s", mode)
	}

	// 5. 验证工具管理器是否初始化
	if converter.toolManager == nil {
		t.Errorf("Tool manager not initialized for mode %s", mode)
	}

	// 6. 验证文件类型检测器是否初始化
	if converter.fileTypeDetector == nil {
		t.Errorf("File type detector not initialized for mode %s", mode)
	}

	// 7. 验证转换策略是否正确创建
	if converter.strategy == nil {
		t.Errorf("Conversion strategy not initialized for mode %s", mode)
	}

	// 8. 验证并发池是否初始化
	if converter.advancedPool == nil {
		t.Errorf("Concurrency pool not initialized for mode %s", mode)
	}

	// 9. 验证进度条管理器已移除（简化架构）
	// ProgressManager已被删除，使用UnifiedProgress统一管理

	// 10. 验证策略名称是否正确
	strategyName := converter.strategy.GetName()
	expectedName := ""

	switch mode {
	case "auto+":
		expectedName = "auto+ (智能决策)"
	case "quality":
		expectedName = "quality (无损优先)"
	case "emoji":
		expectedName = "emoji (极限压缩)"
	}

	if strategyName != expectedName {
		t.Errorf("Expected strategy name '%s' for mode '%s', got '%s'", expectedName, mode, strategyName)
	}

	t.Logf("Mode %s core functionality validation completed", mode)
}

// TestFeatureIntegration 测试功能集成同步
func TestFeatureIntegration(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		Concurrency: config.ConcurrencyConfig{
			ConversionWorkers: 4,
			ScanWorkers:       2,
			MemoryLimit:       4096,
			AutoAdjust:        true,
		},
		Conversion: config.ConversionConfig{
			Quality: config.QualityConfig{
				JPEGQuality: 85,
				WebPQuality: 85,
				AVIFQuality: 75,
				JXLQuality:  85,
			},
			QualityThresholds: config.QualityThresholdsConfig{
				Enabled: true,
				Photo: config.JPEGThresholds{
					HighQuality:   3.0,
					MediumQuality: 1.0,
					LowQuality:    0.1,
				},
				Image: config.PNGThresholds{
					OriginalQuality: 10.0,
					HighQuality:     2.0,
					MediumQuality:   0.5,
				},
			},
		},
		Tools: config.ToolsConfig{
			FFmpegPath:  "ffmpeg",
			FFprobePath: "ffprobe",
			CjxlPath:    "cjxl",
			AvifencPath: "avifenc",
		},
	}

	// 创建测试logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("创建日志记录器失败: %v", err)
	}

	// 创建转换器
	converter, err := NewConverter(cfg, logger, "auto+")
	if err != nil {
		t.Fatalf("Failed to create converter: %v", err)
	}
	defer func() {
		if err := converter.Close(); err != nil {
			t.Logf("关闭转换器失败: %v", err)
		}
	}()

	// 验证功能集成是否正确同步
	// 1. 验证智能判断功能是否与转换器正确集成
	autoPlusStrategy, ok := converter.strategy.(*AutoPlusStrategy)
	if !ok {
		t.Errorf("Expected AutoPlusStrategy, got %T", converter.strategy)
	}

	// 2. 验证品质分析功能是否正确工作
	testFile := &MediaFile{
		Extension: ".jpg",
		Size:      5 * 1024 * 1024, // 5MB
	}

	quality := autoPlusStrategy.analyzeImageQuality(testFile)
	if quality != "原画" {
		t.Errorf("Expected '原画' for 5MB JPEG, got '%s'", quality)
	}

	// 3. 验证配置是否正确传递到各个组件
	if converter.config.Conversion.Quality.JPEGQuality != 85 {
		t.Errorf("JPEG quality not synchronized correctly")
	}

	t.Logf("Feature integration test completed")
}

// TestConfigurationSync 测试配置同步机制
func TestConfigurationSync(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		Conversion: config.ConversionConfig{
			DefaultMode: "quality",
			Quality: config.QualityConfig{
				JPEGQuality: 90,
				WebPQuality: 85,
				AVIFQuality: 75,
				JXLQuality:  95,
			},
		},
		Concurrency: config.ConcurrencyConfig{
			ScanWorkers:       8,
			ConversionWorkers: 6,
			MemoryLimit:       2048,
			AutoAdjust:        true,
		},
		Output: config.OutputConfig{
			KeepOriginal:      true,
			DirectoryTemplate: "/output",
			FilenameTemplate:  "converted_{filename}",
			GenerateReport:    true,
		},
		Tools: config.ToolsConfig{
			FFmpegPath:  "/usr/local/bin/ffmpeg",
			FFprobePath: "/usr/local/bin/ffprobe",
			CjxlPath:    "/usr/local/bin/cjxl",
			AvifencPath: "/usr/local/bin/avifenc",
		},
		Security: config.SecurityConfig{
			AllowedDirectories:   []string{"/tmp", "/var/tmp"},
			ForbiddenDirectories: []string{"/etc", "/usr"},
			MaxFileSize:          5120,
			CheckDiskSpace:       false,
		},
	}

	// 创建测试logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("创建日志记录器失败: %v", err)
	}

	// 创建转换器
	converter, err := NewConverter(cfg, logger, "quality")
	if err != nil {
		t.Fatalf("Failed to create converter: %v", err)
	}
	defer func() {
		if err := converter.Close(); err != nil {
			t.Logf("关闭转换器失败: %v", err)
		}
	}()

	// 验证配置是否正确同步到所有组件
	// 1. 验证转换器配置
	if converter.config.Conversion.DefaultMode != "quality" {
		t.Errorf("Default mode not synchronized correctly")
	}

	if converter.config.Conversion.Quality.JXLQuality != 95 {
		t.Errorf("JXL quality not synchronized correctly")
	}

	// 2. 验证并发配置
	if converter.config.Concurrency.ScanWorkers != 8 {
		t.Errorf("Scan workers not synchronized correctly")
	}

	// 3. 验证输出配置
	if converter.config.Output.KeepOriginal != true {
		t.Errorf("Keep original not synchronized correctly")
	}

	// 4. 验证工具配置
	if converter.config.Tools.FFmpegPath != "/usr/local/bin/ffmpeg" {
		t.Errorf("FFmpeg path not synchronized correctly")
	}

	// 5. 验证安全配置
	if len(converter.config.Security.AllowedDirectories) != 2 {
		t.Errorf("Allowed directories not synchronized correctly")
	}

	// 6. 验证配置是否传递到工具管理器
	if converter.toolManager.config.Tools.FFmpegPath != "/usr/local/bin/ffmpeg" {
		t.Errorf("Tool manager config not synchronized correctly")
	}

	// 7. 验证配置是否传递到文件类型检测器
	if converter.fileTypeDetector.config.Tools.FFprobePath != "/usr/local/bin/ffprobe" {
		t.Errorf("File type detector config not synchronized correctly")
	}

	t.Logf("Configuration sync test completed")
}
