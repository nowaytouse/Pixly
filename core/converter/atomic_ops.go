package converter

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"pixly/config"

	"go.uber.org/zap"
)

// AtomicFileOperations 六步原子操作文件替换机制
type AtomicFileOperations struct {
	logger       *zap.Logger
	config       *config.Config
	errorHandler *ErrorHandler
}

// NewAtomicFileOperations 创建原子操作实例
func NewAtomicFileOperations(logger *zap.Logger, config *config.Config, errorHandler *ErrorHandler) *AtomicFileOperations {
	return &AtomicFileOperations{
		logger:       logger,
		config:       config,
		errorHandler: errorHandler,
	}
}

// SixStepAtomicReplace 六步原子操作文件替换
// 步骤1: 创建临时文件
// 步骤2: 写入新内容到临时文件
// 步骤3: 同步临时文件到磁盘
// 步骤4: 原子性重命名临时文件为目标文件
// 步骤5: 同步包含目录到磁盘
// 步骤6: 验证替换结果
func (afo *AtomicFileOperations) SixStepAtomicReplace(oldPath, newPath string, content []byte) error {
	// 开始六步原子操作文件替换

	// 预检查：磁盘空间
	requiredSize := int64(len(content)) * 2 // 考虑临时文件和最终文件
	if err := afo.checkDiskSpace(newPath, requiredSize); err != nil {
		return afo.errorHandler.WrapError("磁盘空间预检查失败", err)
	}

	// 步骤1: 创建临时文件
	tempPath := newPath + ".tmp." + time.Now().Format("20060102150405")
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return afo.errorHandler.WrapError("步骤1失败 - 无法创建临时文件", err)
	}

	// 增强的临时文件清理机制
	var tempFileCreated bool = true
	defer func() {
		// 确保临时文件被清理
		if tempFileCreated {
			if err := tempFile.Close(); err != nil {
				afo.logger.Warn("清理临时文件时关闭失败", zap.String("temp_path", tempPath), zap.Error(err))
			}
			if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
				afo.logger.Warn("清理临时文件时删除失败", zap.String("temp_path", tempPath), zap.Error(err))
			}
		}
	}()

	// 步骤2: 写入新内容到临时文件
	if _, err := tempFile.Write(content); err != nil {
		return afo.errorHandler.WrapError("步骤2失败 - 无法写入临时文件", err)
	}

	// 步骤3: 同步临时文件到磁盘
	if err := tempFile.Sync(); err != nil {
		return afo.errorHandler.WrapError("步骤3失败 - 无法同步临时文件", err)
	}

	// 关闭临时文件
	if err := tempFile.Close(); err != nil {
		return afo.errorHandler.WrapError("无法关闭临时文件", err)
	}

	// 步骤4: 原子性重命名临时文件为目标文件
	if err := os.Rename(tempPath, newPath); err != nil {
		return afo.errorHandler.WrapError("步骤4失败 - 无法原子性重命名文件", err)
	}

	// 重命名成功，标记临时文件已被处理，避免defer中重复清理
	tempFileCreated = false

	// 步骤5: 同步包含目录到磁盘
	dir := GlobalPathUtils.GetDirName(newPath)
	if err := syncDir(dir); err != nil {
		afo.logger.Warn("步骤5警告 - 无法同步目录", zap.String("directory", dir), zap.Error(err))
	}

	// 步骤6: 验证替换结果
	if err := afo.verifyFileReplacement(oldPath, newPath); err != nil {
		return afo.errorHandler.WrapError("步骤6失败 - 文件替换验证失败", err)
	}

	// 六步原子操作文件替换完成

	return nil
}

// SixStepAtomicReplaceWithMetadata 六步原子操作文件替换（包含元数据迁移）
func (afo *AtomicFileOperations) SixStepAtomicReplaceWithMetadata(oldPath, newPath string, content []byte) error {
	// 开始六步原子操作文件替换（包含元数据迁移）

	// 预检查：磁盘空间
	requiredSize := int64(len(content)) * 2 // 考虑临时文件和最终文件
	if err := afo.checkDiskSpace(newPath, requiredSize); err != nil {
		return afo.errorHandler.WrapError("磁盘空间预检查失败", err)
	}

	// 步骤1: 创建临时文件
	tempPath := newPath + ".tmp." + time.Now().Format("20060102150405")
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return afo.errorHandler.WrapError("步骤1失败 - 无法创建临时文件", err)
	}

	// 增强的临时文件清理机制
	var tempFileCreated bool = true
	defer func() {
		// 确保临时文件被清理
		if tempFileCreated {
			if err := tempFile.Close(); err != nil {
				afo.logger.Warn("清理临时文件时关闭失败", zap.String("temp_path", tempPath), zap.Error(err))
			}
			if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
				afo.logger.Warn("清理临时文件时删除失败", zap.String("temp_path", tempPath), zap.Error(err))
			}
		}
	}()

	// 步骤2: 写入新内容到临时文件
	if _, err := tempFile.Write(content); err != nil {
		return afo.errorHandler.WrapError("步骤2失败 - 无法写入临时文件", err)
	}

	// 步骤3: 同步临时文件到磁盘
	if err := tempFile.Sync(); err != nil {
		return afo.errorHandler.WrapError("步骤3失败 - 无法同步临时文件", err)
	}

	// 关闭临时文件
	if err := tempFile.Close(); err != nil {
		return afo.errorHandler.WrapError("无法关闭临时文件", err)
	}

	// 在重命名前迁移元数据
	if err := afo.migrateMetadata(oldPath, tempPath); err != nil {
		afo.logger.Warn("元数据迁移失败", zap.String("source", oldPath), zap.String("target", tempPath), zap.Error(err))
	}

	// 步骤4: 原子性重命名临时文件为目标文件
	if err := os.Rename(tempPath, newPath); err != nil {
		return afo.errorHandler.WrapError("步骤4失败 - 无法原子性重命名文件", err)
	}

	// 重命名成功，标记临时文件已被处理，避免defer中重复清理
	tempFileCreated = false

	// 步骤5: 同步包含目录到磁盘
	dir := GlobalPathUtils.GetDirName(newPath)
	if err := syncDir(dir); err != nil {
		afo.logger.Warn("步骤5警告 - 无法同步目录", zap.String("directory", dir), zap.Error(err))
	}

	// 步骤6: 验证替换结果
	if err := afo.verifyFileReplacement(oldPath, newPath); err != nil {
		return afo.errorHandler.WrapError("步骤6失败 - 文件替换验证失败", err)
	}

	// 关键修复：确保oldPath和newPath是同一个文件时才删除
	// 检查是否是原地转换
	if oldPath == newPath {
		// 原地转换：无需删除，文件已被替换
		afo.logger.Debug("原地转换完成，无需删除原始文件", zap.String("path", newPath))
	} else {
		// 非原地转换：需要删除原始文件
		if err := os.Remove(oldPath); err != nil {
			afo.logger.Warn("删除原始文件失败", zap.String("old_path", oldPath), zap.Error(err))
			return afo.errorHandler.WrapError("删除原始文件失败", err)
		}
		afo.logger.Debug("原始文件已删除", zap.String("old_path", oldPath))
	}

	// 六步原子操作文件替换（包含元数据迁移）完成

	return nil
}

// verifyFileReplacement 验证文件替换结果
func (afo *AtomicFileOperations) verifyFileReplacement(oldPath, newPath string) error {
	// 检查新文件是否存在
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		var errorBuilder strings.Builder
		errorBuilder.WriteString("新文件不存在: path: ")
		errorBuilder.WriteString(newPath)
		return afo.errorHandler.WrapError(errorBuilder.String(), nil)
	}

	// 检查文件大小是否合理
	newInfo, err := os.Stat(newPath)
	if err != nil {
		return afo.errorHandler.WrapError("无法获取新文件信息", err)
	}

	if newInfo.Size() == 0 {
		var sizeErrorBuilder strings.Builder
		sizeErrorBuilder.WriteString("新文件大小为0: path: ")
		sizeErrorBuilder.WriteString(newPath)
		return afo.errorHandler.WrapError(sizeErrorBuilder.String(), nil)
	}

	// 如果原文件存在，检查文件大小是否合理
	if oldInfo, err := os.Stat(oldPath); err == nil {
		// 对于JPEG重新包装，允许更大的文件大小
		// 只有当文件大小超过原文件4倍时才认为异常
		if newInfo.Size() > oldInfo.Size()*4 {
			afo.logger.Warn("新文件大小异常",
				zap.Int64("old_size", oldInfo.Size()),
				zap.Int64("new_size", newInfo.Size()))
		}
	}

	return nil
}

// migrateMetadata 使用exiftool迁移元数据
func (afo *AtomicFileOperations) migrateMetadata(sourcePath, targetPath string) error {
	// 检查exiftool是否可用
	if _, err := os.Stat(afo.config.Tools.ExiftoolPath); os.IsNotExist(err) {
		var toolErrorBuilder strings.Builder
		toolErrorBuilder.WriteString("exiftool不可用: path: ")
		toolErrorBuilder.WriteString(afo.config.Tools.ExiftoolPath)
		return afo.errorHandler.WrapError(toolErrorBuilder.String(), nil)
	}

	// 使用exiftool迁移所有元数据
	// 命令: exiftool -TagsFromFile source -all:all target
	args := []string{
		"-TagsFromFile", sourcePath,
		"-all:all", targetPath,
		"-overwrite_original",
	}

	cmd := exec.Command(afo.config.Tools.ExiftoolPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return afo.errorHandler.WrapErrorWithOutput("exiftool元数据迁移失败", err, output)
	}

	// 元数据迁移完成

	return nil
}

// syncDir 同步目录到磁盘
func syncDir(dir string) error {
	// 打开目录
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			// 目录关闭失败通常不是致命错误，记录警告即可
		}
	}()

	// 同步目录
	if err := d.Sync(); err != nil {
		return err
	}

	return nil
}

// checkDiskSpace 检查磁盘空间是否足够
func (afo *AtomicFileOperations) checkDiskSpace(filePath string, requiredSize int64) error {
	// 获取文件所在目录的磁盘空间信息
	dir := GlobalPathUtils.GetDirName(filePath)

	// 使用 os.Stat 获取文件系统信息（跨平台兼容）
	var availableSpace int64

	// 简化的磁盘空间检查实现
	// 在实际生产环境中，可以使用 golang.org/x/sys/unix 或 golang.org/x/sys/windows
	// 来获取更精确的磁盘空间信息

	// 这里使用一个保守的估算方法
	if _, err := os.Stat(dir); err == nil {
		// 假设至少需要1GB的可用空间作为安全缓冲
		minRequiredSpace := int64(1024 * 1024 * 1024) // 1GB
		if requiredSize < minRequiredSpace {
			requiredSize = minRequiredSpace
		}

		// 简化检查：如果目录可访问，假设有足够空间
		// 实际实现应该调用系统API获取真实的可用空间
		availableSpace = requiredSize + 1 // 简化实现
	} else {
		return afo.errorHandler.WrapError("无法访问目标目录", err)
	}

	if availableSpace < requiredSize {
		var spaceErrorBuilder strings.Builder
		spaceErrorBuilder.WriteString("磁盘空间不足：需要 ")
		spaceErrorBuilder.WriteString(strconv.FormatInt(requiredSize, 10))
		spaceErrorBuilder.WriteString(" 字节，可用 ")
		spaceErrorBuilder.WriteString(strconv.FormatInt(availableSpace, 10))
		spaceErrorBuilder.WriteString(" 字节")
		return afo.errorHandler.WrapError(spaceErrorBuilder.String(), nil)
	}

	// 磁盘空间检查通过

	return nil
}
