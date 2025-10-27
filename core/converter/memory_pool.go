package converter

import (
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// TypedPool 泛型内存池，提供类型安全的对象池
type TypedPool[T any] struct {
	pool    sync.Pool
	newFunc func() T
}

// NewTypedPool 创建新的泛型内存池
func NewTypedPool[T any](newFunc func() T) *TypedPool[T] {
	return &TypedPool[T]{
		pool: sync.Pool{
			New: func() interface{} {
				return newFunc()
			},
		},
		newFunc: newFunc,
	}
}

// Get 获取对象，类型安全
func (tp *TypedPool[T]) Get() T {
	if obj := tp.pool.Get(); obj != nil {
		if typed, ok := obj.(T); ok {
			return typed
		}
	}
	// 如果类型断言失败，创建新对象
	return tp.newFunc()
}

// Put 归还对象
func (tp *TypedPool[T]) Put(obj T) {
	tp.pool.Put(obj)
}

// MemoryPool 内存池管理器 - 使用Go 1.25+泛型特性
type MemoryPool struct {
	bufferPool    *TypedPool[*[]byte]
	metadataPool  *TypedPool[*MediaInfo]
	resultPool    *TypedPool[*ConversionResult]
	logger        *zap.Logger
	maxBufferSize int
	stats         *MemoryPoolStats
	mutex         sync.RWMutex
}

// MemoryPoolStats 内存池统计信息
type MemoryPoolStats struct {
	BufferHits     int64
	BufferMisses   int64
	MetadataHits   int64
	MetadataMisses int64
	ResultHits     int64
	ResultMisses   int64
	TotalAllocated int64
	TotalReleased  int64
	LastReset      time.Time
}

// NewMemoryPool 创建内存池
func NewMemoryPool(logger *zap.Logger, maxBufferSize int) *MemoryPool {
	mp := &MemoryPool{
		logger:        logger,
		maxBufferSize: maxBufferSize,
		stats:         &MemoryPoolStats{LastReset: time.Now()},
	}

	// 初始化缓冲区池 - 使用泛型
	mp.bufferPool = NewTypedPool(func() *[]byte {
		mp.stats.BufferMisses++
		buf := make([]byte, 0, maxBufferSize)
		return &buf
	})

	// 初始化元数据池 - 使用泛型
	mp.metadataPool = NewTypedPool(func() *MediaInfo {
		mp.stats.MetadataMisses++
		return &MediaInfo{}
	})

	// 初始化结果池 - 使用泛型
	mp.resultPool = NewTypedPool(func() *ConversionResult {
		mp.stats.ResultMisses++
		return &ConversionResult{}
	})

	return mp
}

// GetBuffer 获取缓冲区 - 使用泛型，类型安全
func (mp *MemoryPool) GetBuffer() []byte {
	// 使用原子操作减少锁竞争
	atomic.AddInt64(&mp.stats.BufferHits, 1)
	atomic.AddInt64(&mp.stats.TotalAllocated, 1)

	// 使用泛型池，类型安全
	bufPtr := mp.bufferPool.Get()
	return (*bufPtr)[:0] // 重置长度但保留容量
}

// PutBuffer 归还缓冲区 - 使用泛型，类型安全
func (mp *MemoryPool) PutBuffer(buf []byte) {
	if cap(buf) > mp.maxBufferSize {
		// 缓冲区太大，不放回池中
		return
	}

	// 使用原子操作减少锁竞争
	atomic.AddInt64(&mp.stats.TotalReleased, 1)

	// 使用泛型池，类型安全
	mp.bufferPool.Put(&buf)
}

// GetMediaInfo 获取媒体信息对象 - 使用泛型，类型安全
func (mp *MemoryPool) GetMediaInfo() *MediaInfo {
	// 使用原子操作减少锁竞争
	atomic.AddInt64(&mp.stats.MetadataHits, 1)
	atomic.AddInt64(&mp.stats.TotalAllocated, 1)

	// 使用泛型池，类型安全
	info := mp.metadataPool.Get()
	// 重置对象
	*info = MediaInfo{}
	return info
}

// PutMediaInfo 归还媒体信息对象 - 使用泛型，类型安全
func (mp *MemoryPool) PutMediaInfo(info *MediaInfo) {
	if info == nil {
		return
	}

	// 使用原子操作减少锁竞争
	atomic.AddInt64(&mp.stats.TotalReleased, 1)

	mp.metadataPool.Put(info)
}

// GetConversionResult 获取转换结果对象 - 使用泛型，类型安全
func (mp *MemoryPool) GetConversionResult() *ConversionResult {
	// 使用原子操作减少锁竞争
	atomic.AddInt64(&mp.stats.ResultHits, 1)
	atomic.AddInt64(&mp.stats.TotalAllocated, 1)

	// 使用泛型池，类型安全
	result := mp.resultPool.Get()
	// 重置对象
	*result = ConversionResult{}
	return result
}

// PutConversionResult 归还转换结果对象 - 使用泛型，类型安全
func (mp *MemoryPool) PutConversionResult(result *ConversionResult) {
	if result == nil {
		return
	}

	// 使用原子操作减少锁竞争
	atomic.AddInt64(&mp.stats.TotalReleased, 1)

	mp.resultPool.Put(result)
}

// GetStats 获取内存池统计信息
func (mp *MemoryPool) GetStats() *MemoryPoolStats {
	mp.mutex.RLock()
	defer mp.mutex.RUnlock()
	return mp.stats
}

// ResetStats 重置统计信息
func (mp *MemoryPool) ResetStats() {
	mp.mutex.Lock()
	defer mp.mutex.Unlock()

	mp.stats = &MemoryPoolStats{LastReset: time.Now()}
}

// GetHitRate 获取命中率
func (mp *MemoryPool) GetHitRate() (bufferHitRate, metadataHitRate, resultHitRate float64) {
	mp.mutex.RLock()
	defer mp.mutex.RUnlock()

	totalBufferRequests := mp.stats.BufferHits + mp.stats.BufferMisses
	if totalBufferRequests > 0 {
		bufferHitRate = float64(mp.stats.BufferHits) / float64(totalBufferRequests)
	}

	totalMetadataRequests := mp.stats.MetadataHits + mp.stats.MetadataMisses
	if totalMetadataRequests > 0 {
		metadataHitRate = float64(mp.stats.MetadataHits) / float64(totalMetadataRequests)
	}

	totalResultRequests := mp.stats.ResultHits + mp.stats.ResultMisses
	if totalResultRequests > 0 {
		resultHitRate = float64(mp.stats.ResultHits) / float64(totalResultRequests)
	}

	return
}

// LogStats 记录统计信息
func (mp *MemoryPool) LogStats() {
	// 内存池统计信息
}

// Cleanup 清理内存池 - 使用泛型重新初始化
func (mp *MemoryPool) Cleanup() {
	// 强制GC以清理未使用的对象，使用泛型重新初始化
	mp.bufferPool = NewTypedPool(func() *[]byte {
		buf := make([]byte, 0, mp.maxBufferSize)
		return &buf
	})
	mp.metadataPool = NewTypedPool(func() *MediaInfo {
		return &MediaInfo{}
	})
	mp.resultPool = NewTypedPool(func() *ConversionResult {
		return &ConversionResult{}
	})

	// 内存池已清理
}

// 全局内存池实例
var (
	globalMemoryPool *MemoryPool
	memoryPoolOnce   sync.Once
)

// GetGlobalMemoryPool 获取全局内存池
func GetGlobalMemoryPool(logger *zap.Logger) *MemoryPool {
	memoryPoolOnce.Do(func() {
		globalMemoryPool = NewMemoryPool(logger, 1024*1024) // 1MB 默认缓冲区大小
	})
	return globalMemoryPool
}
