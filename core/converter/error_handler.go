package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ErrorType 定义错误类型
type ErrorType string

const (
	// 文件操作错误
	ErrorTypeFileOperation ErrorType = "FILE_OPERATION"
	// 转换错误
	ErrorTypeConversion ErrorType = "CONVERSION"
	// 工具执行错误
	ErrorTypeToolExecution ErrorType = "TOOL_EXECUTION"
	// 配置错误
	ErrorTypeConfiguration ErrorType = "CONFIGURATION"
	// 系统资源错误
	ErrorTypeSystemResource ErrorType = "SYSTEM_RESOURCE"
	// 用户输入错误
	ErrorTypeUserInput ErrorType = "USER_INPUT"
	// 未知错误
	ErrorTypeUnknown ErrorType = "UNKNOWN"
)

// ErrorSeverity 定义错误严重程度
type ErrorSeverity string

const (
	SeverityLow      ErrorSeverity = "LOW"
	SeverityMedium   ErrorSeverity = "MEDIUM"
	SeverityHigh     ErrorSeverity = "HIGH"
	SeverityCritical ErrorSeverity = "CRITICAL"
)

// ErrorContext 泛型错误上下文
type ErrorContext[T any] struct {
	Data map[string]T `json:"data,omitempty"`
}

// NewErrorContext 创建新的错误上下文
func NewErrorContext[T any]() *ErrorContext[T] {
	return &ErrorContext[T]{
		Data: make(map[string]T),
	}
}

// Set 设置上下文值
func (ec *ErrorContext[T]) Set(key string, value T) {
	ec.Data[key] = value
}

// Get 获取上下文值
func (ec *ErrorContext[T]) Get(key string) (T, bool) {
	value, exists := ec.Data[key]
	return value, exists
}

// PixlyError 增强的错误结构
type PixlyError struct {
	ID         string             `json:"id"`
	Type       ErrorType          `json:"type"`
	Severity   ErrorSeverity      `json:"severity"`
	Message    string             `json:"message"`
	Operation  string             `json:"operation"`
	FilePath   string             `json:"file_path,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
	StackTrace string             `json:"stack_trace,omitempty"`
	Cause      error              `json:"-"`
	Retryable  bool               `json:"retryable"`
	Context    *ErrorContext[any] `json:"context,omitempty"`
}

// Error 实现error接口
func (pe *PixlyError) Error() string {
	var builder strings.Builder
	builder.WriteString("[")
	builder.WriteString(string(pe.Type))
	builder.WriteString(":")
	builder.WriteString(string(pe.Severity))
	builder.WriteString("] ")
	builder.WriteString(pe.Message)
	builder.WriteString(" (operation: ")
	builder.WriteString(pe.Operation)
	if pe.FilePath != "" {
		builder.WriteString(", file: ")
		builder.WriteString(pe.FilePath)
	}
	builder.WriteString(")")
	return builder.String()
}

// Unwrap 支持错误链
func (pe *PixlyError) Unwrap() error {
	return pe.Cause
}

// ErrorHandler 统一的错误处理器
type ErrorHandler struct {
	logger       *zap.Logger
	errorCounter map[string]int
	retryPolicy  *RetryPolicy
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

// DefaultRetryPolicy 默认重试策略
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:    3,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
	}
}

// NewErrorHandler 创建新的错误处理器
func NewErrorHandler(logger *zap.Logger) *ErrorHandler {
	return &ErrorHandler{
		logger:       logger,
		errorCounter: make(map[string]int),
		retryPolicy:  DefaultRetryPolicy(),
	}
}

// NewPixlyError 创建新的Pixly错误
func (eh *ErrorHandler) NewPixlyError(errorType ErrorType, severity ErrorSeverity, operation, message string, cause error) *PixlyError {
	errorID := eh.generateErrorID(errorType, operation)

	pe := &PixlyError{
		ID:        errorID,
		Type:      errorType,
		Severity:  severity,
		Message:   message,
		Operation: operation,
		Timestamp: time.Now(),
		Cause:     cause,
		Retryable: eh.isRetryable(errorType, cause),
		Context:   NewErrorContext[any](),
	}

	// 只在Critical级别错误时添加堆栈跟踪，减少日志污染
	if severity == SeverityCritical {
		pe.StackTrace = eh.getStackTrace()
	}

	eh.errorCounter[errorID]++
	return pe
}

// generateErrorID 生成错误ID
func (eh *ErrorHandler) generateErrorID(errorType ErrorType, operation string) string {
	// 简化操作名称作为ID的一部分
	opName := strings.ReplaceAll(strings.ToUpper(operation), " ", "_")
	var idBuilder strings.Builder
	idBuilder.WriteString(string(errorType))
	idBuilder.WriteString("_")
	idBuilder.WriteString(opName)
	return idBuilder.String()
}

// isRetryable 判断错误是否可重试
func (eh *ErrorHandler) isRetryable(errorType ErrorType, cause error) bool {
	if cause == nil {
		return false
	}

	// 根据错误类型和原因判断是否可重试
	switch errorType {
	case ErrorTypeFileOperation:
		// 文件操作错误通常可重试
		return !os.IsNotExist(cause) && !os.IsPermission(cause)
	case ErrorTypeToolExecution:
		// 工具执行错误可能可重试
		return true
	case ErrorTypeSystemResource:
		// 系统资源错误可能可重试
		return true
	default:
		return false
	}
}

// getStackTrace 获取堆栈跟踪
func (eh *ErrorHandler) getStackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// WrapError 统一的错误包装函数
func (eh *ErrorHandler) WrapError(operation string, err error, details ...interface{}) error {
	if err == nil {
		return nil
	}

	// 构建详细信息字符串
	var detailStr string
	if len(details) > 0 {
		var builder strings.Builder
		builder.WriteString(", details: ")
		// 使用字符串构建器代替fmt.Sprintf
		for i, detail := range details {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(fmt.Sprint(detail))
		}
		detailStr = builder.String()
	}

	return fmt.Errorf("%s failed: %w%s", operation, err, detailStr)
}

// WrapErrorWithType 使用类型化错误包装
func (eh *ErrorHandler) WrapErrorWithType(errorType ErrorType, severity ErrorSeverity, operation, message string, cause error) *PixlyError {
	pe := eh.NewPixlyError(errorType, severity, operation, message, cause)
	eh.logError(pe)
	return pe
}

// logError 记录错误日志
func (eh *ErrorHandler) logError(pe *PixlyError) {
	// 基础字段，减少详细信息
	fields := []zap.Field{
		zap.String("error_type", string(pe.Type)),
		zap.String("operation", pe.Operation),
	}

	// 只在Critical级别时添加详细信息
	if pe.Severity == SeverityCritical {
		fields = append(fields,
			zap.String("error_id", pe.ID),
			zap.String("severity", string(pe.Severity)),
			zap.Bool("retryable", pe.Retryable),
			zap.Time("timestamp", pe.Timestamp),
		)

		if pe.FilePath != "" {
			fields = append(fields, zap.String("file_path", pe.FilePath))
		}

		if pe.Cause != nil {
			fields = append(fields, zap.Error(pe.Cause))
		}

		if pe.StackTrace != "" {
			fields = append(fields, zap.String("stack_trace", pe.StackTrace))
		}
	} else {
		// 非Critical级别只记录基本错误信息
		if pe.Cause != nil {
			fields = append(fields, zap.String("cause", pe.Cause.Error()))
		}
	}

	// 根据严重程度选择日志级别，提高阈值减少输出
	switch pe.Severity {
	case SeverityLow:
		// Low级别错误不记录日志，避免污染
		return
	case SeverityMedium:
		// Medium级别只在Debug模式下记录
		if eh.logger.Core().Enabled(zap.DebugLevel) {
			// eh.logger.Debug(pe.Message, fields...)
		}
	case SeverityHigh:
		eh.logger.Warn(pe.Message, fields...)
	case SeverityCritical:
		eh.logger.Error(pe.Message, fields...)
	}
}

// RetryWithBackoff 使用退避策略重试操作
func (eh *ErrorHandler) RetryWithBackoff(operation func() error, errorType ErrorType, operationName string) error {
	var lastErr error
	delay := eh.retryPolicy.BaseDelay

	for attempt := 0; attempt <= eh.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			// 等待退避时间
			// 重试操作
			time.Sleep(delay)

			// 计算下次退避时间
			delay = time.Duration(float64(delay) * eh.retryPolicy.BackoffFactor)
			if delay > eh.retryPolicy.MaxDelay {
				delay = eh.retryPolicy.MaxDelay
			}
		}

		err := operation()
		if err == nil {
			if attempt > 0 {
				// 操作重试成功
			}
			return nil
		}

		lastErr = err

		// 检查是否可重试
		if !eh.isRetryable(errorType, err) {
			// 错误不可重试
			break
		}

		eh.logger.Warn("Operation failed, will retry",
			zap.String("operation", operationName),
			zap.Int("attempt", attempt+1),
			zap.Int("max_retries", eh.retryPolicy.MaxRetries),
			zap.Duration("next_delay", delay),
			zap.Error(err))
	}

	// 所有重试都失败了
	eh.logger.Error("操作重试全部失败",
		zap.String("operation", operationName),
		zap.Int("max_retries", eh.retryPolicy.MaxRetries),
		zap.Error(lastErr))
	var builder strings.Builder
	builder.WriteString("Operation failed after ")
	builder.WriteString(strconv.Itoa(eh.retryPolicy.MaxRetries + 1))
	builder.WriteString(" attempts")
	return eh.WrapErrorWithType(errorType, SeverityHigh, operationName, builder.String(), lastErr)
}

// GetErrorStats 获取错误统计信息
func (eh *ErrorHandler) GetErrorStats() map[string]int {
	stats := make(map[string]int)
	for k, v := range eh.errorCounter {
		stats[k] = v
	}
	return stats
}

// ResetErrorStats 重置错误统计
func (eh *ErrorHandler) ResetErrorStats() {
	eh.errorCounter = make(map[string]int)
}

// WrapErrorWithOutput 包装带输出信息的错误
func (eh *ErrorHandler) WrapErrorWithOutput(operation string, err error, output []byte) error {
	if err == nil {
		return nil
	}
	var errorBuilder strings.Builder
	errorBuilder.WriteString(operation)
	errorBuilder.WriteString(" failed: ")
	errorBuilder.WriteString(err.Error())
	errorBuilder.WriteString(", output: ")
	errorBuilder.WriteString(string(output))
	return fmt.Errorf("%s", errorBuilder.String())
}

// LogAndWrapError 记录日志并包装错误
func (eh *ErrorHandler) LogAndWrapError(operation string, err error, fields ...zap.Field) error {
	if err == nil {
		return nil
	}

	logFields := append([]zap.Field{zap.Error(err)}, fields...)
	eh.logger.Error(operation+" failed", logFields...)

	return fmt.Errorf("%s failed: %w", operation, err)
}

// FileOperationHandler 统一的文件操作处理器
type FileOperationHandler struct {
	errorHandler *ErrorHandler
	logger       *zap.Logger
}

// NewFileOperationHandler 创建新的文件操作处理器
func NewFileOperationHandler(logger *zap.Logger) *FileOperationHandler {
	return &FileOperationHandler{
		errorHandler: NewErrorHandler(logger),
		logger:       logger,
	}
}

// SafeCreateDir 安全创建目录
func (foh *FileOperationHandler) SafeCreateDir(dirPath string) error {
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return foh.errorHandler.WrapError("create directory", err, dirPath)
	}
	return nil
}

// SafeRemoveFile 安全删除文件
func (foh *FileOperationHandler) SafeRemoveFile(filePath string) error {
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return foh.errorHandler.WrapError("remove file", err, filePath)
	}
	return nil
}

// SafeRenameFile 安全重命名文件
func (foh *FileOperationHandler) SafeRenameFile(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		var builder strings.Builder
		builder.WriteString(oldPath)
		builder.WriteString(" -> ")
		builder.WriteString(newPath)
		return foh.errorHandler.WrapError("rename file", err, builder.String())
	}
	return nil
}

// AtomicFileReplace 原子性文件替换
func (foh *FileOperationHandler) AtomicFileReplace(tempPath, finalPath string, isInPlace bool) error {
	// 预检查：确保临时文件存在
	if _, err := os.Stat(tempPath); err != nil {
		return foh.errorHandler.WrapErrorWithType(ErrorTypeFileOperation, SeverityHigh, "AtomicFileReplace",
			"临时文件不存在或无法访问", err)
	}

	// 预检查：确保目标目录存在
	targetDir := filepath.Dir(finalPath)
	if err := foh.SafeCreateDir(targetDir); err != nil {
		return foh.errorHandler.WrapErrorWithType(ErrorTypeFileOperation, SeverityHigh, "AtomicFileReplace",
			"无法创建目标目录", err)
	}

	// 执行原子替换操作
	var replaceFunc func() error
	if isInPlace {
		// 原地替换：删除原文件，重命名临时文件
		replaceFunc = func() error {
			if err := foh.SafeRemoveFile(finalPath); err != nil {
				return err
			}
			if err := foh.SafeRenameFile(tempPath, finalPath); err != nil {
				return err
			}
			return nil
		}
	} else {
		// 非原地替换：直接重命名临时文件
		replaceFunc = func() error {
			if err := foh.SafeRenameFile(tempPath, finalPath); err != nil {
				return err
			}
			return nil
		}
	}

	// 使用重试机制执行替换操作
	if err := foh.errorHandler.RetryWithBackoff(replaceFunc, ErrorTypeFileOperation, "AtomicFileReplace"); err != nil {
		return foh.errorHandler.WrapErrorWithType(ErrorTypeFileOperation, SeverityHigh, "AtomicFileReplace",
			"原子文件替换失败", err)
	}

	// Atomic file replace completed

	return nil
}

// MoveToTrash 移动文件到垃圾箱
func (foh *FileOperationHandler) MoveToTrash(filePath string) error {
	// 规范化文件路径
	normalizedPath, err := GlobalPathUtils.NormalizePath(filePath)
	if err != nil {
		return foh.errorHandler.WrapError("normalize file path", err, filePath)
	}

	// 构建垃圾箱目录路径
	trashDir, err := GlobalPathUtils.JoinPath(GlobalPathUtils.GetDirName(normalizedPath), ".trash")
	if err != nil {
		return foh.errorHandler.WrapError("build trash directory path", err)
	}

	// 创建垃圾箱目录
	if err := foh.SafeCreateDir(trashDir); err != nil {
		return err
	}

	// 构建垃圾箱文件路径
	filename := GlobalPathUtils.GetBaseName(normalizedPath)
	trashPath, err := GlobalPathUtils.JoinPath(trashDir, filename)
	if err != nil {
		return foh.errorHandler.WrapError("build trash file path", err)
	}

	// 移动文件到垃圾箱
	if err := foh.SafeRenameFile(normalizedPath, trashPath); err != nil {
		return err
	}

	// File moved to trash

	return nil
}

// BatchFileOperation 批量文件操作结果
type BatchFileOperation struct {
	SuccessCount int
	FailureCount int
	Errors       []error
}

// BatchRemoveFiles 批量删除文件
func (foh *FileOperationHandler) BatchRemoveFiles(filePaths []string) *BatchFileOperation {
	result := &BatchFileOperation{
		Errors: make([]error, 0),
	}

	for _, filePath := range filePaths {
		if err := foh.SafeRemoveFile(filePath); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err)
		} else {
			result.SuccessCount++
		}
	}

	return result
}

// BatchMoveToTrash 批量移动文件到垃圾箱
func (foh *FileOperationHandler) BatchMoveToTrash(filePaths []string) *BatchFileOperation {
	result := &BatchFileOperation{
		Errors: make([]error, 0),
	}

	for _, filePath := range filePaths {
		if err := foh.MoveToTrash(filePath); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err)
		} else {
			result.SuccessCount++
		}
	}

	return result
}

// EnsureOutputDirectory 确保输出目录存在
func (foh *FileOperationHandler) EnsureOutputDirectory(outputPath string) error {
	dirPath := filepath.Dir(outputPath)
	return foh.SafeCreateDir(dirPath)
}
