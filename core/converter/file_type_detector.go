package converter

import (
	"encoding/json"
	"strconv"
	"strings"

	"pixly/config"

	"go.uber.org/zap"
)

// FileType 文件类型枚举
type FileType string

const (
	FileTypeStaticImage   FileType = "static_image"
	FileTypeAnimatedImage FileType = "animated_image"
	FileTypeVideo         FileType = "video"
	// FileTypeAudio 已删除 - 根据README要求，本程序不处理音频文件
	FileTypeLivePhoto  FileType = "live_photo"  // Live Photo
	FileTypeBurstPhoto FileType = "burst_photo" // 连拍照片
	FileTypePanorama   FileType = "panorama"    // 全景照片
	FileTypeUnknown    FileType = "unknown"
)

// MediaDetails 媒体文件详细信息
type MediaDetails struct {
	FileType    FileType
	Codec       string
	Container   string
	FrameCount  int
	Duration    float64
	Width       int
	Height      int
	IsCorrupted bool
}

// FileTypeDetector 文件类型检测器
type FileTypeDetector struct {
	config      *config.Config
	logger      *zap.Logger
	toolManager *ToolManager
}

// NewFileTypeDetector 创建新的文件类型检测器
func NewFileTypeDetector(config *config.Config, logger *zap.Logger, toolManager *ToolManager) *FileTypeDetector {
	return &FileTypeDetector{
		config:      config,
		logger:      logger,
		toolManager: toolManager,
	}
}

// DetectFileType 精确检测文件类型
func (fd *FileTypeDetector) DetectFileType(filePath string) (*MediaDetails, error) {
	// 使用FFprobe获取媒体信息
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	}

	// 使用工具管理器执行命令，支持路径验证
	output, err := fd.toolManager.ExecuteWithPathValidation(fd.config.Tools.FFprobePath, args...)
	if err != nil {
		// 如果FFprobe失败，标记为损坏文件
		return &MediaDetails{
			FileType:    FileTypeUnknown,
			IsCorrupted: true,
		}, nil
	}

	// 解析FFprobe输出
	var probeData struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
			BitRate    string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			CodecName  string `json:"codec_name"`
			CodecType  string `json:"codec_type"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
			NbFrames   string `json:"nb_frames"`
			Duration   string `json:"duration"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil {
		fd.logger.Warn("解析FFprobe输出失败", zap.String("file", filePath), zap.Error(err))
		return &MediaDetails{
			FileType:    FileTypeUnknown,
			IsCorrupted: true,
		}, nil
	}

	// 创建媒体详情对象
	details := &MediaDetails{
		Container: probeData.Format.FormatName,
	}

	// 解析持续时间
	if probeData.Format.Duration != "" {
		if duration, err := strconv.ParseFloat(probeData.Format.Duration, 64); err == nil {
			details.Duration = duration
		}
	}

	// 分析流信息
	type StreamInfo struct {
		CodecName  string `json:"codec_name"`
		CodecType  string `json:"codec_type"`
		Width      int    `json:"width,omitempty"`
		Height     int    `json:"height,omitempty"`
		RFrameRate string `json:"r_frame_rate,omitempty"`
		NbFrames   string `json:"nb_frames,omitempty"`
		Duration   string `json:"duration,omitempty"`
	}

	var videoStream *StreamInfo
	var audioStream *StreamInfo

	// 查找视频和音频流
	for i := range probeData.Streams {
		stream := probeData.Streams[i]
		switch stream.CodecType {
		case "video":
			videoStream = &StreamInfo{
				CodecName:  stream.CodecName,
				CodecType:  stream.CodecType,
				Width:      stream.Width,
				Height:     stream.Height,
				RFrameRate: stream.RFrameRate,
				NbFrames:   stream.NbFrames,
				Duration:   stream.Duration,
			}
		case "audio":
			audioStream = &StreamInfo{
				CodecName: stream.CodecName,
				CodecType: stream.CodecType,
			}
		}
	}

	// 根据流类型确定文件类型
	if videoStream != nil {
		// 有视频流，先判断是否为视频文件
		details.Codec = videoStream.CodecName
		details.Width = videoStream.Width
		details.Height = videoStream.Height

		// 解析帧数
		if videoStream.NbFrames != "" {
			if frames, err := strconv.Atoi(videoStream.NbFrames); err == nil {
				details.FrameCount = frames
			}
		} else if videoStream.RFrameRate != "" {
			// 如果没有明确的帧数，通过帧率和持续时间计算
			if duration := details.Duration; duration > 0 {
				if frameRate := fd.parseFrameRate(videoStream.RFrameRate); frameRate > 0 {
					details.FrameCount = int(duration * frameRate)
				}
			}
		}

		// 判断是视频还是图片：基于持续时间和文件扩展名
		fileExt := strings.ToLower(filePath[strings.LastIndex(filePath, "."):])
		videoExts := map[string]bool{".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true, ".flv": true, ".wmv": true, ".m4v": true}

		if videoExts[fileExt] || details.Duration > 3.0 {
			// 视频文件扩展名或持续时间超过3秒，判断为视频
			details.FileType = FileTypeVideo

			// 检查是否为 Live Photo
			if fd.isLivePhoto(filePath, details) {
				details.FileType = FileTypeLivePhoto
			}
		} else {
			// 否则判断为图片文件
			// 判断是静图还是动图
			if details.FrameCount > 1 {
				details.FileType = FileTypeAnimatedImage
			} else {
				// 特殊处理：某些格式即使帧数为1也可能是动图
				if fd.isAnimatedFormat(filePath) {
					details.FileType = FileTypeAnimatedImage
					details.FrameCount = 10 // 估计帧数
				} else {
					details.FileType = FileTypeStaticImage
					details.FrameCount = 1
				}
			}

			// 检查是否为特殊图片类型
			if fd.isPanorama(details) {
				details.FileType = FileTypePanorama
			} else if fd.isBurstPhoto(filePath) {
				details.FileType = FileTypeBurstPhoto
			}
		}
	} else if audioStream != nil {
		// 只有音频流，根据README要求不处理音频文件，标记为未知类型
		details.FileType = FileTypeUnknown
		details.Codec = audioStream.CodecName
	} else {
		// 没有识别出有效的流，判断为未知类型
		details.FileType = FileTypeUnknown
	}

	return details, nil
}

// parseFrameRate 解析帧率字符串
func (fd *FileTypeDetector) parseFrameRate(frameRateStr string) float64 {
	parts := strings.Split(frameRateStr, "/")
	if len(parts) != 2 {
		return 0
	}

	numerator, err1 := strconv.ParseFloat(parts[0], 64)
	denominator, err2 := strconv.ParseFloat(parts[1], 64)

	if err1 != nil || err2 != nil || denominator == 0 {
		return 0
	}

	return numerator / denominator
}

// isAnimatedFormat 检查是否为动图格式
func (fd *FileTypeDetector) isAnimatedFormat(filePath string) bool {
	// 获取文件扩展名
	ext := strings.ToLower(filePath)

	// 支持动画的图片格式（与batch_processor.go中的animatedFormats保持一致）
	animatedFormats := []string{
		".gif", ".webp", ".avif", ".apng",
		".flif", ".mng", ".jng", ".jxl", ".tiff", ".tif",
	}

	for _, format := range animatedFormats {
		if strings.HasSuffix(ext, format) {
			return true
		}
	}

	return false
}

// isLivePhoto 检查是否为 Live Photo
func (fd *FileTypeDetector) isLivePhoto(filePath string, details *MediaDetails) bool {
	// Live Photo 特征：
	// 1. 时长很短（通常小于3秒）
	// 2. 有音频轨道
	// 3. 文件名可能包含 "IMG_" 和 "MOV" 等特征

	// 检查时长
	if details.Duration > 3.0 || details.Duration <= 0 {
		return false
	}

	// 检查文件名特征（iOS Live Photo 通常有相似的文件名）
	fileName := strings.ToLower(filePath)
	if strings.Contains(fileName, "img_") && strings.Contains(fileName, ".mov") {
		return true
	}

	// 检查元数据中的特殊标记（这里简化处理）
	// 在实际实现中，可能需要检查 EXIF 或其他元数据
	return false
}

// isPanorama 检查是否为全景照片
func (fd *FileTypeDetector) isPanorama(details *MediaDetails) bool {
	// 全景照片特征：
	// 1. 宽高比很大（通常大于2:1）
	// 2. 分辨率很高

	if details.Width <= 0 || details.Height <= 0 {
		return false
	}

	// 计算宽高比
	aspectRatio := float64(details.Width) / float64(details.Height)

	// 如果宽高比大于2:1，认为是全景照片
	return aspectRatio > 2.0
}

// isBurstPhoto 检查是否为连拍照片
func (fd *FileTypeDetector) isBurstPhoto(filePath string) bool {
	// 连拍照片特征：
	// 1. 文件名可能包含 "BURST" 或类似的标记
	// 2. 文件大小相对较小

	fileName := strings.ToUpper(filePath)
	if strings.Contains(fileName, "BURST") {
		return true
	}

	// 检查文件大小（连拍照片通常较小）
	// 这里简化处理，实际实现中可能需要更复杂的逻辑
	return false
}

// IsCorrupted 检查文件是否损坏
func (fd *FileTypeDetector) IsCorrupted(filePath string) bool {
	// 使用FFprobe检查文件是否可读
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	}

	// 使用工具管理器执行命令，支持路径验证
	_, err := fd.toolManager.ExecuteWithPathValidation(fd.config.Tools.FFprobePath, args...)
	return err != nil
}
