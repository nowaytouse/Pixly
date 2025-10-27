package converter

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// SignalHandlerConfig 信号处理器配置 - 使用现代化配置结构
type SignalHandlerConfig struct {
	MaxInterrupts int
	BufferSize    int
	Timeout       time.Duration
	Signals       []os.Signal
}

// DefaultSignalHandlerConfig 返回默认配置
func DefaultSignalHandlerConfig() SignalHandlerConfig {
	return SignalHandlerConfig{
		MaxInterrupts: 2,
		BufferSize:    1,
		Timeout:       30 * time.Second,
		Signals:       []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}
}

// SignalHandler 信号处理器 - 强制中断处理
type SignalHandler struct {
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	sigChan  chan os.Signal
	shutdown chan struct{}
	mutex    sync.RWMutex

	// 状态管理
	isShuttingDown bool
	converter      *Converter
	checkpoint     *CheckpointManager

	// 配置
	config SignalHandlerConfig

	// 强制中断计数
	interruptCount int
}

// NewSignalHandler 创建新的信号处理器
func NewSignalHandler(logger *zap.Logger, converter *Converter, checkpoint *CheckpointManager) *SignalHandler {
	return NewSignalHandlerWithConfig(logger, converter, checkpoint, DefaultSignalHandlerConfig())
}

// NewSignalHandlerWithConfig 使用配置创建信号处理器
func NewSignalHandlerWithConfig(logger *zap.Logger, converter *Converter, checkpoint *CheckpointManager, config SignalHandlerConfig) *SignalHandler {
	ctx, cancel := context.WithCancel(context.Background())

	return &SignalHandler{
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
		sigChan:        make(chan os.Signal, config.BufferSize),
		shutdown:       make(chan struct{}),
		converter:      converter,
		checkpoint:     checkpoint,
		config:         config,
		interruptCount: 0,
	}
}

// Start 启动信号监听
func (sh *SignalHandler) Start() {
	// 注册信号监听
	signal.Notify(sh.sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动信号处理goroutine
	go sh.handleSignals()

	// 信号处理器已启动
}

// Stop 停止信号处理器
func (sh *SignalHandler) Stop() {
	sh.mutex.Lock()
	defer sh.mutex.Unlock()

	if sh.isShuttingDown {
		return // 已经停止，避免重复关闭
	}

	sh.isShuttingDown = true
	signal.Stop(sh.sigChan)
	close(sh.sigChan)
	sh.cancel()

	// 信号处理器已停止
}

// handleSignals 处理信号
func (sh *SignalHandler) handleSignals() {
	for {
		select {
		case sig := <-sh.sigChan:
			if sig == nil {
				return
			}

			// 收到中断信号

			// 处理中断
			sh.handleInterrupt(sig)

		case <-sh.ctx.Done():
			return
		}
	}
}

// handleInterrupt 处理中断信号 - 强制中断
func (sh *SignalHandler) handleInterrupt(sig os.Signal) {
	sh.mutex.Lock()
	defer sh.mutex.Unlock()

	sh.interruptCount++
	// 收到中断信号

	// 检查是否超过最大中断次数
	if sh.interruptCount >= sh.config.MaxInterrupts {
		sh.logger.Warn("达到最大中断次数，强制退出程序")
		os.Exit(1)
	}

	if sh.isShuttingDown {
		sh.logger.Warn("程序正在关闭中，再次中断将强制退出")
		return
	}

	sh.isShuttingDown = true

	// 保存当前状态到断点续传系统
	if sh.checkpoint != nil {
		if err := sh.checkpoint.SaveCurrentState(); err != nil {
			sh.logger.Error("保存断点状态失败", zap.Error(err))
		} else {
			// 断点状态已保存
		}
	}

	// 立即停止转换器
	if sh.converter != nil {
		sh.converter.RequestStop()
	}

	// 取消上下文，通知所有goroutine退出
	sh.cancel()

	// 显示中断后的选项菜单
	sh.showInterruptMenu()
}

// showInterruptMenu 显示中断信息并立即退出
func (sh *SignalHandler) showInterruptMenu() {
	// 显示中断信息
	// 程序已中断，状态已保存
	// 可以使用相同命令恢复转换进度

	// 立即退出，不等待用户输入
	os.Exit(0)
}

// IsShuttingDown 检查是否正在关闭
func (sh *SignalHandler) IsShuttingDown() bool {
	sh.mutex.RLock()
	defer sh.mutex.RUnlock()
	return sh.isShuttingDown
}

// GetContext 获取上下文
func (sh *SignalHandler) GetContext() context.Context {
	return sh.ctx
}
