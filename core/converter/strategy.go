package converter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// ConversionStrategy 转换策略接口
type ConversionStrategy interface {
	ConvertImage(file *MediaFile) (string, error)
	ConvertVideo(file *MediaFile) (string, error)
	GetName() string
}

// AutoPlusStrategy 自动模式+策略（智能决策核心）
type AutoPlusStrategy struct {
	converter    *Converter
	errorHandler *ErrorHandler
}

// QualityStrategy 品质模式策略
type QualityStrategy struct {
	converter    *Converter
	errorHandler *ErrorHandler
}

// EmojiStrategy 表情包模式策略
type EmojiStrategy struct {
	converter    *Converter
	errorHandler *ErrorHandler
}

// NewStrategy 创建策略实例
func NewStrategy(mode ConversionMode, conv *Converter) ConversionStrategy {
	switch mode {
	case ModeAutoPlus:
		return &AutoPlusStrategy{converter: conv, errorHandler: conv.errorHandler}
	case ModeQuality:
		return &QualityStrategy{converter: conv, errorHandler: conv.errorHandler}
	case ModeEmoji:
		return &EmojiStrategy{converter: conv, errorHandler: conv.errorHandler}
	default:
		return &AutoPlusStrategy{converter: conv, errorHandler: conv.errorHandler}
	}
}

// === 自动模式+ 实现 ===

func (s *AutoPlusStrategy) GetName() string {
	return "auto+ (智能决策)"
}

func (s *AutoPlusStrategy) ConvertImage(file *MediaFile) (string, error) {
	// 0. 优先检测无损JPEG/PNG - 新增功能
	if s.isLosslessFormat(file) {
		// 检测到无损格式，优先使用质量模式
		return s.applyQualityModeLogic(file)
	}

	// 1. 品质分类体系
	quality := s.analyzeImageQuality(file)

	switch quality {
	case "极高", "高品质", "原画":
		// 路由至品质模式的无损压缩逻辑
		return s.applyQualityModeLogic(file)

	case "中高", "中低", "中等":
		// 平衡优化逻辑
		return s.applyBalancedOptimization(file)

	case "极低", "低品质":
		// 根据最新README规范，移除低质量文件跳过功能
		// 直接应用平衡优化逻辑
		return s.applyBalancedOptimization(file)

	default:
		return s.applyBalancedOptimization(file)
	}
}

func (s *AutoPlusStrategy) ConvertVideo(file *MediaFile) (string, error) {
	// 视频转换逻辑，主要是容器转换
	return s.converter.convertVideoContainer(file)
}

// ConvertAudio方法已删除 - 根据README要求，本程序不处理音频文件

// ImageQualityMetrics 图像质量度量
type ImageQualityMetrics struct {
	Complexity           float64 // 图像复杂度 (0-1)
	NoiseLevel           float64 // 噪声水平 (0-1)
	CompressionPotential float64 // 压缩潜力 (0-1)
	ContentType          string  // 内容类型: photo, graphic, mixed
	QualityScore         float64 // 综合质量分数 (0-100)
}

// analyzeImageQuality 智能图像质量分析
func (s *AutoPlusStrategy) analyzeImageQuality(file *MediaFile) string {
	metrics := s.analyzeImageMetrics(file)

	// 基于综合质量分数进行分类
	if metrics.QualityScore >= 85 {
		return "原画"
	} else if metrics.QualityScore >= 75 {
		return "高品质"
	} else if metrics.QualityScore >= 60 {
		return "中等"
	} else if metrics.QualityScore >= 40 {
		return "中低"
	} else {
		return "低品质"
	}
}

// isLosslessFormat 检测是否为无损JPEG/PNG格式
func (s *AutoPlusStrategy) isLosslessFormat(file *MediaFile) bool {
	ext := strings.ToLower(filepath.Ext(file.Path))

	// 只检测JPEG和PNG格式
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		return false
	}

	// 获取图像度量指标
	metrics := s.analyzeImageMetrics(file)

	// 无损判定条件：
	// 1. PNG格式且像素格式为rgba/rgb24（无损格式）
	// 2. JPEG格式且像素格式为yuv444p（无损JPEG）
	// 3. 噪声水平极低（<= 0.1）
	// 4. 质量分数很高（>= 90）
	if ext == ".png" {
		// PNG本身就是无损格式，但要排除过度压缩的情况
		return metrics.NoiseLevel <= 0.1 && metrics.QualityScore >= 85
	}

	if ext == ".jpg" || ext == ".jpeg" {
		// JPEG无损检测：yuv444p像素格式 + 高质量分数 + 低噪声
		return metrics.NoiseLevel <= 0.1 && metrics.QualityScore >= 95
	}

	return false
}

// analyzeImageMetrics 分析图像度量指标
func (s *AutoPlusStrategy) analyzeImageMetrics(file *MediaFile) ImageQualityMetrics {
	ext := strings.ToLower(file.Extension)
	sizeInMB := float64(file.Size) / (1024 * 1024)

	// 获取图像基本信息
	width, height := s.getImageDimensions(file)
	pixelCount := float64(width * height)

	// 计算像素密度 (MB per megapixel)
	pixelDensity := sizeInMB / (pixelCount / 1000000)

	var metrics ImageQualityMetrics

	// 基于格式特性分析
	switch ext {
	case ".jpg", ".jpeg":
		metrics = s.analyzeJPEGQuality(file, pixelDensity, sizeInMB)
	case ".png":
		metrics = s.analyzePNGQuality(file, pixelDensity)
	case ".gif":
		metrics = s.analyzeGIFQuality(sizeInMB)
	case ".webp":
		metrics = s.analyzeWebPQuality(file, pixelDensity, sizeInMB)
	default:
		metrics = s.analyzeGenericQuality(sizeInMB)
	}

	return metrics
}

// getImageDimensions 获取图像尺寸
func (s *AutoPlusStrategy) getImageDimensions(file *MediaFile) (int, int) {
	// 通过FFprobe获取图像尺寸
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		file.Path,
	}

	cmd := exec.Command(s.converter.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		// 如果FFprobe失败，返回默认值
		return 1920, 1080
	}

	// 解析FFprobe输出
	var probeData struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil || len(probeData.Streams) == 0 {
		return 1920, 1080
	}

	// 安全访问第一个流的尺寸信息
	if len(probeData.Streams) == 0 {
		s.converter.logger.Warn("No streams found for dimension analysis, using default dimensions",
			zap.String("file", file.Path))
		return 1920, 1080
	}
	return probeData.Streams[0].Width, probeData.Streams[0].Height
}

// analyzeJPEGQuality 分析JPEG质量
func (s *AutoPlusStrategy) analyzeJPEGQuality(file *MediaFile, pixelDensity, sizeInMB float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "photo"

	// 使用FFprobe获取JPEG详细信息
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		file.Path,
	}

	cmd := exec.Command(s.converter.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		// 回退到基础分析
		return s.fallbackJPEGAnalysis(pixelDensity, sizeInMB)
	}

	// 解析FFprobe输出
	var probeData struct {
		Streams []struct {
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			PixFmt     string `json:"pix_fmt"`
			BitRate    string `json:"bit_rate"`
			ColorSpace string `json:"color_space"`
		} `json:"streams"`
		Format struct {
			BitRate string `json:"bit_rate"`
			Size    string `json:"size"`
		} `json:"format"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil || len(probeData.Streams) == 0 {
		return s.fallbackJPEGAnalysis(pixelDensity, sizeInMB)
	}

	// 安全访问第一个流
	if len(probeData.Streams) == 0 {
		s.converter.logger.Warn("No streams found in probe data, using fallback analysis",
			zap.String("file", file.Path))
		return s.fallbackJPEGAnalysis(pixelDensity, sizeInMB)
	}
	stream := probeData.Streams[0]

	// 基于像素格式分析质量
	switch stream.PixFmt {
	case "yuv444p", "yuvj444p":
		metrics.QualityScore = 98 // 4:4:4采样，接近无损质量
		metrics.Complexity = 0.9
		metrics.NoiseLevel = 0.05 // 极低噪声，接近无损
	case "yuv422p", "yuvj422p":
		metrics.QualityScore = 80 // 4:2:2采样，高质量
		metrics.Complexity = 0.7
		metrics.NoiseLevel = 0.15
	case "yuv420p", "yuvj420p":
		metrics.QualityScore = 65 // 4:2:0采样，标准质量
		metrics.Complexity = 0.6
		metrics.NoiseLevel = 0.25
	default:
		metrics.QualityScore = 50
		metrics.Complexity = 0.5
		metrics.NoiseLevel = 0.35
	}

	// 基于像素密度调整
	if pixelDensity > 1.0 {
		metrics.QualityScore += 10
	} else if pixelDensity < 0.3 {
		metrics.QualityScore -= 15
	}

	// 限制范围
	if metrics.QualityScore > 100 {
		metrics.QualityScore = 100
	} else if metrics.QualityScore < 20 {
		metrics.QualityScore = 20
	}

	// JPEG压缩潜力分析
	if metrics.QualityScore > 80 {
		metrics.CompressionPotential = 0.2 // 高质量JPEG压缩潜力有限
	} else if metrics.QualityScore > 60 {
		metrics.CompressionPotential = 0.4
	} else {
		metrics.CompressionPotential = 0.6 // 低质量JPEG有较大压缩空间
	}

	// 如果之前没有设置噪声水平，则进行估算
	if metrics.NoiseLevel == 0 {
		metrics.NoiseLevel = (100 - metrics.QualityScore) / 200.0
	}

	return metrics
}

// fallbackJPEGAnalysis JPEG分析回退方案
func (s *AutoPlusStrategy) fallbackJPEGAnalysis(pixelDensity, sizeInMB float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "photo"
	metrics.CompressionPotential = 0.3

	// 根据文件大小和像素密度综合判断质量
	// 大文件（>= 2MB）通常表示高质量原图
	if sizeInMB >= 2.0 && pixelDensity > 0.8 {
		metrics.QualityScore = 90 // 原画级别
		metrics.NoiseLevel = 0.1
		metrics.Complexity = 0.8
	} else if pixelDensity > 0.8 {
		metrics.QualityScore = 80 // 高品质
		metrics.NoiseLevel = 0.2
		metrics.Complexity = 0.7
	} else if pixelDensity > 0.4 {
		metrics.QualityScore = 60
		metrics.NoiseLevel = 0.4
		metrics.Complexity = 0.5
	} else {
		metrics.QualityScore = 45
		metrics.NoiseLevel = 0.6
		metrics.Complexity = 0.4
	}

	return metrics
}

// analyzePNGQuality 分析PNG质量
func (s *AutoPlusStrategy) analyzePNGQuality(file *MediaFile, pixelDensity float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "graphic"
	metrics.NoiseLevel = 0.0 // PNG无损格式无噪声

	// 使用FFprobe获取PNG详细信息
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		file.Path,
	}

	cmd := exec.Command(s.converter.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return s.fallbackPNGAnalysis(pixelDensity)
	}

	// 解析FFprobe输出
	var probeData struct {
		Streams []struct {
			Width            int    `json:"width"`
			Height           int    `json:"height"`
			PixFmt           string `json:"pix_fmt"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
			ColorSpace       string `json:"color_space"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil || len(probeData.Streams) == 0 {
		return s.fallbackPNGAnalysis(pixelDensity)
	}

	// 安全访问第一个流
	if len(probeData.Streams) == 0 {
		s.converter.logger.Warn("No streams found in PNG probe data, using fallback analysis",
			zap.String("file", file.Path))
		return s.fallbackPNGAnalysis(pixelDensity)
	}
	stream := probeData.Streams[0]

	// 基于像素格式和位深度分析质量
	switch stream.PixFmt {
	case "rgba", "rgba64be", "rgba64le":
		metrics.QualityScore = 100 // RGBA，最高质量
		metrics.Complexity = 0.9
		metrics.CompressionPotential = 0.8 // 透明通道压缩潜力大
	case "rgb24", "rgb48be", "rgb48le":
		metrics.QualityScore = 95 // RGB真彩色
		metrics.Complexity = 0.8
		metrics.CompressionPotential = 0.7
	case "pal8":
		metrics.QualityScore = 70 // 调色板模式
		metrics.Complexity = 0.5
		metrics.CompressionPotential = 0.9 // 调色板PNG压缩潜力很大
	case "gray", "gray16be", "gray16le":
		metrics.QualityScore = 85 // 灰度图
		metrics.Complexity = 0.6
		metrics.CompressionPotential = 0.8
	default:
		metrics.QualityScore = 80
		metrics.Complexity = 0.7
		metrics.CompressionPotential = 0.7
	}

	// 基于位深度调整
	if stream.BitsPerRawSample == "16" {
		metrics.QualityScore += 5 // 16位深度加分
		metrics.CompressionPotential += 0.1
	}

	// 基于像素密度调整复杂度
	if pixelDensity > 3.0 {
		metrics.Complexity += 0.1
		metrics.CompressionPotential -= 0.1 // 高密度图像压缩潜力稍低
	} else if pixelDensity < 0.5 {
		metrics.CompressionPotential += 0.1 // 低密度图像压缩潜力更大
	}

	// 限制范围
	if metrics.QualityScore > 100 {
		metrics.QualityScore = 100
	}
	if metrics.Complexity > 1.0 {
		metrics.Complexity = 1.0
	}
	if metrics.CompressionPotential > 1.0 {
		metrics.CompressionPotential = 1.0
	}

	return metrics
}

// fallbackPNGAnalysis PNG分析回退方案
func (s *AutoPlusStrategy) fallbackPNGAnalysis(pixelDensity float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "graphic"
	metrics.NoiseLevel = 0.0
	metrics.CompressionPotential = 0.7

	if pixelDensity > 2.0 {
		metrics.QualityScore = 90
		metrics.Complexity = 0.8
	} else if pixelDensity > 0.5 {
		metrics.QualityScore = 75
		metrics.Complexity = 0.6
	} else {
		metrics.QualityScore = 60
		metrics.Complexity = 0.5
	}

	return metrics
}

// analyzeGIFQuality 分析GIF质量
func (s *AutoPlusStrategy) analyzeGIFQuality(sizeInMB float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "animation"

	// GIF格式限制，质量通常较低
	metrics.CompressionPotential = 0.8 // 高压缩潜力
	metrics.NoiseLevel = 0.4           // 抖动噪声

	// GIF质量评估
	if sizeInMB > 10 {
		metrics.QualityScore = 30 // 大GIF通常质量差
		metrics.Complexity = 0.3
	} else if sizeInMB > 2 {
		metrics.QualityScore = 50
		metrics.Complexity = 0.5
	} else {
		metrics.QualityScore = 65
		metrics.Complexity = 0.6
	}

	return metrics
}

// analyzeWebPQuality 分析WebP质量
func (s *AutoPlusStrategy) analyzeWebPQuality(file *MediaFile, pixelDensity, sizeInMB float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "mixed"

	// 使用FFprobe获取WebP详细信息
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		file.Path,
	}

	cmd := exec.Command(s.converter.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return s.fallbackWebPAnalysis(pixelDensity)
	}

	// 解析FFprobe输出
	var probeData struct {
		Streams []struct {
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			PixFmt     string `json:"pix_fmt"`
			CodecName  string `json:"codec_name"`
			ColorSpace string `json:"color_space"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil || len(probeData.Streams) == 0 {
		return s.fallbackWebPAnalysis(pixelDensity)
	}

	// 安全访问第一个流
	if len(probeData.Streams) == 0 {
		s.converter.logger.Warn("No streams found in WebP probe data, using fallback analysis",
			zap.String("file", file.Path))
		return s.fallbackWebPAnalysis(pixelDensity)
	}
	stream := probeData.Streams[0]

	// 基于像素格式判断质量
	switch stream.PixFmt {
	case "yuv420p", "yuv422p", "yuv444p":
		// 有损WebP
		if pixelDensity > 0.8 {
			metrics.QualityScore = 85
			metrics.NoiseLevel = 0.15
			metrics.Complexity = 0.8
		} else if pixelDensity > 0.5 {
			metrics.QualityScore = 70
			metrics.NoiseLevel = 0.25
			metrics.Complexity = 0.6
		} else {
			metrics.QualityScore = 55
			metrics.NoiseLevel = 0.35
			metrics.Complexity = 0.5
		}
		metrics.CompressionPotential = 0.3
	case "rgba", "rgb24":
		// 无损WebP
		metrics.QualityScore = 95
		metrics.NoiseLevel = 0.05
		metrics.Complexity = 0.9
		metrics.CompressionPotential = 0.6
		metrics.ContentType = "graphic"
	default:
		return s.fallbackWebPAnalysis(pixelDensity)
	}

	// 基于文件大小调整
	if sizeInMB > 10 {
		if metrics.QualityScore+10 > 100 {
			metrics.QualityScore = 100
		} else {
			metrics.QualityScore += 10
		}
		if metrics.Complexity+0.1 > 1.0 {
			metrics.Complexity = 1.0
		} else {
			metrics.Complexity += 0.1
		}
	} else if sizeInMB < 0.5 {
		if metrics.QualityScore-15 < 20 {
			metrics.QualityScore = 20
		} else {
			metrics.QualityScore -= 15
		}
		if metrics.NoiseLevel+0.2 > 1.0 {
			metrics.NoiseLevel = 1.0
		} else {
			metrics.NoiseLevel += 0.2
		}
	}

	// 基于色彩空间调整
	if stream.ColorSpace == "bt709" || stream.ColorSpace == "bt2020" {
		if metrics.QualityScore+5 > 100 {
			metrics.QualityScore = 100
		} else {
			metrics.QualityScore += 5
		}
		metrics.ContentType = "photo"
	}

	return metrics
}

func (s *AutoPlusStrategy) fallbackWebPAnalysis(pixelDensity float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "mixed"
	metrics.CompressionPotential = 0.4

	if pixelDensity > 0.6 {
		metrics.QualityScore = 75
		metrics.NoiseLevel = 0.2
		metrics.Complexity = 0.7
	} else if pixelDensity > 0.3 {
		metrics.QualityScore = 60
		metrics.NoiseLevel = 0.3
		metrics.Complexity = 0.5
	} else {
		metrics.QualityScore = 45
		metrics.NoiseLevel = 0.4
		metrics.Complexity = 0.4
	}

	return metrics
}

// analyzeGenericQuality 分析通用格式质量
func (s *AutoPlusStrategy) analyzeGenericQuality(sizeInMB float64) ImageQualityMetrics {
	var metrics ImageQualityMetrics
	metrics.ContentType = "mixed"
	metrics.CompressionPotential = 0.6
	metrics.QualityScore = 65 // 中等质量
	metrics.NoiseLevel = 0.3
	metrics.Complexity = 0.5

	return metrics
}

// applyQualityModeLogic 应用品质模式逻辑
func (s *AutoPlusStrategy) applyQualityModeLogic(file *MediaFile) (string, error) {
	// 保存原始模式
	originalMode := s.converter.mode

	// 临时切换到品质模式
	s.converter.mode = ModeQuality
	defer func() {
		// 恢复原始模式
		s.converter.mode = originalMode
	}()

	qualityStrategy := &QualityStrategy{converter: s.converter}
	return qualityStrategy.ConvertImage(file)
}

// ProbeResult 探测结果结构体
type ProbeResult struct {
	Path    string
	Quality int
	Size    int64
}

// applyBalancedOptimization 平衡优化算法（严格按照README规范实现）
func (s *AutoPlusStrategy) applyBalancedOptimization(file *MediaFile) (string, error) {
	// 自动模式+：应用平衡优化算法

	// 步骤 1: 无损重新包装优先 - 无损转换成功就使用，不管体积
	losslessRepackResult, err := s.attemptLosslessRepackaging(file)
	if err == nil && losslessRepackResult != "" {
		// 平衡优化：无损重包成功
		return losslessRepackResult, nil
	}

	// 步骤 2: 数学无损压缩 - 无损转换成功就使用，不管体积
	mathLosslessResult, err := s.attemptMathematicalLossless(file)
	if err == nil && mathLosslessResult != "" {
		// 平衡优化：数学无损压缩成功
		return mathLosslessResult, nil
	}

	// 步骤 3: 有损探测
	probeResults := []ProbeResult{}
	fileQuality := s.getFileQuality(file)

	var qualityTargets []int
	if fileQuality > 70 {
		qualityTargets = []int{90, 85, 75}
	} else {
		qualityTargets = []int{60, 55}
	}

	for _, quality := range qualityTargets {
		result, err := s.attemptLossyCompression(file, quality)
		if err == nil && result != file.Path { // 避免把原文件路径当作探测结果
			probeResults = append(probeResults, ProbeResult{
				Path:    result,
				Quality: quality,
				Size:    s.getFileSize(result),
			})
			// 平衡优化：有损探测成功
		}
	}

	// 步骤 4: 最终决策
	if len(probeResults) == 0 {
		// 平衡优化：无优化可能，跳过
		return file.Path, nil // skip(file, "No optimization possible")
	}

	bestResult := s.selectBestProbeResult(probeResults, s.getFileSize(file.Path))
	if bestResult != nil {
		// 关键修复：根据最佳结果的实际扩展名确定输出路径
		// bestResult.Path是临时文件，需要根据其扩展名确定最终输出扩展名
		var targetExt string
		if strings.HasSuffix(bestResult.Path, ".avif") {
			targetExt = ".avif"
		} else {
			targetExt = ".jxl"
		}

		finalOutputPath := s.converter.getOutputPath(file, targetExt)

		// 确保输出目录存在（统一走文件操作助手）
		if err := s.converter.fileOpHandler.EnsureOutputDirectory(finalOutputPath); err != nil {
			s.converter.logger.Error("平衡优化：创建输出目录失败",
				zap.String("dir", filepath.Dir(finalOutputPath)),
				zap.Error(err))
			// 清理所有临时文件
			for _, result := range probeResults {
				os.Remove(result.Path)
			}
			return "", s.errorHandler.WrapError("failed to create output directory", err)
		}

		// 将最佳临时文件移动到最终输出路径（原地使用原子替换，非原地直接重命名）
		isInPlace := s.converter.config.Output.DirectoryTemplate == ""
		if isInPlace {
			if err := s.converter.fileOpHandler.AtomicFileReplace(bestResult.Path, finalOutputPath, true); err != nil {
				s.converter.logger.Error("平衡优化：原子替换失败",
					zap.String("from", bestResult.Path),
					zap.String("to", finalOutputPath),
					zap.Error(err))
				for _, result := range probeResults {
					os.Remove(result.Path)
				}
				return "", s.errorHandler.WrapError("failed to atomically replace best result to final path", err)
			}
		} else {
			if err := os.Rename(bestResult.Path, finalOutputPath); err != nil {
				s.converter.logger.Error("平衡优化：文件移动失败",
					zap.String("from", bestResult.Path),
					zap.String("to", finalOutputPath),
					zap.Error(err))
				for _, result := range probeResults {
					os.Remove(result.Path)
				}
				return "", s.errorHandler.WrapError("failed to move best result to final path", err)
			}
		}

		// 平衡优化：选择最佳结果

		// 清理其他探测结果的临时文件
		for _, result := range probeResults {
			if result.Path != bestResult.Path {
				os.Remove(result.Path)
			}
		}

		return finalOutputPath, nil
	} else {
		// 清理所有探测结果的临时文件
		for _, result := range probeResults {
			os.Remove(result.Path)
		}
		// 平衡优化：无显著体积减小，跳过
		return file.Path, nil // skip(file, "No significant size reduction")
	}
}

// attemptLosslessRepackaging 尝试无损重新包装
// 第一步：容器格式优化，不改变编码数据
func (s *AutoPlusStrategy) attemptLosslessRepackaging(file *MediaFile) (string, error) {
	ext := strings.ToLower(file.Extension)

	// 平衡优化步骤1：无损重新包装

	// 无损重新包装：仅改变容器格式，不重新编码像素数据
	switch ext {
	case ".jpg", ".jpeg":
		// JPEG -> JXL 无损重新包装（lossless_jpeg=1）
		// 这是真正的重新包装，不重新编码JPEG数据
		return s.converter.convertJPEGToJXLRepackaging(file)
	case ".png":
		// PNG -> JXL 无损重新包装
		// 保持PNG的无损特性，仅改变容器
		return s.converter.convertPNGToJXLRepackaging(file)
	case ".webp":
		// WebP文件跳过处理，避免性能瓶颈
		// 跳过WebP文件的无损重新包装（性能优化）
		return "", s.errorHandler.WrapError("WebP format skipped for performance optimization", nil)
	default:
		// 其他格式不支持无损重新包装
		return "", s.errorHandler.WrapError("format "+ext+" does not support lossless repackaging", nil)
	}
}

// attemptMathematicalLossless 尝试数学无损压缩
// 第二步：重新编码但保持数学无损
func (s *AutoPlusStrategy) attemptMathematicalLossless(file *MediaFile) (string, error) {
	ext := strings.ToLower(file.Extension)

	// 平衡优化步骤2：数学无损压缩

	// 数学无损压缩：重新编码像素数据但保持完全无损
	switch ext {
	case ".jpg", ".jpeg":
		// JPEG -> JXL 数学无损（distance=0，重新编码像素）
		return s.converter.convertToJXLMathematicalLossless(file)
	case ".png":
		// PNG动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// 动图 -> AVIF（无损）
			// PNG动图转换为AVIF
			return s.converter.convertToAVIF(file, 100) // 无损AVIF
		} else {
			// PNG -> JXL 数学无损
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	case ".webp":
		// WebP动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// 动图 -> AVIF（无损）
			// WebP动图转换为AVIF
			return s.converter.convertToAVIF(file, 100) // 无损AVIF
		} else {
			// WebP -> JXL 数学无损
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	case ".gif":
		// GIF动图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// 动图 -> AVIF（无损）
			// GIF动图转换为AVIF
			return s.converter.convertToAVIF(file, 100) // 无损AVIF
		} else {
			// 静图 -> JXL 数学无损
			// GIF静图转换为JXL
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	case ".avif":
		// AVIF已是目标格式，跳过转换
		return file.Path, nil
	case ".jxl":
		// JXL动静图检测：动图转AVIF，静图保持JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换动态JXL为AVIF (数学无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			// Auto+模式：静态JXL已是目标格式，跳过转换
			return file.Path, nil
		}
	case ".apng":
		// APNG动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换动态APNG为AVIF (数学无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			// Auto+模式：转换静态APNG为JXL (数学无损)
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	case ".tiff", ".tif":
		// TIFF动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换动态TIFF为AVIF (数学无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			// Auto+模式：转换静态TIFF为JXL (数学无损)
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	case ".heif", ".heic":
		// HEIF/HEIC Live Photo检测：Live Photo跳过，静态图转JXL
		detector := NewFileTypeDetector(s.converter.config, s.converter.logger, s.converter.toolManager)
		details, err := detector.DetectFileType(file.Path)
		if err == nil && details.FileType == FileTypeLivePhoto {
			// Auto+模式：Live Photo跳过处理
			return file.Path, nil
		} else {
			// Auto+模式：转换静态HEIF/HEIC为JXL (数学无损)
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	default:
		// 其他格式检测动静图：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换其他动态格式为AVIF (数学无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			// Auto+模式：转换其他静态格式为JXL (数学无损)
			return s.converter.convertToJXLMathematicalLossless(file)
		}
	}
}

// getFileQuality 获取文件质量（基于智能分析）
func (s *AutoPlusStrategy) getFileQuality(file *MediaFile) int {
	// 使用智能图像质量分析系统
	metrics := s.analyzeImageMetrics(file)

	// 将0-100的质量分数转换为压缩质量参数
	qualityScore := int(metrics.QualityScore)

	// 智能质量分析

	return qualityScore
}

// attemptLossyCompression 尝试有损压缩
// attemptLossyCompression 尝试有损压缩
func (s *AutoPlusStrategy) attemptLossyCompression(file *MediaFile, quality int) (string, error) {
	ext := strings.ToLower(file.Extension)

	// 为探测阶段创建带质量后缀的临时输入副本路径（通过硬链接/符号链接，无需拷贝）
	dir := filepath.Dir(file.Path)
	base := filepath.Base(file.Path)
	extName := filepath.Ext(base)
	name := strings.TrimSuffix(base, extName)
	probeBase := fmt.Sprintf("%s._probe_q%d%s", name, quality, extName)
	probePath := filepath.Join(dir, probeBase)

	// 预清理同名残留
	_ = s.converter.fileOpHandler.SafeRemoveFile(probePath)

	// 优先创建硬链接，失败则退回符号链接
	linkCreated := false
	if err := os.Link(file.Path, probePath); err == nil {
		linkCreated = true
	} else if err := os.Symlink(file.Path, probePath); err == nil {
		linkCreated = true
	} else {
		s.converter.logger.Warn("创建探测链接失败，将回退为直接使用源文件（可能导致输出命名冲突）",
			zap.String("source", file.Path),
			zap.String("probe", probePath))
	}

	if linkCreated {
		defer func() {
			if err := os.Remove(probePath); err != nil {
				s.converter.logger.Debug("清理探测链接失败", zap.String("probe", probePath), zap.Error(err))
			}
		}()
	}

	probeFile := *file
	if linkCreated {
		probeFile.Path = probePath
	} else {
		// 链接创建失败时，保持原路径，输出将可能与其他探测产出冲突（极少数情况）
		probeFile.Path = file.Path
	}

	// 根据格式选择最佳有损压缩方法（输出将落在带_probe后缀的独立文件中）
	switch ext {
	case ".jpg", ".jpeg":
		// 使用AVIF进行有损压缩
		return s.converter.convertToAVIF(&probeFile, quality)
	case ".png":
		// PNG使用JXL进行有损压缩（JXL支持透明度且效率更优）
		return s.converter.convertToJXL(&probeFile, quality)
	case ".webp":
		// WebP动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// 动图 -> AVIF（有损）
			// WebP动图有损压缩为AVIF
			return s.converter.convertToAVIF(&probeFile, quality)
		} else {
			// 静图 -> JXL（有损）
			// WebP静图有损压缩为JXL
			return s.converter.convertToJXL(&probeFile, quality)
		}
	case ".gif":
		// GIF动图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// 动图 -> AVIF（有损）
			// GIF动图有损压缩为AVIF
			return s.converter.convertToAVIF(&probeFile, quality)
		} else {
			// 静图 -> JXL（有损）
			// GIF静图有损压缩为JXL
			return s.converter.convertToJXL(&probeFile, quality)
		}
	case ".avif":
		// AVIF已是目标格式，跳过转换
		return file.Path, nil
	case ".jxl":
		// JXL动静图检测：动图转AVIF，静图保持JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换动态JXL为AVIF (有损)
			return s.converter.convertToAVIF(&probeFile, quality)
		} else {
			// Auto+模式：静态JXL已是目标格式，跳过转换
			return file.Path, nil
		}
	case ".apng":
		// APNG动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换动态APNG为AVIF (有损)
			return s.converter.convertToAVIF(&probeFile, quality)
		} else {
			// Auto+模式：转换静态APNG为JXL (有损)
			return s.converter.convertToJXL(&probeFile, quality)
		}
	case ".tiff", ".tif":
		// TIFF动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换动态TIFF为AVIF (有损)
			return s.converter.convertToAVIF(&probeFile, quality)
		} else {
			// Auto+模式：转换静态TIFF为JXL (有损)
			return s.converter.convertToJXL(&probeFile, quality)
		}
	case ".heif", ".heic":
		// HEIF/HEIC Live Photo检测：Live Photo跳过，静态图转JXL
		detector := NewFileTypeDetector(s.converter.config, s.converter.logger, s.converter.toolManager)
		details, err := detector.DetectFileType(file.Path)
		if err == nil && details.FileType == FileTypeLivePhoto {
			// Auto+模式：Live Photo跳过处理
			return file.Path, nil
		} else {
			// Auto+模式：转换静态HEIF/HEIC为JXL (有损)
			return s.converter.convertToJXL(&probeFile, quality)
		}
	default:
		// 其他格式检测动静图：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			// Auto+模式：转换其他动态格式为AVIF (有损)
			return s.converter.convertToAVIF(&probeFile, quality)
		} else {
			// Auto+模式：转换其他静态格式为JXL (有损)
			return s.converter.convertToJXL(&probeFile, quality)
		}
	}
}

// getFileSize 获取文件大小
func (s *AutoPlusStrategy) getFileSize(path string) int64 {
	if stat, err := os.Stat(path); err == nil {
		return stat.Size()
	}
	return 0
}

// selectOptimalStrategy 基于图像特征选择最优转换策略
func (s *AutoPlusStrategy) selectOptimalStrategy(metrics ImageQualityMetrics, file *MediaFile) string {
	ext := strings.ToLower(file.Extension)

	// 高质量原画：仅无损优化
	if metrics.QualityScore > 90 && metrics.CompressionPotential < 0.3 {
		return "lossless_only"
	}

	// 高压缩潜力且非关键格式：优先有损
	if metrics.CompressionPotential > 0.7 && (ext == ".png" || ext == ".bmp" || ext == ".tiff") {
		return "lossy_preferred"
	}

	// 低质量或高噪声：优先有损
	if metrics.QualityScore < 60 || metrics.NoiseLevel > 0.6 {
		return "lossy_preferred"
	}

	// 已经是现代格式且质量不错：可能跳过
	if (ext == ".webp" || ext == ".avif" || ext == ".jxl") && metrics.QualityScore > 70 {
		return "skip"
	}

	// 默认渐进式优化
	return "progressive"
}

// selectBestProbeResult 选择最佳探测结果
func (s *AutoPlusStrategy) selectBestProbeResult(results []ProbeResult, originalSize int64) *ProbeResult {
	if len(results) == 0 {
		return nil
	}

	// 智能决策逻辑：选择空间减小至少1KB或减少比例明显的版本
	const minSizeReduction = 1024  // 1KB
	const minReductionRatio = 0.05 // 5%

	var bestResult *ProbeResult
	bestScore := 0.0

	for i := range results {
		result := &results[i]

		// 检查是否满足最小减小要求
		sizeReduction := originalSize - result.Size
		if sizeReduction < minSizeReduction {
			continue // 减小不足1KB，跳过
		}

		reductionRatio := float64(sizeReduction) / float64(originalSize)
		if reductionRatio < minReductionRatio {
			continue // 减少比例不足5%，跳过
		}

		// 计算综合评分：平衡质量和压缩比
		// 评分 = 压缩比权重 * 压缩比 + 质量权重 * (质量/100)
		compressionWeight := 0.7
		qualityWeight := 0.3

		score := compressionWeight*reductionRatio + qualityWeight*(float64(result.Quality)/100.0)

		if score > bestScore {
			bestScore = score
			bestResult = result
		}

		// 平衡优化：评估探测结果
	}

	if bestResult != nil {
		// 平衡优化：选择最佳结果
	}

	return bestResult
}

// === 品质模式 实现 ===

func (s *QualityStrategy) GetName() string {
	return "quality (无损优先)"
}

func (s *QualityStrategy) ConvertImage(file *MediaFile) (string, error) {
	s.converter.logger.Debug("品质模式图片转换开始", zap.String("file", file.Path), zap.String("extension", file.Extension))
	ext := strings.ToLower(file.Extension)

	// 检查是否已经是目标格式
	if s.converter.IsTargetFormat(ext) {
		s.converter.logger.Debug("文件已是目标格式，跳过转换", zap.String("file", file.Path))
		// 品质模式：文件已是目标格式，跳过转换
		return file.Path, nil
	}

	// 强制转换为目标格式，采用数学无损压缩
	// 目标格式: 静图: JXL, 动图: AVIF (无损), 视频: MOV (仅重包装)
	switch ext {
	case ".jpg", ".jpeg":
		// JPEG必须使用cjxl的lossless_jpeg=1参数
		s.converter.logger.Debug("开始转换JPEG到JXL", zap.String("file", file.Path))
		return s.converter.convertToJXLLossless(file)
	case ".png":
		// PNG动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("PNG是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换动态PNG为AVIF (无损)
			return s.converter.convertToAVIF(file, 100) // 动图AVIF无损
		} else {
			s.converter.logger.Debug("PNG是静图，转换为JXL", zap.String("file", file.Path))
			// PNG无损转换为JXL（JXL完全支持透明度且压缩效率更优）
			// 品质模式：转换静态PNG为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	case ".gif":
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("GIF是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换动图为AVIF (无损)
			return s.converter.convertToAVIF(file, 100) // 动图AVIF无损
		} else {
			s.converter.logger.Debug("GIF是静图，转换为JXL", zap.String("file", file.Path))
			// 品质模式：转换静态GIF为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	case ".webp":
		// WebP动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("WebP是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换动态WebP为AVIF (无损)
			return s.converter.convertToAVIF(file, 100) // 动图AVIF无损
		} else {
			s.converter.logger.Debug("WebP是静图，转换为JXL", zap.String("file", file.Path))
			// 品质模式：转换静态WebP为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	case ".avif":
		// AVIF已是目标格式，跳过转换
		s.converter.logger.Debug("AVIF已是目标格式，跳过转换", zap.String("file", file.Path))
		return file.Path, nil
	case ".jxl":
		// JXL动静图检测：动图转AVIF，静图保持JXL
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("JXL是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换动态JXL为AVIF (无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			s.converter.logger.Debug("JXL是静图，已是目标格式，跳过转换", zap.String("file", file.Path))
			// 品质模式：静态JXL已是目标格式，跳过转换
			return file.Path, nil
		}
	case ".apng":
		// APNG动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("APNG是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换动态APNG为AVIF (无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			s.converter.logger.Debug("APNG是静图，转换为JXL", zap.String("file", file.Path))
			// 品质模式：转换静态APNG为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	case ".tiff", ".tif":
		// TIFF动静图检测：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("TIFF是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换动态TIFF为AVIF (无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			s.converter.logger.Debug("TIFF是静图，转换为JXL", zap.String("file", file.Path))
			// 品质模式：转换静态TIFF为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	case ".heif", ".heic":
		// HEIF/HEIC Live Photo检测：Live Photo跳过，静态图转JXL
		s.converter.logger.Debug("处理HEIF/HEIC文件", zap.String("file", file.Path))
		detector := NewFileTypeDetector(s.converter.config, s.converter.logger, s.converter.toolManager)
		details, err := detector.DetectFileType(file.Path)
		if err == nil && details.FileType == FileTypeLivePhoto {
			s.converter.logger.Debug("HEIF/HEIC是Live Photo，跳过处理", zap.String("file", file.Path))
			// 品质模式：Live Photo跳过处理
			return file.Path, nil
		} else {
			s.converter.logger.Debug("HEIF/HEIC是静态图，转换为JXL", zap.String("file", file.Path))
			// 品质模式：转换静态HEIF/HEIC为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	default:
		// 其他格式检测动静图：动图转AVIF，静图转JXL
		if s.converter.isAnimated(file.Path) {
			s.converter.logger.Debug("其他格式是动图，转换为AVIF", zap.String("file", file.Path))
			// 品质模式：转换其他动态格式为AVIF (无损)
			return s.converter.convertToAVIF(file, 100)
		} else {
			s.converter.logger.Debug("其他格式是静图，转换为JXL", zap.String("file", file.Path))
			// 品质模式：转换其他静态格式为JXL (无损)
			return s.converter.convertToJXLLossless(file)
		}
	}
}

func (s *QualityStrategy) ConvertVideo(file *MediaFile) (string, error) {
	// 视频重包装为MOV
	// 品质模式：视频重包装为MOV
	return s.converter.convertToMOV(file)
}

// ConvertAudio方法已删除 - 根据README要求，本程序不处理音频文件

// === 表情包模式 实现 ===

func (s *EmojiStrategy) GetName() string {
	return "emoji (极限压缩)"
}

func (s *EmojiStrategy) ConvertImage(file *MediaFile) (string, error) {
	ext := strings.ToLower(file.Extension)

	// 检查是否已经是目标格式
	if s.converter.IsTargetFormat(ext) {
		// 表情包模式：文件已是目标格式，跳过转换
		return file.Path, nil
	}

	// 跳过已经是高效格式的文件（AVIF, JXL, WebP）
	// 这些格式不需要进一步压缩，avifenc也不支持JXL作为输入
	if ext == ".webp" || ext == ".avif" || ext == ".jxl" {
		// 表情包模式跳过高效格式文件
		return file.Path, nil
	}

	// 根据README规定：表情包模式下工具链优先级
	// 静态图: 使用 AVIF 官方组件 (avifenc)
	// 动图: 使用 ffmpeg 转换为 AVIF
	switch ext {
	case ".gif":
		if s.converter.isAnimated(file.Path) {
			// 动图使用ffmpeg处理
			// 表情包模式：动图使用ffmpeg处理
			return s.converter.ConvertToAVIFAnimated(file) // 使用ffmpeg
		} else {
			// 静态GIF使用极限压缩策略
			// 表情包模式：静态图使用极限压缩策略
			return s.tryAggressiveAVIF(file)
		}
	default:
		// 所有其他静态图片使用极限压缩策略
		// 表情包模式：静态图使用极限压缩策略
		return s.tryAggressiveAVIF(file)
	}
}

func (s *EmojiStrategy) ConvertVideo(file *MediaFile) (string, error) {
	// 根据README规定：表情包模式下视频文件必须被直接跳过，不得进行任何处理
	// 表情包模式跳过视频文件
	return file.Path, nil
}

// ConvertAudio方法已删除 - 根据README要求，本程序不处理音频文件

// tryAggressiveAVIF 尝试激进的AVIF压缩
func (s *EmojiStrategy) tryAggressiveAVIF(file *MediaFile) (string, error) {
	// 优先尝试无损压缩和重包装
	if result, err := s.converter.convertToAVIF(file, 100); err == nil {
		if s.checkAggressiveReduction(file.Path, result) {
			return result, nil
		}
	}

	// 比平衡优化更激进的有损压缩范围进行探底
	// 只要转换后文件体积相比原图减小 7%-13% 或更多，即视为成功
	aggressiveLevels := []int{60, 50, 40, 30, 25, 20}

	originalStat, err := os.Stat(file.Path)
	if err != nil {
		return file.Path, s.errorHandler.WrapError("无法获取原文件信息", err)
	}
	originalSize := originalStat.Size()

	for _, quality := range aggressiveLevels {
		// 尝试AVIF质量级别

		result, err := s.converter.convertToAVIF(file, quality)
		if err != nil {
			s.converter.logger.Warn("AVIF转换失败，尝试下一个质量级别",
				zap.String("file", file.Path),
				zap.Int("quality", quality),
				zap.Error(err))
			continue
		}

		// 检查转换后的文件
		newStat, err := os.Stat(result)
		if err != nil {
			s.converter.logger.Warn("无法获取转换后文件信息",
				zap.String("file", result),
				zap.Error(err))
			continue
		}

		newSize := newStat.Size()

		// 检查压缩效果：优先选择7%-13%范围，如果没有则选择最接近的
		reductionRatio := float64(originalSize-newSize) / float64(originalSize)
		// 检查压缩比例

		// 理想范围：7%-13%
		if reductionRatio >= 0.07 && reductionRatio <= 0.13 {
			// 找到理想的AVIF压缩级别
			return result, nil
		}

		// 如果压缩比例超过13%但文件确实变小了，也接受（表情包模式追求极限压缩）
		if reductionRatio > 0.13 && quality == 60 { // 使用最高质量的结果
			// 使用高压缩比AVIF结果
			return result, nil
		}
	}

	return file.Path, s.errorHandler.WrapError("无法找到合适的压缩级别", nil)
}

// checkAggressiveReduction 检查是否达到7%-13%的体积减小
func (s *EmojiStrategy) checkAggressiveReduction(originalPath, newPath string) bool {
	originalStat, err1 := os.Stat(originalPath)
	newStat, err2 := os.Stat(newPath)

	if err1 != nil || err2 != nil {
		return false
	}

	// 计算减小比例
	reductionRatio := float64(originalStat.Size()-newStat.Size()) / float64(originalStat.Size())

	// 检查是否达到7%-13%的体积减小
	return reductionRatio >= 0.07 && reductionRatio <= 0.13
}

// convertImage 执行图像转换
func (s *AutoPlusStrategy) convertImage(inputPath, outputPath string, targetFormat string) error {
	// 验证输入文件
	if _, err := os.Stat(inputPath); err != nil {
		return fmt.Errorf("输入文件不存在: %v", err)
	}

	// 确保输出目录存在
	outputDir := GlobalPathUtils.GetDirName(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("无法创建输出目录: %v", err)
	}

	// 标准化目标格式
	targetFormat = strings.ToLower(targetFormat)
	if !strings.HasPrefix(targetFormat, ".") {
		targetFormat = "." + targetFormat
	}

	// 根据目标格式选择转换策略
	switch targetFormat {
	case ".avif":
		return s.convertToAVIF(inputPath, outputPath)
	case ".jxl":
		return s.convertToJXL(inputPath, outputPath)
	case ".webp":
		return s.convertToWebP(inputPath, outputPath)
	case ".png":
		return s.convertToPNG(inputPath, outputPath)
	case ".jpg", ".jpeg":
		return s.convertToJPEG(inputPath, outputPath)
	default:
		return fmt.Errorf("不支持的格式: %s", targetFormat)
	}
}

// convertToAVIF 转换到AVIF格式
func (s *AutoPlusStrategy) convertToAVIF(inputPath, outputPath string) error {
	// 构建ffmpeg命令
	cmd := exec.Command("ffmpeg", "-i", inputPath, 
		"-c:v", "libaom-av1", 
		"-crf", "35", 
		"-b:v", "0", 
		"-pix_fmt", "yuv420p", 
		"-an", 
		outputPath)

	// 执行命令
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("AVIF转换失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// convertToJXL 转换到JXL格式
func (s *AutoPlusStrategy) convertToJXL(inputPath, outputPath string) error {
	// 构建cjxl命令，使用无损转换参数
	cmd := exec.Command("cjxl", inputPath, outputPath, "--distance=0", "-e", "9")

	// 执行命令
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("JXL转换失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// convertToWebP 转换到WebP格式
func (s *AutoPlusStrategy) convertToWebP(inputPath, outputPath string) error {
	cmd := exec.Command("cwebp", inputPath, "-q", "80", "-o", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("WebP转换失败: %v, 输出: %s", err, string(output))
	}
	return nil
}

// convertToPNG 转换到PNG格式
func (s *AutoPlusStrategy) convertToPNG(inputPath, outputPath string) error {
	cmd := exec.Command("ffmpeg", "-i", inputPath, "-c:v", "png", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("PNG转换失败: %v, 输出: %s", err, string(output))
	}
	return nil
}

// convertToJPEG 转换到JPEG格式
func (s *AutoPlusStrategy) convertToJPEG(inputPath, outputPath string) error {
	cmd := exec.Command("ffmpeg", "-i", inputPath, "-c:v", "mjpeg", "-q:v", "2", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("JPEG转换失败: %v, 输出: %s", err, string(output))
	}
	return nil
}
