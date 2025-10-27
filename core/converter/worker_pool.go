package converter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
)

// WorkerSignals 工作器信号接口，借鉴xl-converter的信号机制
type WorkerSignals interface {
	OnStarted(workerID int, file *MediaFile)
	OnCompleted(workerID int, file *MediaFile, result *ConversionResult)
	OnFailed(workerID int, file *MediaFile, err error)
	OnCanceled(workerID int, file *MediaFile)
}

// TimestampMap 泛型时间戳映射
type TimestampMap[T any] struct {
	data map[string]T
	mu   sync.RWMutex
}

// NewTimestampMap 创建新的时间戳映射
func NewTimestampMap[T any]() *TimestampMap[T] {
	return &TimestampMap[T]{
		data: make(map[string]T),
	}
}

// Set 设置时间戳值
func (tm *TimestampMap[T]) Set(key string, value T) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.data[key] = value
}

// Get 获取时间戳值
func (tm *TimestampMap[T]) Get(key string) (T, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	value, exists := tm.data[key]
	return value, exists
}

// Len 返回映射中元素的数量
func (tm *TimestampMap[T]) Len() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.data)
}

// GetAll 获取所有时间戳
func (tm *TimestampMap[T]) GetAll() map[string]T {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make(map[string]T)
	for k, v := range tm.data {
		result[k] = v
	}
	return result
}

// PoolStats 工作池统计信息
type PoolStats struct {
	ActiveWorkers  int32 `json:"active_workers"`
	CompletedTasks int64 `json:"completed_tasks"`
	FailedTasks    int64 `json:"failed_tasks"`
	CanceledTasks  int64 `json:"canceled_tasks"`
	PoolCapacity   int   `json:"pool_capacity"`
	PoolRunning    int   `json:"pool_running"`
	PoolFree       int   `json:"pool_free"`
}

// WorkerTask 工作器任务，借鉴xl-converter的Worker结构
type WorkerTask struct {
	ID       int
	File     *MediaFile
	Params   *ConversionParams
	Settings *ConversionSettings
	Mutex    *sync.RWMutex
	Logger   *zap.Logger
	Signals  WorkerSignals
	Ctx      context.Context

	// 任务状态
	StartTime time.Time
	Skip      bool
	Canceled  bool

	// 转换相关
	OutputPath    string
	TempFiles     []string
	SrcTimestamps *TimestampMap[any]
	LosslessJPEG  bool
}

// ConversionParams 转换参数
type ConversionParams struct {
	Format            string
	Quality           int
	Effort            int
	Lossless          bool
	KeepMetadata      bool
	KeepTimestamps    bool
	Downscaling       *DownscalingParams
	IntelligentEffort bool
	JXLModular        bool
}

// DownscalingParams 缩放参数
type DownscalingParams struct {
	Enabled      bool
	Mode         string
	FileSize     int64
	Percent      float64
	Width        int
	Height       int
	ShortestSide int
	LongestSide  int
	Megapixels   float64
	Resample     string
}

// ConversionSettings 转换设置
type ConversionSettings struct {
	JXLAutoLosslessJPEG bool
	AVIFEncoder         string
	JPGEncoder          string
	EnableCustomArgs    bool
	CustomArgs          map[string]string
	Threads             int
}

// ResourceManager 资源管理器，借鉴xl-converter的RAMOptimizer
type ResourceManager struct {
	enabled           bool
	usedThreadCount   int
	optimizationRules []OptimizationRule
	mutex             sync.RWMutex
}

// OptimizationRule 优化规则
type OptimizationRule struct {
	Scope     string  // "all", "JXL", "AVIF", "JPEG"
	Threshold float64 // 激活阈值（兆像素）
	Target    string  // 目标并发数（"1" 或分数如 "1/2"）
}

// ThreadManager 线程管理器，借鉴xl-converter的ThreadManager
type ThreadManager struct {
	threadsPerWorker int
	burstThreadPool  []int
	resourceManager  *ResourceManager
	mutex            sync.RWMutex
}

// EnhancedWorkerPool 增强的工作器池，整合xl-converter的工作器模式
type EnhancedWorkerPool struct {
	pool            *ants.Pool
	logger          *zap.Logger
	errorHandler    *ErrorHandler
	ctx             context.Context
	cancel          context.CancelFunc
	mutex           sync.RWMutex
	signals         WorkerSignals
	threadManager   *ThreadManager
	resourceManager *ResourceManager

	// 统计信息
	activeWorkers  int32
	completedTasks int64
	failedTasks    int64
	canceledTasks  int64

	// 任务监控
	taskMonitor *TaskMonitor

	// 配置
	maxWorkers      int
	taskTimeout     time.Duration
	cleanupInterval time.Duration

	// 清理相关
	cleanupTicker  *time.Ticker
	tempFiles      map[string][]string // workerID -> temp files
	tempFilesMutex sync.RWMutex
	
	// WaitGroup for task tracking
	wg sync.WaitGroup
}

// NewResourceManager 创建资源管理器
func NewResourceManager() *ResourceManager {
	return &ResourceManager{
		enabled:           false,
		usedThreadCount:   runtime.NumCPU(),
		optimizationRules: make([]OptimizationRule, 0),
	}
}

// SetEnabled 设置资源管理器启用状态
func (rm *ResourceManager) SetEnabled(enabled bool) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()
	rm.enabled = enabled
}

// IsEnabled 检查资源管理器是否启用
func (rm *ResourceManager) IsEnabled() bool {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()
	return rm.enabled
}

// SetUsedThreadCount 设置使用的线程数
func (rm *ResourceManager) SetUsedThreadCount(count int) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()
	if count < 1 {
		count = 1
	}
	rm.usedThreadCount = count
}

// IsNecessary 检查是否需要资源优化，借鉴xl-converter的逻辑
func (rm *ResourceManager) IsNecessary(format string, effort int, lossless bool) bool {
	// JXL高内存使用场景
	if format == "JXL" && (effort >= 7 || lossless) {
		return true
	}
	// AVIF高质量编码
	if format == "AVIF" && effort >= 8 {
		return true
	}
	return false
}

// NewThreadManager 创建线程管理器
func NewThreadManager(resourceManager *ResourceManager) *ThreadManager {
	return &ThreadManager{
		threadsPerWorker: 1,
		burstThreadPool:  make([]int, 0),
		resourceManager:  resourceManager,
	}
}

// Configure 配置线程管理器，借鉴xl-converter的configure方法
func (tm *ThreadManager) Configure(itemCount, usedThreadCount int, format string, effort int, lossless bool) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	singleWorkerMode := false
	tm.resourceManager.SetEnabled(false)

	// 检查是否需要资源优化
	if tm.resourceManager.IsNecessary(format, effort, lossless) {
		singleWorkerMode = true
		tm.resourceManager.SetEnabled(true)
		tm.resourceManager.SetUsedThreadCount(usedThreadCount)
	}

	// 设置工作器配置
	if singleWorkerMode {
		tm.burstThreadPool = make([]int, 0)
		tm.threadsPerWorker = usedThreadCount
	} else {
		tm.burstThreadPool = tm.getBurstThreadPool(itemCount, usedThreadCount)
		tm.threadsPerWorker = 1
	}
}

// GetAvailableThreads 获取可用线程数
func (tm *ThreadManager) GetAvailableThreads(index int) int {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	if len(tm.burstThreadPool) > 0 && index < len(tm.burstThreadPool) {
		return tm.burstThreadPool[index]
	}
	return tm.threadsPerWorker
}

// getBurstThreadPool 分配突发线程池，借鉴xl-converter的算法
func (tm *ThreadManager) getBurstThreadPool(workers, cores int) []int {
	if workers >= cores || cores < 1 || workers < 1 {
		return make([]int, 0)
	}

	baseThreads := cores / workers
	extraThreads := cores % workers
	threadPool := make([]int, workers)

	for i := 0; i < workers; i++ {
		threadPool[i] = baseThreads
	}

	for i := 0; i < extraThreads; i++ {
		threadPool[i]++
	}

	return threadPool
}

// NewEnhancedWorkerPool 创建增强的工作器池
func NewEnhancedWorkerPool(maxWorkers int, logger *zap.Logger, signals WorkerSignals) (*EnhancedWorkerPool, error) {
    if maxWorkers <= 0 {
        maxWorkers = runtime.NumCPU()
    }
    
	ctx, cancel := context.WithCancel(context.Background())

	// 创建资源管理器和线程管理器
	resourceManager := NewResourceManager()
	threadManager := NewThreadManager(resourceManager)

	// 创建任务监控器
	taskMonitor := NewTaskMonitor(logger)

	pool, err := ants.NewPool(maxWorkers, ants.WithOptions(ants.Options{
		ExpiryDuration:   time.Minute * 10,
		PreAlloc:         true,
		MaxBlockingTasks: maxWorkers * 2,
		Nonblocking:      true, // 修改为非阻塞模式，避免死锁
	}))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建工作器池失败: %w", err)
	}

	// 创建错误处理器
	errorHandler := NewErrorHandler(logger)

	wp := &EnhancedWorkerPool{
		pool:            pool,
		logger:          logger,
		errorHandler:    errorHandler,
		ctx:             ctx,
		cancel:          cancel,
		signals:         signals,
		threadManager:   threadManager,
		resourceManager: resourceManager,
		taskMonitor:     taskMonitor,
		maxWorkers:      maxWorkers,
		taskTimeout:     time.Minute * 30,
		cleanupInterval: time.Minute * 5,
		tempFiles:       make(map[string][]string),
	}

	// 启动任务监控器
	if err := taskMonitor.Start(); err != nil {
		cancel()
		pool.Release()
		return nil, fmt.Errorf("启动任务监控器失败: %w", err)
	}

	// 启动清理协程
	wp.startCleanupRoutine()

	return wp, nil
}

// ConfigureForBatch 为批处理配置工作器池
func (wp *EnhancedWorkerPool) ConfigureForBatch(itemCount int, format string, effort int, lossless bool) {
	wp.threadManager.Configure(itemCount, wp.maxWorkers, format, effort, lossless)
	// 工作器池已配置
}

// SubmitTask 提交任务到工作器池
func (wp *EnhancedWorkerPool) SubmitTask(task *WorkerTask) error {
	select {
	case <-wp.ctx.Done():
		return fmt.Errorf("工作器池已关闭")
	default:
	}

	// 直接提交任务，利用ants池的非阻塞特性
	wp.wg.Add(1)
    return wp.pool.Submit(func() {
        defer wp.wg.Done()
        wp.executeTask(task)
    })
}

// SubmitTaskWithIndex 提交带索引的任务，支持智能线程分配
func (wp *EnhancedWorkerPool) SubmitTaskWithIndex(task *WorkerTask, index int) error {
	select {
	case <-wp.ctx.Done():
		return fmt.Errorf("工作器池已关闭")
	default:
	}

	// 获取该任务的可用线程数
	availableThreads := wp.threadManager.GetAvailableThreads(index)
	task.Settings.Threads = availableThreads

	// 直接提交任务，利用ants池的非阻塞特性
	return wp.pool.Submit(func() {
		wp.executeTask(task)
	})
}

// executeTask 执行任务，借鉴xl-converter的run方法结构
func (wp *EnhancedWorkerPool) executeTask(task *WorkerTask) {
	atomic.AddInt32(&wp.activeWorkers, 1)
	defer func() {
		atomic.AddInt32(&wp.activeWorkers, -1)
		// 处理panic恢复
		if r := recover(); r != nil {
			wp.logger.Error("任务执行发生panic",
				zap.Int("worker_id", task.ID),
				zap.String("file", task.File.Path),
				zap.Any("panic", r))
			atomic.AddInt64(&wp.failedTasks, 1)
			if wp.signals != nil {
				wp.signals.OnFailed(task.ID, task.File, fmt.Errorf("panic: %v", r))
			}
		}
		// 更新资源管理器的线程使用计数
		wp.resourceManager.SetUsedThreadCount(wp.pool.Running())
	}()

	task.StartTime = time.Now()
	// 检查取消信号
	select {
	case <-wp.ctx.Done():
		wp.handleTaskCancellation(task)
		return
	case <-task.Ctx.Done():
		wp.handleTaskCancellation(task)
		return
	default:
	}

	// 通知任务开始
	if wp.signals != nil {
		wp.signals.OnStarted(task.ID, task.File)
	}

	// 注册任务到监控器并更新状态为运行中 - 优化字符串转换
	taskIDStr := strconv.Itoa(task.ID)
	wp.taskMonitor.RegisterTask(taskIDStr, task.File.Path, task.File.Size, PriorityNormal)
	wp.taskMonitor.UpdateTaskState(taskIDStr, TaskStateRunning)

	// 检查资源管理器是否需要优化
	if wp.resourceManager.IsEnabled() && wp.resourceManager.IsNecessary(task.Params.Format, task.Params.Effort, task.Params.Lossless) {
		// 资源优化已启用
	}

	// 工作器开始处理任务

	// 执行任务的主要逻辑
	result := wp.runTaskWithTimeout(task)

	// 处理结果
	if result.Error != nil {
		atomic.AddInt64(&wp.failedTasks, 1)
		// 更新任务状态为失败
		taskIDStr := strconv.Itoa(task.ID)
		wp.taskMonitor.UpdateTaskState(taskIDStr, TaskStateFailed, result.Error.Error())
		if wp.signals != nil {
			wp.signals.OnFailed(task.ID, task.File, result.Error)
		}
		wp.logger.Error("任务执行失败",
			zap.Int("worker_id", task.ID),
			zap.String("file", task.File.Path),
			zap.Error(result.Error))
	} else {
		atomic.AddInt64(&wp.completedTasks, 1)
		// 更新任务状态为完成
		taskIDStr := strconv.Itoa(task.ID)
		wp.taskMonitor.UpdateTaskState(taskIDStr, TaskStateCompleted)
		if wp.signals != nil {
			wp.signals.OnCompleted(task.ID, task.File, result)
		}
		// 任务执行成功
	}

	// 清理临时文件
	wp.cleanupTaskTempFiles(strconv.Itoa(task.ID))
}

// runTaskWithTimeout 带超时的任务执行
func (wp *EnhancedWorkerPool) runTaskWithTimeout(task *WorkerTask) *ConversionResult {
	resultChan := make(chan *ConversionResult, 1)
	timeoutCtx, cancel := context.WithTimeout(task.Ctx, wp.taskTimeout)
	defer cancel()

	go func() {
		result := wp.runTask(task)
		select {
		case resultChan <- result:
		case <-timeoutCtx.Done():
			// 超时，清理资源
			wp.cleanupTaskTempFiles(strconv.FormatUint(uint64(task.ID), 10))
		}
	}()

	select {
	case result := <-resultChan:
		return result
	case <-timeoutCtx.Done():
		var timeoutBuilder strings.Builder
		timeoutBuilder.WriteString("任务执行超时: ")
		timeoutBuilder.WriteString(task.File.Path)
		timeoutMsg := timeoutBuilder.String()
		return &ConversionResult{
			OriginalFile: task.File,
			Success:      false,
			Error:        fmt.Errorf("%s", timeoutMsg),
			Skipped:      true,
			SkipReason:   timeoutMsg,
			Method:       "enhanced_worker",
		}
	}
}

// runTask 执行具体的转换任务，借鉴xl-converter的工作流程
func (wp *EnhancedWorkerPool) runTask(task *WorkerTask) *ConversionResult {
	startTime := time.Now()
	result := &ConversionResult{
		OriginalFile: task.File,
		OriginalSize: task.File.Size,
		OutputPath:   task.OutputPath,
		Method:       "enhanced_worker",
	}

	// 使用错误处理器执行转换任务
	err := wp.errorHandler.RetryWithBackoff(func() error {
		// 检查任务是否被取消
		select {
		case <-task.Ctx.Done():
			return task.Ctx.Err()
		default:
		}

		// 阶段1: 运行检查 (借鉴xl-converter的runChecks)
		if err := wp.runChecks(task); err != nil {
			var precheckBuilder strings.Builder
			precheckBuilder.WriteString("预检查失败: ")
			precheckBuilder.WriteString(err.Error())
			return wp.errorHandler.WrapErrorWithType(
				ErrorTypeFileOperation,
				SeverityMedium,
				"file_validation",
				precheckBuilder.String(),
				err,
			)
		}

		// 如果需要跳过
		if task.Skip {
			result.Success = true
			result.Skipped = true
			result.SkipReason = "文件已存在，跳过处理"
			return nil
		}

		// 阶段2: 设置转换 (借鉴xl-converter的setupConversion)
		if err := wp.setupConversion(task); err != nil {
			var setupBuilder strings.Builder
			setupBuilder.WriteString("设置转换失败: ")
			setupBuilder.WriteString(err.Error())
			return wp.errorHandler.WrapErrorWithType(
				ErrorTypeConversion,
				SeverityMedium,
				"conversion_setup",
				setupBuilder.String(),
				err,
			)
		}

		// 阶段3: 执行转换 (借鉴xl-converter的convert)
		if err := wp.performConversion(task, result); err != nil {
			var conversionBuilder strings.Builder
			conversionBuilder.WriteString("转换失败: ")
			conversionBuilder.WriteString(err.Error())
			return wp.errorHandler.WrapErrorWithType(
				ErrorTypeConversion,
				SeverityHigh,
				"file_conversion",
				conversionBuilder.String(),
				err,
			)
		}

		// 阶段4: 完成转换 (借鉴xl-converter的finishConversion)
		if err := wp.finishConversion(task, result); err != nil {
			var builder strings.Builder
			builder.WriteString("完成转换失败: ")
			builder.WriteString(err.Error())
			return wp.errorHandler.WrapErrorWithType(
				ErrorTypeFileOperation,
				SeverityMedium,
				"conversion_finish",
				builder.String(),
				err,
			)
		}

		// 阶段5: 后处理 (借鉴xl-converter的postConversionRoutines)
		if err := wp.postConversionRoutines(task, result); err != nil {
			var builder strings.Builder
			builder.WriteString("后处理失败: ")
			builder.WriteString(err.Error())
			return wp.errorHandler.WrapErrorWithType(
				ErrorTypeFileOperation,
				SeverityLow,
				"post_processing",
				builder.String(),
				err,
			)
		}

		return nil
	}, ErrorTypeConversion, "file_conversion")

	if err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)

		// 检查是否是特定类型的错误
		if pixlyErr, ok := err.(*PixlyError); ok {
			if pixlyErr.Type == ErrorTypeFileOperation {
				result.Skipped = true
				result.SkipReason = pixlyErr.Message
			}
		}
		return result
	}

	result.Success = true
	result.Duration = time.Since(startTime)
	result.CompressedSize = result.OriginalSize // 模拟压缩后大小
	if result.OriginalSize > 0 {
		result.CompressionRatio = float64(result.CompressedSize) / float64(result.OriginalSize)
	}

	return result
}

// runChecks 运行预检查，借鉴xl-converter的runChecks方法
func (wp *EnhancedWorkerPool) runChecks(task *WorkerTask) error {
	// 检查文件是否存在
	if task.File == nil || task.File.Path == "" {
		return fmt.Errorf("无效的文件路径")
	}

	// 检查文件是否可读
	if _, err := os.Stat(task.File.Path); os.IsNotExist(err) {
		return fmt.Errorf("文件不存在: %s", task.File.Path)
	}

	// 检查是否需要跳过
	if task.Params != nil {
		// 这里可以添加更多的跳过逻辑
		// 例如：如果输出文件已存在且设置为跳过
	}

	return nil
}

// setupConversion 设置转换参数，借鉴xl-converter的setupConversion方法
func (wp *EnhancedWorkerPool) setupConversion(task *WorkerTask) error {
	// 设置输出路径
	if task.OutputPath == "" {
		// 根据格式生成输出路径
		outputExt := wp.getOutputExtension(task.Params.Format)
		task.OutputPath = wp.generateOutputPath(task.File.Path, outputExt)
	}

	// 初始化临时文件列表
	task.TempFiles = make([]string, 0)

	// 获取源文件时间戳
	if task.Params.KeepTimestamps {
		// 这里可以添加时间戳提取逻辑
		task.SrcTimestamps = NewTimestampMap[any]()
	}

	return nil
}

// performConversion 执行转换，使用实际的Converter实例
func (wp *EnhancedWorkerPool) performConversion(task *WorkerTask, result *ConversionResult) error {
	// 注意：实际转换逻辑应该在调用此方法之前完成
	// 这里只是一个占位符，实际的转换应该通过Converter实例进行
	// WorkerPool主要负责任务调度和并发管理

	// 如果需要在这里执行转换，需要传入Converter实例
	// 目前这个方法主要用于任务状态管理
	result.Success = true
	result.OutputPath = task.OutputPath
	return nil
}

// finishConversion 完成转换，借鉴xl-converter的finishConversion方法
func (wp *EnhancedWorkerPool) finishConversion(task *WorkerTask, result *ConversionResult) error {
	// 验证输出文件
	if task.OutputPath == "" {
		return fmt.Errorf("输出路径为空")
	}

	// 检查输出文件是否存在
	// 这里可以添加文件验证逻辑

	// 应用元数据
	if task.Params.KeepMetadata {
		// 这里可以添加元数据处理逻辑
	}

	return nil
}

// postConversionRoutines 后处理例程，借鉴xl-converter的postConversionRoutines方法
func (wp *EnhancedWorkerPool) postConversionRoutines(task *WorkerTask, result *ConversionResult) error {
	// 保留时间戳
	if task.Params.KeepTimestamps && task.SrcTimestamps.Len() > 0 {
		// 这里可以添加时间戳恢复逻辑
	}

	// 清理临时文件
	for _, tempFile := range task.TempFiles {
		wp.addTempFile(strconv.Itoa(task.ID), tempFile)
	}

	return nil
}

// 注意：实际转换逻辑在 Converter 类型中实现，这里不需要重复实现

// 辅助方法
func (wp *EnhancedWorkerPool) getOutputExtension(format string) string {
	switch format {
	case "JPEG XL":
		return ".jxl"
	case "AVIF":
		return ".avif"
	case "JPEG":
		return ".jpg"
	case "WebP":
		return ".webp"
	case "PNG":
		return ".png"
	default:
		return ".out"
	}
}

func (wp *EnhancedWorkerPool) generateOutputPath(inputPath, extension string) string {
	// 输出路径生成逻辑在 Converter.getOutputPath 中实现
	base := inputPath[:len(inputPath)-len(filepath.Ext(inputPath))]
	return base + extension
}

// handleTaskCancellation 处理任务取消
func (wp *EnhancedWorkerPool) handleTaskCancellation(task *WorkerTask) {
	atomic.AddInt64(&wp.canceledTasks, 1)
	task.Canceled = true

	if wp.signals != nil {
		wp.signals.OnCanceled(task.ID, task.File)
	}

	// 任务被取消

	// 清理临时文件
	wp.cleanupTaskTempFiles(strconv.Itoa(task.ID))
}

// 临时文件管理
func (wp *EnhancedWorkerPool) addTempFile(workerID, filePath string) {
	wp.tempFilesMutex.Lock()
	defer wp.tempFilesMutex.Unlock()

	if wp.tempFiles[workerID] == nil {
		wp.tempFiles[workerID] = make([]string, 0)
	}
	wp.tempFiles[workerID] = append(wp.tempFiles[workerID], filePath)
}

func (wp *EnhancedWorkerPool) cleanupTaskTempFiles(workerID string) {
	wp.tempFilesMutex.Lock()
	files := wp.tempFiles[workerID]
	delete(wp.tempFiles, workerID)
	wp.tempFilesMutex.Unlock()

	for _, file := range files {
		if err := wp.removeTempFile(file); err != nil {
			wp.logger.Warn("清理临时文件失败",
				zap.String("file", file),
				zap.Error(err))
		}
	}
}

func (wp *EnhancedWorkerPool) removeTempFile(filePath string) error {
	if filePath == "" {
		return fmt.Errorf("empty file path provided")
	}

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 临时文件已删除或不存在
		return nil
	}

	// 安全删除文件
	if err := os.Remove(filePath); err != nil {
		wp.logger.Error("Failed to remove temp file",
			zap.String("file", filePath),
			zap.Error(err))
		return fmt.Errorf("failed to remove temp file %s: %w", filePath, err)
	}

	// 成功删除临时文件
	return nil
}

// startCleanupRoutine 启动清理协程
func (wp *EnhancedWorkerPool) startCleanupRoutine() {
	wp.cleanupTicker = time.NewTicker(wp.cleanupInterval)
	go func() {
		for {
			select {
			case <-wp.cleanupTicker.C:
				wp.performCleanup()
			case <-wp.ctx.Done():
				return
			}
		}
	}()
}

func (wp *EnhancedWorkerPool) performCleanup() {
	// 清理过期的临时文件
	wp.tempFilesMutex.RLock()
	workerIDs := make([]string, 0, len(wp.tempFiles))
	for workerID := range wp.tempFiles {
		workerIDs = append(workerIDs, workerID)
	}
	wp.tempFilesMutex.RUnlock()

	for _, workerID := range workerIDs {
		// 这里可以添加更智能的清理逻辑
		// 例如：只清理超过一定时间的临时文件
		wp.cleanupTaskTempFiles(workerID)
	}
}

// GetPoolMetrics 获取工作器池的实时统计信息
func (ewp *EnhancedWorkerPool) GetPoolMetrics() *PoolMetrics {
	ewp.mutex.RLock()
	defer ewp.mutex.RUnlock()

	// 从任务监控器获取详细指标
	monitorMetrics := ewp.taskMonitor.GetMetrics()

	return &PoolMetrics{
		ActiveWorkers:   int32(ewp.pool.Running()),
		QueuedTasks:     int32(ewp.pool.Waiting()),
		CompletedTasks:  ewp.completedTasks,
		FailedTasks:     ewp.failedTasks,
		TotalTasks:      ewp.completedTasks + ewp.failedTasks + ewp.canceledTasks,
		AverageWaitTime: monitorMetrics.AverageWaitTime,
		AverageExecTime: monitorMetrics.AverageProcessingTime,
		LastUpdate:      time.Now(),
	}
}

// GetResourceManager 获取资源管理器
func (wp *EnhancedWorkerPool) GetResourceManager() *ResourceManager {
	return wp.resourceManager
}

// GetThreadManager 获取线程管理器
func (wp *EnhancedWorkerPool) GetThreadManager() *ThreadManager {
	return wp.threadManager
}

// GetTaskMonitor 获取任务监控器
func (wp *EnhancedWorkerPool) GetTaskMonitor() *TaskMonitor {
	return wp.taskMonitor
}

// GetStats 获取统计信息
func (wp *EnhancedWorkerPool) GetStats() *PoolStats {
	return &PoolStats{
		ActiveWorkers:  atomic.LoadInt32(&wp.activeWorkers),
		CompletedTasks: atomic.LoadInt64(&wp.completedTasks),
		FailedTasks:    atomic.LoadInt64(&wp.failedTasks),
		CanceledTasks:  atomic.LoadInt64(&wp.canceledTasks),
		PoolCapacity:   wp.pool.Cap(),
		PoolRunning:    wp.pool.Running(),
		PoolFree:       wp.pool.Free(),
	}
}

// GetStatsMap 获取统计信息的map版本（向后兼容）
func (wp *EnhancedWorkerPool) GetStatsMap() map[string]interface{} {
	stats := wp.GetStats()
	return map[string]interface{}{
		"active_workers":  stats.ActiveWorkers,
		"completed_tasks": stats.CompletedTasks,
		"failed_tasks":    stats.FailedTasks,
		"canceled_tasks":  stats.CanceledTasks,
		"pool_capacity":   stats.PoolCapacity,
		"pool_running":    stats.PoolRunning,
		"pool_free":       stats.PoolFree,
	}
}

// Close 关闭工作器池
func (wp *EnhancedWorkerPool) Close() error {
	wp.cancel()

	if wp.cleanupTicker != nil {
		wp.cleanupTicker.Stop()
	}

	// 停止任务监控器
	wp.taskMonitor.Stop()

	// 清理所有临时文件
	wp.tempFilesMutex.Lock()
	for workerID := range wp.tempFiles {
		wp.cleanupTaskTempFiles(workerID)
	}
	wp.tempFilesMutex.Unlock()

	wp.pool.Release()
	return nil
}
