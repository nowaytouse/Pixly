package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Config 应用配置结构
type Config struct {
	// 转换设置
	Conversion ConversionConfig `mapstructure:"conversion"`

	// 并发设置
	Concurrency ConcurrencyConfig `mapstructure:"concurrency"`

	// 输出设置
	Output OutputConfig `mapstructure:"output"`

	// 外部工具路径
	Tools ToolsConfig `mapstructure:"tools"`

	// 安全设置
	Security SecurityConfig `mapstructure:"security"`

	// 主题设置
	Theme ThemeConfig `mapstructure:"theme"`

	// 问题文件处理设置
	ProblemFileHandling ProblemFileHandlingConfig `mapstructure:"problem_file_handling"`

	// 日志设置
	Logging LoggingConfig `mapstructure:"logging"`

	// 性能监控设置
	Performance PerformanceConfig `mapstructure:"performance"`

	// 高级设置
	Advanced AdvancedConfig `mapstructure:"advanced"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	// 日志级别 (debug, info, warn, error)
	Level string `mapstructure:"level"`

	// 是否启用文件日志
	EnableFile bool `mapstructure:"enable_file"`

	// 是否启用控制台日志
	EnableConsole bool `mapstructure:"enable_console"`

	// 日志文件最大大小 (MB)
	MaxSize int `mapstructure:"max_size"`

	// 保留的日志文件数量
	MaxBackups int `mapstructure:"max_backups"`

	// 日志文件保留天数
	MaxAge int `mapstructure:"max_age"`

	// 是否压缩旧日志文件
	Compress bool `mapstructure:"compress"`

	// 日志目录
	LogDir string `mapstructure:"log_dir"`

	// 是否启用性能日志
	EnablePerformanceLog bool `mapstructure:"enable_performance_log"`

	// 是否启用错误追踪
	EnableErrorTracking bool `mapstructure:"enable_error_tracking"`
}

// PerformanceConfig 性能监控配置
type PerformanceConfig struct {
	// 是否启用性能监控
	Enabled bool `mapstructure:"enabled"`

	// 监控间隔 (秒)
	MonitorInterval int `mapstructure:"monitor_interval"`

	// 内存使用阈值 (%)
	MemoryThreshold float64 `mapstructure:"memory_threshold"`

	// CPU使用阈值 (%)
	CPUThreshold float64 `mapstructure:"cpu_threshold"`

	// 磁盘使用阈值 (%)
	DiskThreshold float64 `mapstructure:"disk_threshold"`

	// 是否启用自动优化
	AutoOptimize bool `mapstructure:"auto_optimize"`

	// 性能报告生成间隔 (分钟)
	ReportInterval int `mapstructure:"report_interval"`
}

// AdvancedConfig 高级配置
type AdvancedConfig struct {
	// 是否启用配置热重载
	EnableHotReload bool `mapstructure:"enable_hot_reload"`

	// 配置验证级别 (strict, normal, loose)
	ValidationLevel string `mapstructure:"validation_level"`

	// 是否启用实验性功能
	EnableExperimentalFeatures bool `mapstructure:"enable_experimental_features"`

	// 缓存设置
	Cache CacheConfig `mapstructure:"cache"`

	// 网络设置
	Network NetworkConfig `mapstructure:"network"`

	// UI设置
	UI UIConfig `mapstructure:"ui"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// 是否启用缓存
	Enabled bool `mapstructure:"enabled"`

	// 缓存目录
	CacheDir string `mapstructure:"cache_dir"`

	// 缓存大小限制 (MB)
	MaxSize int `mapstructure:"max_size"`

	// 缓存过期时间 (小时)
	TTL int `mapstructure:"ttl"`

	// 是否启用压缩
	Compress bool `mapstructure:"compress"`
}

// UIConfig UI配置
type UIConfig struct {
	// 是否启用静默模式（不显示进度条）
	SilentMode bool `mapstructure:"silent_mode"`

	// 是否启用安静模式（减少输出信息）
	QuietMode bool `mapstructure:"quiet_mode"`

	// 是否禁用所有UI输出
	DisableUI bool `mapstructure:"disable_ui"`
}

// NetworkConfig 网络配置
type NetworkConfig struct {
	// 网络超时 (秒)
	Timeout int `mapstructure:"timeout"`

	// 重试次数
	RetryCount int `mapstructure:"retry_count"`

	// 代理设置
	Proxy string `mapstructure:"proxy"`

	// 用户代理
	UserAgent string `mapstructure:"user_agent"`
}

// ConfigManager 配置管理器
type ConfigManager struct {
	config     *Config
	viper      *viper.Viper
	logger     *zap.Logger
	mutex      sync.RWMutex
	watchers   []ConfigWatcher
	ctx        context.Context
	cancel     context.CancelFunc
	configFile string
}

// ConfigWatcher 配置变更监听器
type ConfigWatcher interface {
	OnConfigChange(oldConfig, newConfig *Config) error
}

// ValidationError 配置验证错误
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	var builder strings.Builder
	builder.WriteString("配置验证失败 [")
	builder.WriteString(e.Field)
	builder.WriteString("]: ")
	builder.WriteString(e.Message)
	builder.WriteString(" (当前值: ")
	builder.WriteString(fmt.Sprint(e.Value))
	builder.WriteString(")")
	return builder.String()
}

// NewConfigManager 创建配置管理器
func NewConfigManager(configFile string, logger *zap.Logger) (*ConfigManager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cm := &ConfigManager{
		viper:      viper.New(),
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		configFile: configFile,
	}

	// 初始化配置
	if err := cm.loadConfig(); err != nil {
		cancel()
		return nil, err
	}

	return cm, nil
}

// loadConfig 加载配置
func (cm *ConfigManager) loadConfig() error {
	// 设置默认值
	setDefaultsAdvanced(cm.viper)

	// 设置配置文件
	if cm.configFile != "" {
		cm.viper.SetConfigFile(cm.configFile)
	} else {
		// 查找配置文件
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		cm.viper.AddConfigPath(home)
		cm.viper.AddConfigPath(".")
		cm.viper.SetConfigName(".pixly")
		cm.viper.SetConfigType("yaml")
	}

	// 读取环境变量
	cm.viper.SetEnvPrefix("PIXLY")
	cm.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	cm.viper.AutomaticEnv()

	// 读取配置文件
	if err := cm.viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return err
		}
		// 配置文件不存在，使用默认配置
		// 配置文件不存在，使用默认配置
	}

	// 解析配置
	var config Config
	if err := cm.viper.Unmarshal(&config); err != nil {
		return err
	}

	// 验证配置
	if err := cm.validateConfigAdvanced(&config); err != nil {
		return err
	}

	cm.mutex.Lock()
	cm.config = &config
	cm.mutex.Unlock()

	return nil
}

// GetConfig 获取当前配置
func (cm *ConfigManager) GetConfig() *Config {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.config
}

// UpdateConfig 更新配置
func (cm *ConfigManager) UpdateConfig(key string, value interface{}) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	oldConfig := *cm.config

	cm.viper.Set(key, value)

	// 重新解析配置
	var newConfig Config
	if err := cm.viper.Unmarshal(&newConfig); err != nil {
		return err
	}

	// 验证新配置
	if err := cm.validateConfigAdvanced(&newConfig); err != nil {
		return err
	}

	cm.config = &newConfig

	// 通知监听器
	for _, watcher := range cm.watchers {
		if err := watcher.OnConfigChange(&oldConfig, &newConfig); err != nil {
			cm.logger.Error("配置变更通知失败", zap.Error(err))
		}
	}

	return nil
}

// SaveConfig 保存配置到文件
func (cm *ConfigManager) SaveConfig() error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	return cm.viper.WriteConfig()
}

// AddWatcher 添加配置监听器
func (cm *ConfigManager) AddWatcher(watcher ConfigWatcher) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.watchers = append(cm.watchers, watcher)
}

// EnableHotReload 启用配置热重载
func (cm *ConfigManager) EnableHotReload() error {
	if !cm.config.Advanced.EnableHotReload {
		return nil
	}

	cm.viper.OnConfigChange(func(e fsnotify.Event) {
		// 检测到配置文件变更

		if err := cm.loadConfig(); err != nil {
			cm.logger.Error("重新加载配置失败", zap.Error(err))
			return
		}

		// 配置已重新加载
	})

	cm.viper.WatchConfig()
	return nil
}

// Close 关闭配置管理器
func (cm *ConfigManager) Close() error {
	cm.cancel()
	return nil
}

// setDefaultsAdvanced 设置高级默认配置
func setDefaultsAdvanced(v *viper.Viper) {
	// 设置原有默认值
	setDefaults(v)

	// 日志配置默认值
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.enable_file", true)
	v.SetDefault("logging.enable_console", true)
	v.SetDefault("logging.max_size", 100)
	v.SetDefault("logging.max_backups", 3)
	v.SetDefault("logging.max_age", 28)
	v.SetDefault("logging.compress", true)
	v.SetDefault("logging.log_dir", "")
	v.SetDefault("logging.enable_performance_log", false)
	v.SetDefault("logging.enable_error_tracking", true)

	// 性能监控配置默认值
	v.SetDefault("performance.enabled", true)
	v.SetDefault("performance.monitor_interval", 5)
	v.SetDefault("performance.memory_threshold", 80.0)
	v.SetDefault("performance.cpu_threshold", 80.0)
	v.SetDefault("performance.disk_threshold", 90.0)
	v.SetDefault("performance.auto_optimize", true)
	v.SetDefault("performance.report_interval", 30)

	// 高级配置默认值
	v.SetDefault("advanced.enable_hot_reload", false)
	v.SetDefault("advanced.validation_level", "normal")
	v.SetDefault("advanced.enable_experimental_features", false)

	// 缓存配置默认值
	v.SetDefault("advanced.cache.enabled", true)
	v.SetDefault("advanced.cache.cache_dir", "")
	v.SetDefault("advanced.cache.max_size", 1024)
	v.SetDefault("advanced.cache.ttl", 24)
	v.SetDefault("advanced.cache.compress", true)

	// UI配置默认值
	v.SetDefault("advanced.ui.silent_mode", false)
	v.SetDefault("advanced.ui.quiet_mode", false)
	v.SetDefault("advanced.ui.disable_ui", false)

	// 网络配置默认值
	v.SetDefault("advanced.network.timeout", 30)
	v.SetDefault("advanced.network.retry_count", 3)
	v.SetDefault("advanced.network.proxy", "")
	v.SetDefault("advanced.network.user_agent", "Pixly/1.0")
}

// validateConfigAdvanced 验证高级配置
func (cm *ConfigManager) validateConfigAdvanced(config *Config) error {
	// 验证日志配置
	if err := cm.validateLoggingConfig(&config.Logging); err != nil {
		return err
	}

	// 验证性能配置
	if err := cm.validatePerformanceConfig(&config.Performance); err != nil {
		return err
	}

	// 验证高级配置
	if err := cm.validateAdvancedConfig(&config.Advanced); err != nil {
		return err
	}

	// 调用原有验证
	return validateConfig(config)
}

// validateLoggingConfig 验证日志配置
func (cm *ConfigManager) validateLoggingConfig(config *LoggingConfig) error {
	validLevels := []string{"debug", "info", "warn", "error"}
	validLevel := false
	for _, level := range validLevels {
		if config.Level == level {
			validLevel = true
			break
		}
	}
	if !validLevel {
		return &ValidationError{
			Field:   "logging.level",
			Value:   config.Level,
			Message: "日志级别必须是 debug, info, warn, error 之一",
		}
	}

	if config.MaxSize <= 0 {
		return &ValidationError{
			Field:   "logging.max_size",
			Value:   config.MaxSize,
			Message: "日志文件最大大小必须大于0",
		}
	}

	if config.MaxBackups < 0 {
		return &ValidationError{
			Field:   "logging.max_backups",
			Value:   config.MaxBackups,
			Message: "日志备份数量不能为负数",
		}
	}

	return nil
}

// validatePerformanceConfig 验证性能配置
func (cm *ConfigManager) validatePerformanceConfig(config *PerformanceConfig) error {
	if config.MonitorInterval <= 0 {
		return &ValidationError{
			Field:   "performance.monitor_interval",
			Value:   config.MonitorInterval,
			Message: "监控间隔必须大于0",
		}
	}

	if config.MemoryThreshold < 0 || config.MemoryThreshold > 100 {
		return &ValidationError{
			Field:   "performance.memory_threshold",
			Value:   config.MemoryThreshold,
			Message: "内存阈值必须在0-100之间",
		}
	}

	if config.CPUThreshold < 0 || config.CPUThreshold > 100 {
		return &ValidationError{
			Field:   "performance.cpu_threshold",
			Value:   config.CPUThreshold,
			Message: "CPU阈值必须在0-100之间",
		}
	}

	if config.DiskThreshold < 0 || config.DiskThreshold > 100 {
		return &ValidationError{
			Field:   "performance.disk_threshold",
			Value:   config.DiskThreshold,
			Message: "磁盘阈值必须在0-100之间",
		}
	}

	return nil
}

// validateAdvancedConfig 验证高级配置
func (cm *ConfigManager) validateAdvancedConfig(config *AdvancedConfig) error {
	validLevels := []string{"strict", "normal", "loose"}
	validLevel := false
	for _, level := range validLevels {
		if config.ValidationLevel == level {
			validLevel = true
			break
		}
	}
	if !validLevel {
		return &ValidationError{
			Field:   "advanced.validation_level",
			Value:   config.ValidationLevel,
			Message: "验证级别必须是 strict, normal, loose 之一",
		}
	}

	// 验证缓存配置
	if config.Cache.MaxSize <= 0 {
		return &ValidationError{
			Field:   "advanced.cache.max_size",
			Value:   config.Cache.MaxSize,
			Message: "缓存最大大小必须大于0",
		}
	}

	if config.Cache.TTL <= 0 {
		return &ValidationError{
			Field:   "advanced.cache.ttl",
			Value:   config.Cache.TTL,
			Message: "缓存过期时间必须大于0",
		}
	}

	// 验证网络配置
	if config.Network.Timeout <= 0 {
		return &ValidationError{
			Field:   "advanced.network.timeout",
			Value:   config.Network.Timeout,
			Message: "网络超时时间必须大于0",
		}
	}

	if config.Network.RetryCount < 0 {
		return &ValidationError{
			Field:   "advanced.network.retry_count",
			Value:   config.Network.RetryCount,
			Message: "重试次数不能为负数",
		}
	}

	return nil
}

// ConversionConfig 转换配置
type ConversionConfig struct {
	// 默认转换模式
	DefaultMode string `mapstructure:"default_mode"`

	// 质量设置
	Quality QualityConfig `mapstructure:"quality"`

	// 智能判断设置
	QualityThresholds QualityThresholdsConfig `mapstructure:"quality_thresholds"`

	// 格式映射
	FormatMapping map[string]string `mapstructure:"format_mapping"`

	// 跳过的文件扩展名（黑名单模式，已废弃）
	SkipExtensions []string `mapstructure:"skip_extensions"`

	// 支持的文件扩展名（白名单模式）
	SupportedExtensions []string `mapstructure:"supported_extensions"`

	// 图片文件扩展名
	ImageExtensions []string `mapstructure:"image_extensions"`

	// 视频文件扩展名
	VideoExtensions []string `mapstructure:"video_extensions"`
}

// QualityConfig 质量配置
type QualityConfig struct {
	// JPEG质量 (1-100)
	JPEGQuality int `mapstructure:"jpeg_quality"`

	// WebP质量 (1-100)
	WebPQuality int `mapstructure:"webp_quality"`

	// AVIF质量 (1-100)
	AVIFQuality int `mapstructure:"avif_quality"`

	// JXL质量 (1-100)
	JXLQuality int `mapstructure:"jxl_quality"`

	// 视频CRF值 (0-51)
	VideoCRF int `mapstructure:"video_crf"`
}

// QualityThresholdsConfig 智能判断设置（用户友好名称）
type QualityThresholdsConfig struct {
	// 是否启用智能判断
	Enabled bool `mapstructure:"enabled"`

	// JPEG照片设置
	Photo JPEGThresholds `mapstructure:"photo"`

	// PNG图片设置
	Image PNGThresholds `mapstructure:"image"`

	// GIF动图设置
	Animation GIFThresholds `mapstructure:"animation"`

	// 视频设置
	Video VideoThresholds `mapstructure:"video"`
}

// JPEGThresholds JPEG照片质量判断阈值（MB）
type JPEGThresholds struct {
	// 高品质阈值（超过此值为高品质）
	HighQuality float64 `mapstructure:"high_quality"`
	// 中等品质阈值（超过此值为中等品质）
	MediumQuality float64 `mapstructure:"medium_quality"`
	// 低品质阈值（超过此值为低品质，否则为极低）
	LowQuality float64 `mapstructure:"low_quality"`
}

// PNGThresholds PNG图片质量判断阈值（MB）
type PNGThresholds struct {
	// 原画品质阈值（超过此值为原画品质）
	OriginalQuality float64 `mapstructure:"original_quality"`
	// 高品质阈值
	HighQuality float64 `mapstructure:"high_quality"`
	// 中等品质阈值
	MediumQuality float64 `mapstructure:"medium_quality"`
}

// GIFThresholds GIF动图质量判断阈值（MB）
type GIFThresholds struct {
	// 中等品质阈值（超过此值为中等品质）
	MediumQuality float64 `mapstructure:"medium_quality"`
	// 低品质阈值（超过此值为低品质，否则为极低）
	LowQuality float64 `mapstructure:"low_quality"`
}

// VideoThresholds 视频质量判断阈值（MB）
type VideoThresholds struct {
	// 高品质阈值
	HighQuality float64 `mapstructure:"high_quality"`
	// 中等品质阈值
	MediumQuality float64 `mapstructure:"medium_quality"`
	// 低品质阈值
	LowQuality float64 `mapstructure:"low_quality"`
}

// ConcurrencyConfig 并发配置
type ConcurrencyConfig struct {
	// 扫描并发数
	ScanWorkers int `mapstructure:"scan_workers"`

	// 转换并发数
	ConversionWorkers int `mapstructure:"conversion_workers"`

	// 内存限制 (MB)
	MemoryLimit int `mapstructure:"memory_limit"`

	// 自动调整并发数
	AutoAdjust bool `mapstructure:"auto_adjust"`
}

// OutputConfig 输出配置
type OutputConfig struct {
	// 保留原文件
	KeepOriginal bool `mapstructure:"keep_original"`

	// 输出目录模板
	DirectoryTemplate string `mapstructure:"directory_template"`

	// 文件名模板
	FilenameTemplate string `mapstructure:"filename_template"`

	// 生成报告
	GenerateReport bool `mapstructure:"generate_report"`
}

// ToolsConfig 外部工具配置
type ToolsConfig struct {
	// FFmpeg路径
	FFmpegPath string `mapstructure:"ffmpeg_path"`

	// FFprobe路径
	FFprobePath string `mapstructure:"ffprobe_path"`

	// cjxl路径
	CjxlPath string `mapstructure:"cjxl_path"`

	// avifenc路径
	AvifencPath string `mapstructure:"avifenc_path"`

	// exiftool路径
	ExiftoolPath string `mapstructure:"exiftool_path"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	// 允许的目录白名单
	AllowedDirectories []string `mapstructure:"allowed_directories"`

	// 禁止的目录黑名单
	ForbiddenDirectories []string `mapstructure:"forbidden_directories"`

	// 最大文件大小 (MB)
	MaxFileSize int `mapstructure:"max_file_size"`

	// 检查磁盘空间
	CheckDiskSpace bool `mapstructure:"check_disk_space"`
}

// ThemeConfig 主题配置
type ThemeConfig struct {
	// 主题模式：light, dark, auto
	Mode string `mapstructure:"mode"`
	// 启用启动动画
	EnableStartupAnimation bool `mapstructure:"enable_startup_animation"`
	// ASCII艺术字显示模式：compact, simplified, enhanced
	AsciiArtMode string `mapstructure:"ascii_art_mode"`
	// 启用ASCII艺术字颜色
	EnableAsciiArtColors bool `mapstructure:"enable_ascii_art_colors"`
}

// ProblemFileHandlingConfig 问题文件处理配置
type ProblemFileHandlingConfig struct {
	// 损坏文件自动处理策略 (ignore, delete, move_to_trash)
	CorruptedFileStrategy string `mapstructure:"corrupted_file_strategy"`

	// 编解码器不兼容文件自动处理策略 (ignore, force_process, move_to_trash)
	CodecIncompatibilityStrategy string `mapstructure:"codec_incompatibility_strategy"`

	// 容器不兼容文件自动处理策略 (ignore, force_process, move_to_trash)
	ContainerIncompatibilityStrategy string `mapstructure:"container_incompatibility_strategy"`
}

// NewConfig 创建新的配置实例
func NewConfig(configFile string, logger *zap.Logger) (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// 设置配置文件
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		// 查找配置文件
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		v.AddConfigPath(home)
		v.AddConfigPath(".")
		v.SetConfigName(".pixly")
		v.SetConfigType("yaml")
	}

	// 读取环境变量
	v.SetEnvPrefix("PIXLY")
	v.AutomaticEnv()

	// 创建配置迁移器并执行迁移
	// 修复：不要在这里传递logger，避免重复初始化日志系统
	migrator := NewConfigMigrator(nil)
	if err := migrator.MigrateConfig(configFile); err != nil {
		// 如果有logger，使用它记录警告，否则忽略
		if logger != nil {
			logger.Warn("配置迁移失败", zap.Error(err))
		}
	}

	// 重新读取配置文件（迁移后）
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// 配置文件不存在，使用默认配置
	}

	// 解析配置
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	// 验证和调整配置
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// setDefaults 设置默认配置值
func setDefaults(v *viper.Viper) {
	// 使用统一的默认值设置函数 - "好品味"：消除重复配置
	setDefaultValues(v)

	// 转换设置默认值
	v.SetDefault("conversion.default_mode", "auto+")
	v.SetDefault("conversion.quality.jpeg_quality", 85)
	v.SetDefault("conversion.quality.webp_quality", 85)
	v.SetDefault("conversion.quality.avif_quality", 75)
	v.SetDefault("conversion.quality.jxl_quality", 85)
	v.SetDefault("conversion.quality.video_crf", 23)
	v.SetDefault("conversion.skip_extensions", []string{".db", ".log", ".tmp"})

	// 支持的文件扩展名（白名单模式）- 包含所有支持的媒体格式
	v.SetDefault("conversion.supported_extensions", []string{
		// 图片格式
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".tif", ".webp", ".avif", ".jxl",
		".heic", ".heif", ".ico", ".svg", ".psd", ".raw", ".cr2", ".nef", ".arw", ".dng",
		// 视频格式
		".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ogv",
		".mpg", ".mpeg", ".ts", ".mts", ".m2ts", ".vob", ".asf", ".rm", ".rmvb", ".divx",
		// 音频格式
		".mp3", ".wav", ".flac", ".aac", ".ogg", ".wma", ".m4a", ".opus", ".ape", ".ac3",
	})

	// 图片文件扩展名分类
	v.SetDefault("conversion.image_extensions", []string{
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".tif", ".webp", ".avif", ".jxl",
		".heic", ".heif", ".ico", ".svg", ".psd", ".raw", ".cr2", ".nef", ".arw", ".dng",
	})

	// 视频文件扩展名分类
	v.SetDefault("conversion.video_extensions", []string{
		".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ogv",
		".mpg", ".mpeg", ".ts", ".mts", ".m2ts", ".vob", ".asf", ".rm", ".rmvb", ".divx",
	})

	// 智能判断设置默认值（用户友好名称）
	v.SetDefault("conversion.quality_thresholds.enabled", true)

	// JPEG照片默认阈值（MB）
	v.SetDefault("conversion.quality_thresholds.photo.high_quality", 3.0)
	v.SetDefault("conversion.quality_thresholds.photo.medium_quality", 1.0)
	v.SetDefault("conversion.quality_thresholds.photo.low_quality", 0.1)

	// PNG图片默认阈值（MB）
	v.SetDefault("conversion.quality_thresholds.image.original_quality", 10.0)
	v.SetDefault("conversion.quality_thresholds.image.high_quality", 2.0)
	v.SetDefault("conversion.quality_thresholds.image.medium_quality", 0.5)

	// GIF动图默认阈值（MB）
	v.SetDefault("conversion.quality_thresholds.animation.medium_quality", 1.0)
	v.SetDefault("conversion.quality_thresholds.animation.low_quality", 20.0) // 大GIF通常品质不佳

	// 视频默认阈值（MB）
	v.SetDefault("conversion.quality_thresholds.video.high_quality", 100.0)
	v.SetDefault("conversion.quality_thresholds.video.medium_quality", 10.0)
	v.SetDefault("conversion.quality_thresholds.video.low_quality", 1.0)

	// 并发设置默认值 - 优化为保守配置避免系统卡顿
	maxWorkers := runtime.NumCPU()
	if maxWorkers > 4 {
		maxWorkers = 4 // 限制最大worker数量，避免过度并发
	}
	v.SetDefault("concurrency.scan_workers", maxWorkers)
	v.SetDefault("concurrency.conversion_workers", maxWorkers)
	v.SetDefault("concurrency.memory_limit", 4096) // 4GB，降低内存使用
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
	v.SetDefault("theme.ascii_art_mode", "compact") // compact, simplified, enhanced
	v.SetDefault("theme.enable_ascii_art_colors", true)

	// 问题文件处理设置默认值
	v.SetDefault("problem_file_handling.corrupted_file_strategy", "ignore")
	v.SetDefault("problem_file_handling.codec_incompatibility_strategy", "ignore")
	v.SetDefault("problem_file_handling.container_incompatibility_strategy", "ignore")

	v.SetDefault("problem_file_handling.trash_file_strategy", "delete")
	v.SetDefault("problem_file_handling.trash_file_extensions", []string{".tmp", ".bak", ".old", ".cache", ".log", ".db"})
	v.SetDefault("problem_file_handling.trash_file_path_keywords", []string{"temp", "cache", "backup", "old", "trash"})
}

// validateConfig 验证配置
func validateConfig(config *Config) error {
	// 验证并发数设置
	if config.Concurrency.ScanWorkers <= 0 {
		config.Concurrency.ScanWorkers = runtime.NumCPU() * 2
	}

	if config.Concurrency.ConversionWorkers <= 0 {
		config.Concurrency.ConversionWorkers = runtime.NumCPU()
	}

	// 验证质量设置
	quality := &config.Conversion.Quality
	if quality.JPEGQuality < 1 || quality.JPEGQuality > 100 {
		quality.JPEGQuality = 85
	}
	if quality.WebPQuality < 1 || quality.WebPQuality > 100 {
		quality.WebPQuality = 85
	}
	if quality.AVIFQuality < 1 || quality.AVIFQuality > 100 {
		quality.AVIFQuality = 75
	}
	if quality.JXLQuality < 1 || quality.JXLQuality > 100 {
		quality.JXLQuality = 85
	}
	if quality.VideoCRF < 0 || quality.VideoCRF > 51 {
		quality.VideoCRF = 23
	}

	// 验证问题文件处理策略
	validateProblemFileHandlingConfig(&config.ProblemFileHandling)

	// 验证工具路径
	validateToolPaths(config)

	return nil
}

// validateProblemFileHandlingConfig 验证问题文件处理配置
func validateProblemFileHandlingConfig(config *ProblemFileHandlingConfig) {
	// 验证损坏文件处理策略
	validCorruptedStrategies := map[string]bool{
		"ignore":        true,
		"delete":        true,
		"move_to_trash": true,
	}
	if !validCorruptedStrategies[config.CorruptedFileStrategy] {
		config.CorruptedFileStrategy = "ignore"
	}

	// 验证编解码器不兼容处理策略
	validCodecStrategies := map[string]bool{
		"ignore":        true,
		"force_process": true,
		"move_to_trash": true,
	}
	if !validCodecStrategies[config.CodecIncompatibilityStrategy] {
		config.CodecIncompatibilityStrategy = "ignore"
	}

	// 验证容器不兼容处理策略
	validContainerStrategies := map[string]bool{
		"ignore":        true,
		"force_process": true,
		"move_to_trash": true,
	}
	if !validContainerStrategies[config.ContainerIncompatibilityStrategy] {
		config.ContainerIncompatibilityStrategy = "ignore"
	}

}

// validateToolPaths 验证工具路径
func validateToolPaths(config *Config) {
	tools := &config.Tools

	// 检查工具是否存在，如果不存在则尝试查找
	checkAndUpdatePath := func(toolPath *string, toolName string) {
		if *toolPath == toolName {
			// 使用默认名称，尝试在PATH中查找
			if path, err := findInPath(toolName); err == nil {
				*toolPath = path
			}
		} else {
			// 使用指定路径，检查是否存在
			if !fileExists(*toolPath) {
				// 指定路径不存在，回退到默认名称
				*toolPath = toolName
			}
		}
	}

	checkAndUpdatePath(&tools.FFmpegPath, "ffmpeg")
	checkAndUpdatePath(&tools.FFprobePath, "ffprobe")
	checkAndUpdatePath(&tools.CjxlPath, "cjxl")
	checkAndUpdatePath(&tools.AvifencPath, "avifenc")
	checkAndUpdatePath(&tools.ExiftoolPath, "exiftool")
}

// findInPath 在PATH中查找可执行文件
func findInPath(name string) (string, error) {
	path := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		file := filepath.Join(dir, name)
		if fileExists(file) {
			return file, nil
		}
	}
	return "", os.ErrNotExist
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
