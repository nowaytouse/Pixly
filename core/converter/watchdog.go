package converter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WatchdogMode 看门狗模式枚举
type WatchdogMode int

const (
	ModeUserInteraction WatchdogMode = iota // 用户交互模式（弱作用）

)

// WatchdogConfig 看门狗配置
type WatchdogConfig struct {
	// 进度停滞检测时间（秒）
	StagnantTimeout int
	// 大文件处理超时时间（秒）
	LargeFileTimeout int
	// 大文件阈值（MB）
	LargeFileThreshold int64
	// 单个文件处理超时时间（秒）
	FileProcessingTimeout int
	// 是否启用看门狗
	Enabled bool
	// 看门狗模式
	Mode WatchdogMode

	// 内存使用限制（MB）
	MemoryLimit int
}

// ProgressWatchdog 进度看门狗
type ProgressWatchdog struct {
	config *WatchdogConfig
	logger *zap.Logger

	// 进度跟踪
	lastProgress    float64
	lastUpdateTime  time.Time
	currentFile     string
	currentFileSize int64

	// 文件处理超时控制
	fileStartTime     time.Time
	fileTimeoutCancel context.CancelFunc
	fileTimeoutCtx    context.Context

	// 控制信号
	ctx     context.Context
	cancel  context.CancelFunc
	stopped chan struct{}

	// 用户交互
	userResponseChan chan string

	// 线程安全
	mutex sync.RWMutex
}

// NewProgressWatchdog 创建进度看门狗
func NewProgressWatchdog(config *WatchdogConfig, logger *zap.Logger) *ProgressWatchdog {
	ctx, cancel := context.WithCancel(context.Background())

	return &ProgressWatchdog{
		config:           config,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		stopped:          make(chan struct{}),
		userResponseChan: make(chan string, 1),
		lastUpdateTime:   time.Now(),
	}
}

// Start 启动看门狗
func (w *ProgressWatchdog) Start() {
	if !w.config.Enabled {
		return
	}

	// 启动进度看门狗

	go w.monitor()
}

// Stop 停止看门狗
func (w *ProgressWatchdog) Stop() {
	w.cancel()
	select {
	case <-w.stopped:
		// 看门狗已停止
	case <-time.After(5 * time.Second):
		w.logger.Warn("看门狗停止超时")
	}
}

// UpdateProgress 更新进度
func (w *ProgressWatchdog) UpdateProgress(progress float64, currentFile string, fileSize int64) {
	if !w.config.Enabled {
		return
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 检查是否是新文件
	if currentFile != w.currentFile {
		// 取消之前的文件超时
		if w.fileTimeoutCancel != nil {
			w.fileTimeoutCancel()
		}

		// 为新文件设置超时
		if w.config.FileProcessingTimeout > 0 {
			w.fileTimeoutCtx, w.fileTimeoutCancel = context.WithTimeout(w.ctx, time.Duration(w.config.FileProcessingTimeout)*time.Second)
			go w.fileTimeoutMonitor(currentFile)
		}

		// 更新文件开始时间
		w.fileStartTime = time.Now()
	}

	// 检查进度是否有实质性变化
	// 修改检查条件，允许更小的进度更新，避免进度条看起来卡住
	// 同时确保100%进度能被正确更新
	if progress > w.lastProgress+0.001 || progress == 100 || currentFile != w.currentFile { // 进度需要至少增加0.001%就算有效更新，或者达到100%
		w.lastProgress = progress
		w.lastUpdateTime = time.Now()
		w.currentFile = currentFile
		w.currentFileSize = fileSize

		// 看门狗进度更新

		// 如果文件处理完成（100%），取消该文件的超时监控
		if progress >= 100 {
			if w.fileTimeoutCancel != nil {
				w.fileTimeoutCancel()
				w.fileTimeoutCancel = nil
			}
			// 文件处理完成，取消超时监控
		}
	}
}

// fileTimeoutMonitor 文件处理超时监控
func (w *ProgressWatchdog) fileTimeoutMonitor(currentFile string) {
	<-w.fileTimeoutCtx.Done()

	// 检查是否是正常完成还是超时
	if w.fileTimeoutCtx.Err() == context.DeadlineExceeded {
		w.mutex.Lock()
		// 检查是否仍在处理同一个文件
		if w.currentFile == currentFile {
			w.mutex.Unlock()

			// 触发文件处理超时处理
			w.handleFileTimeout(currentFile)
			return
		}
		w.mutex.Unlock()
	}
}

// handleStagnation 处理进度停滞
func (w *ProgressWatchdog) handleStagnation(currentFile string, duration time.Duration, isLargeFile bool) {
	// 根据看门狗模式采取不同行动
	switch w.config.Mode {
	case ModeUserInteraction:
		// 用户交互场景（弱作用）：分层次处理不同严重程度的停滞
		stagnantTimeout := time.Duration(w.config.StagnantTimeout) * time.Second

		// 轻微停滞：仅记录日志
		if duration <= stagnantTimeout {
			// 进度轻微停滞（用户交互模式）
			return
		}

		// 中等停滞：提供更多警告信息
		if duration <= stagnantTimeout*2 {
			w.logger.Warn("⚠️  进度中等停滞，可能需要关注",
				zap.String("current_file", currentFile),
				zap.Duration("stagnant_duration", duration),
				zap.Bool("is_large_file", isLargeFile))

			// 对于大文件，提供更多上下文信息
			if isLargeFile {
				// 提示：正在处理大文件，可能需要更多时间
			}
			return
		}

		// 严重停滞：提供更多操作选项
		if duration <= stagnantTimeout*3 {
			w.logger.Error("🚨 进度严重停滞，建议检查系统资源",
				zap.String("current_file", currentFile),
				zap.Duration("stagnant_duration", duration),
				zap.Bool("is_large_file", isLargeFile))

			// 提供系统资源信息
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			// 系统资源使用情况
			return
		}

		// 极端停滞：询问用户是否需要强制退出
		w.logger.Warn("⚠️  检测到极端进度停滞，询问用户是否需要强制退出",
			zap.String("current_file", currentFile),
			zap.Duration("stagnant_duration", duration),
			zap.Bool("is_large_file", isLargeFile))

		// 在用户交互模式下询问用户
		if w.askUserForAction("检测到程序可能已卡死，是否需要强制退出？(y/N): ") {
			w.logger.Fatal("用户选择强制退出程序")
			os.Exit(1)
		} else {
			// 用户选择继续，重置计时器
			w.mutex.Lock()
			w.lastUpdateTime = time.Now()
			w.mutex.Unlock()
			// 用户选择继续执行
		}
	default:
		// 默认情况：仅记录日志
		// 进度停滞检测（默认模式）
	}

	// 重置计时器，继续处理
	w.mutex.Lock()
	w.lastUpdateTime = time.Now()
	w.mutex.Unlock()
}

// handleFileTimeout 处理文件处理超时
func (w *ProgressWatchdog) handleFileTimeout(currentFile string) {
	// 根据看门狗模式采取不同行动
	switch w.config.Mode {
	case ModeUserInteraction:
		// 用户交互场景（弱作用）：提供更多上下文信息
		w.logger.Warn("⏰ 文件处理超时（用户交互模式）",
			zap.String("current_file", currentFile),
			zap.Duration("timeout", time.Duration(w.config.FileProcessingTimeout)*time.Second))

		// 提供文件大小信息
		// 文件信息

		// 提供系统资源信息
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		// 系统资源使用情况
	default:
		// 默认情况：记录警告
		w.logger.Warn("⏰ 文件处理超时（默认模式）",
			zap.String("current_file", currentFile),
			zap.Duration("timeout", time.Duration(w.config.FileProcessingTimeout)*time.Second))
	}
}

// askUserForAction 询问用户是否执行某个操作
func (w *ProgressWatchdog) askUserForAction(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// monitor 监控主循环
func (w *ProgressWatchdog) monitor() {
	defer close(w.stopped)

	// 根据模式设置检查频率
	var ticker *time.Ticker
	// 用户模式下每10秒检查一次
	ticker = time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// 添加内存监控
	memTicker := time.NewTicker(30 * time.Second)
	defer memTicker.Stop()

	// 添加系统资源压力检查（每分钟检查一次）
	resourceTicker := time.NewTicker(60 * time.Second)
	defer resourceTicker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			// 取消文件超时
			if w.fileTimeoutCancel != nil {
				w.fileTimeoutCancel()
			}
			return
		case <-ticker.C:
			// 进度停滞检查
			w.checkStagnation()
		case <-memTicker.C:
			// 内存使用检查
			w.checkMemoryUsage()
		case <-resourceTicker.C:
			// 系统资源压力检查
			// 这里可以添加更复杂的系统资源检查逻辑
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			memLimitMB := uint64(w.config.MemoryLimit)
			currentMB := m.Alloc / (1024 * 1024)

			// 如果内存使用超过限制的90%，处理资源压力
			if memLimitMB > 0 && currentMB > memLimitMB*90/100 {
				w.handleSystemResourcePressure()
			}
		}
	}
}

// checkMemoryUsage 检查内存使用情况
func (w *ProgressWatchdog) checkMemoryUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 检查是否超过配置的内存限制
	memLimitMB := uint64(w.config.MemoryLimit)
	currentMB := m.Alloc / (1024 * 1024)

	// 在所有模式下都记录内存使用情况（但级别不同）
	switch w.config.Mode {
	case ModeUserInteraction:
		// 内存使用情况（用户模式）

		// 用户模式下，如果内存使用超过限制的80%，给出警告
		if memLimitMB > 0 && currentMB > memLimitMB*80/100 {
			w.logger.Warn("⚠️  内存使用接近限制",
				zap.Uint64("current_mb", currentMB),
				zap.Uint64("limit_mb", memLimitMB),
				zap.Uint64("threshold_mb", memLimitMB*80/100))

			// 如果内存使用超过限制的95%，提供更强烈的警告
			if currentMB > memLimitMB*95/100 {
				w.logger.Error("🚨 内存使用严重接近限制，建议释放资源",
					zap.Uint64("current_mb", currentMB),
					zap.Uint64("limit_mb", memLimitMB))
			}
		}
	default:
		// 内存使用情况（默认模式）
	}
}

// SetMemoryLimit 设置内存限制（MB）
func (w *ProgressWatchdog) SetMemoryLimit(limit int) {
	w.config.MemoryLimit = limit
}

// handleSystemResourcePressure 处理系统资源紧张情况
func (w *ProgressWatchdog) handleSystemResourcePressure() {
	// 根据模式采取不同行动
	switch w.config.Mode {
	case ModeUserInteraction:
		// 用户交互模式：记录警告并提供优化建议
		w.logger.Warn("⚠️  系统资源紧张，建议优化处理")

		// 提供优化建议
		// 优化建议

		// 询问用户是否需要自动调整
		if w.askUserForAction("是否需要自动减少并发处理数量以释放资源？(y/N): ") {
			// 这里可以添加自动调整逻辑
			// 已建议用户手动优化系统资源
		}
	default:
		// 默认模式：仅记录信息
		// 系统资源使用情况
	}
}

// checkStagnation 检查进度停滞
func (w *ProgressWatchdog) checkStagnation() {
	w.mutex.RLock()
	lastUpdate := w.lastUpdateTime
	currentFile := w.currentFile
	fileSize := w.currentFileSize
	progress := w.lastProgress
	w.mutex.RUnlock()

	if currentFile == "" {
		return // 还没有开始处理文件
	}

	// 如果文件已经处理完成（100%），跳过停滞检测
	if progress >= 100 {
		return
	}

	stagnantDuration := time.Since(lastUpdate)
	isLargeFile := fileSize > w.config.LargeFileThreshold*1024*1024

	// 根据文件大小选择不同的超时策略
	var timeout time.Duration
	if isLargeFile {
		timeout = time.Duration(w.config.LargeFileTimeout) * time.Second
		// 检测大文件处理
	} else {
		timeout = time.Duration(w.config.StagnantTimeout) * time.Second
	}

	if stagnantDuration > timeout {
		// 检测到进度停滞

		// 处理进度停滞
		w.handleStagnation(currentFile, stagnantDuration, isLargeFile)
	}
}

// GetDefaultWatchdogConfig 获取默认看门狗配置
func GetDefaultWatchdogConfig() *WatchdogConfig {
	return &WatchdogConfig{
		StagnantTimeout:       60,   // 进度停滞检测时间：用户模式60秒
		LargeFileTimeout:      180,  // 大文件处理超时：用户模式180秒
		LargeFileThreshold:    50,   // 50MB以上视为大文件
		FileProcessingTimeout: 120,  // 单个文件处理超时：用户模式120秒
		MemoryLimit:           8192, // 默认内存限制：8GB
		Enabled:               true,
		Mode:                  ModeUserInteraction, // 默认为用户交互模式
	}
}

// GetEnhancedUserWatchdogConfig 获取增强的用户交互看门狗配置
func GetEnhancedUserWatchdogConfig() *WatchdogConfig {
	return &WatchdogConfig{
		StagnantTimeout:       60,   // 进度停滞检测时间：用户模式60秒
		LargeFileTimeout:      180,  // 大文件处理超时：用户模式180秒
		LargeFileThreshold:    50,   // 50MB以上视为大文件
		FileProcessingTimeout: 120,  // 单个文件处理超时：用户模式120秒
		MemoryLimit:           8192, // 默认内存限制：8GB
		Enabled:               true,
		Mode:                  ModeUserInteraction, // 用户交互模式
	}
}

// GetExtremeCaseWatchdogConfig 获取极端情况处理看门狗配置
func GetExtremeCaseWatchdogConfig() *WatchdogConfig {
	return &WatchdogConfig{
		StagnantTimeout:       30,   // 进度停滞检测时间：30秒
		LargeFileTimeout:      90,   // 大文件处理超时：90秒
		LargeFileThreshold:    50,   // 50MB以上视为大文件
		FileProcessingTimeout: 60,   // 单个文件处理超时：60秒
		MemoryLimit:           4096, // 内存限制：4GB
		Enabled:               true,
		Mode:                  ModeUserInteraction, // 用户交互模式但更敏感
	}
}


