package state

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"
)

// State 程序状态
type State string

const (
	StateIdle          State = "IDLE"
	StateScanning      State = "SCANNING"
	StateProcessing    State = "PROCESSING"
	StateAwaitingInput State = "AWAITING_INPUT"
	StateError         State = "ERROR"
)

// StateInfo 状态信息
type StateInfo struct {
	Current   State     `json:"current"`
	Previous  State     `json:"previous"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// ProcessInfo 处理信息
type ProcessInfo struct {
	TotalFiles     int       `json:"total_files"`
	ProcessedFiles int       `json:"processed_files"`
	CurrentFile    string    `json:"current_file"`
	StartTime      time.Time `json:"start_time"`
	EstimatedEnd   time.Time `json:"estimated_end"`
	// 断点续传相关字段
	LastProcessedFile string    `json:"last_processed_file"`
	LastProcessedTime time.Time `json:"last_processed_time"`
	CheckpointData    []byte    `json:"checkpoint_data,omitempty"`
}

// Manager bbolt状态管理器
type Manager struct {
	db                 *bbolt.DB
	dbPath             string
	compressionEnabled bool
	mu                 sync.RWMutex
	stats              *PerformanceStats
	logger             *zap.Logger
}

// 数据库桶名称
const (
	stateBucket   = "state"
	processBucket = "process"
	resultsBucket = "results"
	metaBucket    = "meta"
	backupBucket  = "backup"
)

// NewManager 创建新的状态管理器
func NewManager(dbPath string, logger *zap.Logger) (*Manager, error) {
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	manager := &Manager{
		db:                 db,
		dbPath:             dbPath,
		compressionEnabled: false,
		stats:              &PerformanceStats{},
		logger:             logger,
	}

	// 初始化数据库桶
	if err := manager.initBuckets(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			var builder strings.Builder
			builder.WriteString("failed to initialize buckets: ")
			builder.WriteString(err.Error())
			builder.WriteString(", and failed to close db: ")
			builder.WriteString(closeErr.Error())
			return nil, fmt.Errorf("%s", builder.String())
		}
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	// 设置初始状态
	if err := manager.SetState(StateIdle); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			var builder strings.Builder
			builder.WriteString("failed to set initial state: ")
			builder.WriteString(err.Error())
			builder.WriteString(", and failed to close db: ")
			builder.WriteString(closeErr.Error())
			return nil, fmt.Errorf("%s", builder.String())
		}
		return nil, fmt.Errorf("failed to set initial state: %w", err)
	}

	// 加载配置
	if err := manager.loadConfig(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to load config: %w, and failed to close db: %v", err, closeErr)
		}
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return manager, nil
}

// initBuckets 初始化数据库桶
func (m *Manager) initBuckets() error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		buckets := []string{stateBucket, processBucket, resultsBucket, metaBucket, backupBucket}
		for _, bucket := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
}

// SetState 设置程序状态
func (m *Manager) SetState(newState State) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(stateBucket))
		if bucket == nil {
			return fmt.Errorf("state bucket not found")
		}

		// 获取当前状态
		currentStateInfo, _ := m.getStateInfo(bucket)

		// 创建新状态信息
		stateInfo := StateInfo{
			Current:   newState,
			Previous:  StateIdle,
			Timestamp: time.Now(),
		}

		if currentStateInfo != nil {
			stateInfo.Previous = currentStateInfo.Current
		}

		// 序列化状态信息
		data, err := json.Marshal(stateInfo)
		if err != nil {
			return fmt.Errorf("failed to marshal state info: %w", err)
		}

		// 保存状态
		return bucket.Put([]byte("current"), data)
	})
}

// GetState 获取当前状态
func (m *Manager) GetState() (*StateInfo, error) {
	var stateInfo *StateInfo

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(stateBucket))
		if bucket == nil {
			return fmt.Errorf("state bucket not found")
		}

		var err error
		stateInfo, err = m.getStateInfo(bucket)
		return err
	})

	return stateInfo, err
}

// getStateInfo 从桶中获取状态信息
func (m *Manager) getStateInfo(bucket *bbolt.Bucket) (*StateInfo, error) {
	data := bucket.Get([]byte("current"))
	if data == nil {
		return nil, nil
	}

	var stateInfo StateInfo
	if err := json.Unmarshal(data, &stateInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state info: %w", err)
	}

	return &stateInfo, nil
}

// SetProcessInfo 设置处理信息
func (m *Manager) SetProcessInfo(info *ProcessInfo) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(processBucket))
		if bucket == nil {
			return fmt.Errorf("process bucket not found")
		}

		data, err := json.Marshal(info)
		if err != nil {
			return fmt.Errorf("failed to marshal process info: %w", err)
		}

		return bucket.Put([]byte("current"), data)
	})
}

// GetProcessInfo 获取处理信息
func (m *Manager) GetProcessInfo() (*ProcessInfo, error) {
	var processInfo *ProcessInfo

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(processBucket))
		if bucket == nil {
			return fmt.Errorf("process bucket not found")
		}

		data := bucket.Get([]byte("current"))
		if data == nil {
			return nil // 没有处理信息
		}

		processInfo = &ProcessInfo{}
		return json.Unmarshal(data, processInfo)
	})

	return processInfo, err
}

// SaveResult 保存转换结果
func (m *Manager) SaveResult(key string, result interface{}) error {
	start := time.Now()
	defer func() {
		m.updateWriteStats(time.Since(start))
	}()

	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(resultsBucket))
		if bucket == nil {
			return fmt.Errorf("results bucket not found")
		}

		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}

		// 如果启用压缩且数据足够大，则压缩数据
		if m.compressionEnabled && len(data) > 1024 {
			compressedData, err := m.compressData(data)
			if err == nil && len(compressedData) < len(data) {
				// 添加压缩标记
				compressedKey := "compressed:" + key
				return bucket.Put([]byte(compressedKey), compressedData)
			}
		}

		return bucket.Put([]byte(key), data)
	})
}

// GetResult 获取转换结果
func (m *Manager) GetResult(key string, result interface{}) error {
	start := time.Now()
	defer func() {
		m.updateReadStats(time.Since(start))
	}()

	return m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(resultsBucket))
		if bucket == nil {
			return fmt.Errorf("results bucket not found")
		}

		// 首先尝试获取压缩版本
		compressedKey := "compressed:" + key
		data := bucket.Get([]byte(compressedKey))
		if data != nil {
			// 解压缩数据
			decompressedData, err := m.decompressData(data)
			if err != nil {
				return fmt.Errorf("failed to decompress data: %w", err)
			}
			return json.Unmarshal(decompressedData, result)
		}

		// 获取未压缩版本
		data = bucket.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("result not found for key: %s", key)
		}

		return json.Unmarshal(data, result)
	})
}

// ListResults 列出所有结果键
func (m *Manager) ListResults() ([]string, error) {
	var keys []string

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(resultsBucket))
		if bucket == nil {
			return fmt.Errorf("results bucket not found")
		}

		return bucket.ForEach(func(k, v []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})

	return keys, err
}

// ClearResults 清除所有结果
func (m *Manager) ClearResults() error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.DeleteBucket([]byte(resultsBucket)); err != nil {
			return fmt.Errorf("failed to delete results bucket: %w", err)
		}

		_, err := tx.CreateBucket([]byte(resultsBucket))
		if err != nil {
			return fmt.Errorf("failed to recreate results bucket: %w", err)
		}

		return nil
	})
}

// IsRunning 检查是否正在运行
func (m *Manager) IsRunning() (bool, error) {
	stateInfo, err := m.GetState()
	if err != nil {
		return false, err
	}

	if stateInfo == nil {
		return false, nil
	}

	switch stateInfo.Current {
	case StateScanning, StateProcessing:
		return true, nil
	default:
		return false, nil
	}
}

// Close 关闭状态管理器
func (m *Manager) Close() error {
	return m.db.Close()
}

// loadConfig 加载配置
func (m *Manager) loadConfig() error {
	return m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return nil
		}

		// 加载压缩配置
		if data := bucket.Get([]byte("compression_config")); data != nil {
			var config CompressionConfig
			if err := json.Unmarshal(data, &config); err == nil {
				m.compressionEnabled = config.Enabled
			}
		}

		return nil
	})
}

// compressData 压缩数据
func (m *Manager) compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decompressData 解压缩数据
func (m *Manager) decompressData(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			// 记录关闭错误但不影响主要逻辑
			if m.logger != nil {
				m.logger.Warn("Failed to close gzip reader", zap.Error(err))
			}
		}
	}()

	return io.ReadAll(reader)
}

// updateReadStats 更新读取统计
func (m *Manager) updateReadStats(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats.ReadOperations++
	m.stats.TotalReadTime += duration
	m.stats.AverageReadTime = m.stats.TotalReadTime / time.Duration(m.stats.ReadOperations)
}

// updateWriteStats 更新写入统计
func (m *Manager) updateWriteStats(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats.WriteOperations++
	m.stats.TotalWriteTime += duration
	m.stats.AverageWriteTime = m.stats.TotalWriteTime / time.Duration(m.stats.WriteOperations)
}

// GetPerformanceStats 获取性能统计
func (m *Manager) GetPerformanceStats() *PerformanceStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 返回统计数据的副本，避免复制锁
	m.stats.mu.RLock()
	defer m.stats.mu.RUnlock()

	stats := &PerformanceStats{
		ReadOperations:   m.stats.ReadOperations,
		WriteOperations:  m.stats.WriteOperations,
		TotalReadTime:    m.stats.TotalReadTime,
		TotalWriteTime:   m.stats.TotalWriteTime,
		AverageReadTime:  m.stats.AverageReadTime,
		AverageWriteTime: m.stats.AverageWriteTime,
		CompressionRatio: m.stats.CompressionRatio,
		LastBackupTime:   m.stats.LastBackupTime,
		BackupCount:      m.stats.BackupCount,
	}
	return stats
}

// CreateBackup 创建备份
func (m *Manager) CreateBackup(description string) (*BackupInfo, error) {
	backupDir := filepath.Dir(m.dbPath)
	backupName := "backup_" + strconv.FormatInt(time.Now().Unix(), 10) + ".db"
	backupPath := filepath.Join(backupDir, backupName)

	// 创建备份文件
	src, err := os.Open(m.dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source database: %w", err)
	}
	defer func() {
		if err := src.Close(); err != nil {
			// 记录关闭错误但不影响主要逻辑
			if m.logger != nil {
				m.logger.Warn("Failed to close source file", zap.Error(err))
			}
		}
	}()

	dst, err := os.Create(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() {
		if err := dst.Close(); err != nil {
			// 记录关闭错误但不影响主要逻辑
			if m.logger != nil {
				m.logger.Warn("Failed to close destination file", zap.Error(err))
			}
		}
	}()

	// 复制文件
	size, err := io.Copy(dst, src)
	if err != nil {
		return nil, fmt.Errorf("failed to copy database: %w", err)
	}

	// 创建备份信息
	backupInfo := &BackupInfo{
		Timestamp:   time.Now(),
		FilePath:    backupPath,
		Size:        size,
		Compressed:  false,
		Description: description,
	}

	// 保存备份信息到数据库
	if err := m.saveBackupInfo(backupInfo); err != nil {
		return nil, fmt.Errorf("failed to save backup info: %w", err)
	}

	// 更新统计
	m.mu.Lock()
	m.stats.LastBackupTime = time.Now()
	m.stats.BackupCount++
	m.mu.Unlock()

	return backupInfo, nil
}

// saveBackupInfo 保存备份信息
func (m *Manager) saveBackupInfo(info *BackupInfo) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(backupBucket))
		if bucket == nil {
			return fmt.Errorf("backup bucket not found")
		}

		data, err := json.Marshal(info)
		if err != nil {
			return fmt.Errorf("failed to marshal backup info: %w", err)
		}

		key := "backup_" + strconv.FormatInt(info.Timestamp.Unix(), 10)
		return bucket.Put([]byte(key), data)
	})
}

// SetCompressionConfig 设置压缩配置
func (m *Manager) SetCompressionConfig(config *CompressionConfig) error {
	err := m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return fmt.Errorf("meta bucket not found")
		}

		data, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("failed to marshal compression config: %w", err)
		}

		return bucket.Put([]byte("compression_config"), data)
	})

	if err == nil {
		m.compressionEnabled = config.Enabled
	}

	return err
}

// CreateCheckpoint 创建断点
func (m *Manager) CreateCheckpoint(processInfo *ProcessInfo, additionalData []byte) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(processBucket))
		if bucket == nil {
			return fmt.Errorf("process bucket not found")
		}

		// 更新处理信息
		if processInfo != nil {
			processInfo.LastProcessedTime = time.Now()
			processInfo.CheckpointData = additionalData

			data, err := json.Marshal(processInfo)
			if err != nil {
				return fmt.Errorf("failed to marshal process info: %w", err)
			}

			if err := bucket.Put([]byte("current"), data); err != nil {
				return fmt.Errorf("failed to save process info: %w", err)
			}
		}

		// 保存断点标记
		checkpointInfo := struct {
			Timestamp time.Time `json:"timestamp"`
			Data      []byte    `json:"data,omitempty"`
		}{
			Timestamp: time.Now(),
			Data:      additionalData,
		}

		checkpointData, err := json.Marshal(checkpointInfo)
		if err != nil {
			return fmt.Errorf("failed to marshal checkpoint info: %w", err)
		}

		return bucket.Put([]byte("checkpoint"), checkpointData)
	})
}

// GetCheckpoint 获取断点
func (m *Manager) GetCheckpoint() (*time.Time, []byte, error) {
	var timestamp *time.Time
	var data []byte

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(processBucket))
		if bucket == nil {
			return fmt.Errorf("process bucket not found")
		}

		checkpointData := bucket.Get([]byte("checkpoint"))
		if checkpointData == nil {
			return nil // 没有断点
		}

		var checkpointInfo struct {
			Timestamp time.Time `json:"timestamp"`
			Data      []byte    `json:"data,omitempty"`
		}

		if err := json.Unmarshal(checkpointData, &checkpointInfo); err != nil {
			return fmt.Errorf("failed to unmarshal checkpoint info: %w", err)
		}

		timestamp = &checkpointInfo.Timestamp
		data = checkpointInfo.Data
		return nil
	})

	return timestamp, data, err
}

// ClearCheckpoint 清除断点
func (m *Manager) ClearCheckpoint() error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(processBucket))
		if bucket == nil {
			return fmt.Errorf("process bucket not found")
		}

		return bucket.Delete([]byte("checkpoint"))
	})
}

// HasValidCheckpoint 检查是否存在有效的断点
func (m *Manager) HasValidCheckpoint() (bool, error) {
	timestamp, _, err := m.GetCheckpoint()
	if err != nil {
		return false, err
	}

	if timestamp == nil {
		return false, nil
	}

	// 检查断点是否在有效期内（24小时内）
	if time.Since(*timestamp) > 24*time.Hour {
		// 断点已过期，清除它
		if err := m.ClearCheckpoint(); err != nil {
			return false, fmt.Errorf("failed to clear expired checkpoint: %w", err)
		}
		return false, nil
	}

	return true, nil
}

// Stats 状态统计
type Stats struct {
	Database struct {
		Size     int64 `json:"size"`
		PageSize int   `json:"page_size"`
	} `json:"database"`

	Buckets map[string]int `json:"buckets"`
}

// PerformanceStats 性能统计
type PerformanceStats struct {
	mu               sync.RWMutex
	ReadOperations   int64         `json:"read_operations"`
	WriteOperations  int64         `json:"write_operations"`
	TotalReadTime    time.Duration `json:"total_read_time"`
	TotalWriteTime   time.Duration `json:"total_write_time"`
	AverageReadTime  time.Duration `json:"average_read_time"`
	AverageWriteTime time.Duration `json:"average_write_time"`
	CompressionRatio float64       `json:"compression_ratio"`
	LastBackupTime   time.Time     `json:"last_backup_time"`
	BackupCount      int64         `json:"backup_count"`
}

// BackupInfo 备份信息
type BackupInfo struct {
	Timestamp   time.Time `json:"timestamp"`
	FilePath    string    `json:"file_path"`
	Size        int64     `json:"size"`
	Compressed  bool      `json:"compressed"`
	Description string    `json:"description"`
	Checksum    string    `json:"checksum"`
}

// CompressionConfig 压缩配置
type CompressionConfig struct {
	Enabled     bool `json:"enabled"`
	Level       int  `json:"level"`        // gzip压缩级别 1-9
	MinSize     int  `json:"min_size"`     // 最小压缩大小（字节）
	AutoCleanup bool `json:"auto_cleanup"` // 自动清理旧备份
}

// GetStats 获取状态统计
func (m *Manager) GetStats() (*Stats, error) {
	var stats Stats
	stats.Buckets = make(map[string]int)

	err := m.db.View(func(tx *bbolt.Tx) error {
		// 获取数据库统计
		stats.Database.PageSize = int(tx.DB().Info().PageSize)

		// 获取数据库文件大小
		if fileInfo, err := os.Stat(m.dbPath); err == nil {
			stats.Database.Size = fileInfo.Size()
		}

		// 统计各个桶的键数量
		buckets := []string{stateBucket, processBucket, resultsBucket, metaBucket, backupBucket}
		for _, bucketName := range buckets {
			bucket := tx.Bucket([]byte(bucketName))
			if bucket != nil {
				count := 0
				bucket.ForEach(func(k, v []byte) error {
					count++
					return nil
				})
				stats.Buckets[bucketName] = count
			}
		}

		return nil
	})

	return &stats, err
}

// ListBackups 列出所有备份
func (m *Manager) ListBackups() ([]*BackupInfo, error) {
	var backups []*BackupInfo

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(backupBucket))
		if bucket == nil {
			return fmt.Errorf("backup bucket not found")
		}

		return bucket.ForEach(func(k, v []byte) error {
			var backup BackupInfo
			if err := json.Unmarshal(v, &backup); err != nil {
				return fmt.Errorf("failed to unmarshal backup info: %w", err)
			}
			backups = append(backups, &backup)
			return nil
		})
	})

	return backups, err
}

// RestoreFromBackup 从备份恢复数据库
func (m *Manager) RestoreFromBackup(backupPath string) error {
	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", backupPath)
	}

	// 关闭当前数据库连接
	if err := m.db.Close(); err != nil {
		return fmt.Errorf("failed to close current database: %w", err)
	}

	// 备份当前数据库文件
	currentBackup := m.dbPath + ".restore_backup"
	if err := m.copyFile(m.dbPath, currentBackup); err != nil {
		return fmt.Errorf("failed to backup current database: %w", err)
	}

	// 恢复备份文件
	if err := m.copyFile(backupPath, m.dbPath); err != nil {
		// 恢复失败，回滚
		if rerr := m.copyFile(currentBackup, m.dbPath); rerr != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to rollback database from backup", zap.String("backup", currentBackup), zap.Error(rerr))
			}
		}
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	// 重新打开数据库
	db, err := bbolt.Open(m.dbPath, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		// 恢复失败，回滚
		if rerr := m.copyFile(currentBackup, m.dbPath); rerr != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to rollback database after reopen error", zap.String("backup", currentBackup), zap.Error(rerr))
			}
		}
		return fmt.Errorf("failed to reopen database after restore: %w", err)
	}

	m.db = db

	// 删除临时备份文件
	if err := os.Remove(currentBackup); err != nil {
		if m.logger != nil {
			m.logger.Warn("Failed to remove backup file", zap.String("file", currentBackup), zap.Error(err))
		}
	}

	return nil
}

// copyFile 复制文件
func (m *Manager) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := sourceFile.Close(); err != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to close source file", zap.Error(err))
			}
		}
	}()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to close destination file", zap.Error(err))
			}
		}
	}()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// CompactDatabase 压缩数据库
func (m *Manager) CompactDatabase() error {
	start := time.Now()
	defer func() {
		m.updateWriteStats(time.Since(start))
	}()

	// 创建临时文件
	tempPath := m.dbPath + ".compact"
	tempDB, err := bbolt.Open(tempPath, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to create temp database: %w", err)
	}

	// 复制所有数据到临时数据库
	err = m.db.View(func(tx *bbolt.Tx) error {
		return tempDB.Update(func(tempTx *bbolt.Tx) error {
			return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
				tempBucket, err := tempTx.CreateBucket(name)
				if err != nil {
					return err
				}

				return b.ForEach(func(k, v []byte) error {
					return tempBucket.Put(k, v)
				})
			})
		})
	})

	if err != nil {
		if cerr := tempDB.Close(); cerr != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to close temp database during cleanup", zap.Error(cerr))
			}
		}
		if rerr := os.Remove(tempPath); rerr != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to remove temp file during cleanup", zap.Error(rerr))
			}
		}
		return fmt.Errorf("failed to copy data during compaction: %w", err)
	}

	// 关闭临时数据库
	if err := tempDB.Close(); err != nil {
		if rerr := os.Remove(tempPath); rerr != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to remove temp file after close error", zap.Error(rerr))
			}
		}
		return fmt.Errorf("failed to close temp database: %w", err)
	}

	// 关闭当前数据库
	if err := m.db.Close(); err != nil {
		if rerr := os.Remove(tempPath); rerr != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to remove temp file after database close error", zap.Error(rerr))
			}
		}
		return fmt.Errorf("failed to close current database: %w", err)
	}

	// 替换数据库文件
	if err := os.Rename(tempPath, m.dbPath); err != nil {
		return fmt.Errorf("failed to replace database file: %w", err)
	}

	// 重新打开数据库
	db, err := bbolt.Open(m.dbPath, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to reopen database after compaction: %w", err)
	}

	m.db = db
	return nil
}

// DeleteBackup 删除备份文件和记录
func (m *Manager) DeleteBackup(timestamp time.Time) error {
	key := "backup_" + strconv.FormatInt(timestamp.Unix(), 10)

	// 从数据库中获取备份信息
	var backupInfo BackupInfo
	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(backupBucket))
		if bucket == nil {
			return fmt.Errorf("backup bucket not found")
		}

		data := bucket.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("backup not found")
		}

		return json.Unmarshal(data, &backupInfo)
	})

	if err != nil {
		return err
	}

	// 删除备份文件
	if err := os.Remove(backupInfo.FilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete backup file: %w", err)
	}

	// 从数据库中删除备份记录
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(backupBucket))
		if bucket == nil {
			return fmt.Errorf("backup bucket not found")
		}

		return bucket.Delete([]byte(key))
	})
}

// CleanupOldBackups 清理旧备份（保留最近N个）
func (m *Manager) CleanupOldBackups(keepCount int) error {
	backups, err := m.ListBackups()
	if err != nil {
		return err
	}

	// 按时间戳排序（最新的在前）
	for i := 0; i < len(backups)-1; i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[i].Timestamp.Before(backups[j].Timestamp) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	// 删除超出保留数量的备份
	for i := keepCount; i < len(backups); i++ {
		if err := m.DeleteBackup(backups[i].Timestamp); err != nil {
			return fmt.Errorf("failed to delete old backup: %w", err)
		}
	}

	return nil
}

// GetDatabaseSize 获取数据库文件大小
func (m *Manager) GetDatabaseSize() (int64, error) {
	fileInfo, err := os.Stat(m.dbPath)
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}

// ResetPerformanceStats 重置性能统计
func (m *Manager) ResetPerformanceStats() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats = &PerformanceStats{}
}
