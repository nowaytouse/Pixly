package converter

import (
	"os/exec"
	"strconv"
	"strings"
)

// VideoInfo 视频信息结构
type VideoInfo struct {
	Duration   float64
	Width      int
	Height     int
	Bitrate    int
	FrameRate  float64
	Codec      string
	AudioCodec string
	HasAudio   bool
	FileSize   int64
}

// getVideoInfo 获取视频信息
func (c *Converter) getVideoInfo(path string) (*VideoInfo, error) {
	// 使用FFprobe获取视频信息
	args := []string{
		"-v", "quiet",
		"-print_format", "compact",
		"-show_entries", "format=duration,size:stream=width,height,r_frame_rate,bit_rate,codec_name,codec_type",
		path,
	}

	cmd := exec.Command(c.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, c.errorHandler.WrapError("ffprobe failed", err)
	}

	info := &VideoInfo{}
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	// 解析compact格式输出
	for _, line := range lines {
		if strings.Contains(line, "format|") {
			// 解析format行: format|duration=10.5|size=1024000
			parts := strings.Split(line, "|")
			for _, part := range parts {
				if strings.HasPrefix(part, "duration=") {
					info.Duration = parseFloat(strings.TrimPrefix(part, "duration="))
				} else if strings.HasPrefix(part, "size=") {
					info.FileSize = int64(parseInt(strings.TrimPrefix(part, "size=")))
				}
			}
		} else if strings.Contains(line, "stream|") {
			// 解析stream行
			parts := strings.Split(line, "|")
			codecType := ""
			for _, part := range parts {
				if strings.HasPrefix(part, "codec_type=") {
					codecType = strings.TrimPrefix(part, "codec_type=")
				}
			}

			switch codecType {
			case "video":
				for _, part := range parts {
					if strings.HasPrefix(part, "width=") {
						info.Width = parseInt(strings.TrimPrefix(part, "width="))
					} else if strings.HasPrefix(part, "height=") {
						info.Height = parseInt(strings.TrimPrefix(part, "height="))
					} else if strings.HasPrefix(part, "r_frame_rate=") {
						frameRateStr := strings.TrimPrefix(part, "r_frame_rate=")
						if strings.Contains(frameRateStr, "/") {
							frameParts := strings.Split(frameRateStr, "/")
							if len(frameParts) == 2 {
								num := parseFloat(frameParts[0])
								den := parseFloat(frameParts[1])
								if den > 0 {
									info.FrameRate = num / den
								}
							}
						} else {
							info.FrameRate = parseFloat(frameRateStr)
						}
					} else if strings.HasPrefix(part, "bit_rate=") {
						info.Bitrate = parseInt(strings.TrimPrefix(part, "bit_rate=")) / 1000 // 转换为kbps
					} else if strings.HasPrefix(part, "codec_name=") {
						info.Codec = strings.TrimPrefix(part, "codec_name=")
					}
				}
			case "audio":
				info.HasAudio = true
				for _, part := range parts {
					if strings.HasPrefix(part, "codec_name=") {
						info.AudioCodec = strings.TrimPrefix(part, "codec_name=")
					}
				}
			}
		}
	}

	// 设置默认值（如果解析失败）
	if info.Duration == 0 {
		info.Duration = 10.0
	}
	if info.Width == 0 {
		info.Width = 1920
	}
	if info.Height == 0 {
		info.Height = 1080
	}
	if info.FrameRate == 0 {
		info.FrameRate = 30.0
	}
	if info.Codec == "" {
		info.Codec = "h264"
	}

	return info, nil
}

// needsOptimization 判断是否需要优化
func (c *Converter) needsOptimization(info *VideoInfo) bool {
	// 检查是否需要优化的条件

	// 如果码率过高
	if info.Bitrate > 5000 {
		return true
	}

	// 如果分辨率过高但文件较小（可能质量不好）
	if info.Width > 1920 && info.FileSize < 10*1024*1024 {
		return true
	}

	// 如果使用较老的编码器
	if info.Codec != "h264" && info.Codec != "h265" && info.Codec != "vp9" {
		return true
	}

	return false
}

// VideoConversionConfig 视频转换配置
type VideoConversionConfig struct {
	OutputExt    string
	VideoCodec   string
	AudioCodec   string
	CRF          *int
	Preset       string
	AudioBitrate string
	ExtraArgs    []string
	CopyStreams  bool // 是否直接复制流（重包装模式）
}

// executeVideoConversion 统一的视频转换执行函数
func (c *Converter) executeVideoConversion(file *MediaFile, config VideoConversionConfig) (string, error) {
	outputPath := c.getOutputPath(file, config.OutputExt)

	args := []string{"-i", file.Path}

	if config.CopyStreams {
		// 重包装模式：直接复制流
		args = append(args, "-c:v", "copy", "-c:a", "copy")
	} else {
		// 重新编码模式
		if config.VideoCodec != "" {
			args = append(args, "-c:v", config.VideoCodec)
		}
		if config.CRF != nil {
			var crfBuilder strings.Builder
			crfBuilder.WriteString(strconv.Itoa(*config.CRF))
			args = append(args, "-crf", crfBuilder.String())
		}
		if config.Preset != "" {
			args = append(args, "-preset", config.Preset)
		}
		if config.AudioCodec != "" {
			args = append(args, "-c:a", config.AudioCodec)
		}
		if config.AudioBitrate != "" {
			args = append(args, "-b:a", config.AudioBitrate)
		}
	}

	// 添加额外参数
	args = append(args, config.ExtraArgs...)
	args = append(args, "-y", outputPath)

	// Executing video conversion

	cmd := exec.Command(c.config.Tools.FFmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", c.errorHandler.WrapErrorWithOutput("video conversion failed", err, output)
	}

	return outputPath, nil
}

// isCodecCompatibleWithMOV 检查编码格式是否与MOV容器兼容
func (c *Converter) isCodecCompatibleWithMOV(videoInfo *VideoInfo) bool {
	// VP8编码不支持MOV容器
	if strings.Contains(strings.ToLower(videoInfo.Codec), "vp8") {
		return false
	}

	// VP9编码在某些情况下不支持MOV容器
	if strings.Contains(strings.ToLower(videoInfo.Codec), "vp9") {
		return false
	}

	// AV1编码只支持MP4和AVIF容器
	if strings.Contains(strings.ToLower(videoInfo.Codec), "av01") ||
		strings.Contains(strings.ToLower(videoInfo.Codec), "av1") {
		return false
	}

	// 其他常见编码格式（H.264, H.265, MPEG-4等）支持MOV容器
	return true
}

// convertToMOV 转换为MOV格式（重包装方式，不重新编码）
func (c *Converter) convertToMOV(file *MediaFile) (string, error) {
	// 首先获取视频信息以检查编码兼容性
	videoInfo, err := c.getVideoInfo(file.Path)
	if err != nil {
		return "", c.errorHandler.WrapError("failed to get video info for compatibility check", err)
	}

	// 检查编码格式是否与MOV容器兼容
	if !c.isCodecCompatibleWithMOV(videoInfo) {
		// Skipping video conversion due to codec incompatibility

		// 返回原文件路径，表示跳过转换
		return file.Path, nil
	}

	// 检查是否为原地转换
	isInPlace := c.config.Output.DirectoryTemplate == ""
	outputPath := c.getOutputPath(file, ".mov")
	
	// 如果是原地转换，需要使用临时文件
	if isInPlace {
		tempOutput := outputPath + ".tmp"
		defer func() {
			if err := c.fileOpHandler.SafeRemoveFile(tempOutput); err != nil {
				// 清理临时文件
			}
		}()
		
		config := VideoConversionConfig{
			OutputExt:   ".tmp",
			CopyStreams: true,
			ExtraArgs:   []string{"-movflags", "+faststart"},
		}
		
		// 先转换到临时文件
		_, err := c.executeVideoConversion(file, config)
		if err != nil {
			return "", err
		}
		
		// 然后原子替换
		if err := c.fileOpHandler.AtomicFileReplace(tempOutput, outputPath, true); err != nil {
			return "", err
		}
		
		return outputPath, nil
	}

	// 非原地转换
	config := VideoConversionConfig{
		OutputExt:   ".mov",
		CopyStreams: true,
		ExtraArgs:   []string{"-movflags", "+faststart"},
	}
	return c.executeVideoConversion(file, config)
}

// optimizeMOV 优化MOV文件（重包装方式）
func (c *Converter) optimizeMOV(file *MediaFile) (string, error) {
	// 首先获取视频信息以检查编码兼容性
	videoInfo, err := c.getVideoInfo(file.Path)
	if err != nil {
		return "", c.errorHandler.WrapError("failed to get video info for optimization compatibility check", err)
	}

	// 检查编码格式是否与MOV容器兼容
	if !c.isCodecCompatibleWithMOV(videoInfo) {
		// Skipping MOV optimization due to codec incompatibility

		// 返回原文件路径，表示跳过优化
		return file.Path, nil
	}

	config := VideoConversionConfig{
		OutputExt:   ".mov",
		CopyStreams: true,
		ExtraArgs:   []string{"-movflags", "+faststart"},
	}
	return c.executeVideoConversion(file, config)
}

// convertToEmojiVideo 转换为表情包视频
// convertToEmojiVideo 已删除 - 表情包模式不处理视频文件

// 辅助函数

// parseFloat 安全解析浮点数
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return f
}

// parseInt 安全解析整数
func parseInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}

// getVideoBitrate 计算视频目标码率

// getVideoFPS 获取视频帧率
func (c *Converter) getVideoFPS(path string) (float64, error) {
	info, err := c.getVideoInfo(path)
	if err != nil {
		return 0, err
	}
	return info.FrameRate, nil
}

// 为策略系统提供的方法

// convertVideoContainer 视频容器转换（为策略系统提供）
func (c *Converter) convertVideoContainer(file *MediaFile) (string, error) {
	ext := strings.ToLower(file.Extension)

	// 根据用户要求：如果已经是目标格式即 avif jxl mov 则全部跳过 不进行更多检查 扫描 处理等行为
	if ext == ".avif" || ext == ".jxl" || ext == ".mov" {
		// 直接返回原文件路径，跳过所有处理
		return file.Path, nil
	}

	// 根据README规定：所有视频格式都转换为MOV格式（重包装方式）
	videoInfo, err := c.getVideoInfo(file.Path)
	if err != nil {
		return "", c.errorHandler.WrapError("failed to get video info", err)
	}

	// 检查编码格式是否与MOV容器兼容
	if !c.isCodecCompatibleWithMOV(videoInfo) {
		// Skipping video conversion due to codec incompatibility

		// 返回原文件路径，表示跳过转换
		return file.Path, nil
	}

	// 所有其他视频格式转换为MOV（重包装方式）
	return c.convertToMOV(file)
}
