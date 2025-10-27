package converter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DetailedReport 详细转换报告
type DetailedReport struct {
	StartTime        time.Time              `json:"start_time"`
	EndTime          time.Time              `json:"end_time"`
	Duration         time.Duration          `json:"duration"`
	SourceDirectory  string                 `json:"source_directory"`
	TotalFiles       int                    `json:"total_files"`
	ProcessedFiles   int                    `json:"processed_files"`
	SuccessfulFiles  int                    `json:"successful_files"`
	FailedFiles      int                    `json:"failed_files"`
	SkippedFiles     int                    `json:"skipped_files"`
	TotalSizeBefore  int64                  `json:"total_size_before"`
	TotalSizeAfter   int64                  `json:"total_size_after"`
	SpaceSaved       int64                  `json:"space_saved"`
	CompressionRatio float64                `json:"compression_ratio"`
	ConversionMode   string                 `json:"conversion_mode"`
	FileDetails      []FileConversionDetail `json:"file_details"`
	FormatSummary    map[string]FormatStats `json:"format_summary"`
	SystemInfo       SystemInfo             `json:"system_info"`
	Errors           []ConversionError      `json:"errors"`
}

// FileConversionDetail 文件转换详细信息
type FileConversionDetail struct {
	OriginalPath     string        `json:"original_path"`
	OutputPath       string        `json:"output_path"`
	OriginalSize     int64         `json:"original_size"`
	OutputSize       int64         `json:"output_size"`
	CompressionRatio float64       `json:"compression_ratio"`
	OriginalFormat   string        `json:"original_format"`
	OutputFormat     string        `json:"output_format"`
	ProcessingTime   time.Duration `json:"processing_time"`
	Method           string        `json:"method"`
	Success          bool          `json:"success"`
	Skipped          bool          `json:"skipped"`               // 文件是否被跳过
	SkipReason       string        `json:"skip_reason,omitempty"` // 跳过原因
	Error            string        `json:"error,omitempty"`
	MediaInfo        *MediaInfo    `json:"media_info,omitempty"`
}

// MediaInfo 媒体文件详细信息
type MediaInfo struct {
	// README中定义的字段
	FullPath       string    `json:"full_path,omitempty"`       // 规范化后的绝对路径
	FileSize       int64     `json:"file_size,omitempty"`       // 文件大小（字节）
	ModTime        time.Time `json:"mod_time,omitempty"`        // 文件最后修改时间
	SHA256Hash     string    `json:"sha256_hash,omitempty"`     // 文件内容的 SHA256 哈希值，用于状态跟踪
	Codec          string    `json:"codec,omitempty"`           // 主要编解码器名称
	FrameCount     int       `json:"frame_count,omitempty"`     // 帧数，用于区分动图和静图
	IsAnimated     bool      `json:"is_animated,omitempty"`     // 是否为动图或视频
	IsCorrupted    bool      `json:"is_corrupted,omitempty"`    // 是否检测为损坏文件
	InitialQuality int       `json:"initial_quality,omitempty"` // 预估的初始质量（1-100）

	// 怀疑度评分制度 - README要求的关键功能
	SuspicionScore   int      `json:"suspicion_score,omitempty"`   // 可疑特征累计评分（0-100），达到阈值才进行深度分析
	SuspicionReasons []string `json:"suspicion_reasons,omitempty"` // 可疑特征列表，用于调试和日志

	// Magic Number 检测和扩展名修正字段
	DetectedFormat           string `json:"detected_format,omitempty"`            // 通过Magic Number检测到的实际格式
	NeedsExtensionCorrection bool   `json:"needs_extension_correction,omitempty"` // 是否需要扩展名修正

	// 新增字段用于批处理器
	Container               string `json:"container,omitempty"`                 // 容器格式
	IsCodecIncompatible     bool   `json:"is_codec_incompatible,omitempty"`     // 编解码器是否不兼容
	IsContainerIncompatible bool   `json:"is_container_incompatible,omitempty"` // 容器是否不兼容

	// 现有字段
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	Bitrate    int     `json:"bitrate,omitempty"`
	FrameRate  float64 `json:"frame_rate,omitempty"`
	HasAudio   bool    `json:"has_audio,omitempty"`
	ColorSpace string  `json:"color_space,omitempty"`
}

// FormatStats 格式统计
type FormatStats struct {
	Count              int     `json:"count"`
	TotalSizeBefore    int64   `json:"total_size_before"`
	TotalSizeAfter     int64   `json:"total_size_after"`
	AverageCompression float64 `json:"average_compression"`
}

// SystemInfo 系统信息
type SystemInfo struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPUCores    int    `json:"cpu_cores"`
	Concurrency int    `json:"concurrency"`
	MemoryUsed  uint64 `json:"memory_used_mb"`
}

// ConversionError 转换错误
type ConversionError struct {
	File  string    `json:"file"`
	Error string    `json:"error"`
	Time  time.Time `json:"time"`
}

// generateReport 生成详细转换报告
func (c *Converter) generateReport() error {
	// 生成详细转换报告

	endTime := time.Now()
	startTime := endTime.Add(-c.stats.TotalDuration)

	report := DetailedReport{
		StartTime:        startTime,
		EndTime:          endTime,
		Duration:         c.stats.TotalDuration,
		SourceDirectory:  c.getSourceDirectory(),
		TotalFiles:       c.stats.TotalFiles,
		ProcessedFiles:   c.stats.ProcessedFiles,
		SuccessfulFiles:  c.stats.SuccessfulFiles,
		FailedFiles:      c.stats.FailedFiles,
		SkippedFiles:     c.stats.SkippedFiles,
		TotalSizeBefore:  c.stats.TotalSize,
		TotalSizeAfter:   c.stats.CompressedSize,
		SpaceSaved:       c.stats.TotalSize - c.stats.CompressedSize,
		CompressionRatio: c.stats.CompressionRatio,
		ConversionMode:   string(c.mode),
		FileDetails:      c.generateFileDetails(),
		FormatSummary:    c.generateFormatSummary(),
		SystemInfo:       c.getSystemInfo(),
		Errors:           c.collectErrors(),
	}

	// 保存JSON报告
	if err := c.saveJSONReport(report); err != nil {
		c.logger.Error("保存JSON报告失败", zap.Error(err))
	}

	// 生成可读报告
	if err := c.generateReadableReport(report); err != nil {
		c.logger.Error("生成可读报告失败", zap.Error(err))
	}

	// 验证转换结果
	if err := c.verifyConversionResults(); err != nil {
		c.logger.Error("验证转换结果失败", zap.Error(err))
	}

	return nil
}

// generateFileDetails 生成文件详细信息
func (c *Converter) generateFileDetails() []FileConversionDetail {
	var details []FileConversionDetail

	for _, result := range c.results {
		detail := FileConversionDetail{
			OriginalPath:     result.OriginalFile.Path,
			OriginalSize:     result.OriginalSize,
			OutputSize:       result.CompressedSize,
			CompressionRatio: result.CompressionRatio,
			OriginalFormat:   result.OriginalFile.Extension,
			ProcessingTime:   result.Duration,
			Method:           result.Method,
			Success:          result.Success,
			Skipped:          result.Skipped,
			SkipReason:       result.SkipReason,
		}

		// 处理跳过的文件
		if result.Skipped {
			// 跳过的文件输出路径为原路径，输出格式为原格式
			detail.OutputPath = result.OriginalFile.Path
			detail.OutputFormat = result.OriginalFile.Extension
		} else {
			// 正常转换的文件
			detail.OutputPath = result.OutputPath
			detail.OutputFormat = GlobalPathUtils.GetExtension(result.OutputPath)
		}

		if result.Error != nil {
			detail.Error = result.Error.Error()
		}

		// 获取媒体信息
		if result.Success && !result.Skipped && (result.OriginalFile.Type == TypeImage || result.OriginalFile.Type == TypeVideo) {
			if mediaInfo := c.getMediaInfo(result.OutputPath); mediaInfo != nil {
				detail.MediaInfo = mediaInfo
			}
		}

		details = append(details, detail)
	}

	return details
}

// generateFormatSummary 生成格式统计
func (c *Converter) generateFormatSummary() map[string]FormatStats {
	formatStats := make(map[string]FormatStats)

	for _, result := range c.results {
		if !result.Success || result.Skipped {
			continue
		}

		format := result.OriginalFile.Extension
		stats := formatStats[format]

		stats.Count++
		stats.TotalSizeBefore += result.OriginalSize
		stats.TotalSizeAfter += result.CompressedSize

		if stats.TotalSizeBefore > 0 {
			stats.AverageCompression = float64(stats.TotalSizeBefore-stats.TotalSizeAfter) / float64(stats.TotalSizeBefore) * 100
		}

		formatStats[format] = stats
	}

	return formatStats
}

// getMediaInfo 获取媒体文件信息
func (c *Converter) getMediaInfo(filePath string) *MediaInfo {
	if !c.isMediaFile(filePath) {
		return nil
	}

	// 首先尝试从内存池获取缓存的媒体信息
	if cachedMediaInfo := c.memoryPool.GetMediaInfo(); cachedMediaInfo != nil && cachedMediaInfo.FullPath == filePath {
		return cachedMediaInfo
	}

	// 使用FFprobe获取媒体信息
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	}

	cmd := exec.Command(c.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	// 解析FFprobe输出
	var probeData struct {
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			CodecName  string `json:"codec_name"`
			CodecType  string `json:"codec_type"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
			ColorSpace string `json:"color_space"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil {
		return nil
	}

	mediaInfo := &MediaInfo{}

	// 填充基本文件信息
	mediaInfo.FullPath = filePath

	// 获取文件信息
	if fileInfo, err := os.Stat(filePath); err == nil {
		mediaInfo.FileSize = fileInfo.Size()
		mediaInfo.ModTime = fileInfo.ModTime()
	}

	// 解析格式信息
	if probeData.Format.Duration != "" {
		if duration, err := strconv.ParseFloat(probeData.Format.Duration, 64); err == nil {
			mediaInfo.Duration = duration
		}
	}

	if probeData.Format.BitRate != "" {
		if bitrate, err := strconv.Atoi(probeData.Format.BitRate); err == nil {
			mediaInfo.Bitrate = bitrate
		}
	}

	// 解析流信息
	for _, stream := range probeData.Streams {
		switch stream.CodecType {
		case "video":
			mediaInfo.Width = stream.Width
			mediaInfo.Height = stream.Height
			mediaInfo.Codec = stream.CodecName
			mediaInfo.ColorSpace = stream.ColorSpace

			// 解析帧率
			if stream.RFrameRate != "" && strings.Contains(stream.RFrameRate, "/") {
				parts := strings.Split(stream.RFrameRate, "/")
				if len(parts) == 2 {
					if num, err := strconv.ParseFloat(parts[0], 64); err == nil {
						if den, err := strconv.ParseFloat(parts[1], 64); err == nil && den > 0 {
							mediaInfo.FrameRate = num / den
						}
					}
				}
			}

		case "audio":
			mediaInfo.HasAudio = true
		}
	}

	// 将获取到的媒体信息放入内存池
	c.memoryPool.PutMediaInfo(mediaInfo)

	return mediaInfo
}

// getSystemInfo 获取系统信息
func (c *Converter) getSystemInfo() SystemInfo {
	return SystemInfo{
		OS:          "darwin", // 简化版本
		Arch:        "arm64",
		CPUCores:    c.config.Concurrency.ScanWorkers,
		Concurrency: c.config.Concurrency.ConversionWorkers,
		MemoryUsed:  0, // 可以通过gopsutil获取
	}
}

// collectErrors 收集错误信息
func (c *Converter) collectErrors() []ConversionError {
	var errors []ConversionError

	for _, result := range c.results {
		if !result.Success && result.Error != nil {
			errors = append(errors, ConversionError{
				File:  result.OriginalFile.Path,
				Error: result.Error.Error(),
				Time:  time.Now(), // 简化版本
			})
		}
	}

	return errors
}

// ensureReportsDir 确保reports目录结构存在
func (c *Converter) ensureReportsDir() error {
	// 创建主reports目录
	if err := os.MkdirAll("reports", 0755); err != nil {
		return c.errorHandler.WrapError("创建reports目录失败", err)
	}

	// 创建子目录
	subDirs := []string{"conversion", "analysis", "test", "archive"}
	for _, subDir := range subDirs {
		path, err := GlobalPathUtils.JoinPath("reports", subDir)
		if err != nil {
			var errBuilder strings.Builder
		errBuilder.WriteString("构建reports子目录路径失败 ")
		errBuilder.WriteString(subDir)
		return c.errorHandler.WrapError(errBuilder.String(), err)
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			var errBuilder strings.Builder
		errBuilder.WriteString("创建reports子目录失败 ")
		errBuilder.WriteString(subDir)
		return c.errorHandler.WrapError(errBuilder.String(), err)
		}
	}

	return nil
}

// saveJSONReport 保存JSON格式报告
func (c *Converter) saveJSONReport(report DetailedReport) error {
	// 确保目录存在
	if err := c.ensureReportsDir(); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102_150405")
	var filenameBuilder strings.Builder
	filenameBuilder.WriteString("pixly_detailed_report_")
	filenameBuilder.WriteString(timestamp)
	filenameBuilder.WriteString(".json")
	filename, err := GlobalPathUtils.JoinPath("reports", "conversion", filenameBuilder.String())
	if err != nil {
		return c.errorHandler.WrapError("构建报告文件路径失败", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return c.errorHandler.WrapError("序列化报告失败", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return c.errorHandler.WrapError("保存JSON报告失败", err)
	}

	// JSON报告已保存
	return nil
}

// generateReadableReport 生成可读报告
func (c *Converter) generateReadableReport(report DetailedReport) error {
	// 确保目录存在
	if err := c.ensureReportsDir(); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102_150405")
	var filenameBuilder strings.Builder
	filenameBuilder.WriteString("pixly_report_")
	filenameBuilder.WriteString(timestamp)
	filenameBuilder.WriteString(".txt")
	filename, err := GlobalPathUtils.JoinPath("reports", "conversion", filenameBuilder.String())
	if err != nil {
		return c.errorHandler.WrapError("构建报告文件路径失败", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return c.errorHandler.WrapError("创建报告文件失败", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			c.logger.Warn("关闭报告文件失败", zap.Error(err))
		}
	}()

	// 写入报告头部
	if _, err := fmt.Fprintf(file, "=== Pixly 媒体转换详细报告 ===\n"); err != nil {
		return c.errorHandler.WrapError("写入报告头部失败", err)
	}
	if _, err := fmt.Fprintf(file, "生成时间: %s\n", report.EndTime.Format("2006-01-02 15:04:05")); err != nil {
		return c.errorHandler.WrapError("写入生成时间失败", err)
	}
	if _, err := fmt.Fprintf(file, "转换模式: %s\n", report.ConversionMode); err != nil {
		return c.errorHandler.WrapError("写入转换模式失败", err)
	}
	if _, err := fmt.Fprintf(file, "源目录: %s\n", report.SourceDirectory); err != nil {
		return c.errorHandler.WrapError("写入源目录失败", err)
	}
	if _, err := fmt.Fprintf(file, "处理时长: %v\n", report.Duration); err != nil {
		return c.errorHandler.WrapError("write duration to report", err)
	}
	if _, err := fmt.Fprintf(file, "\n"); err != nil {
		return c.errorHandler.WrapError("write newline to report", err)
	}

	// 写入统计信息
	if _, err := fmt.Fprintf(file, "=== 转换统计 ===\n"); err != nil {
		return c.errorHandler.WrapError("write statistics header to report", err)
	}
	if _, err := fmt.Fprintf(file, "总文件数: %d\n", report.TotalFiles); err != nil {
		return c.errorHandler.WrapError("write total files to report", err)
	}
	if _, err := fmt.Fprintf(file, "处理文件数: %d\n", report.ProcessedFiles); err != nil {
		return c.errorHandler.WrapError("write processed files to report", err)
	}
	if _, err := fmt.Fprintf(file, "成功转换: %d\n", report.SuccessfulFiles); err != nil {
		return c.errorHandler.WrapError("write successful files to report", err)
	}
	if _, err := fmt.Fprintf(file, "转换失败: %d\n", report.FailedFiles); err != nil {
		return c.errorHandler.WrapError("write failed files to report", err)
	}
	if _, err := fmt.Fprintf(file, "跳过文件: %d\n", report.SkippedFiles); err != nil {
		return c.errorHandler.WrapError("write skipped files to report", err)
	}
	if _, err := fmt.Fprintf(file, "总压缩率: %.2f%%\n", report.CompressionRatio); err != nil {
		return c.errorHandler.WrapError("write compression ratio to report", err)
	}
	if _, err := fmt.Fprintf(file, "节省空间: %.2f MB\n", float64(report.SpaceSaved)/(1024*1024)); err != nil {
		return c.errorHandler.WrapError("write space saved to report", err)
	}
	if _, err := fmt.Fprintf(file, "\n"); err != nil {
		return c.errorHandler.WrapError("write newline to report", err)
	}

	// 写入格式统计
	if _, err := fmt.Fprintf(file, "=== 格式统计 ===\n"); err != nil {
		return c.errorHandler.WrapError("write format statistics header to report", err)
	}
	for format, stats := range report.FormatSummary {
		if _, err := fmt.Fprintf(file, "%s: %d个文件, 平均压缩率: %.2f%%\n",
			format, stats.Count, stats.AverageCompression); err != nil {
			return c.errorHandler.WrapError("write format statistics to report", err)
		}
	}
	if _, err := fmt.Fprintf(file, "\n"); err != nil {
		return c.errorHandler.WrapError("write newline to report", err)
	}

	// 写入文件详情（限制前20个）
	if _, err := fmt.Fprintf(file, "=== 文件转换详情 (前20个) ===\n"); err != nil {
		return c.errorHandler.WrapError("write file details header to report", err)
	}
	for i, detail := range report.FileDetails {
		if i >= 20 {
			break
		}

		status := "✅ 成功"
		if !detail.Success {
			status = "❌ 失败"
		}

		if _, err := fmt.Fprintf(file, "%s - %s\n", status, GlobalPathUtils.GetBaseName(detail.OriginalPath)); err != nil {
			return c.errorHandler.WrapError("write file status to report", err)
		}
		if _, err := fmt.Fprintf(file, "  原始大小: %.2f MB\n", float64(detail.OriginalSize)/(1024*1024)); err != nil {
			return c.errorHandler.WrapError("write original size to report", err)
		}
		if detail.Success {
			if _, err := fmt.Fprintf(file, "  输出大小: %.2f MB\n", float64(detail.OutputSize)/(1024*1024)); err != nil {
				return c.errorHandler.WrapError("write output size to report", err)
			}
			if _, err := fmt.Fprintf(file, "  压缩率: %.2f%%\n", detail.CompressionRatio); err != nil {
				return c.errorHandler.WrapError("write compression ratio to report", err)
			}
			if _, err := fmt.Fprintf(file, "  格式: %s → %s\n", detail.OriginalFormat, detail.OutputFormat); err != nil {
				return c.errorHandler.WrapError("write format conversion to report", err)
			}

			if detail.MediaInfo != nil {
				if detail.MediaInfo.Width > 0 {
					if _, err := fmt.Fprintf(file, "  分辨率: %dx%d\n", detail.MediaInfo.Width, detail.MediaInfo.Height); err != nil {
						return c.errorHandler.WrapError("write resolution to report", err)
					}
				}
				if detail.MediaInfo.Duration > 0 {
					if _, err := fmt.Fprintf(file, "  时长: %.2f秒\n", detail.MediaInfo.Duration); err != nil {
						return c.errorHandler.WrapError("write duration to report", err)
					}
				}
			}
		} else {
			if _, err := fmt.Fprintf(file, "  错误: %s\n", detail.Error); err != nil {
				return c.errorHandler.WrapError("write error to report", err)
			}
		}
		if _, err := fmt.Fprintf(file, "  处理时间: %v\n", detail.ProcessingTime); err != nil {
			return c.errorHandler.WrapError("write processing time to report", err)
		}
		if _, err := fmt.Fprintf(file, "\n"); err != nil {
			return c.errorHandler.WrapError("write newline to report", err)
		}
	}

	// 写入错误信息
	if len(report.Errors) > 0 {
		if _, err := fmt.Fprintf(file, "=== 错误详情 ===\n"); err != nil {
			return c.errorHandler.WrapError("write errors header to report", err)
		}
		for _, errDetail := range report.Errors {
			if _, err := fmt.Fprintf(file, "文件: %s\n", errDetail.File); err != nil {
				return c.errorHandler.WrapError("write error file to report", err)
			}
			if _, err := fmt.Fprintf(file, "错误: %s\n", errDetail.Error); err != nil {
				return c.errorHandler.WrapError("write error message to report", err)
			}
			if _, err := fmt.Fprintf(file, "\n"); err != nil {
				return c.errorHandler.WrapError("write error newline to report", err)
			}
		}
	}

	// 可读报告已保存
	return nil
}

// verifyConversionResults 验证转换结果
func (c *Converter) verifyConversionResults() error {
	// 验证转换结果

	var verificationErrors []string

	for _, result := range c.results {
		if !result.Success {
			continue
		}

		// 确定实际需要验证的文件路径
		var actualPath string

		// 检查是否为原地转换
		isInPlace := c.config.Output.DirectoryTemplate == ""

		if isInPlace {
			// 原地转换：验证输出路径（原文件已被删除并重命名为新扩展名）
			actualPath = result.OutputPath
			// 原地转换验证
		} else {
			// 指定目录转换：验证输出路径
			actualPath = result.OutputPath
			// 指定目录转换验证
		}

		// 检查实际文件是否存在
		if _, err := os.Stat(actualPath); os.IsNotExist(err) {
			var errBuilder strings.Builder
			errBuilder.WriteString("输出文件不存在: ")
			errBuilder.WriteString(actualPath)
			verificationErrors = append(verificationErrors, errBuilder.String())
			continue
		}

		// 检查文件大小是否合理
		if result.CompressedSize <= 0 {
			var errBuilder strings.Builder
			errBuilder.WriteString("输出文件大小异常: ")
			errBuilder.WriteString(actualPath)
			errBuilder.WriteString(" (大小: ")
			errBuilder.WriteString(strconv.FormatInt(result.CompressedSize, 10))
			errBuilder.WriteString(")")
			verificationErrors = append(verificationErrors, errBuilder.String())
		}
	}

	if len(verificationErrors) > 0 {
		c.logger.Warn("发现验证错误", zap.Strings("errors", verificationErrors))
		var errBuilder strings.Builder
		errBuilder.WriteString("发现 ")
		errBuilder.WriteString(strconv.Itoa(len(verificationErrors)))
		errBuilder.WriteString(" 个验证错误")
		return c.errorHandler.WrapError(errBuilder.String(), nil)
	}

	// 转换结果验证通过
	return nil
}

// getSourceDirectory 获取源目录
func (c *Converter) getSourceDirectory() string {
	// 简化版本，从第一个结果获取目录
	if len(c.results) > 0 {
		return GlobalPathUtils.GetDirName(c.results[0].OriginalFile.Path)
	}
	return "unknown"
}
