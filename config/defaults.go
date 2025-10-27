package config

import "github.com/spf13/viper"

// setDefaultValues 设置所有默认配置值 - "好品味"：消除重复的配置设置
// 这个函数统一管理所有默认值，避免在多个文件中重复定义
func setDefaultValues(v *viper.Viper) {
	// 问题文件处理默认值
	setProblemFileHandlingDefaults(v)

	// 质量阈值默认值
	setQualityThresholdsDefaults(v)

	// 工具路径默认值 - 使用相对路径而非绝对路径
	setToolsDefaults(v)

	// UI显示默认值 - 确保进度条正常显示
	setUIDefaults(v)
}

// setProblemFileHandlingDefaults 设置问题文件处理的默认值
// 注意：垃圾文件处理功能已被移除
func setProblemFileHandlingDefaults(v *viper.Viper) {
	// 垃圾文件处理功能已移除，保留空函数以避免编译错误
}

// setQualityThresholdsDefaults 设置质量阈值的默认值
func setQualityThresholdsDefaults(v *viper.Viper) {
	v.SetDefault("conversion.quality_thresholds.enabled", true)

	// 照片质量阈值
	v.SetDefault("conversion.quality_thresholds.photo.high_quality", 3.0)
	v.SetDefault("conversion.quality_thresholds.photo.medium_quality", 1.0)
	v.SetDefault("conversion.quality_thresholds.photo.low_quality", 0.1)

	// 图像质量阈值
	v.SetDefault("conversion.quality_thresholds.image.original_quality", 10.0)
	v.SetDefault("conversion.quality_thresholds.image.high_quality", 2.0)
	v.SetDefault("conversion.quality_thresholds.image.medium_quality", 0.5)

	// 动画质量阈值
	v.SetDefault("conversion.quality_thresholds.animation.medium_quality", 1.0)
	v.SetDefault("conversion.quality_thresholds.animation.low_quality", 20.0) // 大GIF通常品质不佳

	// 视频质量阈值
	v.SetDefault("conversion.quality_thresholds.video.high_quality", 100.0)
	v.SetDefault("conversion.quality_thresholds.video.medium_quality", 10.0)
	v.SetDefault("conversion.quality_thresholds.video.low_quality", 1.0)
}

// setToolsDefaults 设置工具路径的默认值
func setToolsDefaults(v *viper.Viper) {
	// 使用相对路径而非绝对路径，让程序能够在系统PATH中查找工具
	v.SetDefault("tools.ffmpeg_path", "ffmpeg")
	v.SetDefault("tools.ffprobe_path", "ffprobe")
	v.SetDefault("tools.cjxl_path", "cjxl")
	v.SetDefault("tools.avifenc_path", "avifenc")
	v.SetDefault("tools.exiftool_path", "exiftool")
}

// setUIDefaults 设置UI显示的默认值
func setUIDefaults(v *viper.Viper) {
	// 确保进度条正常显示，禁用静默模式
	v.SetDefault("advanced.ui.silent_mode", false)
	v.SetDefault("advanced.ui.quiet_mode", false)
	v.SetDefault("advanced.ui.disable_ui", false)
}