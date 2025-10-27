package i18n

import (
	"fmt"
	"os"
	"path/filepath"

	"pixly/internal/version"

	"github.com/spf13/viper"
)

// Language 语言枚举
type Language string

const (
	LanguageChinese Language = "zh"
	LanguageEnglish Language = "en"
)

// I18nManager 国际化管理器
type I18nManager struct {
	currentLanguage Language
	translations    map[Language]map[string]string
}

// 全局国际化管理器实例
var globalI18nManager *I18nManager

// TextKey 文本键枚举
type TextKey string

// 常用文本键定义
const (
	// 欢迎界面文本
	TextWelcomeTitle          TextKey = "welcome_title"
	TextSystemInfo            TextKey = "system_info"
	TextCoreFeatures          TextKey = "core_features"
	TextReadyMessage          TextKey = "ready_message"
	TextVersionInfo           TextKey = "version_info"
	TextPowerfulArchitecture  TextKey = "powerful_architecture"
	TextExternalTools         TextKey = "external_tools"
	TextSupportedFormats      TextKey = "supported_formats"
	TextIntelligentConversion TextKey = "intelligent_conversion"
	TextHighSpeedProcessing   TextKey = "high_speed_processing"
	TextSafetyMechanism       TextKey = "safety_mechanism"
	TextDetailedReports       TextKey = "detailed_reports"

	// 菜单文本
	TextMainMenuTitle   TextKey = "main_menu_title"
	TextConvertOption   TextKey = "convert_option"
	TextAnalyzeOption   TextKey = "analyze_option"
	TextSettingsOption  TextKey = "settings_option"
	TextDepsOption      TextKey = "deps_option"
	TextTestSuiteOption TextKey = "test_suite_option"
	TextHelpOption      TextKey = "help_option"
	TextAboutOption     TextKey = "about_option"
	TextExitOption      TextKey = "exit_option"

	// 菜单描述文本
	TextConvertOptionDesc   TextKey = "convert_option_desc"
	TextSettingsOptionDesc  TextKey = "settings_option_desc"
	TextTestSuiteOptionDesc TextKey = "test_suite_option_desc"
	TextHelpOptionDesc      TextKey = "help_option_desc"
	TextAboutOptionDesc     TextKey = "about_option_desc"
	TextExitOptionDesc      TextKey = "exit_option_desc"

	// 转换模式文本
	TextAutoPlusMode    TextKey = "auto_plus_mode"
	TextQualityMode     TextKey = "quality_mode"
	TextEmojiMode       TextKey = "emoji_mode"
	TextModeDescription TextKey = "mode_description"

	// 设置菜单文本
	TextSettingsMenuTitle         TextKey = "settings_menu_title"
	TextShowSettingsOption        TextKey = "show_settings_option"
	TextAdjustSettingsOption      TextKey = "adjust_settings_option"
	TextResetSettingsOption       TextKey = "reset_settings_option"
	TextThemeSettingsOption       TextKey = "theme_settings_option"
	TextLanguageSettingsOption    TextKey = "language_settings_option"
	TextConversionSettingsOption  TextKey = "conversion_settings_option"
	TextConcurrencySettingsOption TextKey = "concurrency_settings_option"
	TextOutputSettingsOption      TextKey = "output_settings_option"
	TextSecuritySettingsOption    TextKey = "security_settings_option"
	TextKeepOriginalFilesOption   TextKey = "keep_original_files_option"
	TextSaveSettingsOption        TextKey = "save_settings_option"
	TextShowCurrentSettings       TextKey = "show_current_settings"
	TextResetToDefaults           TextKey = "reset_to_defaults"
	TextAdjustQualityThresholds   TextKey = "adjust_quality_thresholds"

	// 主题相关文本
	TextThemeMenuTitle        TextKey = "theme_menu_title"
	TextCurrentTheme          TextKey = "current_theme"
	TextLightMode             TextKey = "light_mode"
	TextDarkMode              TextKey = "dark_mode"
	TextAutoMode              TextKey = "auto_mode"
	TextThemeSwitched         TextKey = "theme_switched"
	TextAsciiArtColors        TextKey = "ascii_art_colors"
	TextEmojiDisplay          TextKey = "emoji_display"
	TextThemeSettings         TextKey = "theme_settings"
	TextThemeMode             TextKey = "theme_mode"
	TextCurrentThemeSettings  TextKey = "current_theme_settings"
	TextUpdatedThemeSettings  TextKey = "updated_theme_settings"
	TextChangeThemeSettings   TextKey = "change_theme_settings"
	TextPleaseSelectThemeMode TextKey = "please_select_theme_mode"
	TextCurrentMode           TextKey = "current_mode"
	TextSetToThemeMode        TextKey = "set_to_theme_mode"
	TextKeepCurrentThemeMode  TextKey = "keep_current_theme_mode"

	TextDisplay                  TextKey = "display"
	TextThemeSettingsDescription TextKey = "theme_settings_description"

	// 语言相关文本
	TextLanguageMenuTitle           TextKey = "language_menu_title"
	TextCurrentLanguage             TextKey = "current_language"
	TextSupportedLanguages          TextKey = "supported_languages"
	TextLanguageSwitched            TextKey = "language_switched"
	TextLanguageSettings            TextKey = "language_settings"
	TextLanguageSettingsDescription TextKey = "language_settings_description"
	TextPleaseSelectLanguage        TextKey = "please_select_language"
	TextSetLanguageTo               TextKey = "set_language_to"
	TextKeepCurrentLanguage         TextKey = "keep_current_language"
	TextKeepCurrentLanguageSettings TextKey = "keep_current_language_settings"
	TextInvalidSelection            TextKey = "invalid_selection"

	// 进度和状态文本
	TextScanning         TextKey = "scanning"
	TextProcessing       TextKey = "processing"
	TextAnalyzing        TextKey = "analyzing"
	TextConversionStats  TextKey = "conversion_stats"
	TextFilesProcessed   TextKey = "files_processed"
	TextSuccessful       TextKey = "successful"
	TextFailed           TextKey = "failed"
	TextSkipped          TextKey = "skipped"
	TextSpaceSaved       TextKey = "space_saved"
	TextCompressionRatio TextKey = "compression_ratio"
	TextTotalTime        TextKey = "total_time"
	TextPleaseWait       TextKey = "please_wait" // 新增文本

	// 文件操作文本
	TextFileCorrupted      TextKey = "file_corrupted"
	TextLowQualityFile     TextKey = "low_quality_file"
	TextConversionComplete TextKey = "conversion_complete"
	TextBackupCreated      TextKey = "backup_created"
	TextOriginalRestored   TextKey = "original_restored"

	// 依赖管理文本
	TextDepsMenuTitle            TextKey = "deps_menu_title"
	TextCheckDepsOption          TextKey = "check_deps_option"
	TextInstallDepsOption        TextKey = "install_deps_option"
	TextInteractiveInstallOption TextKey = "interactive_install_option"
	TextDepsInstalled            TextKey = "deps_installed"
	TextDepsMissing              TextKey = "deps_missing"

	// 测试套件文本

	// 交互提示文本
	TextChooseOption         TextKey = "choose_option"
	TextPressEnterToContinue TextKey = "press_enter_to_continue"
	TextConfirmAction        TextKey = "confirm_action"
	TextOperationCanceled    TextKey = "operation_canceled"
	TextInvalidInput         TextKey = "invalid_input"
	TextInputDirectory       TextKey = "input_directory"
	TextInputDirectoryHelp   TextKey = "input_directory_help"
	TextSelectedDirectory    TextKey = "selected_directory"
	TextDirectoryNotFound    TextKey = "directory_not_found"
	TextConfirmConversion    TextKey = "confirm_conversion"
	TextStartingConversion   TextKey = "starting_conversion"
	TextConversionFinished   TextKey = "conversion_finished"
	TextThankYou             TextKey = "thank_you"

	// 帮助和关于文本
	TextHelpTitle             TextKey = "help_title"
	TextBasicUsage            TextKey = "basic_usage"
	TextConversionModes       TextKey = "conversion_modes"
	TextSupportedFormatsTitle TextKey = "supported_formats_title"
	TextSupportedImageFormats TextKey = "supported_image_formats"
	TextSupportedVideoFormats TextKey = "supported_video_formats"
	TextSupportedDocFormats   TextKey = "supported_doc_formats"
	TextImportantNotes        TextKey = "important_notes"
	TextBackupFiles           TextKey = "backup_files"
	TextDiskSpace             TextKey = "disk_space"
	TextLargeFiles            TextKey = "large_files"
	TextAboutTitle            TextKey = "about_title"
	TextAboutPixly            TextKey = "about_pixly"
	TextVersion               TextKey = "version"
	TextTechnology            TextKey = "technology"
	TextFeatures              TextKey = "features"
	TextDependencies          TextKey = "dependencies"
	TextVideoProcessing       TextKey = "video_processing"
	TextEncoding              TextKey = "encoding"
	TextMetadataProcessing    TextKey = "metadata_processing"

	// 静默模式相关文本
	TextSilentMode                  TextKey = "silent_mode"
	TextQuietMode                   TextKey = "quiet_mode"
	TextDisableUI                   TextKey = "disable_ui"
	TextSilentModeDesc              TextKey = "silent_mode_desc"
	TextQuietModeDesc               TextKey = "quiet_mode_desc"
	TextDisableUIDesc               TextKey = "disable_ui_desc"

	// 通用状态文本
	TextError                       TextKey = "error"
	TextWarning                     TextKey = "warning"
	TextSuccess                     TextKey = "success"
	TextInfo                        TextKey = "info"
	TextEnabled                     TextKey = "enabled"
	TextDisabled                    TextKey = "disabled"
	TextSettings                    TextKey = "settings"
	TextConfiguration               TextKey = "configuration"
	TextDirectory                   TextKey = "directory"
	TextMode                        TextKey = "mode"
	TextConcurrency                 TextKey = "concurrency"
	TextOutputDirectory             TextKey = "output_directory"
	TextVerboseLogging              TextKey = "verbose_logging"
	TextKeepOriginalFiles           TextKey = "keep_original_files"
	TextGenerateReport              TextKey = "generate_report"
	TextStatus                      TextKey = "status"
	TextUnknownMode                 TextKey = "unknown_mode"
	TextAvailableOperations         TextKey = "available_operations"
	TextTip                         TextKey = "tip"
	TextQualityThresholdsTip        TextKey = "quality_thresholds_tip"
	TextPhoto                       TextKey = "photo"
	TextImage                       TextKey = "image"
	TextAnimation                   TextKey = "animation"
	TextVideo                       TextKey = "video"
	TextHighQuality                 TextKey = "high_quality"
	TextMediumQuality               TextKey = "medium_quality"
	TextLowQuality                  TextKey = "low_quality"
	TextOriginalQuality             TextKey = "original_quality"
	TextAbove                       TextKey = "above"
	TextBelow                       TextKey = "below"
	TextLargeFileWarning            TextKey = "large_file_warning"
	TextUsuallyPoorQuality          TextKey = "usually_poor_quality"
	TextAllFilesMediumQuality       TextKey = "all_files_medium_quality"
	TextNewDefaultSettings          TextKey = "new_default_settings"
	TextCurrentSettings             TextKey = "current_settings"
	TextEnableIntelligentConversion TextKey = "enable_intelligent_conversion"
	TextPleaseEnterNewThresholds    TextKey = "please_enter_new_thresholds"
	TextPressEnterToKeepCurrent     TextKey = "press_enter_to_keep_current"
	TextSetTo                       TextKey = "set_to"
	TextKeepCurrent                 TextKey = "keep_current"
	TextUpdatedSettings             TextKey = "updated_settings"
	TextPleaseSelect                TextKey = "please_select"
	TextKeepCurrentSettings         TextKey = "keep_current_settings"
	TextChinese                     TextKey = "chinese"
	TextEnglish                     TextKey = "english"
	TextUnknown                     TextKey = "unknown"

	TextInputOutOfRange TextKey = "input_out_of_range" // 添加这一行
	TextYes             TextKey = "yes"                // 添加这一行
	TextNo              TextKey = "no"                 // 添加这一行
)

// NewI18nManager 创建国际化管理器
func NewI18nManager() *I18nManager {
	manager := &I18nManager{
		currentLanguage: LanguageChinese, // 默认中文
		translations:    make(map[Language]map[string]string),
	}

	// 初始化翻译文本
	manager.initTranslations()

	// 加载配置
	manager.loadConfig()

	return manager
}

// initTranslations 初始化翻译文本
func (im *I18nManager) initTranslations() {
	translations := im.translations

	// 中文翻译
	translations[LanguageChinese] = map[string]string{
		// 欢迎界面文本
		string(TextWelcomeTitle):          "启动 Pixly 转换器",
		string(TextSystemInfo):            "系统信息",
		string(TextCoreFeatures):          "核心特性",
		string(TextReadyMessage):          "准备就绪，开始您的媒体转换之旅！",
		string(TextVersionInfo):           "高性能媒体转换工具 " + version.GetVersionWithPrefix(),
		string(TextPowerfulArchitecture):  "Go 1.25+ 高性能并发架构",
		string(TextExternalTools):         "外部工具: FFmpeg 8.0, cjxl, avifenc, exiftool",
		string(TextSupportedFormats):      "支持格式: JXL, AVIF, WebP, MP4, MOV, GIF 等",
		string(TextIntelligentConversion): "智能转换策略 - 自动选择最佳格式",
		string(TextHighSpeedProcessing):   "高效并发处理 - 支持大批量文件",
		string(TextSafetyMechanism):       "安全机制 - 原子操作与回滚保护",
		string(TextDetailedReports):       "详细报告 - 完整的转换分析",

		// 菜单文本
		string(TextMainMenuTitle):   "Pixly 主菜单",
		string(TextConvertOption):   "开始转换",
		string(TextAnalyzeOption):   "分析媒体文件",
		string(TextSettingsOption):  "转换设置",
		string(TextDepsOption):      "管理依赖组件",
		string(TextTestSuiteOption): "AI测试套件",
		string(TextHelpOption):      "使用帮助",
		string(TextAboutOption):     "关于",
		string(TextExitOption):      "退出",

		// 菜单描述文本
		string(TextConvertOptionDesc):   "开始转换媒体文件",
		string(TextSettingsOptionDesc):  "配置转换参数和系统设置",
		string(TextTestSuiteOptionDesc): "运行AI测试套件验证功能",
		string(TextHelpOptionDesc):      "查看使用说明和帮助信息",
		string(TextAboutOptionDesc):     "查看软件信息和版权声明",
		string(TextExitOptionDesc):      "退出Pixly媒体转换器",
		string(TextSaveSettingsOption):  "保存当前设置",

		// 转换模式文本
		string(TextAutoPlusMode):    "auto+: 自动模式+ (默认，智能选择最佳转换策略)",
		string(TextQualityMode):     "quality: 品质模式 (保持高质量，适度压缩)",
		string(TextEmojiMode):       "emoji: 表情包模式 (针对GIF动图优化)",
		string(TextModeDescription): "请选择转换模式",

		// 设置菜单文本
		string(TextSettingsMenuTitle):         "Pixly 设置",
		string(TextShowSettingsOption):        "查看当前设置",
		string(TextAdjustSettingsOption):      "调整质量判断",
		string(TextResetSettingsOption):       "恢复默认设置",
		string(TextThemeSettingsOption):       "主题设置",
		string(TextLanguageSettingsOption):    "语言设置",
		string(TextConversionSettingsOption):  "修改转换模式",
		string(TextConcurrencySettingsOption): "调整并发数",
		string(TextOutputSettingsOption):      "设置输出目录",
		string(TextSecuritySettingsOption):    "文件保留设置",
		string(TextKeepOriginalFilesOption):   "保留原文件设置",

		// 主题相关文本
		string(TextThemeMenuTitle):        "主题设置",
		string(TextCurrentTheme):          "当前主题",
		string(TextLightMode):             "明亮模式",
		string(TextDarkMode):              "暗色模式",
		string(TextAutoMode):              "自动模式",
		string(TextThemeSwitched):         "主题已切换",
		string(TextAsciiArtColors):        "字符画彩色",
		string(TextEmojiDisplay):          "Emoji显示",
		string(TextThemeSettings):         "主题设置",
		string(TextThemeMode):             "主题模式",
		string(TextCurrentThemeSettings):  "当前主题设置",
		string(TextUpdatedThemeSettings):  "更新后的主题设置",
		string(TextChangeThemeSettings):   "是否要更改主题设置？",
		string(TextPleaseSelectThemeMode): "请选择主题模式",
		string(TextCurrentMode):           "当前模式",
		string(TextSetToThemeMode):        "已设置为主题模式",
		string(TextKeepCurrentThemeMode):  "保持当前主题模式",

		string(TextDisplay):                  "显示",
		string(TextThemeSettingsDescription): "管理Pixly的主题设置，包括主题模式和颜色配置",

		// 语言相关文本
		string(TextLanguageMenuTitle):           "语言设置",
		string(TextCurrentLanguage):             "当前语言",
		string(TextSupportedLanguages):          "支持的语言",
		string(TextLanguageSwitched):            "语言已切换",
		string(TextLanguageSettings):            "语言设置",
		string(TextLanguageSettingsDescription): "管理Pixly的语言设置，支持中英文切换",
		string(TextPleaseSelectLanguage):        "请选择语言",
		string(TextSetLanguageTo):               "已设置语言为",
		string(TextKeepCurrentLanguage):         "保持当前语言",
		string(TextKeepCurrentLanguageSettings): "保持当前语言设置",
		string(TextInvalidSelection):            "无效选择",

		// 进度和状态文本
		string(TextScanning):         "扫描中...",
		string(TextProcessing):       "处理中...",
		string(TextAnalyzing):        "分析中...",
		string(TextConversionStats):  "转换统计",
		string(TextSuccessful):       "成功",
		string(TextFailed):           "失败",
		string(TextSkipped):          "跳过",
		string(TextSpaceSaved):       "节省空间",
		string(TextCompressionRatio): "压缩率",
		string(TextTotalTime):        "总耗时",
		string(TextPleaseWait):       "请稍等...",

		// 文件操作文本
		string(TextFileCorrupted):      "⚠️ 文件损坏",
		string(TextLowQualityFile):     "📊 低质量文件",
		string(TextConversionComplete): "✨ 转换完成",
		string(TextBackupCreated):      "💾 备份已创建",
		string(TextOriginalRestored):   "原文件已还原",

		// 依赖管理文本
		string(TextDepsMenuTitle):            "依赖组件管理",
		string(TextCheckDepsOption):          "检查依赖组件状态",
		string(TextInstallDepsOption):        "安装缺失的依赖组件",
		string(TextInteractiveInstallOption): "交互式安装依赖组件",
		string(TextDepsInstalled):            "依赖已安装",
		string(TextDepsMissing):              "依赖缺失",

		// 测试套件文本

		// 交互提示文本
		string(TextChooseOption):         "请选择 (输入数字): ",
		string(TextPressEnterToContinue): "按 Enter 键继续...",
		string(TextConfirmAction):        "是否确认此操作？(y/N): ",
		string(TextOperationCanceled):    "操作已取消",
		string(TextInvalidInput):         "无效输入",
		string(TextInputDirectory):       "请输入要转换的目录路径:",
		string(TextInputDirectoryHelp):   "直接按 Enter 使用当前目录",
		string(TextSelectedDirectory):    "已选择目录",
		string(TextDirectoryNotFound):    "目录不存在",
		string(TextConfirmConversion):    "确认开始转换",
		string(TextStartingConversion):   "正在启动转换",
		string(TextConversionFinished):   "转换完成！",
		string(TextThankYou):             "感谢使用 Pixly！再见！",

		// 帮助和关于文本
		string(TextHelpTitle):             "使用帮助",
		string(TextBasicUsage):            "基本使用",
		string(TextConversionModes):       "转换模式说明",
		string(TextSupportedFormatsTitle): "支持的文件格式",
		string(TextSupportedImageFormats): "图片: JPG, PNG, GIF, WebP, HEIC, TIFF, JXL, AVIF",
		string(TextSupportedVideoFormats): "视频: MP4, MOV, AVI, WebM, MKV",
		string(TextSupportedDocFormats):   "文档: PDF",
		string(TextImportantNotes):        "注意事项",
		string(TextBackupFiles):           "• 转换前请备份重要文件",
		string(TextDiskSpace):             "• 确保有足够的磁盘空间",
		string(TextLargeFiles):            "• 大批量文件转换可能需要较长时间",
		string(TextAboutTitle):            "关于 Pixly",
		string(TextAboutPixly):            "Pixly 媒体转换工具",
		string(TextVersion):               "版本: " + version.GetVersionWithPrefix(),
		string(TextTechnology):            "技术: Go 1.25+ 高性能并发架构",
		string(TextFeatures):              "特性",
		string(TextDependencies):          "依赖工具",
		string(TextVideoProcessing):       "视频处理",
		string(TextEncoding):              "编码",
		string(TextMetadataProcessing):    "元数据处理",

		// 静默模式相关文本
		string(TextSilentMode):                  "静默模式",
		string(TextQuietMode):                   "安静模式",
		string(TextDisableUI):                   "禁用界面",
		string(TextSilentModeDesc):              "运行时不显示进度条",
		string(TextQuietModeDesc):               "减少输出信息",
		string(TextDisableUIDesc):               "禁用所有界面输出",

		// 通用状态文本
		string(TextError):                       "❌ 错误",
		string(TextWarning):                     "⚠️  警告",
		string(TextSuccess):                     "✅ 成功",
		string(TextInfo):                        "ℹ️  信息",
		string(TextEnabled):                     "✅ 已启用",
		string(TextDisabled):                    "❌ 已禁用",
		string(TextSettings):                    "⚙️ 设置",
		string(TextConfiguration):               "📄 配置",
		string(TextDirectory):                   "📁 目录",
		string(TextMode):                        "🎯 模式",
		string(TextConcurrency):                 "🔄 并发",
		string(TextOutputDirectory):             "📁 输出目录",
		string(TextVerboseLogging):              "📝 详细日志",
		string(TextKeepOriginalFiles):           "🔒 保留原文件",
		string(TextGenerateReport):              "📊 生成报告",
		string(TextStatus):                      "状态",
		string(TextUnknownMode):                 "未知模式",
		string(TextAvailableOperations):         "可用操作",
		string(TextTip):                         "提示",
		string(TextQualityThresholdsTip):        "质量判断影响自动模式+的文件处理策略",
		string(TextPhoto):                       "照片",
		string(TextImage):                       "图片",
		string(TextAnimation):                   "动图",
		string(TextVideo):                       "视频",
		string(TextHighQuality):                 "高品质",
		string(TextMediumQuality):               "中等质量",
		string(TextLowQuality):                  "低品质",
		string(TextOriginalQuality):             "原画质量",
		string(TextAbove):                       "以上",
		string(TextBelow):                       "以下",
		string(TextLargeFileWarning):            "大文件警告",
		string(TextUsuallyPoorQuality):          "通常品质不佳",
		string(TextAllFilesMediumQuality):       "所有文件将被视为中等质量",
		string(TextNewDefaultSettings):          "新的默认设置",
		string(TextCurrentSettings):             "当前设置",
		string(TextEnableIntelligentConversion): "是否启用智能判断？",
		string(TextPleaseEnterNewThresholds):    "请输入新的阈值",
		string(TextPressEnterToKeepCurrent):     "直接回车保持当前值",
		string(TextSetTo):                       "已设置为",
		string(TextKeepCurrent):                 "保持当前值",
		string(TextUpdatedSettings):             "更新后的设置",
		string(TextPleaseSelect):                "请选择",
		string(TextKeepCurrentSettings):         "保持当前设置",
		string(TextChinese):                     "中文",
		string(TextEnglish):                     "English",
		string(TextUnknown):                     "未知",

		string(TextInputOutOfRange): "输入超出范围，请输入 %d 到 %d 之间的值",
		string(TextYes):             "是",
		string(TextNo):              "否",
	}

	// 英文翻译
	translations[LanguageEnglish] = map[string]string{
		// 欢迎界面文本
		string(TextWelcomeTitle):          "🚀 Launching Pixly Converter",
		string(TextSystemInfo):            "🚀 System Information",
		string(TextCoreFeatures):          "⚡ Core Features",
		string(TextReadyMessage):          "🌟 Ready to start your media conversion journey!",
		string(TextVersionInfo):           "🏆 High-performance Media Converter " + version.GetVersionWithPrefix(),
		string(TextPowerfulArchitecture):  "💫 Go 1.25+ High-performance Concurrent Architecture",
		string(TextExternalTools):         "🛠️  External Tools: FFmpeg 8.0, cjxl, avifenc, exiftool",
		string(TextSupportedFormats):      "Supported Formats: JXL, AVIF, WebP, MP4, MOV, GIF, etc.",
		string(TextIntelligentConversion): "Intelligent Conversion Strategy - Automatically Selects Best Format",
		string(TextHighSpeedProcessing):   "High-speed Concurrent Processing - Supports Large Batch Files",
		string(TextSafetyMechanism):       "Safety Mechanism - Atomic Operations and Rollback Protection",
		string(TextDetailedReports):       "Detailed Reports - Complete Conversion Analysis",

		// 菜单文本
		string(TextMainMenuTitle):   "Pixly Main Menu",
		string(TextConvertOption):   "Start Conversion",
		string(TextAnalyzeOption):   "Analyze Media Files",
		string(TextSettingsOption):  "Conversion Settings",
		string(TextDepsOption):      "Manage Dependencies",
		string(TextTestSuiteOption): "AI Test Suite",
		string(TextHelpOption):      "Help",
		string(TextAboutOption):     "About",
		string(TextExitOption):      "Exit",

		// 菜单描述文本
		string(TextConvertOptionDesc):   "Start converting media files",
		string(TextSettingsOptionDesc):  "Configure conversion parameters and system settings",
		string(TextTestSuiteOptionDesc): "Run AI test suite to verify functionality",
		string(TextHelpOptionDesc):      "View usage instructions and help information",
		string(TextAboutOptionDesc):     "View software information and copyright notice",
		string(TextExitOptionDesc):      "Exit Pixly Media Converter",
		string(TextSaveSettingsOption):  "Save current settings",

		// 转换模式文本
		string(TextAutoPlusMode):    "auto+: Auto Mode+ (Default, intelligent selection of best conversion strategy)",
		string(TextQualityMode):     "quality: Quality Mode (Maintain high quality, moderate compression)",
		string(TextEmojiMode):       "emoji: Emoji Mode (Optimized for GIF animations)",
		string(TextModeDescription): "Please select conversion mode",

		// 设置菜单文本
		string(TextSettingsMenuTitle):         "Pixly Settings",
		string(TextShowSettingsOption):        "View Current Settings",
		string(TextAdjustSettingsOption):      "Adjust Quality Thresholds",
		string(TextResetSettingsOption):       "Reset to Default Settings",
		string(TextThemeSettingsOption):       "Theme Settings",
		string(TextLanguageSettingsOption):    "Language Settings",
		string(TextConversionSettingsOption):  "Change Conversion Mode",
		string(TextConcurrencySettingsOption): "Adjust Concurrency",
		string(TextOutputSettingsOption):      "Set Output Directory",
		string(TextSecuritySettingsOption):    "Keep Original Files",
		string(TextKeepOriginalFilesOption):   "Keep Original Files Setting",

		// 主题相关文本
		string(TextThemeMenuTitle):        "Theme Settings",
		string(TextCurrentTheme):          "Current Theme",
		string(TextLightMode):             "Light Mode",
		string(TextDarkMode):              "Dark Mode",
		string(TextAutoMode):              "Auto Mode",
		string(TextThemeSwitched):         "Theme Switched",
		string(TextAsciiArtColors):        "ASCII Art Colors",
		string(TextEmojiDisplay):          "Emoji Display",
		string(TextThemeSettings):         "Theme Settings",
		string(TextThemeMode):             "Theme Mode",
		string(TextCurrentThemeSettings):  "Current Theme Settings",
		string(TextUpdatedThemeSettings):  "Updated Theme Settings",
		string(TextChangeThemeSettings):   "Do you want to change theme settings?",
		string(TextPleaseSelectThemeMode): "Please select theme mode",
		string(TextCurrentMode):           "Current Mode",
		string(TextSetToThemeMode):        "Set to theme mode",
		string(TextKeepCurrentThemeMode):  "Keep current theme mode",

		string(TextDisplay):                  "Display",
		string(TextThemeSettingsDescription): "Manage Pixly's theme settings, including theme mode and color configuration",

		// 语言相关文本
		string(TextLanguageMenuTitle):           "Language Settings",
		string(TextCurrentLanguage):             "Current Language",
		string(TextSupportedLanguages):          "Supported Languages",
		string(TextLanguageSwitched):            "Language Switched",
		string(TextLanguageSettings):            "Language Settings",
		string(TextLanguageSettingsDescription): "Manage Pixly's language settings, supporting Chinese and English switching",
		string(TextPleaseSelectLanguage):        "Please select language",
		string(TextSetLanguageTo):               "Set language to",
		string(TextKeepCurrentLanguage):         "Keep current language",
		string(TextKeepCurrentLanguageSettings): "Keep current language settings",
		string(TextInvalidSelection):            "Invalid selection",

		// 进度和状态文本
		string(TextScanning):         "Scanning...",
		string(TextProcessing):       "Processing...",
		string(TextAnalyzing):        "Analyzing...",
		string(TextConversionStats):  "Conversion Statistics",
		string(TextFilesProcessed):   "Files Processed",
		string(TextSuccessful):       "Successful",
		string(TextFailed):           "Failed",
		string(TextSkipped):          "Skipped",
		string(TextSpaceSaved):       "Space Saved",
		string(TextCompressionRatio): "Compression Ratio",
		string(TextTotalTime):        "Total Time",
		string(TextPleaseWait):       "Please wait while processing...",

		// 文件操作文本
		string(TextFileCorrupted):      "File Corrupted",
		string(TextLowQualityFile):     "Low Quality File",
		string(TextConversionComplete): "Conversion Complete",
		string(TextBackupCreated):      "Backup Created",
		string(TextOriginalRestored):   "Original Restored",

		// 依赖管理文本
		string(TextDepsMenuTitle):            "Dependency Management",
		string(TextCheckDepsOption):          "Check Dependency Status",
		string(TextInstallDepsOption):        "Install Missing Dependencies",
		string(TextInteractiveInstallOption): "Interactive Dependency Installation",
		string(TextDepsInstalled):            "Dependencies Installed",
		string(TextDepsMissing):              "Dependencies Missing",

		// 测试套件文本

		// 交互提示文本
		string(TextChooseOption):         "Please choose (enter number): ",
		string(TextPressEnterToContinue): "Press Enter to continue...",
		string(TextConfirmAction):        "Confirm this action? (y/N): ",
		string(TextOperationCanceled):    "Operation canceled",
		string(TextInvalidInput):         "Invalid input",
		string(TextInputDirectory):       "Please enter the directory path to convert:",
		string(TextInputDirectoryHelp):   "Press Enter to use current directory",
		string(TextSelectedDirectory):    "Selected directory",
		string(TextDirectoryNotFound):    "Directory not found",
		string(TextConfirmConversion):    "Confirm start conversion",
		string(TextStartingConversion):   "Starting conversion",
		string(TextConversionFinished):   "Conversion finished!",
		string(TextThankYou):             "Thank you for using Pixly! Goodbye!",

		// 帮助和关于文本
		string(TextHelpTitle):             "Help",
		string(TextBasicUsage):            "Basic Usage",
		string(TextConversionModes):       "Conversion Modes",
		string(TextSupportedFormatsTitle): "Supported File Formats",
		string(TextSupportedImageFormats): "Images: JPG, PNG, GIF, WebP, HEIC, TIFF, JXL, AVIF",
		string(TextSupportedVideoFormats): "Videos: MP4, MOV, AVI, WebM, MKV",
		string(TextSupportedDocFormats):   "Documents: PDF",
		string(TextImportantNotes):        "Important Notes",
		string(TextBackupFiles):           "• Please backup important files before conversion",
		string(TextDiskSpace):             "• Ensure sufficient disk space",
		string(TextLargeFiles):            "• Large batch conversion may take a long time",
		string(TextAboutTitle):            "About Pixly",
		string(TextAboutPixly):            "Pixly Media Converter",
		string(TextVersion):               "Version: " + version.GetVersionWithPrefix(),
		string(TextTechnology):            "Technology: Go 1.25+ High-performance Concurrent Architecture",
		string(TextFeatures):              "Features",
		string(TextDependencies):          "Dependencies",
		string(TextVideoProcessing):       "Video Processing",
		string(TextEncoding):              "Encoding",
		string(TextMetadataProcessing):    "Metadata Processing",

		// 静默模式相关文本
	string(TextSilentMode):                  "Silent Mode",
	string(TextQuietMode):                   "Quiet Mode", 
	string(TextDisableUI):                   "Disable UI",
	string(TextSilentModeDesc):              "Run without progress bars",
	string(TextQuietModeDesc):               "Reduce output information",
	string(TextDisableUIDesc):               "Disable all UI output",

	// 通用状态文本
	string(TextError):                       "Error",
	string(TextWarning):                     "Warning",
	string(TextSuccess):                     "Success",
	string(TextInfo):                        "Info",
	string(TextEnabled):                     "Enabled",
	string(TextDisabled):                    "Disabled",
	string(TextSettings):                    "Settings",
	string(TextConfiguration):               "Configuration",
		string(TextDirectory):                   "Directory",
		string(TextMode):                        "Mode",
		string(TextConcurrency):                 "Concurrency",
		string(TextOutputDirectory):             "Output Directory",
		string(TextVerboseLogging):              "Verbose Logging",
		string(TextKeepOriginalFiles):           "Keep Original Files",
		string(TextGenerateReport):              "Generate Report",
		string(TextStatus):                      "Status",
		string(TextUnknownMode):                 "Unknown Mode",
		string(TextAvailableOperations):         "Available Operations",
		string(TextTip):                         "Tip",
		string(TextQualityThresholdsTip):        "Quality thresholds affect file processing strategies in auto+ mode",
		string(TextPhoto):                       "Photo",
		string(TextImage):                       "Image",
		string(TextAnimation):                   "Animation",
		string(TextVideo):                       "Video",
		string(TextHighQuality):                 "High Quality",
		string(TextMediumQuality):               "Medium Quality",
		string(TextLowQuality):                  "Low Quality",
		string(TextOriginalQuality):             "Original Quality",
		string(TextAbove):                       "Above",
		string(TextBelow):                       "Below",
		string(TextLargeFileWarning):            "Large File Warning",
		string(TextUsuallyPoorQuality):          "Usually Poor Quality",
		string(TextAllFilesMediumQuality):       "All files will be treated as medium quality",
		string(TextNewDefaultSettings):          "New Default Settings",
		string(TextCurrentSettings):             "Current Settings",
		string(TextEnableIntelligentConversion): "Enable intelligent conversion?",
		string(TextPleaseEnterNewThresholds):    "Please enter new thresholds",
		string(TextPressEnterToKeepCurrent):     "Press Enter to keep current value",
		string(TextSetTo):                       "Set to",
		string(TextKeepCurrent):                 "Keep current",
		string(TextUpdatedSettings):             "Updated Settings",
		string(TextPleaseSelect):                "Please select",
		string(TextKeepCurrentSettings):         "Keep current settings",
		string(TextChinese):                     "Chinese",
		string(TextEnglish):                     "English",
		string(TextUnknown):                     "Unknown",

		string(TextShowCurrentSettings):     "📊 Show Current Settings",
		string(TextResetToDefaults):         "🔄 Reset to Defaults",
		string(TextAdjustQualityThresholds): "⚙️  Adjust Quality Thresholds",
		string(TextInputOutOfRange):         "Input out of range, please enter a value between %d and %d",
		string(TextYes):                     "yes",
		string(TextNo):                      "no",
	}
}

// loadConfig 加载语言配置
func (im *I18nManager) loadConfig() {
	// 尝试从配置文件加载
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	configPath := filepath.Join(home, ".pixly.yaml")
	if _, err := os.Stat(configPath); err == nil {
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err == nil {
			lang := viper.GetString("language")
			if lang != "" {
				im.currentLanguage = Language(lang)
			}
		}
	}
}

// GetText 获取指定键的文本
func (im *I18nManager) GetText(key TextKey) string {
	translations, exists := im.translations[im.currentLanguage]
	if !exists {
		// 如果当前语言不存在，回退到中文
		translations, exists = im.translations[LanguageChinese]
		if !exists {
			return string(key) // 如果连中文都没有，返回键名
		}
	}

	text, exists := translations[string(key)]
	if !exists {
		return string(key) // 如果键不存在，返回键名
	}

	return text
}

// SetLanguage 设置语言
func (im *I18nManager) SetLanguage(lang Language) error {
	// 检查语言是否支持
	if _, exists := im.translations[lang]; !exists {
		return fmt.Errorf("unsupported language: %s", lang)
	}

	im.currentLanguage = lang

	// 保存配置
	return im.saveConfig()
}

// saveConfig 保存语言配置
func (im *I18nManager) saveConfig() error {
	viper.Set("language", string(im.currentLanguage))

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(home, ".pixly.yaml")
	return viper.WriteConfigAs(configPath)
}

// GetCurrentLanguage 获取当前语言
func (im *I18nManager) GetCurrentLanguage() Language {
	return im.currentLanguage
}

// GetSupportedLanguages 获取支持的语言列表
func (im *I18nManager) GetSupportedLanguages() []Language {
	languages := make([]Language, 0, len(im.translations))
	for lang := range im.translations {
		languages = append(languages, lang)
	}
	return languages
}

// GetGlobalI18nManager 获取全局国际化管理器
func GetGlobalI18nManager() *I18nManager {
	if globalI18nManager == nil {
		globalI18nManager = NewI18nManager()
	}
	return globalI18nManager
}

// InitializeGlobalI18n 初始化全局国际化管理器
func InitializeGlobalI18n() {
	globalI18nManager = NewI18nManager()
}

// T 获取文本的便捷函数
func T(key TextKey) string {
	return GetGlobalI18nManager().GetText(key)
}
