package converter

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// TaskState 任务状态枚举
type TaskState int

const (
	TaskStatePending TaskState = iota
	TaskStateQueued
	TaskStateRunning
	TaskStateCompleted
	TaskStateFailed
	TaskStateCanceled
	TaskStateSkipped
	TaskStateRetrying
)

// String 返回任务状态字符串
func (ts TaskState) String() string {
	switch ts {
	case TaskStatePending:
		return "pending"
	case TaskStateQueued:
		return "queued"
	case TaskStateRunning:
		return "running"
	case TaskStateCompleted:
		return "completed"
	case TaskStateFailed:
		return "failed"
	case TaskStateCanceled:
		return "canceled"
	case TaskStateSkipped:
		return "skipped"
	case TaskStateRetrying:
		return "retrying"
	default:
		return "unknown"
	}
}

// TaskInfo 任务信息
type TaskInfo struct {
	ID           string        `json:"id"`
	FilePath     string        `json:"file_path"`
	FileSize     int64         `json:"file_size"`
	State        TaskState     `json:"state"`
	Progress     float64       `json:"progress"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	ErrorMessage string        `json:"error_message,omitempty"`
	RetryCount   int           `json:"retry_count"`
	Priority     TaskPriority  `json:"priority"`
	WorkerID     int           `json:"worker_id"`
	QueueTime    time.Time     `json:"queue_time"`
	WaitDuration time.Duration `json:"wait_duration"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// TaskMetrics 任务监控指标
type TaskMetrics struct {
	// 基础计数器
	TotalTasks     int64 `json:"total_tasks"`
	PendingTasks   int64 `json:"pending_tasks"`
	QueuedTasks    int64 `json:"queued_tasks"`
	RunningTasks   int64 `json:"running_tasks"`
	CompletedTasks int64 `json:"completed_tasks"`
	FailedTasks    int64 `json:"failed_tasks"`
	CanceledTasks  int64 `json:"canceled_tasks"`
	SkippedTasks   int64 `json:"skipped_tasks"`
	RetryingTasks  int64 `json:"retrying_tasks"`

	// 性能指标
	AverageWaitTime       time.Duration `json:"average_wait_time"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
	ThroughputPerSecond   float64       `json:"throughput_per_second"`
	SuccessRate           float64       `json:"success_rate"`
	FailureRate           float64       `json:"failure_rate"`

	// 资源使用
	ActiveWorkers     int32   `json:"active_workers"`
	MaxWorkers        int32   `json:"max_workers"`
	WorkerUtilization float64 `json:"worker_utilization"`

	// 时间戳
	LastUpdate time.Time `json:"last_update"`
	StartTime  time.Time `json:"start_time"`
}

// TaskMonitor 任务监控器
type TaskMonitor struct {
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// 任务存储
	tasks     map[string]*TaskInfo
	taskMutex sync.RWMutex

	// 指标统计
	metrics      TaskMetrics
	metricsMutex sync.RWMutex

	// 监控配置
	updateInterval  time.Duration
	cleanupInterval time.Duration
	maxTaskHistory  int

	// 事件通道
	taskUpdateChan   chan *TaskInfo
	metricUpdateChan chan TaskMetrics

	// 状态
	isRunning bool
	startTime time.Time
}

// NewTaskMonitor 创建新的任务监控器
func NewTaskMonitor(logger *zap.Logger) *TaskMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TaskMonitor{
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		tasks:            make(map[string]*TaskInfo),
		updateInterval:   time.Second,
		cleanupInterval:  5 * time.Minute,
		maxTaskHistory:   1000,
		taskUpdateChan:   make(chan *TaskInfo, 100),
		metricUpdateChan: make(chan TaskMetrics, 10),
		startTime:        time.Now(),
	}

	// 初始化指标
	tm.metrics.StartTime = time.Now()
	tm.metrics.LastUpdate = time.Now()

	return tm
}

// Start 启动任务监控器
func (tm *TaskMonitor) Start() error {
	if tm.isRunning {
		return fmt.Errorf("任务监控器已在运行")
	}

	tm.isRunning = true
	// 启动任务监控器

	// 启动监控协程
	go tm.monitorLoop()
	go tm.metricsUpdateLoop()
	go tm.cleanupLoop()

	return nil
}

// Stop 停止任务监控器
func (tm *TaskMonitor) Stop() {
	if !tm.isRunning {
		return
	}

	// 停止任务监控器
	tm.cancel()
	tm.isRunning = false

	// 关闭通道
	close(tm.taskUpdateChan)
	close(tm.metricUpdateChan)
}

// RegisterTask 注册新任务
func (tm *TaskMonitor) RegisterTask(taskID, filePath string, fileSize int64, priority TaskPriority) *TaskInfo {
	now := time.Now()
	task := &TaskInfo{
		ID:        taskID,
		FilePath:  filePath,
		FileSize:  fileSize,
		State:     TaskStatePending,
		Progress:  0.0,
		Priority:  priority,
		CreatedAt: now,
		UpdatedAt: now,
	}

	tm.taskMutex.Lock()
	tm.tasks[taskID] = task
	atomic.AddInt64(&tm.metrics.TotalTasks, 1)
	atomic.AddInt64(&tm.metrics.PendingTasks, 1)
	tm.taskMutex.Unlock()

	// 注册新任务

	// 发送任务更新事件
	select {
	case tm.taskUpdateChan <- task:
	default:
		// 通道满了，跳过
	}

	return task
}

// UpdateTaskState 更新任务状态
func (tm *TaskMonitor) UpdateTaskState(taskID string, newState TaskState, errorMessage ...string) error {
	tm.taskMutex.Lock()
	defer tm.taskMutex.Unlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return fmt.Errorf("任务不存在: %s", taskID)
	}

	oldState := task.State
	now := time.Now()

	// 更新计数器
	tm.updateStateCounters(oldState, newState)

	// 更新任务信息
	task.State = newState
	task.UpdatedAt = now

	// 处理特定状态的逻辑
	switch newState {
	case TaskStateQueued:
		task.QueueTime = now
	case TaskStateRunning:
		task.StartTime = now
		if !task.QueueTime.IsZero() {
			task.WaitDuration = now.Sub(task.QueueTime)
		}
	case TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateSkipped:
		task.EndTime = now
		if !task.StartTime.IsZero() {
			task.Duration = now.Sub(task.StartTime)
		}
		if len(errorMessage) > 0 {
			task.ErrorMessage = errorMessage[0]
		}
	case TaskStateRetrying:
		task.RetryCount++
	}

	// 更新任务状态

	// 发送任务更新事件
	select {
	case tm.taskUpdateChan <- task:
	default:
		// 通道满了，跳过
	}

	return nil
}

// UpdateTaskProgress 更新任务进度
func (tm *TaskMonitor) UpdateTaskProgress(taskID string, progress float64) error {
	tm.taskMutex.Lock()
	defer tm.taskMutex.Unlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return fmt.Errorf("任务不存在: %s", taskID)
	}

	// 确保进度在有效范围内
	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}

	task.Progress = progress
	task.UpdatedAt = time.Now()

	// 如果进度达到100%，自动更新状态为完成
	if progress >= 100 && task.State == TaskStateRunning {
		tm.updateStateCounters(task.State, TaskStateCompleted)
		task.State = TaskStateCompleted
		task.EndTime = time.Now()
		if !task.StartTime.IsZero() {
			task.Duration = task.EndTime.Sub(task.StartTime)
		}
	}

	return nil
}

// AssignTaskToWorker 分配任务给工作器
func (tm *TaskMonitor) AssignTaskToWorker(taskID string, workerID int) error {
	tm.taskMutex.Lock()
	defer tm.taskMutex.Unlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return fmt.Errorf("任务不存在: %s", taskID)
	}

	task.WorkerID = workerID
	task.UpdatedAt = time.Now()

	// 分配任务给工作器

	return nil
}

// GetTask 获取任务信息
func (tm *TaskMonitor) GetTask(taskID string) (*TaskInfo, error) {
	tm.taskMutex.RLock()
	defer tm.taskMutex.RUnlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("任务不存在: %s", taskID)
	}

	// 返回任务副本
	taskCopy := *task
	return &taskCopy, nil
}

// GetAllTasks 获取所有任务
func (tm *TaskMonitor) GetAllTasks() map[string]*TaskInfo {
	tm.taskMutex.RLock()
	defer tm.taskMutex.RUnlock()

	tasks := make(map[string]*TaskInfo)
	for id, task := range tm.tasks {
		taskCopy := *task
		tasks[id] = &taskCopy
	}

	return tasks
}

// GetTasksByState 根据状态获取任务
func (tm *TaskMonitor) GetTasksByState(state TaskState) []*TaskInfo {
	tm.taskMutex.RLock()
	defer tm.taskMutex.RUnlock()

	var tasks []*TaskInfo
	for _, task := range tm.tasks {
		if task.State == state {
			taskCopy := *task
			tasks = append(tasks, &taskCopy)
		}
	}

	return tasks
}

// GetMetrics 获取监控指标
func (tm *TaskMonitor) GetMetrics() TaskMetrics {
	tm.metricsMutex.RLock()
	defer tm.metricsMutex.RUnlock()

	return tm.metrics
}

// updateStateCounters 更新状态计数器
func (tm *TaskMonitor) updateStateCounters(oldState, newState TaskState) {
	// 减少旧状态计数
	switch oldState {
	case TaskStatePending:
		atomic.AddInt64(&tm.metrics.PendingTasks, -1)
	case TaskStateQueued:
		atomic.AddInt64(&tm.metrics.QueuedTasks, -1)
	case TaskStateRunning:
		atomic.AddInt64(&tm.metrics.RunningTasks, -1)
	case TaskStateCompleted:
		atomic.AddInt64(&tm.metrics.CompletedTasks, -1)
	case TaskStateFailed:
		atomic.AddInt64(&tm.metrics.FailedTasks, -1)
	case TaskStateCanceled:
		atomic.AddInt64(&tm.metrics.CanceledTasks, -1)
	case TaskStateSkipped:
		atomic.AddInt64(&tm.metrics.SkippedTasks, -1)
	case TaskStateRetrying:
		atomic.AddInt64(&tm.metrics.RetryingTasks, -1)
	}

	// 增加新状态计数
	switch newState {
	case TaskStatePending:
		atomic.AddInt64(&tm.metrics.PendingTasks, 1)
	case TaskStateQueued:
		atomic.AddInt64(&tm.metrics.QueuedTasks, 1)
	case TaskStateRunning:
		atomic.AddInt64(&tm.metrics.RunningTasks, 1)
	case TaskStateCompleted:
		atomic.AddInt64(&tm.metrics.CompletedTasks, 1)
	case TaskStateFailed:
		atomic.AddInt64(&tm.metrics.FailedTasks, 1)
	case TaskStateCanceled:
		atomic.AddInt64(&tm.metrics.CanceledTasks, 1)
	case TaskStateSkipped:
		atomic.AddInt64(&tm.metrics.SkippedTasks, 1)
	case TaskStateRetrying:
		atomic.AddInt64(&tm.metrics.RetryingTasks, 1)
	}
}

// monitorLoop 监控主循环
func (tm *TaskMonitor) monitorLoop() {
	ticker := time.NewTicker(tm.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.updateMetrics()
		case task := <-tm.taskUpdateChan:
			tm.handleTaskUpdate(task)
		}
	}
}

// metricsUpdateLoop 指标更新循环
func (tm *TaskMonitor) metricsUpdateLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.calculateAdvancedMetrics()
		}
	}
}

// cleanupLoop 清理循环
func (tm *TaskMonitor) cleanupLoop() {
	ticker := time.NewTicker(tm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.cleanupOldTasks()
		}
	}
}

// updateMetrics 更新基础指标
func (tm *TaskMonitor) updateMetrics() {
	tm.metricsMutex.Lock()
	defer tm.metricsMutex.Unlock()

	tm.metrics.LastUpdate = time.Now()
}

// calculateAdvancedMetrics 计算高级指标
func (tm *TaskMonitor) calculateAdvancedMetrics() {
	tm.taskMutex.RLock()
	tasks := make([]*TaskInfo, 0, len(tm.tasks))
	for _, task := range tm.tasks {
		tasks = append(tasks, task)
	}
	tm.taskMutex.RUnlock()

	tm.metricsMutex.Lock()
	defer tm.metricsMutex.Unlock()

	// 计算平均等待时间和处理时间
	var totalWaitTime, totalProcessingTime time.Duration
	var waitCount, processingCount int

	for _, task := range tasks {
		if task.WaitDuration > 0 {
			totalWaitTime += task.WaitDuration
			waitCount++
		}
		if task.Duration > 0 {
			totalProcessingTime += task.Duration
			processingCount++
		}
	}

	if waitCount > 0 {
		tm.metrics.AverageWaitTime = totalWaitTime / time.Duration(waitCount)
	}
	if processingCount > 0 {
		tm.metrics.AverageProcessingTime = totalProcessingTime / time.Duration(processingCount)
	}

	// 计算成功率和失败率
	totalCompleted := atomic.LoadInt64(&tm.metrics.CompletedTasks) + atomic.LoadInt64(&tm.metrics.FailedTasks)
	if totalCompleted > 0 {
		tm.metrics.SuccessRate = float64(atomic.LoadInt64(&tm.metrics.CompletedTasks)) / float64(totalCompleted) * 100
		tm.metrics.FailureRate = float64(atomic.LoadInt64(&tm.metrics.FailedTasks)) / float64(totalCompleted) * 100
	}

	// 计算吞吐量
	elapsed := time.Since(tm.metrics.StartTime)
	if elapsed > 0 {
		tm.metrics.ThroughputPerSecond = float64(atomic.LoadInt64(&tm.metrics.CompletedTasks)) / elapsed.Seconds()
	}

	// 计算工作器利用率
	if tm.metrics.MaxWorkers > 0 {
		tm.metrics.WorkerUtilization = float64(tm.metrics.ActiveWorkers) / float64(tm.metrics.MaxWorkers) * 100
	}
}

// handleTaskUpdate 处理任务更新事件
func (tm *TaskMonitor) handleTaskUpdate(task *TaskInfo) {
	// 这里可以添加任务更新的额外处理逻辑
	// 比如发送通知、更新UI等
	// 处理任务更新事件
}

// cleanupOldTasks 清理旧任务
func (tm *TaskMonitor) cleanupOldTasks() {
	tm.taskMutex.Lock()
	defer tm.taskMutex.Unlock()

	if len(tm.tasks) <= tm.maxTaskHistory {
		return
	}

	// 收集已完成的旧任务
	var oldTasks []string
	cutoff := time.Now().Add(-24 * time.Hour) // 保留24小时内的任务

	for id, task := range tm.tasks {
		if (task.State == TaskStateCompleted || task.State == TaskStateFailed ||
			task.State == TaskStateCanceled || task.State == TaskStateSkipped) &&
			task.UpdatedAt.Before(cutoff) {
			oldTasks = append(oldTasks, id)
		}
	}

	// 删除旧任务
	for _, id := range oldTasks {
		delete(tm.tasks, id)
	}

	if len(oldTasks) > 0 {
		// 清理旧任务
	}
}

// SetMaxWorkers 设置最大工作器数量
func (tm *TaskMonitor) SetMaxWorkers(maxWorkers int32) {
	tm.metricsMutex.Lock()
	defer tm.metricsMutex.Unlock()

	tm.metrics.MaxWorkers = maxWorkers
}

// SetActiveWorkers 设置活跃工作器数量
func (tm *TaskMonitor) SetActiveWorkers(activeWorkers int32) {
	atomic.StoreInt32(&tm.metrics.ActiveWorkers, activeWorkers)
}

// GetTaskUpdateChannel 获取任务更新通道（只读）
func (tm *TaskMonitor) GetTaskUpdateChannel() <-chan *TaskInfo {
	return tm.taskUpdateChan
}

// GetMetricUpdateChannel 获取指标更新通道（只读）
func (tm *TaskMonitor) GetMetricUpdateChannel() <-chan TaskMetrics {
	return tm.metricUpdateChan
}

// IsRunning 检查监控器是否运行中
func (tm *TaskMonitor) IsRunning() bool {
	return tm.isRunning
}

// GetTaskCount 获取各状态任务数量
func (tm *TaskMonitor) GetTaskCount() map[string]int64 {
	return map[string]int64{
		"total":     atomic.LoadInt64(&tm.metrics.TotalTasks),
		"pending":   atomic.LoadInt64(&tm.metrics.PendingTasks),
		"queued":    atomic.LoadInt64(&tm.metrics.QueuedTasks),
		"running":   atomic.LoadInt64(&tm.metrics.RunningTasks),
		"completed": atomic.LoadInt64(&tm.metrics.CompletedTasks),
		"failed":    atomic.LoadInt64(&tm.metrics.FailedTasks),
		"canceled":  atomic.LoadInt64(&tm.metrics.CanceledTasks),
		"skipped":   atomic.LoadInt64(&tm.metrics.SkippedTasks),
		"retrying":  atomic.LoadInt64(&tm.metrics.RetryingTasks),
	}
}
