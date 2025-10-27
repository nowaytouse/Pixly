package converter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"
)

// FileStatus 文件处理状态
type FileStatus int

const (
	StatusPending FileStatus = iota
	StatusProcessing
	StatusCompleted
	StatusFailed
	StatusSkipped
)

// String 返回状态字符串
func (s FileStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusProcessing:
		return "processing"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// FileRecord 文件处理记录
type FileRecord struct {
	FilePath     string     `json:"file_path"`
	Status       FileStatus `json:"status"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      time.Time  `json:"end_time"`
	ErrorMessage string     `json:"error_message,omitempty"`
	OutputPath   string     `json:"output_path,omitempty"`
	FileSize     int64      `json:"file_size"`
	Mode         string     `json:"mode"`
}

// SessionInfo 转换会话信息
type SessionInfo struct {
	SessionID  string    `json:"session_id"`
	TargetDir  string    `json:"target_dir"`
	Mode       string    `json:"mode"`
	StartTime  time.Time `json:"start_time"`
	LastUpdate time.Time `json:"last_update"`
	TotalFiles int       `json:"total_files"`
	Processed  int       `json:"processed"`
	Completed  int       `json:"completed"`
	Failed     int       `json:"failed"`
	Skipped    int       `json:"skipped"`
}

// CheckpointManager 断点续传管理器
type CheckpointManager struct {
	db           *bbolt.DB
	logger       *zap.Logger
	mutex        sync.RWMutex
	sessionID    string
	session      *SessionInfo
	dbPath       string
	errorHandler *ErrorHandler
}

// 数据库bucket名称
const (
	SessionBucket = "sessions"
	FilesBucket   = "files"
)

// NewCheckpointManager 创建新的断点续传管理器
func NewCheckpointManager(logger *zap.Logger, targetDir string, errorHandler *ErrorHandler) (*CheckpointManager, error) {
	// 创建数据库文件路径
	dbDir := filepath.Join(os.TempDir(), "pixly_checkpoints")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, errorHandler.WrapError("创建检查点目录失败", err)
	}

	dbPath := filepath.Join(dbDir, "conversion.db")

	// 打开数据库 - 增加超时时间并添加重试机制
	var db *bbolt.DB
	var err error

	// 重试机制：最多尝试3次
	for i := 0; i < 3; i++ {
		db, err = bbolt.Open(dbPath, 0600, &bbolt.Options{
			Timeout: 30 * time.Second, // 增加超时时间
		})
		if err == nil {
			break
		}

		// 如果是超时错误，尝试删除可能损坏的数据库文件
		if i < 2 { // 前两次尝试时
			if os.Remove(dbPath) == nil {
				logger.Warn("删除可能损坏的数据库文件", zap.String("path", dbPath))
			}
			time.Sleep(time.Second) // 等待1秒后重试
		}
	}
	if err != nil {
		return nil, errorHandler.WrapError("打开检查点数据库失败", err)
	}

	// 创建buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(SessionBucket)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(FilesBucket)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, errorHandler.WrapError("初始化数据库失败", err)
	}

	cm := &CheckpointManager{
		db:           db,
		logger:       logger,
		dbPath:       dbPath,
		errorHandler: errorHandler,
	}

	return cm, nil
}

// StartSession 开始新的转换会话
func (cm *CheckpointManager) StartSession(targetDir, mode string, totalFiles int) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 生成会话ID
	var builder strings.Builder
	builder.WriteString(filepath.Base(targetDir))
	builder.WriteString("_")
	builder.WriteString(strconv.FormatInt(time.Now().Unix(), 10))
	cm.sessionID = builder.String()

	// 创建会话信息
	cm.session = &SessionInfo{
		SessionID:  cm.sessionID,
		TargetDir:  targetDir,
		Mode:       mode,
		StartTime:  time.Now(),
		LastUpdate: time.Now(),
		TotalFiles: totalFiles,
	}

	// 保存到数据库
	return cm.saveSession()
}

// ResumeSession 恢复会话
func (cm *CheckpointManager) ResumeSession(sessionID string) (*SessionInfo, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.sessionID = sessionID

	// 从数据库加载会话
	err := cm.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(SessionBucket))
		data := b.Get([]byte(sessionID))
		if data == nil {
			var builder strings.Builder
			builder.WriteString("会话不存在: sessionID: ")
			builder.WriteString(sessionID)
			return cm.errorHandler.WrapError(builder.String(), nil)
		}

		return json.Unmarshal(data, &cm.session)
	})

	if err != nil {
		return nil, err
	}

	return cm.session, nil
}

// ListSessions 列出所有可恢复的会话
func (cm *CheckpointManager) ListSessions() ([]*SessionInfo, error) {
	var sessions []*SessionInfo

	err := cm.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(SessionBucket))
		return b.ForEach(func(k, v []byte) error {
			var session SessionInfo
			if err := json.Unmarshal(v, &session); err != nil {
				return err
			}
			sessions = append(sessions, &session)
			return nil
		})
	})

	return sessions, err
}

// UpdateFileStatus 更新文件状态 - 原子操作，立即同步到磁盘
func (cm *CheckpointManager) UpdateFileStatus(filePath string, status FileStatus, errorMsg string, outputPath string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.sessionID == "" {
		return cm.errorHandler.WrapError("没有活动会话", nil)
	}

	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	var fileSize int64
	if err == nil {
		fileSize = fileInfo.Size()
	}

	// 创建文件记录
	record := &FileRecord{
		FilePath:     filePath,
		Status:       status,
		StartTime:    time.Now(),
		EndTime:      time.Now(),
		ErrorMessage: errorMsg,
		OutputPath:   outputPath,
		FileSize:     fileSize,
		Mode:         cm.session.Mode,
	}

	// 如果是开始处理，只设置开始时间
	if status == StatusProcessing {
		record.EndTime = time.Time{}
	}

	// 原子保存文件记录和会话状态
	var keyBuilder strings.Builder
	keyBuilder.WriteString(cm.sessionID)
	keyBuilder.WriteString(":")
	keyBuilder.WriteString(filePath)
	key := keyBuilder.String()
	err = cm.db.Update(func(tx *bbolt.Tx) error {
		// 保存文件记录
		filesBucket := tx.Bucket([]byte(FilesBucket))
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := filesBucket.Put([]byte(key), data); err != nil {
			return err
		}

		// 同时更新会话统计
		cm.updateSessionStats(status)

		// 保存会话状态
		sessionBucket := tx.Bucket([]byte(SessionBucket))
		sessionData, err := json.Marshal(cm.session)
		if err != nil {
			return err
		}
		return sessionBucket.Put([]byte(cm.sessionID), sessionData)
	})

	if err != nil {
		return err
	}

	// 强制同步到磁盘 - 确保断电也不丢失
	return cm.db.Sync()
}

// GetFileStatus 获取文件状态
func (cm *CheckpointManager) GetFileStatus(filePath string) (*FileRecord, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.sessionID == "" {
		return nil, cm.errorHandler.WrapError("没有活动会话", nil)
	}

	var keyBuilder strings.Builder
	keyBuilder.WriteString(cm.sessionID)
	keyBuilder.WriteString(":")
	keyBuilder.WriteString(filePath)
	key := keyBuilder.String()
	var record FileRecord

	err := cm.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(FilesBucket))
		data := b.Get([]byte(key))
		if data == nil {
			return cm.errorHandler.WrapError("文件记录不存在", nil)
		}
		return json.Unmarshal(data, &record)
	})

	if err != nil {
		return nil, err
	}

	return &record, nil
}

// GetPendingFiles 获取待处理的文件列表
func (cm *CheckpointManager) GetPendingFiles() ([]string, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.sessionID == "" {
		return nil, cm.errorHandler.WrapError("没有活动会话", nil)
	}

	var pendingFiles []string
	prefix := cm.sessionID + ":"

	err := cm.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(FilesBucket))
		c := b.Cursor()

		for k, v := c.Seek([]byte(prefix)); k != nil && string(k)[:len(prefix)] == prefix; k, v = c.Next() {
			var record FileRecord
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}

			if record.Status == StatusPending {
				pendingFiles = append(pendingFiles, record.FilePath)
			}
		}
		return nil
	})

	return pendingFiles, err
}

// SaveCurrentState 保存当前状态 - 强制同步到磁盘
func (cm *CheckpointManager) SaveCurrentState() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.session == nil {
		return nil
	}

	cm.session.LastUpdate = time.Now()
	if err := cm.saveSession(); err != nil {
		return err
	}

	// 强制同步到磁盘
	return cm.db.Sync()
}

// CleanupSession 清理会话
func (cm *CheckpointManager) CleanupSession(sessionID string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	return cm.db.Update(func(tx *bbolt.Tx) error {
		// 删除会话
		sessionBucket := tx.Bucket([]byte(SessionBucket))
		if err := sessionBucket.Delete([]byte(sessionID)); err != nil {
			return err
		}

		// 删除相关文件记录
		filesBucket := tx.Bucket([]byte(FilesBucket))
		c := filesBucket.Cursor()
		prefix := sessionID + ":"

		var keysToDelete [][]byte
		for k, v := c.Seek([]byte(prefix)); k != nil && string(k)[:len(prefix)] == prefix; k, v = c.Next() {
			// v is the value associated with the key, we don't need it for deletion
			_ = v // explicitly acknowledge we're not using the value
			keysToDelete = append(keysToDelete, append([]byte(nil), k...))
		}

		for _, key := range keysToDelete {
			if err := filesBucket.Delete(key); err != nil {
				return err
			}
		}

		return nil
	})
}

// Close 关闭检查点管理器
func (cm *CheckpointManager) Close() error {
	if cm.db != nil {
		return cm.db.Close()
	}
	return nil
}

// saveSession 保存会话到数据库
func (cm *CheckpointManager) saveSession() error {
	if cm.session == nil {
		return cm.errorHandler.WrapError("没有会话数据", nil)
	}

	return cm.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(SessionBucket))
		data, err := json.Marshal(cm.session)
		if err != nil {
			return err
		}
		return b.Put([]byte(cm.sessionID), data)
	})
}

// updateSessionStats 更新会话统计
func (cm *CheckpointManager) updateSessionStats(status FileStatus) {
	if cm.session == nil {
		return
	}

	switch status {
	case StatusProcessing:
		cm.session.Processed++
	case StatusCompleted:
		cm.session.Completed++
	case StatusFailed:
		cm.session.Failed++
	case StatusSkipped:
		cm.session.Skipped++
	}

	cm.session.LastUpdate = time.Now()
}

// GetSessionInfo 获取当前会话信息
func (cm *CheckpointManager) GetSessionInfo() *SessionInfo {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.session
}
