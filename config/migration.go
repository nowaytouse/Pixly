package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ConfigVersion 配置文件版本
type ConfigVersion string

const (
	Version1_0 ConfigVersion = "1.0" // 初始版本
	Version1_1 ConfigVersion = "1.1" // 添加问题文件处理配置
	Version1_2 ConfigVersion = "1.2" // 配置版本更新
)

// ConfigMigrator 配置迁移器
type ConfigMigrator struct {
	logger *zap.Logger
}

// NewConfigMigrator 创建配置迁移器
func NewConfigMigrator(logger *zap.Logger) *ConfigMigrator {
	// 修复：如果logger为nil，创建一个默认的logger
	if logger == nil {
		logger = createDefaultLogger()
	}

	return &ConfigMigrator{
		logger: logger,
	}
}

// createDefaultLogger 创建默认logger
func createDefaultLogger() *zap.Logger {
	// 创建一个简单的logger，避免重复初始化
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	config.Encoding = "console"
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	logger, _ := config.Build()
	return logger
}

// GetCurrentVersion 获取当前配置文件版本
func (cm *ConfigMigrator) GetCurrentVersion(v *viper.Viper) ConfigVersion {
	version := v.GetString("version")
	if version == "" {
		// 如果没有版本号，假设是初始版本
		return Version1_0
	}
	return ConfigVersion(version)
}

// MigrateConfig 迁移配置文件到最新版本
func (cm *ConfigMigrator) MigrateConfig(configFile string) error {
	// 注意：我们不再在这里记录日志，因为调用者会记录
	// 这样可以避免重复的日志输出

	if configFile == "" {
		// 如果没有指定配置文件，查找默认配置文件
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("无法获取用户主目录: %w", err)
		}

		configFile = filepath.Join(home, ".pixly.yaml")
	}

	// 检查配置文件是否存在
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// 配置文件不存在，创建新配置文件
		// 注意：不记录日志以避免重复
		return cm.createDefaultConfig(configFile)
	}

	// 读取现有配置文件
	v := viper.New()
	v.SetConfigFile(configFile)

	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 获取当前版本
	currentVersion := cm.GetCurrentVersion(v)
	// 注意：不记录日志以避免重复

	// 根据当前版本执行迁移
	switch currentVersion {
	case Version1_0:
		// 迁移到版本1.1
		if err := cm.migrateToVersion1_1(v); err != nil {
			return fmt.Errorf("迁移到版本1.1失败: %w", err)
		}
		fallthrough // 继续迁移到下一个版本

	case Version1_1:
		// 迁移到版本1.2
		if err := cm.migrateToVersion1_2(v); err != nil {
			return fmt.Errorf("迁移到版本1.2失败: %w", err)
		}
		fallthrough // 继续迁移到下一个版本

	case Version1_2:
		// 已经是最新版本，无需迁移
		// 注意：不记录日志以避免重复
		return nil

	default:
		// 未知版本
		// 注意：不记录日志以避免重复
		return nil
	}
}

// migrateToVersion1_1 迁移到版本1.1
func (cm *ConfigMigrator) migrateToVersion1_1(v *viper.Viper) error {
	// 修复：只有当logger不为nil时才记录日志
	if cm.logger != nil {
		// 迁移到配置版本1.1
	}

	// 添加问题文件处理配置
	v.Set("problem_file_handling.corrupted_file_strategy", "ignore")
	v.Set("problem_file_handling.codec_incompatibility_strategy", "ignore")
	v.Set("problem_file_handling.container_incompatibility_strategy", "ignore")

	// 使用统一的问题文件处理配置 - "好品味"：消除重复设置
	setProblemFileHandlingDefaults(v)

	// 更新版本号
	v.Set("version", string(Version1_1))

	// 保存配置文件
	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// migrateToVersion1_2 迁移到版本1.2
func (cm *ConfigMigrator) migrateToVersion1_2(v *viper.Viper) error {
	// 修复：只有当logger不为nil时才记录日志
	if cm.logger != nil {
		// 迁移到配置版本1.2
	}

	// 更新版本号
	v.Set("version", string(Version1_2))

	// 保存配置文件
	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// createDefaultConfig 创建默认配置文件
func (cm *ConfigMigrator) createDefaultConfig(configFile string) error {
	// 创建默认配置
	v := viper.New()

	// 设置默认值
	v.SetConfigFile(configFile)

	// 设置所有默认配置值
	v.SetDefault("version", string(Version1_2))
	v.SetDefault("conversion.default_mode", "auto+")
	v.SetDefault("conversion.quality.jpeg_quality", 85)
	v.SetDefault("conversion.quality.webp_quality", 85)
	v.SetDefault("conversion.quality.avif_quality", 75)
	v.SetDefault("conversion.quality.jxl_quality", 85)
	v.SetDefault("conversion.quality.video_crf", 23)
	v.SetDefault("conversion.skip_extensions", []string{".db", ".log", ".tmp"})

	// 使用统一的质量阈值默认值 - "好品味"：消除重复配置
	setQualityThresholdsDefaults(v)

	// 并发设置默认值
	v.SetDefault("concurrency.scan_workers", 4)
	v.SetDefault("concurrency.conversion_workers", 2)
	v.SetDefault("concurrency.memory_limit", 8192) // 8GB
	v.SetDefault("concurrency.auto_adjust", true)

	// 输出设置默认值
	v.SetDefault("output.keep_original", false)
	v.SetDefault("output.directory_template", "")
	v.SetDefault("output.filename_template", "")
	v.SetDefault("output.generate_report", true)

	// 外部工具默认路径
	v.SetDefault("tools.ffmpeg_path", "ffmpeg")
	v.SetDefault("tools.ffprobe_path", "ffprobe")
	v.SetDefault("tools.cjxl_path", "cjxl")
	v.SetDefault("tools.avifenc_path", "avifenc")
	v.SetDefault("tools.exiftool_path", "exiftool")

	// 安全设置默认值
	v.SetDefault("security.forbidden_directories", []string{
		"/System", "/Library", "/usr", "/bin", "/sbin", "/etc",
		"/var", "/tmp", "/private", "/Applications",
	})
	v.SetDefault("security.max_file_size", 10240) // 10GB
	v.SetDefault("security.check_disk_space", true)

	// 主题设置默认值
	v.SetDefault("theme.mode", "auto")
	v.SetDefault("theme.enable_startup_animation", true)

	// 使用统一的问题文件处理默认值 - "好品味"：消除重复配置
	v.SetDefault("problem_file_handling.corrupted_file_strategy", "ignore")
	v.SetDefault("problem_file_handling.codec_incompatibility_strategy", "ignore")
	v.SetDefault("problem_file_handling.container_incompatibility_strategy", "ignore")

	setProblemFileHandlingDefaults(v)

	// 写入配置文件
	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	// 修复：只有当logger不为nil时才记录日志
	if cm.logger != nil {
		// 创建默认配置文件成功
	}
	return nil
}
