package converter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pixly/config"
	"pixly/internal/theme"
	"pixly/internal/ui"

	"go.uber.org/zap"
)

// MediaType 媒体类型枚举
type MediaType string

const (
	TypeImage MediaType = "image"
	TypeVideo MediaType = "video"
	// TypeAudio 已删除 - 根据README要求，本程序不处理音频文件
	TypeUnknown MediaType = "unknown"
)

// MediaFile 媒体文件结构
type MediaFile struct {
	Path         string
	Name         string
	Size         int64
	Extension    string
	Type         MediaType
	ModTime      time.Time
	IsCorrupted  bool
	IsLowQuality bool
	// 添加编解码器和容器不兼容标记
	IsCodecIncompatible     bool
	IsContainerIncompatible bool
	SkipReason             string // 跳过原因，用于记录为何跳过此文件
}

// ConversionMode 转换模式枚举
type ConversionMode string

const (
	ModeAutoPlus ConversionMode = "auto+"
	ModeQuality  ConversionMode = "quality"
	ModeEmoji    ConversionMode = "emoji"
)

// ConversionStats 转换统计信息
type ConversionStats struct {
	TotalFiles       int
	ProcessedFiles   int
	SuccessfulFiles  int
	FailedFiles      int
	SkippedFiles     int // 跳过的文件数量
	TotalSize        int64
	CompressedSize   int64
	StartTime        time.Time
	TotalDuration    time.Duration
	CompressionRatio float64
}

// ConversionResult 转换结果
type ConversionResult struct {
	OriginalFile     *MediaFile
	OutputPath       string
	OriginalSize     int64
	CompressedSize   int64
	CompressionRatio float64
	Duration         time.Duration
	Success          bool
	Method           string
	Error            error
	Skipped          bool   // 文件是否被跳过
	SkipReason       string // 跳过原因
}

// Converter 转换器主结构
type Converter struct {
	config           *config.Config
	logger           *zap.Logger
	mode             ConversionMode
	themeManager     *theme.ThemeManager
	stats            *ConversionStats
	results          []*ConversionResult
	strategy         ConversionStrategy
	watchdog         *ProgressWatchdog
	atomicOps        *AtomicFileOperations
	metadataManager  *MetadataManager
	toolManager      *ToolManager
	fileTypeDetector *FileTypeDetector
	checkpointMgr    *CheckpointManager
	signalHandler    *SignalHandler
	fileOpHandler    *FileOperationHandler // 统一文件操作处理器
	errorHandler     *ErrorHandler         // 统一错误处理器
	memoryPool       *MemoryPool           // 内存池

	// 增强系统组件已删除 - 根据"好品味"原则，删除过度设计的复杂日志系统

	// 控制信号
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 并发控制

	advancedPool *AdvancedPool // 高级ants池管理器

	// 线程安全
	mutex sync.RWMutex
}

// NewConverter 创建新的转换器实例
func NewConverter(config *config.Config, logger *zap.Logger, mode string) (*Converter, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建主题管理器
	themeManager := theme.NewThemeManager()

	// 增强扫描器已移除，简化架构

	// 创建看门狗
	watchdogConfig := GetDefaultWatchdogConfig()
	watchdog := NewProgressWatchdog(watchdogConfig, logger)

	// 创建统一处理器
	errorHandler := NewErrorHandler(logger)
	fileOpHandler := NewFileOperationHandler(logger)

	// 创建高级ants池配置
	advancedPoolConfig := GetDefaultAdvancedPoolConfig()
	advancedPoolConfig.InitialSize = config.Concurrency.ConversionWorkers
	advancedPoolConfig.MaxSize = config.Concurrency.ConversionWorkers * 2
	advancedPoolConfig.MinSize = 2
	advancedPoolConfig.EnablePriority = true
	advancedPoolConfig.EnableMetrics = true

	// 创建高级ants池（统一并发控制）
	advancedPool, err := NewAdvancedPool(advancedPoolConfig, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建高级ants池失败: %w", err)
	}

	converter := &Converter{
		config:           config,
		logger:           logger,
		mode:             ConversionMode(mode),
		themeManager:     themeManager,
		stats:            &ConversionStats{},
		results:          make([]*ConversionResult, 0),
		watchdog:         watchdog,
		atomicOps:        NewAtomicFileOperations(logger, config, errorHandler),
		metadataManager:  NewMetadataManager(logger, config, errorHandler),
		toolManager:      NewToolManager(config, logger, errorHandler),
		fileTypeDetector: NewFileTypeDetector(config, logger, NewToolManager(config, logger, errorHandler)),
		errorHandler:     errorHandler,
		fileOpHandler:    fileOpHandler,
		memoryPool:       GetGlobalMemoryPool(logger),
		ctx:              ctx,
		cancel:           cancel,
		advancedPool:     advancedPool,
	}

	// 创建转换策略
	converter.strategy = NewStrategy(converter.mode, converter)

	// 移除传统channel池，统一使用高级ants池

	// 初始化checkpoint管理器
	checkpointMgr, err := NewCheckpointManager(logger, "", errorHandler)
	if err != nil {
		advancedPool.Close() // 清理高级ants池
		return nil, errorHandler.WrapError("初始化checkpoint管理器失败", err)
	}
	converter.checkpointMgr = checkpointMgr

	// 初始化信号处理器
	signalHandler := NewSignalHandler(logger, converter, checkpointMgr)
	converter.signalHandler = signalHandler

	// 启动看门狗
	converter.watchdog.Start()

	return converter, nil
}

// NewConverterWithWatchdog 创建新的转换器实例，支持自定义看门狗配置
func NewConverterWithWatchdog(config *config.Config, logger *zap.Logger, mode string, watchdogConfig *WatchdogConfig) (*Converter, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建主题管理器
	themeManager := theme.NewThemeManager()

	// 创建统一错误处理器
	errorHandler := NewErrorHandler(logger)

	// 创建文件操作处理器
	fileOpHandler := NewFileOperationHandler(logger)

	// 创建看门狗
	watchdog := NewProgressWatchdog(watchdogConfig, logger)

	// 创建高级ants池配置
	advancedPoolConfig := GetDefaultAdvancedPoolConfig()
	advancedPoolConfig.InitialSize = config.Concurrency.ConversionWorkers
	advancedPoolConfig.MaxSize = config.Concurrency.ConversionWorkers * 2
	advancedPoolConfig.MinSize = 2
	advancedPoolConfig.EnablePriority = true
	advancedPoolConfig.EnableMetrics = true

	// 创建高级ants池（统一并发控制）
	advancedPool, err := NewAdvancedPool(advancedPoolConfig, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建高级ants池失败: %w", err)
	}

	converter := &Converter{
		config:           config,
		logger:           logger,
		mode:             ConversionMode(mode),
		themeManager:     themeManager,
		stats:            &ConversionStats{},
		results:          make([]*ConversionResult, 0),
		watchdog:         watchdog,
		atomicOps:        NewAtomicFileOperations(logger, config, errorHandler),
		metadataManager:  NewMetadataManager(logger, config, errorHandler),
		toolManager:      NewToolManager(config, logger, errorHandler),
		fileTypeDetector: NewFileTypeDetector(config, logger, NewToolManager(config, logger, errorHandler)),
		errorHandler:     errorHandler,
		fileOpHandler:    fileOpHandler,
		memoryPool:       GetGlobalMemoryPool(logger),
		ctx:              ctx,
		cancel:           cancel,
		advancedPool:     advancedPool,
	}

	// 创建转换策略
	converter.strategy = NewStrategy(converter.mode, converter)

	// 初始化checkpoint管理器
	checkpointMgr, err := NewCheckpointManager(logger, "", errorHandler)
	if err != nil {
		advancedPool.Close() // 清理高级ants池
		return nil, errorHandler.WrapError("初始化checkpoint管理器失败", err)
	}
	converter.checkpointMgr = checkpointMgr

	// 初始化信号处理器
	signalHandler := NewSignalHandler(logger, converter, checkpointMgr)
	converter.signalHandler = signalHandler

	// 启动看门狗
	converter.watchdog.Start()

	return converter, nil
}

// GetWorkerPool 获取工作池，用于批处理器的并发控制
func (c *Converter) GetWorkerPool() *AdvancedPool {
	return c.advancedPool
}

// Convert 执行转换操作
func (c *Converter) Convert(inputDir string) error {
	// 启动信号处理器
	c.signalHandler.Start()
	defer c.signalHandler.Stop()

	// 检查是否有未完成的会话需要恢复
	sessions, err := c.checkpointMgr.ListSessions()
	if err != nil {
		c.logger.Warn("检查断点会话失败", zap.Error(err))
	} else if len(sessions) > 0 {
		// 找到未完成的会话，询问用户是否恢复
		for _, session := range sessions {
			if session.TargetDir == inputDir && session.Mode == string(c.mode) {
				// 发现未完成的转换会话

				// 恢复会话
				_, err = c.checkpointMgr.ResumeSession(session.SessionID)
				if err != nil {
					c.logger.Warn("恢复会话失败，将启动新会话", zap.Error(err))
					break
				}

				// 成功恢复转换会话，继续处理未完成的文件
				return c.resumeConversion(inputDir)
			}
		}
	}

	// 检查路径权限
	if err := c.checkPathPermissions(inputDir); err != nil {
		return c.errorHandler.WrapError("路径权限检查失败", err)
	}

	// 初始化统计信息开始时间
	c.mutex.Lock()
	c.stats.StartTime = time.Now()
	c.mutex.Unlock()

	// 创建批处理器
	batchProcessor := NewBatchProcessor(c, c.logger)

	// 使用批处理器进行统一扫描和分析
	if err := batchProcessor.ScanAndAnalyze(inputDir); err != nil {
		return c.errorHandler.WrapError("扫描和分析文件失败", err)
	}

	// 检查是否有文件需要处理
	if c.stats.TotalFiles == 0 {
		c.logger.Info("没有找到需要转换的文件")
		return nil
	}

	// 启动新的转换会话
	err = c.checkpointMgr.StartSession(inputDir, string(c.mode), c.stats.TotalFiles)
	if err != nil {
		return c.errorHandler.WrapError("启动转换会话失败", err)
	}

	// 处理损坏文件
	if err := batchProcessor.HandleCorruptedFiles(); err != nil {
		// 记录错误但不中断转换过程
		c.logger.Warn("处理损坏文件时出错", zap.Error(err))
	}

	// 处理任务队列
	if err := batchProcessor.ProcessTaskQueue(); err != nil {
		return c.errorHandler.WrapError("处理任务队列失败", err)
	}

	// 等待所有goroutine完成
	c.wg.Wait()

	// 最终计算统计信息
	if !c.stats.StartTime.IsZero() {
		c.stats.TotalDuration = time.Since(c.stats.StartTime)
	}

	// 生成报告
	if err := c.generateReport(); err != nil {
		c.logger.Error("生成报告失败", zap.Error(err))
	}

	// 清理会话
	sessionInfo := c.checkpointMgr.GetSessionInfo()
	if sessionInfo != nil {
		if err := c.checkpointMgr.CleanupSession(sessionInfo.SessionID); err != nil {
			c.logger.Warn("清理会话失败", zap.Error(err))
		}
	}

	return nil
}

// checkPathPermissions 检查路径权限和白名单
func (c *Converter) checkPathPermissions(inputDir string) error {
	// 创建路径安全检查器
	pathChecker := NewPathSecurityChecker(c.config, c.logger, c.errorHandler)

	// 使用统一的安全检查选项（允许文件或目录）
	options := SecurityCheckOptions{
		CheckRead:        true,
		CheckWrite:       c.config.Output.KeepOriginal || c.config.Output.GenerateReport,
		RequireDirectory: false,
		CheckWhitelist:   true,
		CheckBlacklist:   true,
	}

	err := pathChecker.ValidatePath(inputDir, options)
	if err != nil {
		c.logger.Error("路径权限检查失败", zap.String("path", inputDir), zap.Error(err))
		return err
	}

	// 路径权限检查通过
	return nil
}

// scanFiles 扫描目录中的文件
func (c *Converter) scanFiles(inputDir string) ([]*MediaFile, error) {
	// 简化文件扫描逻辑
	// 开始扫描文件

	// 使用现代化的 filepath.WalkDir 进行文件遍历
	var files []*MediaFile
	err := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !c.isMediaFile(path) {
			return nil
		}

		// 获取文件信息
		info, err := d.Info()
		if err != nil {
			c.logger.Error("获取文件信息失败", zap.String("路径", path), zap.Error(err))
			return nil
		}

		// 使用GlobalPathUtils处理路径
		normalizedPath, pathErr := GlobalPathUtils.NormalizePath(path)
		if pathErr != nil {
			c.logger.Error("路径规范化失败", zap.String("路径", path), zap.Error(pathErr))
			return nil
		}
		mediaFile := &MediaFile{
			Path:      normalizedPath,
			Name:      GlobalPathUtils.GetBaseName(normalizedPath),
			Size:      info.Size(),
			Extension: strings.ToLower(GlobalPathUtils.GetExtension(normalizedPath)),
			Type:      c.getFileType(GlobalPathUtils.GetExtension(normalizedPath)),
			ModTime:   info.ModTime(),
		}
		files = append(files, mediaFile)
		return nil
	})

	if err != nil {
		c.logger.Error("文件扫描失败", zap.Error(err))
		return nil, err
	}

	// 文件扫描完成
	return files, nil
}

// isMediaFile 检查是否为媒体文件
func (c *Converter) isMediaFile(path string) bool {
	// 使用GlobalPathUtils处理路径
	normalizedPath, err := GlobalPathUtils.NormalizePath(path)
	if err != nil {
		return false
	}
	ext := strings.ToLower(GlobalPathUtils.GetExtension(normalizedPath))

	// 使用配置文件中的支持扩展名白名单
	for _, supportedExt := range c.config.Conversion.SupportedExtensions {
		// 确保扩展名格式一致（都以.开头）
		normalizedSupportedExt := strings.ToLower(supportedExt)
		if !strings.HasPrefix(normalizedSupportedExt, ".") {
			normalizedSupportedExt = "." + normalizedSupportedExt
		}
		if ext == normalizedSupportedExt {
			return true
		}
	}

	return false
}

// getFileType 获取文件类型
func (c *Converter) getFileType(filePath string) MediaType {
	ext := strings.ToLower(GlobalPathUtils.GetExtension(filePath))
	
	// 修复扩展名处理：确保点前缀
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	
	// 映射扩展名到统一格式
	switch ext {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png":
		return ".png"
	case ".gif":
		return ".gif"
	case ".webp":
		return ".webp"
	case ".avif":
		return ".avif"
	case ".jxl", ".jpeg-xl":
		return ".jxl"
	case ".jfif":
		return ".jpg" // JFIF作为JPEG处理
	default:
		return MediaType(ext)
	}
}

// processFiles 处理文件
func (c *Converter) processFiles(files []*MediaFile) error {
	// 注意：统计信息已由BatchProcessor在ScanAndAnalyze阶段设置
	// 启动转换进度条
	ui.StartDynamicProgress(int64(len(files)), "转换处理")
	defer ui.FinishDynamicProgress()
	// 开始处理文件

	// 创建结果通道
	resultChan := make(chan *ConversionResult, len(files))

	// 启动goroutine处理每个文件
	for _, file := range files {
		c.wg.Add(1)
		file := file // 避免闭包问题

		// 确定任务优先级（大文件优先处理）
		priority := PriorityNormal
		if file.Size > 50*1024*1024 { // 50MB以上的文件
			priority = PriorityHigh
		} else if file.Size < 1024*1024 { // 1MB以下的文件
			priority = PriorityLow
		}

		// 使用统一的高级池进行并发控制
		var taskIDBuilder strings.Builder
		taskIDBuilder.WriteString("file_")
		taskIDBuilder.WriteString(filepath.Base(file.Path))
		taskIDBuilder.WriteString("_")
		taskIDBuilder.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
		taskID := taskIDBuilder.String()
		err := c.advancedPool.SubmitWithPriority(func() {
			defer c.wg.Done()
			result := c.processFile(file)
			resultChan <- result
		}, priority, taskID)

		if err != nil {
			c.wg.Done() // 如果提交失败，需要减少计数器
			c.logger.Error("提交任务到高级池失败", zap.String("file", file.Path), zap.Error(err))
			// 创建失败结果
			result := &ConversionResult{
				OriginalFile: file,
				OriginalSize: file.Size,
				Error:        err,
				Success:      false,
			}
			resultChan <- result
		}
	}

	// 收集结果
	go func() {
		c.wg.Wait()
		close(resultChan)
	}()

	// 处理结果
	processedCount := int64(0)
	for result := range resultChan {
		// 在归还到内存池之前，先创建快照，避免后续复用导致数据被清零
		snapshot := *result

		c.mutex.Lock()
		c.results = append(c.results, &snapshot)
		c.mutex.Unlock()

		// 更新统计信息使用快照，保证一致性
		c.UpdateStats(&snapshot)

		// 结果已复制，安全归还原对象到内存池
		c.memoryPool.PutConversionResult(result)

		// 更新进度
		processedCount++
		ui.UpdateDynamicProgress(int64(processedCount), "转换处理")

		// 记录处理结果
		if snapshot.Success {
			// 文件处理成功
		} else {
			c.logger.Error("文件处理失败", zap.String("文件", snapshot.OriginalFile.Name), zap.Error(snapshot.Error))
		}
	}

	// 等待所有goroutine完成
	c.wg.Wait()

	// 进度条完成由BatchProcessor统一管理

	c.stats.TotalDuration = time.Since(c.stats.StartTime)
	// 文件处理完成
	return nil
}

// processFile 处理单个文件
func (c *Converter) processFile(file *MediaFile) *ConversionResult {
	startTime := time.Now()

	c.logger.Debug("开始处理文件", zap.String("file", file.Path), zap.String("type", string(file.Type)))

	// 标记文件开始处理
	if err := c.checkpointMgr.UpdateFileStatus(file.Path, StatusProcessing, "", ""); err != nil {
		c.logger.Warn("更新文件状态失败", zap.String("file", file.Path), zap.Error(err))
	}

	// 从内存池获取ConversionResult对象
	result := c.memoryPool.GetConversionResult()
	result.OriginalFile = file
	result.OriginalSize = file.Size
	// 初始化CompressedSize为0
	result.CompressedSize = 0

	defer func() {
		result.Duration = time.Since(startTime)

		// 保存最终状态
		var status FileStatus
		var errorMsg string
		if result.Success {
			status = StatusCompleted
		} else if result.Skipped {
			status = StatusSkipped
			errorMsg = result.SkipReason
		} else {
			status = StatusFailed
			if result.Error != nil {
				errorMsg = result.Error.Error()
			}
		}

		if err := c.checkpointMgr.UpdateFileStatus(file.Path, status, errorMsg, result.OutputPath); err != nil {
			c.logger.Warn("保存文件最终状态失败", zap.String("file", file.Path), zap.Error(err))
		}

		// 更新统计信息
		c.UpdateStats(result)

		c.logger.Debug("文件处理完成",
			zap.String("file", file.Path),
			zap.Bool("success", result.Success),
			zap.String("output", result.OutputPath),
			zap.Error(result.Error))
	}()

	// 使用文件类型检测器精确识别文件类型
	if c.fileTypeDetector != nil {
		c.logger.Debug("开始文件类型检测", zap.String("file", file.Path))
		details, err := c.fileTypeDetector.DetectFileType(file.Path)
		if err == nil && !details.IsCorrupted {
			// 根据精确的文件类型更新文件信息
			switch details.FileType {
			case FileTypeVideo:
				file.Type = TypeVideo
				c.logger.Debug("检测为视频文件", zap.String("file", file.Path))
			// 音频文件不再支持，跳过处理
			case FileTypeAnimatedImage:
				file.Type = TypeImage
				c.logger.Debug("检测为动图文件", zap.String("file", file.Path))
				// 标记为动图
				// 这里可以添加额外的标记逻辑
			case FileTypeStaticImage:
				file.Type = TypeImage
				c.logger.Debug("检测为静态图片文件", zap.String("file", file.Path))
			}
		} else if err != nil {
			c.logger.Warn("文件类型检测失败", zap.String("file", file.Path), zap.Error(err))
		} else if details.IsCorrupted {
			c.logger.Warn("检测到损坏文件", zap.String("file", file.Path))
		}
	}

	// 直接处理文件，避免不必要的goroutine创建
	// 根据文件类型分发处理逻辑
	c.logger.Debug("开始文件类型分发处理", zap.String("file", file.Path), zap.String("type", string(file.Type)))
	switch file.Type {
	case TypeImage:
		c.logger.Debug("开始图片转换处理", zap.String("file", file.Path))
		// 使用策略模式处理图片转换
		var err error
		var outputPath string

		// 调用对应的转换策略
		outputPath, err = c.strategy.ConvertImage(file)
		if err != nil {
			c.logger.Error("图片转换失败", zap.String("file", file.Path), zap.Error(err))
			result.Error = c.errorHandler.WrapError("图片转换失败", err)
			result.Success = false
		} else {
			c.logger.Debug("图片转换成功", zap.String("file", file.Path), zap.String("output", outputPath))
			result.OutputPath = outputPath
			// 获取实际转换后的文件大小
			if stat, err := os.Stat(outputPath); err == nil {
				result.CompressedSize = stat.Size()
				result.Success = true
				c.logger.Debug("获取输出文件信息成功", zap.String("file", file.Path), zap.Int64("size", stat.Size()))
			} else {
				c.logger.Error("无法获取输出文件信息", zap.String("file", file.Path), zap.Error(err))
				result.Error = c.errorHandler.WrapError("无法获取输出文件信息", err)
				result.Success = false
			}
		}

	case TypeVideo:
		c.logger.Debug("开始视频转换处理", zap.String("file", file.Path))
		// 使用策略模式处理视频转换
		outputPath, err := c.strategy.ConvertVideo(file)
		if err != nil {
			c.logger.Error("视频转换失败", zap.String("file", file.Path), zap.Error(err))
			result.Error = c.errorHandler.WrapError("视频转换失败", err)
			result.Success = false
		} else {
			c.logger.Debug("视频转换成功", zap.String("file", file.Path), zap.String("output", outputPath))
			result.OutputPath = outputPath
			// 获取实际转换后的文件大小
			if stat, err := os.Stat(outputPath); err == nil {
				result.CompressedSize = stat.Size()
				result.Success = true
				c.logger.Debug("获取输出文件信息成功", zap.String("file", file.Path), zap.Int64("size", stat.Size()))
			} else {
				c.logger.Error("无法获取输出文件信息", zap.String("file", file.Path), zap.Error(err))
				result.Error = c.errorHandler.WrapError("无法获取输出文件信息", err)
				result.Success = false
			}
		}

	default:
		var errorBuilder strings.Builder
		errorBuilder.WriteString("不支持的文件类型: ")
		errorBuilder.WriteString(string(file.Type))
		c.logger.Error("不支持的文件类型", zap.String("file", file.Path), zap.String("type", string(file.Type)))
		result.Error = c.errorHandler.WrapError(errorBuilder.String(), nil)
		result.Success = false
		// 不支持的文件类型
	}

	return result
}

// UpdateStats 更新统计信息
func (c *Converter) UpdateStats(result *ConversionResult) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.stats.ProcessedFiles++

	// 累加原始文件大小到总大小
	c.stats.TotalSize += result.OriginalSize

	if result.Success {
		if result.Skipped {
			c.stats.SkippedFiles++
			// 跳过的文件保持原始大小
			c.stats.CompressedSize += result.OriginalSize
		} else {
			c.stats.SuccessfulFiles++
			c.stats.CompressedSize += result.CompressedSize
		}
	} else {
		c.stats.FailedFiles++
		// 失败的文件也累加原始大小到压缩后大小，以避免压缩率为负数
		c.stats.CompressedSize += result.OriginalSize
	}

	// 计算压缩率
	if c.stats.TotalSize > 0 {
		// 确保压缩率不会是负数，当压缩后大小大于原始大小时，压缩率为0
		if c.stats.CompressedSize < c.stats.TotalSize {
			c.stats.CompressionRatio = float64(c.stats.TotalSize-c.stats.CompressedSize) / float64(c.stats.TotalSize) * 100
		} else {
			c.stats.CompressionRatio = 0
		}
	} else {
		c.stats.CompressionRatio = 0
	}
}

// isTargetFormat 检查是否为目标格式
func (c *Converter) IsTargetFormat(ext string) bool {
	// 严格定义最终目标格式：只有.jxl、.avif、.mov是最终目标格式
	// 其他所有格式（包括.mp4、.webm、.avi等）都不是目标格式，需要被转换或跳过
	ext = strings.ToLower(ext)
	switch ext {
	case ".jxl", ".avif", ".mov":
		return true
	default:
		return false
	}
}

// convertToAVIFAnimated 转换动图为AVIF（使用ffmpeg）
func (c *Converter) ConvertToAVIFAnimated(file *MediaFile) (string, error) {
	outputPath := c.getOutputPath(file, ".avif")

	// 使用FFmpeg将动图转换为AVIF格式
	// 根据README规定：表情包模式下动图使用ffmpeg处理
	fps, err := c.getVideoFPS(file.Path)
	if err != nil {
		return "", c.errorHandler.WrapError("获取视频帧率失败", err)
	}

	args := []string{
		"-i", file.Path,
		"-c:v", "libaom-av1", // 使用libaom AV1编码器
		"-crf", "30", // 适度压缩
		"-b:v", "0", // 使用CRF模式
		"-pix_fmt", "yuv420p", // 像素格式
		"-auto-alt-ref", "0", // 禁用自动参考帧
		"-lag-in-frames", "0", // 禁用帧延迟
		"-r", fmt.Sprintf("%f", fps), // 转换为字符串
		"-y", // 覆盖输出文件
		outputPath,
	}

	cmd := exec.CommandContext(c.ctx, c.config.Tools.FFmpegPath, args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", c.errorHandler.WrapErrorWithOutput("ffmpeg AVIF animation conversion failed", err, output)
	}

	return outputPath, nil
}

// HandleCodecIncompatibility 处理编解码器不兼容文件
func (c *Converter) HandleCodecIncompatibility(files []*MediaFile) error {
	incompatibleFiles := make([]*MediaFile, 0)
	for _, file := range files {
		if file.IsCodecIncompatible {
			incompatibleFiles = append(incompatibleFiles, file)
		}
	}

	if len(incompatibleFiles) == 0 {
		return nil
	}

	// 检查是否启用了自动处理策略
	strategy := c.config.ProblemFileHandling.CodecIncompatibilityStrategy
	if strategy != "" && strategy != "ignore" {
		// 使用配置驱动的编解码器不兼容文件处理策略

		switch strategy {
		case "force_process":
			// 强制处理，将这些文件添加到任务队列
			// 强制处理编解码器不兼容文件
			// 这些文件已经在任务队列中，不需要额外操作
			return nil

		case "move_to_trash":
			// 移动到垃圾箱
			// 使用GlobalPathUtils处理路径
			firstFilePath, err := GlobalPathUtils.NormalizePath(incompatibleFiles[0].Path)
			if err != nil {
				return c.errorHandler.WrapError("无法规范化文件路径", err)
			}
			trashDir, err := GlobalPathUtils.JoinPath(GlobalPathUtils.GetDirName(firstFilePath), ".trash")
			if err != nil {
				c.logger.Error("构建垃圾箱目录路径失败", zap.Error(err))
				return nil
			}
			if err := os.MkdirAll(trashDir, 0755); err != nil {
				c.logger.Error("创建垃圾箱目录失败", zap.String("dir", trashDir), zap.Error(err))
				// 回退到忽略策略
				return nil
			}

			// 移动编解码器不兼容文件到垃圾箱
			for _, file := range incompatibleFiles {
				// 使用GlobalPathUtils处理文件路径
				normalizedFilePath, err := GlobalPathUtils.NormalizePath(file.Path)
				if err != nil {
					c.logger.Error("路径规范化失败", zap.String("路径", file.Path), zap.Error(err))
					continue
				}
				trashPath, err := GlobalPathUtils.JoinPath(trashDir, GlobalPathUtils.GetBaseName(normalizedFilePath))
				if err != nil {
					c.logger.Error("构建垃圾箱文件路径失败", zap.String("路径", normalizedFilePath), zap.Error(err))
					continue
				}
				if err := os.Rename(file.Path, trashPath); err != nil {
					c.logger.Error("移动编解码器不兼容文件到垃圾箱失败", zap.String("file", file.Path), zap.String("trash_path", trashPath), zap.Error(err))
				} else {
					// 已移动编解码器不兼容文件到垃圾箱
				}
			}
			return nil

		case "delete":
			// 删除文件
			// 删除编解码器不兼容文件
			for _, file := range incompatibleFiles {
				if err := os.Remove(file.Path); err != nil {
					c.logger.Error("删除编解码器不兼容文件失败", zap.String("file", file.Path), zap.Error(err))
				} else {
					// 已删除编解码器不兼容文件
				}
			}
			return nil
		}
	}

	// 默认忽略策略
	// 忽略编解码器不兼容文件
	return nil
}

// HandleContainerIncompatibility 处理容器不兼容文件
func (c *Converter) HandleContainerIncompatibility(files []*MediaFile) error {
	incompatibleFiles := make([]*MediaFile, 0)
	for _, file := range files {
		if file.IsContainerIncompatible {
			incompatibleFiles = append(incompatibleFiles, file)
		}
	}

	if len(incompatibleFiles) == 0 {
		return nil
	}

	// 检查是否启用了自动处理策略
	strategy := c.config.ProblemFileHandling.ContainerIncompatibilityStrategy
	if strategy != "" && strategy != "ignore" {
		// 使用配置驱动的容器不兼容文件处理策略

		switch strategy {
		case "force_process":
			// 强制处理，将这些文件添加到任务队列
			// 强制处理容器不兼容文件
			// 这些文件已经在任务队列中，不需要额外操作
			return nil

		case "move_to_trash":
			// 移动到垃圾箱
			// 使用GlobalPathUtils处理路径
			firstFilePath, err := GlobalPathUtils.NormalizePath(incompatibleFiles[0].Path)
			if err != nil {
				return c.errorHandler.WrapError("无法规范化文件路径", err)
			}
			trashDir, err := GlobalPathUtils.JoinPath(GlobalPathUtils.GetDirName(firstFilePath), ".trash")
			if err != nil {
				c.logger.Error("构建垃圾箱目录路径失败", zap.Error(err))
				return nil
			}
			if err := os.MkdirAll(trashDir, 0755); err != nil {
				c.logger.Error("创建垃圾箱目录失败", zap.String("dir", trashDir), zap.Error(err))
				// 回退到忽略策略
				return nil
			}

			// 移动容器不兼容文件到垃圾箱
			for _, file := range incompatibleFiles {
				// 使用GlobalPathUtils处理文件路径
				normalizedFilePath, err := GlobalPathUtils.NormalizePath(file.Path)
				if err != nil {
					c.logger.Error("路径规范化失败", zap.String("路径", file.Path), zap.Error(err))
					continue
				}
				trashPath, err := GlobalPathUtils.JoinPath(trashDir, GlobalPathUtils.GetBaseName(normalizedFilePath))
				if err != nil {
					c.logger.Error("构建垃圾箱文件路径失败", zap.String("路径", normalizedFilePath), zap.Error(err))
					continue
				}
				if err := os.Rename(file.Path, trashPath); err != nil {
					c.logger.Error("移动容器不兼容文件到垃圾箱失败", zap.String("file", file.Path), zap.String("trash_path", trashPath), zap.Error(err))
				} else {
					// 已移动容器不兼容文件到垃圾箱
				}
			}
			return nil

		case "delete":
			// 删除文件
			// 删除容器不兼容文件
			for _, file := range incompatibleFiles {
				if err := os.Remove(file.Path); err != nil {
					c.logger.Error("删除容器不兼容文件失败", zap.String("file", file.Path), zap.Error(err))
				} else {
					// 已删除容器不兼容文件
				}
			}
			return nil
		}
	}

	// 默认忽略策略
	// 忽略容器不兼容文件
	return nil
}

// 以下方法已在各自文件中实现，这里只保留声明
// hasTransparency 在image.go中实现
// isAnimated 在image.go中实现
// convertToJXLLossless 在image.go中实现
// convertToJXL 在image.go中实现
// convertToAVIF 在image.go中实现
// convertVideoContainer 在video.go中实现
// generateReport 在report.go中实现

// GetOutputPathForTest 用于测试的getOutputPath包装函数
func (c *Converter) GetOutputPathForTest(file *MediaFile, newExt string) string {
	return c.getOutputPath(file, newExt)
}

// IsAnimatedForTest 用于测试的isAnimated包装函数
func (c *Converter) IsAnimatedForTest(path string) bool {
	return c.isAnimated(path)
}

// ConvertToJXLLosslessForTest 用于测试的convertToJXLLossless包装函数
func (c *Converter) ConvertToJXLLosslessForTest(file *MediaFile) (string, error) {
	return c.convertToJXLLossless(file)
}

// RequestStop 请求停止转换器 - 优雅停止
func (c *Converter) RequestStop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 收到停止请求，开始优雅停止转换器

	// 取消上下文，通知所有goroutine停止
	c.cancel()
}

// Close 关闭转换器
func (c *Converter) Close() error {
	// 取消上下文
	c.cancel()

	// 等待所有goroutine完成
	c.wg.Wait()

	// 关闭高级ants池
	if c.advancedPool != nil {
		if err := c.advancedPool.Close(); err != nil {
			c.logger.Warn("关闭高级ants池失败", zap.Error(err))
		}
	}

	// 停止看门狗
	if c.watchdog != nil {
		c.watchdog.Stop()
	}

	// 停止信号处理器
	if c.signalHandler != nil {
		c.signalHandler.Stop()
	}

	// 关闭checkpoint管理器
	if c.checkpointMgr != nil {
		if err := c.checkpointMgr.Close(); err != nil {
			c.logger.Warn("关闭checkpoint管理器失败", zap.Error(err))
		}
	}

	return nil
}

// ProcessFileForTest 用于测试的文件处理方法
func (c *Converter) ProcessFileForTest(filePath string) *ConversionResult {
	// 创建媒体文件对象
	file := &MediaFile{
		Path:      filePath,
		Name:      filepath.Base(filePath),
		Extension: filepath.Ext(filePath),
		Type:      c.getFileType(filepath.Ext(filePath)),
	}

	// 获取文件信息
	if info, err := os.Stat(filePath); err == nil {
		file.Size = info.Size()
		file.ModTime = info.ModTime()
	}

	// 处理文件
	result := c.processFile(file)

	// 更新统计信息
	c.UpdateStats(result)

	return result
}

// GetFileType 获取文件类型
func (c *Converter) GetFileType(ext string) MediaType {
	return c.getFileType(ext)
}

// GetStats 获取转换统计信息
func (c *Converter) GetStats() *ConversionStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// 更新统计信息
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// 计算压缩比
	if c.stats.TotalSize > 0 {
		// 确保压缩率不会是负数，当压缩后大小大于原始大小时，压缩率为0
		if c.stats.CompressedSize < c.stats.TotalSize {
			c.stats.CompressionRatio = float64(c.stats.TotalSize-c.stats.CompressedSize) / float64(c.stats.TotalSize) * 100
		} else {
			c.stats.CompressionRatio = 0
		}
	} else {
		c.stats.CompressionRatio = 0
	}

	// 计算总耗时
	if !c.stats.StartTime.IsZero() {
		c.stats.TotalDuration = time.Since(c.stats.StartTime)
	}

	// 返回统计信息的副本
	stats := *c.stats
	return &stats
}

// GetPoolMetrics 获取高级池的监控指标
func (c *Converter) GetPoolMetrics() *PoolMetrics {
	if c.advancedPool != nil {
		return c.advancedPool.GetMetrics()
	}
	return nil
}

// GetPoolInfo 获取池的详细信息
func (c *Converter) GetPoolInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// 高级池信息
	if c.advancedPool != nil {
		advancedInfo := c.advancedPool.GetPoolInfo()
		for k, v := range advancedInfo {
			info[k] = v
		}
	}

	return info
}

// TunePoolSize 动态调整池大小
func (c *Converter) TunePoolSize(size int) {
	if c.advancedPool != nil {
		c.advancedPool.Tune(size)
		// 动态调整高级池大小
	}
}

// GetMetadataManager 获取元数据管理器
func (c *Converter) GetMetadataManager() *MetadataManager {
	return c.metadataManager
}

// resumeConversion 恢复未完成的转换会话
func (c *Converter) resumeConversion(inputDir string) error {
	// 获取会话信息并设置统计数据
	sessionInfo := c.checkpointMgr.GetSessionInfo()
	if sessionInfo != nil {
		c.mutex.Lock()
		c.stats.TotalFiles = sessionInfo.TotalFiles
		c.stats.StartTime = sessionInfo.StartTime
		c.mutex.Unlock()
		// 从会话恢复统计信息
	}

	// 获取未完成的文件列表
	pendingFiles, err := c.checkpointMgr.GetPendingFiles()
	if err != nil {
		return c.errorHandler.WrapError("获取未完成文件列表失败", err)
	}

	if len(pendingFiles) == 0 {
		// 所有文件已完成，清理会话
		if sessionInfo != nil {
			if err := c.checkpointMgr.CleanupSession(sessionInfo.SessionID); err != nil {
				c.logger.Warn("清理会话失败",
					zap.String("sessionID", sessionInfo.SessionID),
					zap.Error(err))
			}
		}
		return nil
	}

	// 继续处理未完成的文件

	// 将文件路径转换为MediaFile对象
	var mediaFiles []*MediaFile
	for _, filePath := range pendingFiles {
		// 检查文件是否仍然存在
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.logger.Warn("文件不存在，跳过", zap.String("file", filePath))
			if err := c.checkpointMgr.UpdateFileStatus(filePath, StatusSkipped, "文件不存在", ""); err != nil {
				c.logger.Warn("更新文件状态失败",
					zap.String("file", filePath),
					zap.Error(err))
			}
			continue
		}

		// 创建MediaFile对象
		mediaFile := &MediaFile{
			Path: filePath,
			Name: filepath.Base(filePath),
		}

		// 获取文件信息
		if info, err := os.Stat(filePath); err == nil {
			mediaFile.Size = info.Size()
			mediaFile.ModTime = info.ModTime()
		}

		// 确定文件类型
		ext := strings.ToLower(filepath.Ext(filePath))
		mediaFile.Extension = ext
		mediaFile.Type = c.getFileType(ext)

		mediaFiles = append(mediaFiles, mediaFile)
	}

	if len(mediaFiles) == 0 {
		// 没有有效的未完成文件
		return nil
	}

	// 检查路径权限
	if err := c.checkPathPermissions(inputDir); err != nil {
		return c.errorHandler.WrapError("路径权限检查失败", err)
	}

	// 处理文件
	// 启动动态进度条
	ui.StartDynamicProgress(int64(len(mediaFiles)), "转换进度")
	
	return c.processFiles(mediaFiles)
}
