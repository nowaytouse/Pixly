package converter

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"go.uber.org/zap"
)

// PerformanceOptimizer 性能优化器
type PerformanceOptimizer struct {
	logger          *zap.Logger
	maxWorkers      int32
	currentWorkers  int32
	memoryThreshold float64 // 内存使用阈值 (0.0-1.0)
	cpuThreshold    float64 // CPU使用阈值 (0.0-1.0)
	monitorInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	mutex           sync.RWMutex

	// 性能指标
	metrics    *PerformanceMetrics
	memoryPool *MemoryPool

	// 动态调整参数
	adjustmentFactor     float64
	lastAdjustment       time.Time
	minWorkers           int32
	maxMemoryMB          int64
	optimizationCooldown time.Duration
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	TotalProcessed  int64
	AverageTime     time.Duration
	MemoryUsage     float64
	MemoryAvailable uint64
	MemoryTotal     uint64
	CPUUsage        float64
	CPUCores        int
	GoroutineCount  int
	GCPauseTime     time.Duration
	Throughput      float64 // 文件/秒
	ProcessingRate  float64
	ErrorRate       float64
	// 磁盘监控
	DiskUsagePercent float64
	DiskReadBytes    uint64
	DiskWriteBytes   uint64
	DiskIOPS         float64
	// 网络监控
	NetworkSentBytes uint64
	NetworkRecvBytes uint64
	NetworkPackets   uint64
	// 系统负载
	LoadAverage1  float64
	LoadAverage5  float64
	LoadAverage15 float64
	// 系统信息
	Uptime       uint64
	ProcessCount uint64
	LastUpdate   time.Time
	mutex        sync.RWMutex
}

// NewPerformanceOptimizer 创建性能优化器
func NewPerformanceOptimizer(logger *zap.Logger, maxWorkers int, memoryLimitMB int64) *PerformanceOptimizer {
	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &PerformanceOptimizer{
		logger:               logger,
		maxWorkers:           int32(maxWorkers),
		currentWorkers:       int32(maxWorkers / 2), // 从一半开始
		memoryThreshold:      0.75,                  // 75%内存阈值
		cpuThreshold:         0.8,                   // 80%CPU阈值
		monitorInterval:      time.Second * 3,       // 更频繁的监控
		ctx:                  ctx,
		cancel:               cancel,
		metrics:              &PerformanceMetrics{},
		memoryPool:           GetGlobalMemoryPool(logger),
		adjustmentFactor:     1.2,
		minWorkers:           int32(max(1, runtime.NumCPU()/2)),
		maxMemoryMB:          memoryLimitMB,
		optimizationCooldown: time.Second * 10,
	}

	// 启动监控协程
	go optimizer.monitor()

	return optimizer
}

// GetOptimalWorkerCount 获取最优工作线程数
func (po *PerformanceOptimizer) GetOptimalWorkerCount() int {
	return int(atomic.LoadInt32(&po.currentWorkers))
}

// AdjustWorkerCount 调整工作线程数
func (po *PerformanceOptimizer) AdjustWorkerCount(delta int) {
	po.mutex.Lock()
	defer po.mutex.Unlock()

	newCount := po.currentWorkers + int32(delta)
	if newCount < po.minWorkers {
		newCount = po.minWorkers
	} else if newCount > po.maxWorkers {
		newCount = po.maxWorkers
	}

	if newCount != po.currentWorkers {
		atomic.StoreInt32(&po.currentWorkers, newCount)
		po.lastAdjustment = time.Now()
		// 调整工作线程数
	}
}

// monitor 监控系统性能并动态调整
func (po *PerformanceOptimizer) monitor() {
	ticker := time.NewTicker(po.monitorInterval)
	defer ticker.Stop()

	// 内存池统计定时器
	memoryPoolTicker := time.NewTicker(time.Minute * 5)
	defer memoryPoolTicker.Stop()

	for {
		select {
		case <-po.ctx.Done():
			return
		case <-ticker.C:
			memInfo, err := mem.VirtualMemory()
			if err != nil {
				po.logger.Error("获取内存信息失败", zap.Error(err))
				continue
			}
			po.updateMetrics(memInfo)
			if time.Since(po.lastAdjustment) >= po.optimizationCooldown {
				po.performOptimization()
			}
		case <-memoryPoolTicker.C:
			po.memoryPool.LogStats()
		}
	}
}

// performOptimization 执行性能优化
func (po *PerformanceOptimizer) performOptimization() {
	// 获取内存使用情况
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		po.logger.Warn("获取内存信息失败", zap.Error(err))
		return
	}

	// 更新性能指标
	po.updateMetrics(memInfo)

	// 检查是否需要调整
	if time.Since(po.lastAdjustment) < time.Minute {
		return // 避免频繁调整
	}

	currentMemoryUsage := memInfo.UsedPercent / 100.0
	currentWorkers := atomic.LoadInt32(&po.currentWorkers)

	// 获取CPU使用率
	po.metrics.mutex.RLock()
	cpuUsage := po.metrics.CPUUsage
	goroutineCount := po.metrics.GoroutineCount
	gcPauseTime := po.metrics.GCPauseTime
	po.metrics.mutex.RUnlock()

	// 内存压力过大，减少工作线程
	if currentMemoryUsage > po.memoryThreshold {
		reduction := int(float64(currentWorkers) * 0.2) // 减少20%
		if reduction < 1 {
			reduction = 1
		}
		po.AdjustWorkerCount(-reduction)
		// 内存压力过大，减少工作线程

		// 清理内存池
		po.memoryPool.Cleanup()
		// 强制GC
		runtime.GC()
		return
	}

	// CPU压力过大，减少工作线程
	if cpuUsage > po.cpuThreshold {
		reduction := int(float64(currentWorkers) * 0.15) // 减少15%
		if reduction < 1 {
			reduction = 1
		}
		po.AdjustWorkerCount(-reduction)
		// CPU压力过大，减少工作线程
		return
	}

	// 内存使用较低且性能良好，可以增加工作线程
	if currentMemoryUsage < 0.6 && cpuUsage < 0.6 && currentWorkers < po.maxWorkers {
		// 检查吞吐量是否稳定
		po.metrics.mutex.RLock()
		throughput := po.metrics.Throughput
		po.metrics.mutex.RUnlock()

		if throughput > 0 { // 有活跃处理
			increase := int(float64(currentWorkers) * 0.1) // 增加10%
			if increase < 1 {
				increase = 1
			}
			po.AdjustWorkerCount(increase)
			// 性能良好，增加工作线程
		}
	}

	// 检查GC压力和Goroutine数量
	if gcPauseTime > time.Millisecond*50 || goroutineCount > runtime.NumCPU()*100 {
		// GC暂停时间过长或Goroutine数量过多，减少工作线程
		reduction := int(float64(currentWorkers) * 0.1)
		if reduction < 1 {
			reduction = 1
		}
		po.AdjustWorkerCount(-reduction)
	}
}

// updateMetrics 更新性能指标
func (po *PerformanceOptimizer) updateMetrics(memInfo *mem.VirtualMemoryStat) {
	po.metrics.mutex.Lock()
	defer po.metrics.mutex.Unlock()

	// 获取CPU信息
	cpuPercent, err := cpu.Percent(time.Millisecond*100, false)
	if err != nil {
		po.logger.Warn("获取CPU信息失败", zap.Error(err))
		// 回退到基于Goroutine的估算
		po.metrics.CPUUsage = float64(runtime.NumGoroutine()) / float64(runtime.NumCPU()*10)
		if po.metrics.CPUUsage > 1.0 {
			po.metrics.CPUUsage = 1.0
		}
	} else if len(cpuPercent) > 0 {
		po.metrics.CPUUsage = cpuPercent[0] / 100.0
	}

	// 获取CPU核心数
	po.metrics.CPUCores = runtime.NumCPU()

	// 获取运行时信息
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	po.metrics.MemoryUsage = memInfo.UsedPercent / 100.0
	po.metrics.MemoryAvailable = memInfo.Available
	po.metrics.MemoryTotal = memInfo.Total
	po.metrics.GoroutineCount = runtime.NumGoroutine()
	po.metrics.GCPauseTime = time.Duration(m.PauseNs[(m.NumGC+255)%256])

	// 获取磁盘使用情况
	if diskStat, err := disk.Usage("/"); err == nil {
		po.metrics.DiskUsagePercent = diskStat.UsedPercent
	}

	// 获取磁盘IO统计
	if diskIOStats, err := disk.IOCounters(); err == nil {
		var totalReadBytes, totalWriteBytes uint64
		var totalIOPS float64
		for _, stat := range diskIOStats {
			totalReadBytes += stat.ReadBytes
			totalWriteBytes += stat.WriteBytes
			totalIOPS += float64(stat.ReadCount + stat.WriteCount)
		}
		po.metrics.DiskReadBytes = totalReadBytes
		po.metrics.DiskWriteBytes = totalWriteBytes
		po.metrics.DiskIOPS = totalIOPS
	}

	// 获取网络统计
	if netIOStats, err := net.IOCounters(false); err == nil && len(netIOStats) > 0 {
		var totalSent, totalRecv, totalPackets uint64
		for _, stat := range netIOStats {
			totalSent += stat.BytesSent
			totalRecv += stat.BytesRecv
			totalPackets += stat.PacketsSent + stat.PacketsRecv
		}
		po.metrics.NetworkSentBytes = totalSent
		po.metrics.NetworkRecvBytes = totalRecv
		po.metrics.NetworkPackets = totalPackets
	}

	// 获取系统负载
	if loadStat, err := load.Avg(); err == nil {
		po.metrics.LoadAverage1 = loadStat.Load1
		po.metrics.LoadAverage5 = loadStat.Load5
		po.metrics.LoadAverage15 = loadStat.Load15
	}

	// 获取系统信息
	if hostInfo, err := host.Info(); err == nil {
		po.metrics.Uptime = hostInfo.Uptime
		po.metrics.ProcessCount = uint64(hostInfo.Procs)
	}

	po.metrics.LastUpdate = time.Now()

	// 计算处理速率（如果有历史数据）
	if po.metrics.TotalProcessed > 0 {
		elapsed := time.Since(po.metrics.LastUpdate)
		if elapsed > 0 {
			po.metrics.ProcessingRate = float64(po.metrics.TotalProcessed) / elapsed.Seconds()
		}
	}
}

// RecordProcessing 记录处理性能
func (po *PerformanceOptimizer) RecordProcessing(duration time.Duration) {
	po.metrics.mutex.Lock()
	defer po.metrics.mutex.Unlock()

	po.metrics.TotalProcessed++

	// 计算平均处理时间
	if po.metrics.AverageTime == 0 {
		po.metrics.AverageTime = duration
	} else {
		// 使用指数移动平均
		alpha := 0.1
		po.metrics.AverageTime = time.Duration(float64(po.metrics.AverageTime)*(1-alpha) + float64(duration)*alpha)
	}

	// 计算吞吐量 (最近5分钟)
	now := time.Now()
	if now.Sub(po.metrics.LastUpdate) > 0 {
		po.metrics.Throughput = float64(po.metrics.TotalProcessed) / now.Sub(po.metrics.LastUpdate).Seconds()
	}
}

// GetMetrics 获取性能指标
func (po *PerformanceOptimizer) GetMetrics() PerformanceMetrics {
	po.metrics.mutex.RLock()
	defer po.metrics.mutex.RUnlock()

	// 返回不包含锁的副本
	return PerformanceMetrics{
		TotalProcessed:   po.metrics.TotalProcessed,
		AverageTime:      po.metrics.AverageTime,
		MemoryUsage:      po.metrics.MemoryUsage,
		MemoryAvailable:  po.metrics.MemoryAvailable,
		MemoryTotal:      po.metrics.MemoryTotal,
		CPUUsage:         po.metrics.CPUUsage,
		CPUCores:         po.metrics.CPUCores,
		GoroutineCount:   po.metrics.GoroutineCount,
		GCPauseTime:      po.metrics.GCPauseTime,
		Throughput:       po.metrics.Throughput,
		ProcessingRate:   po.metrics.ProcessingRate,
		ErrorRate:        po.metrics.ErrorRate,
		DiskUsagePercent: po.metrics.DiskUsagePercent,
		DiskReadBytes:    po.metrics.DiskReadBytes,
		DiskWriteBytes:   po.metrics.DiskWriteBytes,
		DiskIOPS:         po.metrics.DiskIOPS,
		NetworkSentBytes: po.metrics.NetworkSentBytes,
		NetworkRecvBytes: po.metrics.NetworkRecvBytes,
		NetworkPackets:   po.metrics.NetworkPackets,
		LoadAverage1:     po.metrics.LoadAverage1,
		LoadAverage5:     po.metrics.LoadAverage5,
		LoadAverage15:    po.metrics.LoadAverage15,
		Uptime:           po.metrics.Uptime,
		ProcessCount:     po.metrics.ProcessCount,
		LastUpdate:       po.metrics.LastUpdate,
	}
}

// Shutdown 关闭优化器
func (po *PerformanceOptimizer) Shutdown() {
	po.cancel()
	// 性能优化器已关闭
}

// IsMemoryPressureHigh 检查内存压力是否过高
func (po *PerformanceOptimizer) IsMemoryPressureHigh() bool {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return false
	}
	return memInfo.UsedPercent/100.0 > po.memoryThreshold
}

// GetRecommendedBatchSize 获取推荐的批处理大小
func (po *PerformanceOptimizer) GetRecommendedBatchSize() int {
	currentWorkers := atomic.LoadInt32(&po.currentWorkers)
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return int(currentWorkers) * 2
	}

	// 根据内存使用情况调整批处理大小
	memoryUsage := memInfo.UsedPercent / 100.0
	if memoryUsage > 0.8 {
		return int(currentWorkers) // 高内存压力时减少批处理大小
	} else if memoryUsage < 0.5 {
		return int(currentWorkers) * 3 // 低内存使用时增加批处理大小
	}
	return int(currentWorkers) * 2 // 默认批处理大小
}
