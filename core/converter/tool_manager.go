package converter

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"pixly/config"

	"go.uber.org/zap"
)

// ToolManager 工具管理器
type ToolManager struct {
	config       *config.Config
	logger       *zap.Logger
	toolCache    map[string]bool
	cacheMutex   sync.RWMutex
	errorHandler *ErrorHandler
}

// NewToolManager 创建新的工具管理器
func NewToolManager(config *config.Config, logger *zap.Logger, errorHandler *ErrorHandler) *ToolManager {
	return &ToolManager{
		config:       config,
		logger:       logger,
		toolCache:    make(map[string]bool),
		errorHandler: errorHandler,
	}
}

// IsToolAvailable 检查工具是否可用 - 改进版本
func (tm *ToolManager) IsToolAvailable(toolPath string) bool {
	tm.cacheMutex.RLock()
	if available, exists := tm.toolCache[toolPath]; exists {
		tm.cacheMutex.RUnlock()
		return available
	}
	tm.cacheMutex.RUnlock()

	// 使用更可靠的工具检查机制
	available := tm.checkTool(toolPath)

	// 缓存结果
	tm.cacheMutex.Lock()
	tm.toolCache[toolPath] = available
	tm.cacheMutex.Unlock()

	return available
}

// checkTool 实际检查工具是否可用
func (tm *ToolManager) checkTool(toolPath string) bool {
	// 使用更短的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 使用一个更轻量级的命令检查工具可用性
	cmd := exec.CommandContext(ctx, toolPath, "-version")
	err := cmd.Run()

	// 如果-version不可用，尝试使用--version
	if err != nil {
		cmd = exec.CommandContext(ctx, toolPath, "--version")
		err = cmd.Run()
	}

	// 如果--version也不可用，尝试使用不带参数的方式
	if err != nil {
		cmd = exec.CommandContext(ctx, toolPath)
		err = cmd.Run()
	}

	// 记录工具检查结果
	if err != nil {
		tm.logger.Debug("工具不可用", zap.String("tool", toolPath), zap.Error(err))
	} else {
		tm.logger.Debug("工具可用", zap.String("tool", toolPath))
	}

	return err == nil
}

// FindToolInPath 在系统PATH中查找工具
func (tm *ToolManager) FindToolInPath(toolName string) (string, error) {
	path, err := exec.LookPath(toolName)
	if err != nil {
		return "", tm.errorHandler.WrapError("tool not found in PATH", err)
	}
	return path, nil
}

// GetAvailableTool 获取可用的工具路径，支持回退机制和自动查找
func (tm *ToolManager) GetAvailableTool(primaryPath, fallbackPath string) (string, error) {
	// 首先检查主工具
	if tm.IsToolAvailable(primaryPath) {
		return primaryPath, nil
	}

	// 如果主工具不可用，检查回退工具
	if fallbackPath != "" && tm.IsToolAvailable(fallbackPath) {
		tm.logger.Warn("主工具不可用，使用回退工具",
			zap.String("primary", primaryPath),
			zap.String("fallback", fallbackPath))
		return fallbackPath, nil
	}

	// 尝试在系统PATH中查找主工具
	toolName := primaryPath
	// 如果是绝对路径，提取工具名称
	if strings.Contains(primaryPath, "/") {
		toolName = primaryPath[strings.LastIndex(primaryPath, "/")+1:]
	}

	if path, err := tm.FindToolInPath(toolName); err == nil {
		if tm.IsToolAvailable(path) {
			tm.logger.Info("在系统PATH中找到工具",
				zap.String("tool", toolName),
				zap.String("path", path))
			return path, nil
		}
	}

	// 如果都没有可用工具，返回错误
	var errorBuilder strings.Builder
	errorBuilder.WriteString("no available tool found: primary=")
	errorBuilder.WriteString(primaryPath)
	errorBuilder.WriteString(", fallback=")
	errorBuilder.WriteString(fallbackPath)
	return "", tm.errorHandler.WrapError(errorBuilder.String(), nil)
}

// validateAndNormalizePath 验证并规范化文件路径
func (tm *ToolManager) validateAndNormalizePath(path string) (string, error) {
	// 使用GlobalPathUtils进行路径规范化
	normalizedPath, err := GlobalPathUtils.NormalizePath(path)
	if err != nil {
		return "", tm.errorHandler.WrapError("path normalization failed", err)
	}

	// 验证路径是否有效
	if !GlobalPathUtils.ValidatePath(normalizedPath) {
		var pathErrorBuilder strings.Builder
		pathErrorBuilder.WriteString("invalid path: ")
		pathErrorBuilder.WriteString(path)
		return "", tm.errorHandler.WrapError(pathErrorBuilder.String(), nil)
	}

	return normalizedPath, nil
}

// ExecuteWithPathValidation 执行工具命令，自动验证和规范化路径参数
func (tm *ToolManager) ExecuteWithPathValidation(toolPath string, args ...string) ([]byte, error) {
	// 对所有参数进行路径验证和规范化
	validatedArgs := make([]string, len(args))
	for i, arg := range args {
		// 检查参数是否看起来像文件路径（包含路径分隔符）
		if strings.Contains(arg, "/") || strings.Contains(arg, "\\") {
			// 尝试规范化路径
			if normalizedPath, err := tm.validateAndNormalizePath(arg); err == nil {
				validatedArgs[i] = normalizedPath
				// 路径已规范化
			} else {
				// 如果规范化失败，使用原始路径但记录警告
				// 路径规范化失败，使用原始路径
				validatedArgs[i] = arg
			}
		} else {
			validatedArgs[i] = arg
		}
	}

	return tm.Execute(toolPath, validatedArgs...)
}

// Execute 执行单个工具命令，带重试机制和超时控制
func (tm *ToolManager) Execute(toolPath string, args ...string) ([]byte, error) {
	// 最多重试3次
	var output []byte
	var err error

	for i := 0; i < 3; i++ {
		// 创建带超时的上下文（30秒超时）
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, toolPath, args...)

		output, err = cmd.CombinedOutput()
		cancel() // 立即释放资源

		if err == nil {
			// 工具执行成功
			if i > 0 {
				// 工具执行成功（经过重试）
			}
			return output, nil
		}

		// 检查是否为context canceled错误，如果是则立即返回不重试
		if errors.Is(err, context.Canceled) {
			tm.logger.Warn("工具执行被取消",
				zap.String("tool", toolPath),
				zap.Error(err))
			return output, err
		}

		// 检查是否为context deadline exceeded错误，如果是则立即返回不重试
		if errors.Is(err, context.DeadlineExceeded) {
			tm.logger.Warn("工具执行超时",
				zap.String("tool", toolPath),
				zap.Error(err))
			return output, err
		}

		// 只在最后一次重试失败时记录详细日志
		if i == 2 {
			tm.logger.Error("工具执行失败，已达到最大重试次数",
				zap.String("tool", toolPath),
				zap.Error(err),
				zap.String("output", string(output)))
		} else {
			// 中间重试只记录简单信息，且只在Debug级别
			// 工具执行重试
			// 等待一段时间再重试
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	return output, err
}
