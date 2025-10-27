package converter

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
)

// TaskPriority 任务优先级枚举
type TaskPriority int

const (
	PriorityLow      TaskPriority = iota // 低优先级
	PriorityNormal                       // 普通优先级
	PriorityHigh                         // 高优先级
	PriorityCritical                     // 关键优先级
)

// PriorityTask 带优先级的任务
type PriorityTask struct {
	Task     func()       // 任务函数
	Priority TaskPriority // 任务优先级
	ID       string       // 任务ID
	Created  time.Time    // 创建时间
}

// PoolMetrics 池监控指标
type PoolMetrics struct {
	ActiveWorkers   int32         // 活跃工作者数量
	QueuedTasks     int32         // 排队任务数量
	CompletedTasks  int64         // 已完成任务数量
	FailedTasks     int64         // 失败任务数量
	TotalTasks      int64         // 总任务数量
	AverageWaitTime time.Duration // 平均等待时间
	AverageExecTime time.Duration // 平均执行时间
	LastUpdate      time.Time     // 最后更新时间
}

// AdvancedPoolConfig 高级池配置
type AdvancedPoolConfig struct {
	InitialSize        int           // 初始池大小
	MinSize            int           // 最小池大小
	MaxSize            int           // 最大池大小
	ScaleUpThreshold   float64       // 扩容阈值（队列长度/池大小）
	ScaleDownThreshold float64       // 缩容阈值
	ScaleInterval      time.Duration // 调整间隔
	IdleTimeout        time.Duration // 空闲超时
	EnablePriority     bool          // 是否启用优先级
	EnableMetrics      bool          // 是否启用监控

	// 新增：队列与提交的稳态化配置
	QueueCapacityMultiplier int           // 优先级队列容量倍率（相对于 MaxSize）
	EnqueueTimeout          time.Duration // 入队最大等待时长，超时则回退到 ants
	EnqueueRetryInterval    time.Duration // 入队重试间隔
	AntsMaxBlockingTasks    int           // ants 最大阻塞提交数上限（避免 ErrPoolOverload）
	AntsSubmitRetry         int           // ants 提交出错重试次数
	AntsSubmitBackoff       time.Duration // ants 提交重试退避基准
}

// GetDefaultAdvancedPoolConfig 获取默认高级池配置
func GetDefaultAdvancedPoolConfig() *AdvancedPoolConfig {
	// 计算合适的队列大小，确保能处理大量文件
	maxSize := runtime.NumCPU() * 16 // 增加到16倍CPU核心数
	if maxSize < 128 {
		maxSize = 128 // 最小保证128个队列槽位
	}

	return &AdvancedPoolConfig{
		InitialSize:             runtime.NumCPU(),
		MinSize:                 2,
		MaxSize:                 maxSize,
		ScaleUpThreshold:        0.8,                 // 当队列长度达到池大小的80%时扩容
		ScaleDownThreshold:      0.2,                 // 当队列长度低于池大小的20%时缩容
		ScaleInterval:           time.Second * 30,
		IdleTimeout:             time.Minute * 10,
		EnablePriority:          true,
		EnableMetrics:           true,
		QueueCapacityMultiplier: 8,                   // 优先级队列容量扩大到 MaxSize*8
		EnqueueTimeout:          200 * time.Millisecond,
		EnqueueRetryInterval:    2 * time.Millisecond,
		AntsMaxBlockingTasks:    maxSize * 16,        // 允许大量提交在 ants 层等待
		AntsSubmitRetry:         3,
		AntsSubmitBackoff:       20 * time.Millisecond,
	}
}

// AdvancedPool 高级ants池管理器
type AdvancedPool struct {
	config  *AdvancedPoolConfig
	logger  *zap.Logger
	pool    *ants.Pool
	metrics *PoolMetrics
	ctx     context.Context
	cancel  context.CancelFunc
	mutex   sync.RWMutex

	// 优先级队列
	priorityQueues map[TaskPriority]chan *PriorityTask
	queueMutex     sync.RWMutex

	// 监控相关
	monitorTicker *time.Ticker
	scaleTicker   *time.Ticker
	logCounter    int32 // 日志输出计数器

	// 统计信息
	taskWaitTimes []time.Duration
	taskExecTimes []time.Duration
	timesMutex    sync.Mutex

	// 任务监控
	taskMonitor *TaskMonitor
}

// NewAdvancedPool 创建新的高级池
func NewAdvancedPool(config *AdvancedPoolConfig, logger *zap.Logger) (*AdvancedPool, error) {
	if config == nil {
		config = GetDefaultAdvancedPoolConfig()
	}

	// 验证配置参数
	if config.InitialSize <= 0 {
		logger.Warn("InitialSize无效，使用默认值", zap.Int("provided", config.InitialSize))
		config.InitialSize = runtime.NumCPU()
		if config.InitialSize <= 0 {
			config.InitialSize = 1
		}
	}
	if config.MaxSize <= 0 {
		logger.Warn("MaxSize无效，使用默认值", zap.Int("provided", config.MaxSize))
		config.MaxSize = runtime.NumCPU() * 16
		if config.MaxSize < 128 {
			config.MaxSize = 128
		}
	}
	if config.MinSize <= 0 {
		config.MinSize = 1
	}
	// 确保InitialSize在合理范围内
	if config.InitialSize > config.MaxSize {
		config.InitialSize = config.MaxSize
	}
	if config.InitialSize < config.MinSize {
		config.InitialSize = config.MinSize
	}

	// 合理化新增配置
	if config.QueueCapacityMultiplier <= 0 {
		config.QueueCapacityMultiplier = 8
	}
	if config.EnqueueTimeout <= 0 {
		config.EnqueueTimeout = 200 * time.Millisecond
	}
	if config.EnqueueRetryInterval <= 0 {
		config.EnqueueRetryInterval = 2 * time.Millisecond
	}
	if config.AntsMaxBlockingTasks <= 0 {
		config.AntsMaxBlockingTasks = config.MaxSize * 16
	}
	if config.AntsSubmitRetry < 0 {
		config.AntsSubmitRetry = 0
	}
	if config.AntsSubmitBackoff <= 0 {
		config.AntsSubmitBackoff = 20 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 创建基础ants池
	pool, err := ants.NewPool(config.InitialSize, ants.WithOptions(ants.Options{
		ExpiryDuration:   config.IdleTimeout,
		PreAlloc:         true,
		MaxBlockingTasks: config.AntsMaxBlockingTasks,
		Nonblocking:      false,
		PanicHandler: func(p interface{}) {
			logger.Error("ants池中的goroutine发生panic", zap.Any("panic", p))
		},
	}))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建ants池失败: %w", err)
	}

	// 创建任务监控器
	taskMonitor := NewTaskMonitor(logger)

	ap := &AdvancedPool{
		config:      config,
		logger:      logger,
		pool:        pool,
		metrics:     &PoolMetrics{LastUpdate: time.Now()},
		ctx:         ctx,
		cancel:      cancel,
		taskMonitor: taskMonitor,
	}

	// 初始化优先级队列
	if config.EnablePriority {
		ap.priorityQueues = make(map[TaskPriority]chan *PriorityTask)
		// 计算队列容量（放大，缓冲高峰流量）
		capSize := config.MaxSize * config.QueueCapacityMultiplier
		if capSize < config.MaxSize {
			capSize = config.MaxSize
		}
		for priority := PriorityCritical; priority >= PriorityLow; priority-- {
			ap.priorityQueues[priority] = make(chan *PriorityTask, capSize)
		}
		// 启动优先级任务调度器
		go ap.priorityScheduler()
	}

	// 启动任务监控器
	if err := ap.taskMonitor.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("启动任务监控器失败: %w", err)
	}

	// 设置监控器的工作器信息
	ap.taskMonitor.SetMaxWorkers(int32(config.InitialSize))

	// 启动监控
	if config.EnableMetrics {
		ap.monitorTicker = time.NewTicker(time.Second * 5)
		go ap.monitor()
	}

	// 启动动态调整
	ap.scaleTicker = time.NewTicker(config.ScaleInterval)
	go ap.autoScale()

	// 高级ants池已启动

	return ap, nil
}

// Submit 提交普通任务
func (ap *AdvancedPool) Submit(task func()) error {
	return ap.SubmitWithPriority(task, PriorityNormal, "")
}

// SubmitTask 提交任务到池中
func (ap *AdvancedPool) SubmitTask(task PriorityTask) error {
	// 在任务监控器中注册任务
	ap.taskMonitor.RegisterTask(
		task.ID,
		"", // filePath - 从PriorityTask结构体中没有这个字段
		0,  // fileSize - 从PriorityTask结构体中没有这个字段
		task.Priority,
	)

	// 更新任务状态为排队
	if err := ap.taskMonitor.UpdateTaskState(task.ID, TaskStateQueued); err != nil {
		ap.logger.Warn("更新任务状态失败", zap.Error(err))
	}

	// 使用现有的SubmitWithPriority方法
	err := ap.SubmitWithPriority(task.Task, task.Priority, task.ID)
	if err != nil {
		// 更新任务状态为失败
		ap.taskMonitor.UpdateTaskState(task.ID, TaskStateFailed, err.Error())
		return err
	}

	// 任务已提交到队列
	return nil
}

// SubmitWithPriority 提交带优先级的任务
func (ap *AdvancedPool) SubmitWithPriority(task func(), priority TaskPriority, taskID string) error {
	if ap.config.EnablePriority {
		priorityTask := &PriorityTask{
			Task:     task,
			Priority: priority,
			ID:       taskID,
			Created:  time.Now(),
		}

		// 尝试在限定时间内入队，缓解瞬时高峰导致的“队列已满”
		deadline := time.Now().Add(ap.config.EnqueueTimeout)
		for {
			select {
			case ap.priorityQueues[priority] <- priorityTask:
				atomic.AddInt32(&ap.metrics.QueuedTasks, 1)
				atomic.AddInt64(&ap.metrics.TotalTasks, 1)
				return nil
			case <-ap.ctx.Done():
				return fmt.Errorf("池已关闭")
			default:
				if time.Now().After(deadline) {
					// 超时，回退到直接提交ants池
					ap.logger.Debug("优先级队列繁忙，回退到直接提交",
						zap.String("taskID", taskID),
						zap.Int("priority", int(priority)))
					return ap.submitToAnts(task, taskID)
				}
				time.Sleep(ap.config.EnqueueRetryInterval)
			}
		}
	} else {
		// 直接提交到ants池
		return ap.submitToAnts(task, taskID)
	}
}

// submitToAnts 提交任务到ants池
func (ap *AdvancedPool) submitToAnts(task func(), taskID string) error {
	startTime := time.Now()
	atomic.AddInt64(&ap.metrics.TotalTasks, 1)

	// 更新任务状态为运行中
	if taskID != "" {
		if err := ap.taskMonitor.UpdateTaskState(taskID, TaskStateRunning); err != nil {
			ap.logger.Warn("更新任务状态失败", zap.Error(err))
		}
	}

	// 简化重试逻辑，只重试一次以减少延迟
	err := ap.pool.Submit(func() {
		atomic.AddInt32(&ap.metrics.ActiveWorkers, 1)
		defer atomic.AddInt32(&ap.metrics.ActiveWorkers, -1)

		execStart := time.Now()
		waitTime := execStart.Sub(startTime)

		// 执行任务
		taskErr := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt64(&ap.metrics.FailedTasks, 1)
					err = fmt.Errorf("任务执行panic: %v", r)
					ap.logger.Error("任务执行panic", zap.String("taskID", taskID), zap.Any("panic", r))
				}
			}()
			task()
			return nil
		}()

		execTime := time.Since(execStart)

		// 更新任务状态
		if taskID != "" {
			if taskErr != nil {
				ap.taskMonitor.UpdateTaskState(taskID, TaskStateFailed, taskErr.Error())
			} else {
				ap.taskMonitor.UpdateTaskState(taskID, TaskStateCompleted)
				atomic.AddInt64(&ap.metrics.CompletedTasks, 1)
			}
		} else {
			if taskErr == nil {
				atomic.AddInt64(&ap.metrics.CompletedTasks, 1)
			}
		}

		// 记录时间统计
		if ap.config.EnableMetrics {
			ap.timesMutex.Lock()
			ap.taskWaitTimes = append(ap.taskWaitTimes, waitTime)
			ap.taskExecTimes = append(ap.taskExecTimes, execTime)
			// 保持最近1000个记录
			if len(ap.taskWaitTimes) > 1000 {
				ap.taskWaitTimes = ap.taskWaitTimes[len(ap.taskWaitTimes)-1000:]
			}
			if len(ap.taskExecTimes) > 1000 {
				ap.taskExecTimes = ap.taskExecTimes[len(ap.taskExecTimes)-1000:]
			}
			ap.timesMutex.Unlock()
		}
	})

	if err == nil {
		return nil
	}

	// 如果是池过载错误，再尝试一次
	if errors.Is(err, ants.ErrPoolOverload) {
		time.Sleep(ap.config.AntsSubmitBackoff)
		err = ap.pool.Submit(func() {
			atomic.AddInt32(&ap.metrics.ActiveWorkers, 1)
			defer atomic.AddInt32(&ap.metrics.ActiveWorkers, -1)
			// 执行任务
			taskErr := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						atomic.AddInt64(&ap.metrics.FailedTasks, 1)
						err = fmt.Errorf("任务执行panic: %v", r)
						ap.logger.Error("任务执行panic", zap.String("taskID", taskID), zap.Any("panic", r))
					}
				}()
				task()
				return nil
			}()
			// 更新任务状态
			if taskID != "" {
				if taskErr != nil {
					ap.taskMonitor.UpdateTaskState(taskID, TaskStateFailed, taskErr.Error())
				} else {
					ap.taskMonitor.UpdateTaskState(taskID, TaskStateCompleted)
					atomic.AddInt64(&ap.metrics.CompletedTasks, 1)
				}
			} else {
				if taskErr == nil {
					atomic.AddInt64(&ap.metrics.CompletedTasks, 1)
				}
			}
		})
	}

	if err != nil {
		atomic.AddInt64(&ap.metrics.FailedTasks, 1)
		// 更新任务状态为失败
		if taskID != "" {
			ap.taskMonitor.UpdateTaskState(taskID, TaskStateFailed, err.Error())
		}
	}

	return err
}

// priorityScheduler 优先级任务调度器
func (ap *AdvancedPool) priorityScheduler() {
	defer func() {
		if r := recover(); r != nil {
			// 优先级调度器已安全退出
		}
	}()

	for {
		select {
		case <-ap.ctx.Done():
			return
		default:
			// 按优先级顺序处理任务
			processed := false
		PriorityLoop:
			for priority := PriorityCritical; priority >= PriorityLow; priority-- {
				select {
				case <-ap.ctx.Done():
					return
				case task, ok := <-ap.priorityQueues[priority]:
					if !ok {
						// channel已关闭
						return
					}
					atomic.AddInt32(&ap.metrics.QueuedTasks, -1)

					// 提交到ants池
					err := ap.submitToAnts(task.Task, task.ID)
					if err != nil {
						ap.logger.Error("提交优先级任务失败",
							zap.String("taskID", task.ID),
							zap.Int("priority", int(task.Priority)),
							zap.Error(err))
						// 重新入队（降低优先级）
						if task.Priority > PriorityLow {
							task.Priority--
							select {
							case ap.priorityQueues[task.Priority] <- task:
							case <-ap.ctx.Done():
								return
							default:
								// 队列满，丢弃任务
								atomic.AddInt64(&ap.metrics.FailedTasks, 1)
							}
						}
					} else {
						// 优先级任务已提交
					}
					processed = true
					break PriorityLoop
				default:
					// 该优先级队列为空，检查下一个
				}
			}

			// 如果没有处理任何任务，短暂休眠避免CPU占用过高
			if !processed {
				select {
				case <-ap.ctx.Done():
					return
				case <-time.After(time.Millisecond * 10):
					// 继续下一轮循环
				}
			}
		}
	}
}

// monitor 监控池状态
func (ap *AdvancedPool) monitor() {
	for {
		select {
		case <-ap.ctx.Done():
			return
		case <-ap.monitorTicker.C:
			ap.updateMetrics()
			ap.logMetrics()
		}
	}
}

// updateMetrics 更新监控指标
func (ap *AdvancedPool) updateMetrics() {
	ap.mutex.Lock()
	defer ap.mutex.Unlock()

	// 计算平均时间
	ap.timesMutex.Lock()
	if len(ap.taskWaitTimes) > 0 {
		var totalWait time.Duration
		for _, wait := range ap.taskWaitTimes {
			totalWait += wait
		}
		ap.metrics.AverageWaitTime = totalWait / time.Duration(len(ap.taskWaitTimes))
	}

	if len(ap.taskExecTimes) > 0 {
		var totalExec time.Duration
		for _, exec := range ap.taskExecTimes {
			totalExec += exec
		}
		ap.metrics.AverageExecTime = totalExec / time.Duration(len(ap.taskExecTimes))
	}
	ap.timesMutex.Unlock()

	ap.metrics.LastUpdate = time.Now()
}

// logMetrics 记录监控指标
func (ap *AdvancedPool) logMetrics() {
	// 减少日志输出频率，每10次监控周期才输出一次
	counter := atomic.AddInt32(&ap.logCounter, 1)
	if counter%10 == 0 {
		// 池监控指标已记录
	}
}

// autoScale 自动调整池大小
func (ap *AdvancedPool) autoScale() {
	for {
		select {
		case <-ap.ctx.Done():
			return
		case <-ap.scaleTicker.C:
			ap.performAutoScale()
		}
	}
}

// performAutoScale 执行自动调整
func (ap *AdvancedPool) performAutoScale() {
	currentSize := ap.pool.Cap()
	queuedTasks := atomic.LoadInt32(&ap.metrics.QueuedTasks)
	queueRatio := float64(queuedTasks) / float64(currentSize)

	// 扩容逻辑
	if queueRatio > ap.config.ScaleUpThreshold && currentSize < ap.config.MaxSize {
		newSize := currentSize + (currentSize / 4) // 增加25%
		if newSize > ap.config.MaxSize {
			newSize = ap.config.MaxSize
		}
		if newSize > currentSize {
			ap.pool.Tune(newSize)
			// 池扩容完成
		}
	}

	// 缩容逻辑
	if queueRatio < ap.config.ScaleDownThreshold && currentSize > ap.config.MinSize {
		newSize := currentSize - (currentSize / 4) // 减少25%
		if newSize < ap.config.MinSize {
			newSize = ap.config.MinSize
		}
		if newSize < currentSize {
			ap.pool.Tune(newSize)
			// 池缩容完成
		}
	}
}

// GetMetrics 获取监控指标
func (ap *AdvancedPool) GetMetrics() *PoolMetrics {
	ap.mutex.RLock()
	defer ap.mutex.RUnlock()

	// 从任务监控器获取详细指标
	if ap.taskMonitor != nil {
		taskMetrics := ap.taskMonitor.GetMetrics()

		// 同步任务计数
		ap.metrics.CompletedTasks = taskMetrics.CompletedTasks
		ap.metrics.FailedTasks = taskMetrics.FailedTasks
		ap.metrics.TotalTasks = taskMetrics.TotalTasks
		ap.metrics.AverageWaitTime = taskMetrics.AverageWaitTime
		ap.metrics.AverageExecTime = taskMetrics.AverageProcessingTime
	}

	// 返回指标副本
	metrics := *ap.metrics
	return &metrics
}

// GetPoolInfo 获取池信息
func (ap *AdvancedPool) GetPoolInfo() map[string]interface{} {
	return map[string]interface{}{
		"capacity":       ap.pool.Cap(),
		"running":        ap.pool.Running(),
		"free":           ap.pool.Free(),
		"activeWorkers":  atomic.LoadInt32(&ap.metrics.ActiveWorkers),
		"queuedTasks":    atomic.LoadInt32(&ap.metrics.QueuedTasks),
		"completedTasks": atomic.LoadInt64(&ap.metrics.CompletedTasks),
		"failedTasks":    atomic.LoadInt64(&ap.metrics.FailedTasks),
		"totalTasks":     atomic.LoadInt64(&ap.metrics.TotalTasks),
	}
}

// Tune 手动调整池大小
func (ap *AdvancedPool) Tune(size int) {
	if size < ap.config.MinSize {
		size = ap.config.MinSize
	}
	if size > ap.config.MaxSize {
		size = ap.config.MaxSize
	}
	ap.pool.Tune(size)
	// 手动调整池大小完成
}

// Close 关闭池
func (ap *AdvancedPool) Close() error {
	// 首先取消context，通知所有goroutine退出
	ap.cancel()

	// 给goroutine一些时间优雅退出
	time.Sleep(100 * time.Millisecond)

	// 停止任务监控器
	ap.taskMonitor.Stop()

	if ap.monitorTicker != nil {
		ap.monitorTicker.Stop()
	}
	if ap.scaleTicker != nil {
		ap.scaleTicker.Stop()
	}

	// 关闭优先级队列
	if ap.config.EnablePriority {
		ap.queueMutex.Lock()
		for _, queue := range ap.priorityQueues {
			// 安全关闭channel
			select {
			case <-queue:
				// 清空队列中剩余的任务
			default:
			}
			close(queue)
		}
		ap.queueMutex.Unlock()
	}

	// 释放ants池
	ap.pool.Release()

	// 高级ants池已关闭
	return nil
}
