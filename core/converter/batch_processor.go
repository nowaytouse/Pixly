package converter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pixly/core/input"
	"pixly/core/output"
	"pixly/internal/ui"

	"go.uber.org/zap"
)

// BatchProcessor 批处理处理器
type BatchProcessor struct {
	converter      *Converter
	logger         *zap.Logger
	taskQueue      []*MediaFile
	corruptedFiles []*MediaFile
	stats          *ConversionStats
	results        []*ConversionResult
	watchdog       *ProgressWatchdog
	atomicOps      *AtomicFileOperations
	// themeManager   *theme.ThemeManager // 暂时注释避免循环导入
	ctx context.Context

	strategy   ConversionStrategy // 添加策略字段
	mutex      sync.RWMutex
	memoryPool *MemoryPool // 内存池
}

// detectMagicNumberAndCorrectExtension 检测文件的Magic Number并纠正扩展名
func (bp *BatchProcessor) detectMagicNumberAndCorrectExtension(filePath string) (string, bool) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", false
	}
	defer file.Close()

	// 读取文件头部字节
	header := make([]byte, 32)
	n, err := file.Read(header)
	if err != nil || n < 4 {
		return "", false
	}

	// Magic Number匹配逻辑
	switch {
	case bytes.HasPrefix(header, []byte{0xFF, 0xD8, 0xFF}): // JPEG
		return "jpg", true
	case bytes.HasPrefix(header, []byte{0x89, 0x50, 0x4E, 0x47}): // PNG
		return "png", true
	case bytes.HasPrefix(header, []byte{0x47, 0x49, 0x46, 0x38}): // GIF
		return "gif", true
	case bytes.HasPrefix(header, []byte{0x52, 0x49, 0x46, 0x46}) && bytes.Contains(header[8:12], []byte("WEBP")): // WebP
		return "webp", true
	case bytes.HasPrefix(header, []byte{0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70}): // MP4
		return "mp4", true
	case bytes.HasPrefix(header, []byte{0x00, 0x00, 0x00, 0x14, 0x66, 0x74, 0x79, 0x70}): // MP4
		return "mp4", true
	case bytes.HasPrefix(header, []byte{0x1A, 0x45, 0xDF, 0xA3}): // MKV/WebM
		return "mkv", true
	case bytes.HasPrefix(header, []byte{0x46, 0x4C, 0x56}): // FLV
		return "flv", true
	case bytes.HasPrefix(header, []byte{0x30, 0x26, 0xB2, 0x75}): // ASF/WMV
		return "wmv", true
	default:
		return "", false
	}
}

// preValidateAllFiles 预验证所有文件
func (bp *BatchProcessor) preValidateAllFiles(files []*MediaFile) error {
	for _, file := range files {
		// 检查文件是否存在
		if _, err := os.Stat(file.Path); os.IsNotExist(err) {
			return fmt.Errorf("文件不存在: %s", file.Path)
		}

		// 检查文件是否可读
		if f, err := os.Open(file.Path); err != nil {
			return fmt.Errorf("文件无法读取: %s, 错误: %v", file.Path, err)
		} else {
			f.Close()
		}
	}
	return nil
}

// atomicBatchProcess 原子批处理
func (bp *BatchProcessor) atomicBatchProcess(files []*MediaFile) error {
	// 使用原子操作确保批处理的一致性
	// 不要在第一个错误时就中断，而是继续处理所有文件
	var firstError error

	for _, file := range files {
		result := bp.converter.processFile(file)
		// 注意：UpdateStats已经在processFile内部调用，这里不需要重复调用
		if !result.Success && firstError == nil {
			// 记录第一个错误，但继续处理其他文件
			firstError = result.Error
			bp.logger.Error("文件处理失败", zap.String("file", file.Path), zap.Error(result.Error))
		}
	}

	// 如果有错误，返回第一个错误
	if firstError != nil {
		return firstError
	}

	return nil
}

// createBackup 创建文件备份
func (bp *BatchProcessor) createBackup(srcPath, backupPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// rollbackFiles 回滚文件
func (bp *BatchProcessor) rollbackFiles(backupMap map[string]string) {
	for originalPath, backupPath := range backupMap {
		if _, err := os.Stat(backupPath); err == nil {
			os.Rename(backupPath, originalPath)
		}
	}
}

// cleanupBackups 清理备份文件
func (bp *BatchProcessor) cleanupBackups(backupMap map[string]string) {
	for _, backupPath := range backupMap {
		os.Remove(backupPath)
	}
}

// NewBatchProcessor 创建新的批处理处理器
func NewBatchProcessor(converter *Converter, logger *zap.Logger) *BatchProcessor {
	return &BatchProcessor{
		converter:      converter,
		logger:         logger,
		taskQueue:      make([]*MediaFile, 0),
		corruptedFiles: make([]*MediaFile, 0),
		stats:          converter.stats,
		results:        converter.results,
		watchdog:       converter.watchdog,
		atomicOps:      converter.atomicOps,
		// themeManager:   converter.themeManager, // 暂时注释避免循环导入
		ctx: converter.ctx,

		strategy:   converter.strategy,                    // 添加策略字段
		memoryPool: GetGlobalMemoryPool(converter.logger), // 初始化内存池
	}
}

// ScanAndAnalyze 统一扫描和分析（双阶段智能分析架构）
func (bp *BatchProcessor) ScanAndAnalyze(inputDir string) error {
	// 开始统一扫描和分析
	
	// 阶段一：元信息预判（95%）
	// 通过文件扩展名、Magic Number 和文件大小等元信息进行快速筛选和分类
	files, mediaInfoMap, totalScannedFiles, skippedFiles, err := bp.quickScan(inputDir)
	if err != nil {
		return bp.converter.errorHandler.WrapError("快速扫描失败", err)
	}

	// 阶段二：FFmpeg 深度验证（5%）
	// 仅对阶段一无法确定的文件调用 ffprobe 进行深度分析
	uncertainFiles := bp.identifyUncertainFiles(files, mediaInfoMap)
	if err := bp.deepAnalysis(uncertainFiles, mediaInfoMap, inputDir); err != nil {
		return bp.converter.errorHandler.WrapError("深度分析失败", err)
	}

	// 构建任务队列
	taskQueue := make([]*MediaFile, 0, len(files))
	corruptedFiles := make([]*MediaFile, 0)

	// 计算总文件大小
	var totalSize int64 = 0

	// 分类文件
	for _, file := range files {
		// 累加文件大小
		totalSize += file.Size

		// 检查文件是否损坏
		if mediaInfo, exists := mediaInfoMap[file.Path]; exists && mediaInfo.IsCorrupted {
			file.IsCorrupted = true
			corruptedFiles = append(corruptedFiles, file)
			continue
		}

		// 检查是否编解码器不兼容
		if mediaInfo, exists := mediaInfoMap[file.Path]; exists && bp.isCodecIncompatible(mediaInfo) {
			file.IsCodecIncompatible = true
		}

		// 检查是否容器不兼容
		if mediaInfo, exists := mediaInfoMap[file.Path]; exists && bp.isContainerIncompatible(mediaInfo) {
			file.IsContainerIncompatible = true
		}

		// 添加到任务队列
		taskQueue = append(taskQueue, file)
	}

	// 处理不同类型的问题文件
	if err := bp.converter.HandleCodecIncompatibility(taskQueue); err != nil {
		bp.logger.Warn("处理编解码器不兼容文件时出错", zap.Error(err))
	}

	if err := bp.converter.HandleContainerIncompatibility(taskQueue); err != nil {
		bp.logger.Warn("处理容器不兼容文件时出错", zap.Error(err))
	}

	// 处理跳过的文件
	if len(skippedFiles) > 0 {
		bp.mutex.Lock()
		for _, skippedFile := range skippedFiles {
			// 创建跳过的转换结果
			result := &ConversionResult{
				OriginalFile:     skippedFile,
				OutputPath:       skippedFile.Path,
				OriginalSize:     skippedFile.Size,
				CompressedSize:   skippedFile.Size,
				CompressionRatio: 0,
				Success:          true,
				Skipped:          true,
				SkipReason:       "already target format",
				Method:           "skip",
				Duration:         0,
			}

			// 添加到结果列表
			bp.results = append(bp.results, result)
			// 同步到Converter的results
			bp.converter.mutex.Lock()
			bp.converter.results = append(bp.converter.results, result)
			bp.converter.mutex.Unlock()
		}
		bp.mutex.Unlock()
	}

	// 更新批处理器状态
	bp.mutex.Lock()
	bp.taskQueue = taskQueue
	bp.corruptedFiles = corruptedFiles
	// 初始化统计信息 - 使用实际扫描到的文件总数（包括跳过的文件）
	bp.stats.TotalFiles = totalScannedFiles
	bp.stats.TotalSize = totalSize // 同时初始化总大小
	bp.stats.StartTime = time.Now()
	bp.mutex.Unlock()

	// 扫描和分析完成

	return nil
}

// isCodecIncompatible 检查是否编解码器不兼容
func (bp *BatchProcessor) isCodecIncompatible(mediaInfo *MediaInfo) bool {
	// 这里可以添加具体的编解码器不兼容检查逻辑
	// 例如：检查是否为不支持的编解码器
	incompatibleCodecs := []string{"unsupported_codec1", "unsupported_codec2"}
	for _, codec := range incompatibleCodecs {
		if mediaInfo.Codec == codec {
			return true
		}
	}
	return false
}

// isContainerIncompatible 检查是否容器不兼容
func (bp *BatchProcessor) isContainerIncompatible(mediaInfo *MediaInfo) bool {
	// 这里可以添加具体的容器不兼容检查逻辑
	// 例如：检查是否为不支持的容器格式
	// incompatibleContainers := []string{"unsupported_container1", "unsupported_container2"}
	// 这里简化处理，实际应该从mediaInfo中获取容器信息
	return false
}

// quickScan 快速扫描（阶段一：元信息预判）
// 在95%扫描阶段执行Magic Number检测，提高文件识别准确性
func (bp *BatchProcessor) quickScan(inputDir string) ([]*MediaFile, map[string]*MediaInfo, int, []*MediaFile, error) {
	var files []*MediaFile
	mediaInfoMap := make(map[string]*MediaInfo)

	// 扫描阶段使用动态进度显示
	// 虽然不知道总文件数，但可以使用扫描计数显示进度

	// 用于跟踪扫描的文件数量
	var scannedCount int64
	var totalCount int64
	var skippedCount int64
	// 用于存储跳过的文件，确保它们也被记录到结果中
	var skippedFiles []*MediaFile
	
	// 启动扫描进度显示（使用未知总大小的进度条）
	ui.StartDynamicProgressUnknownTotal("扫描文件...")

	// 使用GlobalPathUtils规范化输入目录
	normalizedInputDir, err := GlobalPathUtils.NormalizePath(inputDir)
	if err != nil {
		return nil, nil, 0, nil, bp.converter.errorHandler.WrapError("无法规范化输入目录", err)
	}

	err = GlobalPathUtils.WalkPath(normalizedInputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			bp.logger.Warn("无法访问文件", zap.String("path", path), zap.Error(err))
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// 更新扫描计数
		totalCount++

		// 扫描阶段不更新进度条，只记录日志

		// 每扫描100个文件更新进度
		if totalCount%100 == 0 {
			ui.UpdateDynamicProgress(int64(totalCount), fmt.Sprintf("已扫描 %d 个文件...", totalCount))
		}

		// 检查是否为媒体文件
		if !bp.converter.isMediaFile(path) {
			return nil
		}

		// 获取文件扩展名
		// 使用GlobalPathUtils处理路径
		normalizedPath, pathErr := GlobalPathUtils.NormalizePath(path)
		if pathErr != nil {
			bp.logger.Warn("路径规范化失败", zap.String("path", path), zap.Error(pathErr))
			return nil
		}
		ext := strings.ToLower(GlobalPathUtils.GetExtension(normalizedPath))

		// 只对可疑文件进行Magic Number检测以提高性能
		needsMagicCheck := false
		if !knownFormats[ext] {
			needsMagicCheck = true
		} else if info.Size() == 0 {
			needsMagicCheck = true
		} else if info.Size() > 100*1024*1024 { // 超过100MB的文件
			needsMagicCheck = true
		}

		if needsMagicCheck {
			if actualExt, corrected := bp.detectMagicNumberAndCorrectExtension(path); corrected {
				// 检查修正后的扩展名是否为目标格式，如果是则跳过处理
				correctedExt := "." + actualExt
				if bp.converter.IsTargetFormat(correctedExt) {
					bp.logger.Debug("修正后的扩展名为目标格式，跳过处理",
						zap.String("file", path),
						zap.String("original_ext", ext),
						zap.String("corrected_ext", actualExt))
					ext = correctedExt
					// 将文件标记为跳过
					skippedFile := &MediaFile{
						Path:      path,
						Name:      info.Name(),
						Size:      info.Size(),
						Extension: correctedExt,
						ModTime:   info.ModTime(),
						Type:      bp.converter.GetFileType(correctedExt),
					}
					skippedFiles = append(skippedFiles, skippedFile)
					
					// 创建媒体信息并跳过
					mediaInfo := &MediaInfo{
						FullPath:       path,
						FileSize:       info.Size(),
						ModTime:        info.ModTime(),
						Codec:          actualExt,
						FrameCount:     0,
						IsAnimated:     false,
						IsCorrupted:    info.Size() == 0,
						InitialQuality: 50,
					}
					mediaInfoMap[path] = mediaInfo
					skippedCount++
					return nil
				}
				
				bp.logger.Debug("扩展名修正完成",
					zap.String("file", path),
					zap.String("original_ext", ext),
					zap.String("corrected_ext", actualExt))
				ext = correctedExt
			}
		}

		// 检查文件是否已经是目标格式，如果是则记录为跳过文件
		if bp.converter.IsTargetFormat(ext) {
			// 文件已是目标格式，跳过扫描和扩展名修正
			bp.logger.Info("跳过目标格式文件",
				zap.String("file", path),
				zap.String("format", ext),
				zap.String("reason", "已经是目标格式，无需转换"))

			// 创建跳过的文件信息
			skippedFile := &MediaFile{
				Path:       path,
				Name:       info.Name(),
				Size:       info.Size(),
				Extension:  ext,
				ModTime:    info.ModTime(),
				Type:       bp.converter.GetFileType(ext),
				SkipReason: "已经是目标格式",
			}
			// 添加到跳过文件列表
			skippedFiles = append(skippedFiles, skippedFile)

			// 创建媒体信息
			mediaInfo := &MediaInfo{
				FullPath:       path,
				FileSize:       info.Size(),
				ModTime:        info.ModTime(),
				Codec:          strings.TrimPrefix(ext, "."),
				FrameCount:     0,
				IsAnimated:     false,
				IsCorrupted:    info.Size() == 0,
				InitialQuality: 50,
			}

			// 对于跳过的文件，使用轻量级标识符而不是计算耗时的SHA256哈希值
			// 使用文件路径、大小和修改时间作为唯一标识
			var hashBuilder strings.Builder
			hashBuilder.WriteString("skip_")
			hashBuilder.WriteString(path)
			hashBuilder.WriteString("_")
			hashBuilder.WriteString(strconv.FormatInt(info.Size(), 10))
			hashBuilder.WriteString("_")
			hashBuilder.WriteString(strconv.FormatInt(info.ModTime().Unix(), 10))
			mediaInfo.SHA256Hash = hashBuilder.String()

			mediaInfoMap[path] = mediaInfo
			skippedCount++
			return nil
		}

		mediaFile := &MediaFile{
			Path:      path,
			Name:      info.Name(),
			Size:      info.Size(),
			Extension: ext,
			ModTime:   info.ModTime(),
		}

		// 确定文件类型
		mediaFile.Type = bp.converter.GetFileType(mediaFile.Extension)

		files = append(files, mediaFile)
		scannedCount++

		// 创建初步的媒体信息
		mediaInfo := &MediaInfo{
			FullPath:       path,
			FileSize:       info.Size(),
			ModTime:        info.ModTime(),
			Codec:          strings.TrimPrefix(ext, "."), // 使用扩展名作为初始编解码器
			FrameCount:     0,                            // 将在阶段二中填充
			IsAnimated:     false,
			IsCorrupted:    info.Size() == 0, // 简单判断空文件为损坏文件
			InitialQuality: 50,               // 默认质量
		}

		// 使用轻量级标识符代替耗时的SHA256哈希值计算
		// 使用文件路径、大小和修改时间作为唯一标识，提升扫描性能
		var hashBuilder strings.Builder
		hashBuilder.WriteString("quick_")
		hashBuilder.WriteString(path)
		hashBuilder.WriteString("_")
		hashBuilder.WriteString(strconv.FormatInt(info.Size(), 10))
		hashBuilder.WriteString("_")
		hashBuilder.WriteString(strconv.FormatInt(info.ModTime().Unix(), 10))
		mediaInfo.SHA256Hash = hashBuilder.String()

		mediaInfoMap[path] = mediaInfo

		return nil
	})

	// 完成扫描
	// 扫描完成

	// 快速扫描完成

	// 记录实际需要处理的文件数量，用于调试
	// 实际需要处理的文件数量

	// 将跳过的文件添加到BatchProcessor中，并更新统计信息
	if len(skippedFiles) > 0 {
		bp.mutex.Lock()
		for _, skippedFile := range skippedFiles {
			// 创建跳过的转换结果
			result := &ConversionResult{
				OriginalFile:     skippedFile,
				OutputPath:       skippedFile.Path, // 跳过的文件输出路径为原路径
				OriginalSize:     skippedFile.Size,
				CompressedSize:   skippedFile.Size,
				CompressionRatio: 0,
				Success:          true,
				Skipped:          true,
				SkipReason:       "已经是目标格式",
				Method:           "skip",
				Duration:         0,
			}

			// 添加到结果列表
			bp.results = append(bp.results, result)
			// 同步到Converter的results
			bp.converter.results = append(bp.converter.results, result)

			// 立即更新统计信息，确保跳过文件被正确统计
			bp.converter.UpdateStats(result)
		}
		bp.mutex.Unlock()

		// 已记录跳过的文件到结果中并更新统计
	}

	// 完成扫描进度
	ui.FinishDynamicProgress()
	return files, mediaInfoMap, int(totalCount), skippedFiles, err
}

// calculateFileHash 计算文件的SHA256哈希值
func (bp *BatchProcessor) calculateFileHash(filePath string) (hash string, err error) {
	// 添加panic恢复机制
	defer func() {
		if r := recover(); r != nil {
			bp.logger.Error("Panic in calculateFileHash",
				zap.String("file", filePath),
				zap.Any("panic", r),
				zap.Stack("stacktrace"))
			hash = ""
			var errBuilder strings.Builder
			errBuilder.WriteString("panic in calculateFileHash: ")
			errBuilder.WriteString(fmt.Sprint(r))
			err = fmt.Errorf("%s", errBuilder.String())
		}
	}()

	// 验证文件是否存在和可读
	if _, err := os.Stat(filePath); err != nil {
		return "", fmt.Errorf("file stat failed: %w", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("file open failed: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			bp.logger.Warn("Failed to close file during hash calculation",
				zap.String("file", filePath),
				zap.Error(err))
		}
	}()

	// 使用内存池获取缓冲区，提升性能并减少GC压力
	buffer := bp.memoryPool.GetBuffer()
	defer bp.memoryPool.PutBuffer(buffer)

	// 确保缓冲区有足够的容量用于哈希计算
	const optimalBufferSize = 32 * 1024
	if cap(buffer) < optimalBufferSize {
		// 如果内存池提供的缓冲区太小，直接使用固定大小的缓冲区
		// 避免重新分配和复制，这样更高效
		buffer = make([]byte, optimalBufferSize)
	} else {
		// 重置缓冲区长度为最优大小
		buffer = buffer[:optimalBufferSize]
	}

	hasher := sha256.New()
	if _, err := io.CopyBuffer(hasher, file, buffer); err != nil {
		return "", fmt.Errorf("hash calculation failed: %w", err)
	}

	hashBytes := hasher.Sum(nil)
	var hashBuilder strings.Builder
	hashBuilder.Grow(len(hashBytes) * 2) // 预分配空间
	for _, b := range hashBytes {
		if b < 16 {
			hashBuilder.WriteByte('0')
		}
		hashBuilder.WriteString(strconv.FormatUint(uint64(b), 16))
	}
	return hashBuilder.String(), nil
}

// 文件类型分类器 - 消除复杂的评分逻辑
var (
	knownFormats = map[string]bool{
		// 图片格式 - 基础格式（仅包含真正的图片媒体文件）
		".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".gif": true,
		".bmp": true, ".tiff": true, ".tif": true, ".ico": true,

		// 图片格式 - 现代格式
		".webp": true, ".heic": true, ".heif": true, ".jxl": true, ".avif": true,
		".jp2": true, ".jpx": true, ".j2k": true, ".j2c": true, ".jpc": true,
		".apng": true,

		// 视频格式 - 主流格式（仅包含真正的视频媒体文件）
		".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true,
		".flv": true, ".wmv": true, ".asf": true, ".m4v": true, ".3gp": true,
		".3g2": true, ".f4v": true, ".f4p": true,

		// 视频格式 - 专业格式
		".mxf": true, ".mts": true, ".m2ts": true, ".ts": true, ".vob": true,
		".mpg": true, ".mpeg": true, ".m1v": true, ".m2v": true, ".mpv": true,
		".mpe": true, ".mpv2": true,

		// 视频格式 - 新兴格式
		".av1": true, ".ivf": true, ".y4m": true, ".yuv": true,
		".dv": true, ".hdv": true, ".divx": true, ".xvid": true, ".ogv": true,
		".ogm": true, ".rm": true, ".rmvb": true, ".rv": true, ".amv": true,
	}
	trashFormats = map[string]bool{
		".tmp": true, ".bak": true, ".old": true, ".temp": true,
		".cache": true, ".log": true, ".swp": true, ".swo": true,
		".~": true, ".backup": true, ".orig": true, ".save": true,
	}
	animatedFormats = map[string]bool{
		".gif": true, ".webp": true, ".avif": true, ".apng": true,
		".flif": true, ".mng": true, ".jng": true, ".jxl": true,
		".tiff": true, ".tif": true,
	}
)

// identifyUncertainFiles 识别需要深度分析的文件 - 简化版本
// 怀疑度评分阈值常量 - README要求的核心参数
const (
	SUSPICION_THRESHOLD   = 50 // 达到50分才进行深度分析（提高阈值减少深度分析）
	SCORE_UNKNOWN_FORMAT  = 20 // 未知格式扩展名
	SCORE_ZERO_SIZE       = 25 // 零字节文件
	SCORE_HUGE_SIZE       = 15 // 超大文件(>100MB)
	SCORE_NO_EXTENSION    = 10 // 无扩展名
	SCORE_SUSPICIOUS_NAME = 5  // 可疑文件名模式
	SCORE_RECENT_MODIFIED = 3  // 最近修改的文件
)

func (bp *BatchProcessor) identifyUncertainFiles(files []*MediaFile, mediaInfoMap map[string]*MediaInfo) []*MediaFile {
	uncertainFiles := make([]*MediaFile, 0)
	skippedByScore := 0

	for _, file := range files {
		mediaInfo := mediaInfoMap[file.Path]
		ext := strings.ToLower(file.Extension)

		// 已知格式：预填充信息，无需深度分析
		if knownFormats[ext] {
			mediaInfo.Codec = ext
			mediaInfo.IsAnimated = animatedFormats[ext]
			if mediaInfo.IsAnimated {
				mediaInfo.FrameCount = 10
			} else {
				mediaInfo.FrameCount = 1
			}
			mediaInfo.SuspicionScore = 0 // 已知格式无怀疑
			continue
		}

		// 怀疑度评分制度 - README要求的核心功能
		suspicionScore := 0
		suspicionReasons := make([]string, 0)

		// 评分规则1: 未知格式扩展名和Magic Number检测
		if ext == "" {
			suspicionScore += SCORE_NO_EXTENSION
			suspicionReasons = append(suspicionReasons, "无扩展名")
		} else if !knownFormats[ext] {
			suspicionScore += SCORE_UNKNOWN_FORMAT
			var reasonBuilder strings.Builder
			reasonBuilder.WriteString("未知格式: ")
			reasonBuilder.WriteString(ext)
			suspicionReasons = append(suspicionReasons, reasonBuilder.String())
		}

		// 对于未知格式或可疑文件，进行Magic Number检测
		if !knownFormats[ext] || file.Size == 0 || file.Size > 100*1024*1024 {
			if detectedExt, needsCorrection := bp.detectMagicNumberAndCorrectExtension(file.Path); needsCorrection {
				suspicionScore += 15 // Magic Number不匹配扩展名
				var mismatchBuilder strings.Builder
				mismatchBuilder.WriteString("扩展名不匹配(实际:")
				mismatchBuilder.WriteString(detectedExt)
				mismatchBuilder.WriteString(")")
				suspicionReasons = append(suspicionReasons, mismatchBuilder.String())

				// 记录扩展名修正信息
				if mediaInfo, exists := mediaInfoMap[file.Path]; exists {
					mediaInfo.DetectedFormat = detectedExt
					mediaInfo.NeedsExtensionCorrection = true
				}
			}
		}

		// 评分规则2: 文件大小异常
		if file.Size == 0 {
			suspicionScore += SCORE_ZERO_SIZE
			suspicionReasons = append(suspicionReasons, "零字节文件")
		} else if file.Size > 100*1024*1024 {
			suspicionScore += SCORE_HUGE_SIZE
			var sizeBuilder strings.Builder
			sizeBuilder.WriteString("超大文件: ")
			sizeBuilder.WriteString(strconv.FormatFloat(float64(file.Size)/(1024*1024), 'f', 1, 64))
			sizeBuilder.WriteString("MB")
			suspicionReasons = append(suspicionReasons, sizeBuilder.String())
		}

		// 评分规则3: 可疑文件名模式
		fileName := strings.ToLower(file.Name)
		if strings.Contains(fileName, "temp") || strings.Contains(fileName, "tmp") ||
			strings.Contains(fileName, "cache") || strings.Contains(fileName, "backup") {
			suspicionScore += SCORE_SUSPICIOUS_NAME
			suspicionReasons = append(suspicionReasons, "可疑文件名模式")
		}

		// 评分规则4: 最近修改的文件(可能正在使用中)
		if time.Since(file.ModTime) < 24*time.Hour {
			suspicionScore += SCORE_RECENT_MODIFIED
			suspicionReasons = append(suspicionReasons, "24小时内修改")
		}

		// 记录怀疑度评分
		mediaInfo.SuspicionScore = suspicionScore
		mediaInfo.SuspicionReasons = suspicionReasons

		// 只有达到阈值才进行深度分析 - README的核心要求
		if suspicionScore >= SUSPICION_THRESHOLD {
			uncertainFiles = append(uncertainFiles, file)
			// 文件需要深度分析
		} else {
			skippedByScore++
			// 文件跳过深度分析
		}
	}

	// 怀疑度评分完成
	return uncertainFiles
}

// deepAnalysis 深度分析文件
func (bp *BatchProcessor) deepAnalysis(filesToAnalyze []*MediaFile, mediaInfoMap map[string]*MediaInfo, inputDir string) error {
	// 启动深度分析进度条
	ui.StartNamedProgress("analysis", int64(len(filesToAnalyze)), "深度分析")
	defer ui.FinishNamedProgress("analysis")

	// 开始深度分析
	// 开始深度分析

	// 获取工作池
	pool := bp.converter.GetWorkerPool()
	if pool == nil {
		bp.logger.Error("无法获取工作池进行深度分析")
		return fmt.Errorf("工作池不可用")
	}

	// 使用原子计数器跟踪进度
	var analyzedCount int64
	var wg sync.WaitGroup

	// 对每个不确定的文件进行深度分析
	for _, file := range filesToAnalyze {
		// 检查上下文是否已取消
		select {
		case <-bp.converter.ctx.Done():
			// 深度分析被取消
			return bp.converter.ctx.Err()
		default:
		}

		wg.Add(1)
		fileCopy := file // 避免闭包变量问题
		err := pool.Submit(func() {
			defer wg.Done()

			// 为每个文件创建带超时的上下文
			ctx, cancel := context.WithTimeout(bp.converter.ctx, 30*time.Second)
			defer cancel()

			// 在goroutine中执行分析
			done := make(chan bool, 1)
			go func() {
				defer func() {
					done <- true
				}()

				// 调用 ffprobe 获取详细媒体信息
				mediaInfo, err := bp.getMediaInfo(fileCopy.Path)
				if err != nil {
					bp.logger.Warn("获取媒体信息失败", zap.String("file", fileCopy.Path), zap.Error(err))
					// 标记文件为损坏
					if info, exists := mediaInfoMap[fileCopy.Path]; exists {
						info.IsCorrupted = true
					}
					return
				}

				// 更新媒体信息
				if info, exists := mediaInfoMap[fileCopy.Path]; exists {
					info.Codec = mediaInfo.Codec
					info.FrameCount = mediaInfo.FrameCount
					info.IsAnimated = mediaInfo.IsAnimated
					info.IsCorrupted = mediaInfo.IsCorrupted
					info.InitialQuality = mediaInfo.InitialQuality
					info.Container = mediaInfo.Container
					info.IsCodecIncompatible = mediaInfo.IsCodecIncompatible
					info.IsContainerIncompatible = mediaInfo.IsContainerIncompatible
				}
			}()

			// 等待分析完成或超时
			select {
			case <-ctx.Done():
				// 超时或取消
				bp.logger.Warn("文件深度分析超时", zap.String("file", fileCopy.Path))
				// 标记文件为损坏
				if mediaInfo, exists := mediaInfoMap[fileCopy.Path]; exists {
					mediaInfo.IsCorrupted = true
				}
			case <-done:
				// 分析完成
			}

			// 原子更新分析计数
			currentCount := atomic.AddInt64(&analyzedCount, 1)

			// 更新进度条 - 使用字符串构建器避免fmt.Sprintf
			var progressMsg strings.Builder
			progressMsg.WriteString("深度分析 (")
			progressMsg.WriteString(strconv.FormatInt(currentCount, 10))
			progressMsg.WriteString("/")
			progressMsg.WriteString(strconv.Itoa(len(filesToAnalyze)))
			progressMsg.WriteString(")")
			ui.UpdateNamedProgress("analysis", currentCount, progressMsg.String())

			if currentCount%10 == 0 {
				// 深度分析进度
			}
		})

		if err != nil {
			bp.logger.Error("提交深度分析任务失败", zap.String("file", fileCopy.Path), zap.Error(err))
			wg.Done() // 如果提交失败，需要手动调用 Done
		}
	}

	// 等待所有分析任务完成
	wg.Wait()

	// 深度分析完成
	return nil
}

// getMediaInfo 使用 ffprobe 获取媒体文件信息
func (bp *BatchProcessor) getMediaInfo(filePath string) (*MediaInfo, error) {
	// 首先尝试从内存池获取媒体信息
	if cachedInfo := bp.memoryPool.GetMediaInfo(); cachedInfo != nil && cachedInfo.FullPath == filePath {
		return cachedInfo, nil
	}

	// 使用FFprobe获取媒体信息
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	}

	cmd := exec.Command(bp.converter.config.Tools.FFprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, bp.converter.errorHandler.WrapError("ffprobe failed", err)
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
			ColorSpace string `json:"color_space"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err != nil {
		return nil, bp.converter.errorHandler.WrapError("failed to parse ffprobe output", err)
	}

	mediaInfo := &MediaInfo{
		FullPath:                filePath,
		Codec:                   "",
		FrameCount:              1,
		IsAnimated:              false,
		IsCorrupted:             false,
		Container:               probeData.Format.FormatName,
		IsCodecIncompatible:     false,
		IsContainerIncompatible: false,
	}

	// 填充基本文件信息
	if fileInfo, err := os.Stat(filePath); err == nil {
		mediaInfo.FileSize = fileInfo.Size()
		mediaInfo.ModTime = fileInfo.ModTime()
	}

	// 解析编解码器信息
	if len(probeData.Streams) > 0 {
		// 安全访问第一个流
		if firstStream := probeData.Streams[0]; firstStream.CodecName != "" {
			mediaInfo.Codec = firstStream.CodecName
		} else {
			bp.logger.Warn("第一个流的编解码器名称为空", zap.String("file", filePath))
			mediaInfo.Codec = "unknown"
		}

		// 检查编解码器是否不兼容
		mediaInfo.IsCodecIncompatible = bp.isCodecIncompatibleByFFprobe(probeData.Streams)
	} else {
		bp.logger.Warn("文件没有检测到任何流", zap.String("file", filePath))
		mediaInfo.Codec = "unknown"
		mediaInfo.IsCorrupted = true
	}

	// 检查容器是否不兼容
	mediaInfo.IsContainerIncompatible = bp.isContainerIncompatibleByFFprobe(probeData.Format.FormatName)

	// 判断是否为动图或视频
	for _, stream := range probeData.Streams {
		if stream.CodecType == "video" {
			// 检查帧率是否大于1来判断是否为动图
			if stream.RFrameRate != "0/0" && stream.RFrameRate != "1/1" {
				mediaInfo.IsAnimated = true
				mediaInfo.FrameCount = 10 // 简化处理，假设动图至少10帧
			}
			break
		}
	}

	return mediaInfo, nil
}

// isCodecIncompatibleByFFprobe 根据FFprobe结果检查编解码器是否不兼容
func (bp *BatchProcessor) isCodecIncompatibleByFFprobe(streams []struct {
	CodecName  string `json:"codec_name"`
	CodecType  string `json:"codec_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	ColorSpace string `json:"color_space"`
}) bool {
	// 这里可以添加具体的编解码器不兼容检查逻辑
	// 例如：检查是否为不支持的编解码器
	incompatibleCodecs := []string{"unsupported_codec1", "unsupported_codec2"}
	for _, stream := range streams {
		for _, codec := range incompatibleCodecs {
			if stream.CodecName == codec {
				return true
			}
		}
	}
	return false
}

// isContainerIncompatibleByFFprobe 根据FFprobe结果检查容器格式是否不兼容
func (bp *BatchProcessor) isContainerIncompatibleByFFprobe(formatName string) bool {
	// 这里可以添加具体的容器格式不兼容检查逻辑
	// 例如：检查是否为不支持的容器格式
	incompatibleContainers := []string{"unsupported_container1", "unsupported_container2"}
	for _, container := range incompatibleContainers {
		if formatName == container {
			return true
		}
	}
	return false
}

// HandleCorruptedFiles 处理损坏文件 (批量决策阶段)
// CorruptedFileAction 定义损坏文件处理动作
type CorruptedFileAction int

const (
	ActionIgnore CorruptedFileAction = iota
	ActionDelete
	ActionMoveToTrash
)

// CorruptedFileHandler 处理损坏文件的核心逻辑
type CorruptedFileHandler struct {
	logger *zap.Logger
}

func (h *CorruptedFileHandler) executeAction(action CorruptedFileAction, files []*MediaFile) error {
	switch action {
	case ActionDelete:
		return h.deleteFiles(files)
	case ActionMoveToTrash:
		return h.moveToTrash(files)
	default:
		// 忽略损坏文件
		return nil
	}
}

func (h *CorruptedFileHandler) deleteFiles(files []*MediaFile) error {
	// 删除损坏文件
	for _, file := range files {
		if err := os.Remove(file.Path); err != nil {
			h.logger.Error("删除失败", zap.String("file", file.Path), zap.Error(err))
		} else {
			// 删除成功
		}
	}
	return nil
}

func (h *CorruptedFileHandler) moveToTrash(files []*MediaFile) error {
	if len(files) == 0 {
		return nil
	}

	firstPath, err := GlobalPathUtils.NormalizePath(files[0].Path)
	if err != nil {
		return fmt.Errorf("路径规范化失败: %w", err)
	}

	trashDir, err := GlobalPathUtils.JoinPath(GlobalPathUtils.GetDirName(firstPath), ".trash")
	if err != nil {
		return fmt.Errorf("构建垃圾箱路径失败: %w", err)
	}

	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return fmt.Errorf("创建垃圾箱目录失败: %w", err)
	}

	// 移动到垃圾箱
	for _, file := range files {
		normalizedPath, err := GlobalPathUtils.NormalizePath(file.Path)
		if err != nil {
			h.logger.Error("路径规范化失败", zap.String("path", file.Path), zap.Error(err))
			continue
		}

		trashPath, err := GlobalPathUtils.JoinPath(trashDir, GlobalPathUtils.GetBaseName(normalizedPath))
		if err != nil {
			h.logger.Error("构建垃圾箱文件路径失败", zap.Error(err))
			continue
		}

		if err := os.Rename(file.Path, trashPath); err != nil {
			h.logger.Error("移动失败", zap.String("file", file.Path), zap.Error(err))
		} else {
			// 移动成功
		}
	}
	return nil
}

func (bp *BatchProcessor) HandleCorruptedFiles() error {
	bp.mutex.RLock()
	if len(bp.corruptedFiles) == 0 {
		bp.mutex.RUnlock()
		return nil
	}
	// 直接使用原始切片，避免内存拷贝
	corruptedFiles := bp.corruptedFiles
	bp.mutex.RUnlock()

	handler := &CorruptedFileHandler{logger: bp.logger}
	action := bp.determineAction(corruptedFiles)

	if err := handler.executeAction(action, corruptedFiles); err != nil {
		bp.logger.Error("处理损坏文件失败", zap.Error(err))
		return err
	}

	// 只有删除或移动操作才需要清理任务队列
	if action == ActionDelete || action == ActionMoveToTrash {
		bp.removeCorruptedFromQueue(corruptedFiles)
	}

	return nil
}

func (bp *BatchProcessor) determineAction(files []*MediaFile) CorruptedFileAction {
	strategy := bp.converter.config.ProblemFileHandling.CorruptedFileStrategy

	switch strategy {
	case "delete":
		return ActionDelete
	case "move_to_trash":
		return ActionMoveToTrash
	case "ignore":
		return ActionIgnore
	default:
		return bp.promptUserAction(files)
	}
}

func (bp *BatchProcessor) promptUserAction(files []*MediaFile) CorruptedFileAction {
	// 发现损坏文件，需要用户决策

	for i, file := range files {
		bp.logger.Warn("损坏文件", zap.Int("index", i+1), zap.String("path", file.Path))
	}

	output.WriteLine("\n请选择处理方式:")
	output.WriteLine("[D] 全部删除 (Delete All)")
	output.WriteLine("[I] 忽略不处理 (Ignore)")
	output.WriteLine("\n将在5秒后默认选择 [I] 忽略...")

	inputChan := make(chan string, 1)
	go func() {
		userInput, err := input.ReadLine()
		if err != nil {
			inputChan <- ""
			return
		}
		inputChan <- strings.TrimSpace(strings.ToLower(userInput))
	}()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	var choice string
	select {
	case choice = <-inputChan:
	case <-timer.C:
		output.WriteLine("\n超时，自动选择 [I] 忽略")
		choice = "i"
	}

	switch choice {
	case "d":
		output.WriteLine("已删除所有损坏文件")
		return ActionDelete
	default:
		output.WriteLine("忽略所有损坏文件")
		return ActionIgnore
	}
}

func (bp *BatchProcessor) removeCorruptedFromQueue(corruptedFiles []*MediaFile) {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	corruptedPaths := make(map[string]bool, len(corruptedFiles))
	for _, file := range corruptedFiles {
		corruptedPaths[file.Path] = true
	}

	newQueue := make([]*MediaFile, 0, len(bp.taskQueue))
	for _, file := range bp.taskQueue {
		if !corruptedPaths[file.Path] {
			newQueue = append(newQueue, file)
		}
	}

	bp.taskQueue = newQueue
	bp.corruptedFiles = make([]*MediaFile, 0)
}

// ProcessTaskQueue 处理任务队列 - 实现严格的批处理原子性
func (bp *BatchProcessor) ProcessTaskQueue() error {
	bp.mutex.RLock()
	taskQueue := make([]*MediaFile, len(bp.taskQueue))
	copy(taskQueue, bp.taskQueue)
	bp.mutex.RUnlock()

	if len(taskQueue) == 0 {
		// 没有文件需要处理
		// 即使没有文件处理，也要设置TotalDuration
		bp.mutex.Lock()
		if !bp.stats.StartTime.IsZero() {
			bp.stats.TotalDuration = time.Since(bp.stats.StartTime)
		}
		bp.mutex.Unlock()
		return nil
	}

	// 开始批处理原子性操作

	// 阶段1: 预检查 - 确保所有文件都可以安全处理
	if err := bp.preValidateAllFiles(taskQueue); err != nil {
		return bp.converter.errorHandler.WrapError("预检查失败，批处理中止", err)
	}

	// 阶段2: 原子性批处理 - 要么全部成功，要么全部回滚
	if err := bp.atomicBatchProcess(taskQueue); err != nil {
		return bp.converter.errorHandler.WrapError("原子性批处理失败", err)
	}

	// 批处理原子性操作完成
	return nil
}

// GetTaskQueue 获取任务队列
// GetTaskQueue 返回任务队列的只读视图，避免不必要的内存拷贝
func (bp *BatchProcessor) GetTaskQueue() []*MediaFile {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()
	return bp.taskQueue
}

// GetCorruptedFiles 返回损坏文件列表的只读视图，避免不必要的内存拷贝
func (bp *BatchProcessor) GetCorruptedFiles() []*MediaFile {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()
	return bp.corruptedFiles
}

// processFiles 处理文件
func (bp *BatchProcessor) processFiles(files []*MediaFile) error {
	// 注意：TotalFiles和StartTime已经在ScanAndAnalyze中设置，这里不需要重复设置

	// 启动转换进度条
	ui.StartNamedProgress("convert", int64(len(files)), "转换文件")
	defer ui.FinishNamedProgress("convert")

	// 记录开始处理
	// 开始批处理文件

	// 使用ants池进行并发控制，替代原有的goroutine+channel模式
	processedCount := int64(0)

	// 获取转换器的工作池
	workerPool := bp.converter.GetWorkerPool()
	if workerPool == nil {
		return fmt.Errorf("无法获取工作池")
	}

	// 使用WaitGroup等待所有任务完成
	var wg sync.WaitGroup

	// 提交所有文件处理任务到ants池
	for i, file := range files {
		// 检查是否收到中断信号
		select {
		case <-bp.ctx.Done():
			// 收到中断信号，停止启动新的转换任务
			return bp.ctx.Err()
		default:
		}

		wg.Add(1)
		currentFile := file

		// 提交任务到高级ants池，使用普通优先级
		taskID := fmt.Sprintf("batch_task_%d", i)
		err := workerPool.SubmitWithPriority(func() {
			defer wg.Done()

			// 在任务内部检查中断信号
			select {
			case <-bp.ctx.Done():
				return
			default:
			}

			// 处理文件（Converter的processFile方法会自动更新统计信息）
			_ = bp.converter.processFile(currentFile)
			// 注意：UpdateStats已经在processFile内部调用，这里不需要重复调用

			// 更新进度
			processed := atomic.AddInt64(&processedCount, 1)
			var progressMsg strings.Builder
			progressMsg.WriteString("转换中 (")
			progressMsg.WriteString(strconv.FormatInt(processed, 10))
			progressMsg.WriteString("/")
			progressMsg.WriteString(strconv.Itoa(len(files)))
			progressMsg.WriteString(")")
			ui.UpdateNamedProgress("convert", processed, progressMsg.String())
		}, PriorityNormal, taskID)

		if err != nil {
			wg.Done() // 如果提交失败，需要减少计数器
			bp.logger.Error("提交任务到高级池失败", zap.String("file", currentFile.Path), zap.Error(err))
		}
	}

	// 等待所有任务完成
	wg.Wait()

	return nil
}
