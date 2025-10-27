package converter

import (
	"fmt"
	"os"
	"strings"

	"pixly/config"

	"go.uber.org/zap"
)

// PathSecurityChecker 路径安全检查器
type PathSecurityChecker struct {
	config       *config.Config
	logger       *zap.Logger
	errorHandler *ErrorHandler
}

// NewPathSecurityChecker 创建路径安全检查器
func NewPathSecurityChecker(config *config.Config, logger *zap.Logger, errorHandler *ErrorHandler) *PathSecurityChecker {
	return &PathSecurityChecker{
		config:       config,
		logger:       logger,
		errorHandler: errorHandler,
	}
}

// SecurityCheckOptions 安全检查选项
type SecurityCheckOptions struct {
	CheckRead        bool // 检查读权限
	CheckWrite       bool // 检查写权限
	RequireDirectory bool // 要求是目录
	CheckWhitelist   bool // 检查白名单
	CheckBlacklist   bool // 检查黑名单
}

// DefaultSecurityOptions 默认安全检查选项
func DefaultSecurityOptions() SecurityCheckOptions {
	return SecurityCheckOptions{
		CheckRead:        true,
		CheckWrite:       false,
		RequireDirectory: true,
		CheckWhitelist:   true,
		CheckBlacklist:   true,
	}
}

// ValidatePath 统一的路径验证函数
func (psc *PathSecurityChecker) ValidatePath(inputPath string, options SecurityCheckOptions) error {
	// 开始路径安全检查

	// 1. 规范化路径
	normalizedPath, err := GlobalPathUtils.NormalizePath(inputPath)
	if err != nil {
		return psc.errorHandler.WrapError("路径规范化失败", err)
	}

	// 2. 检查路径存在性和基本属性
	info, err := os.Stat(normalizedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("路径不存在: %s", normalizedPath)
		}
		return psc.errorHandler.WrapError("无法访问路径", err)
	}

	// 3. 检查是否为目录（如果需要）- 这个检查应该在白名单检查之前
	if options.RequireDirectory && !info.IsDir() {
		return fmt.Errorf("指定路径不是目录: %s", normalizedPath)
	}

	// 4. 检查白名单
	if options.CheckWhitelist {
		if err := psc.checkWhitelist(normalizedPath); err != nil {
			return err
		}
	}

	// 5. 检查黑名单
	if options.CheckBlacklist {
		if err := psc.checkBlacklist(normalizedPath); err != nil {
			return err
		}
	}

	// 6. 检查读权限
	if options.CheckRead {
		if err := psc.checkReadPermission(normalizedPath, info.IsDir()); err != nil {
			return psc.errorHandler.WrapError("路径无读权限", err)
		}
	}

	// 7. 检查写权限
	if options.CheckWrite {
		if err := psc.checkWritePermission(normalizedPath, info.IsDir()); err != nil {
			return psc.errorHandler.WrapError("路径无写权限", err)
		}
	}

	// 路径安全检查通过
	return nil
}

// checkWhitelist 检查白名单
func (psc *PathSecurityChecker) checkWhitelist(normalizedPath string) error {
	if len(psc.config.Security.AllowedDirectories) == 0 {
		return nil // 没有白名单限制
	}

	for _, allowedDir := range psc.config.Security.AllowedDirectories {
		normalizedAllowedDir, err := GlobalPathUtils.NormalizePath(allowedDir)
		if err != nil {
			psc.logger.Warn("白名单路径规范化失败", zap.String("path", allowedDir), zap.Error(err))
			continue
		}

		if psc.isPathInDirectory(normalizedPath, normalizedAllowedDir) {
			return nil // 在白名单中
		}
	}

	return fmt.Errorf("路径不在允许的目录白名单中: %s", normalizedPath)
}

// checkBlacklist 检查黑名单
func (psc *PathSecurityChecker) checkBlacklist(normalizedPath string) error {
	for _, forbiddenDir := range psc.config.Security.ForbiddenDirectories {
		normalizedForbiddenDir, err := GlobalPathUtils.NormalizePath(forbiddenDir)
		if err != nil {
			psc.logger.Warn("黑名单路径规范化失败", zap.String("path", forbiddenDir), zap.Error(err))
			continue
		}

		if psc.isPathInDirectory(normalizedPath, normalizedForbiddenDir) {
			return fmt.Errorf("路径在禁止访问的目录中: %s", normalizedPath)
		}
	}

	return nil
}

// isPathInDirectory 检查路径是否在指定目录内
func (psc *PathSecurityChecker) isPathInDirectory(path, directory string) bool {
	normalizedPath, err := GlobalPathUtils.NormalizePath(path)
	if err != nil {
		return false
	}

	normalizedDir, err := GlobalPathUtils.NormalizePath(directory)
	if err != nil {
		return false
	}

	// 检查路径是否相等或者是子路径
	if normalizedPath == normalizedDir {
		return true
	}

	// 使用 strings.HasPrefix 检查是否为子路径
	// 确保目录路径以 / 结尾，避免误匹配
	dirWithSlash := normalizedDir
	if !strings.HasSuffix(dirWithSlash, "/") {
		dirWithSlash += "/"
	}

	return strings.HasPrefix(normalizedPath, dirWithSlash)
}

// checkReadPermission 检查读权限
func (psc *PathSecurityChecker) checkReadPermission(path string, isDir bool) error {
	if isDir {
		// 目录读权限检查 - 直接使用 os.ReadDir，无需手动打开文件
		_, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		return nil
	} else {
		// 文件读权限检查
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := file.Close(); err != nil {
				psc.logger.Warn("Failed to close file during read permission check",
					zap.String("path", path),
					zap.Error(err))
			}
		}()
	}
	return nil
}

// checkWritePermission 检查写权限
func (psc *PathSecurityChecker) checkWritePermission(path string, isDir bool) error {
	normalizedPath, err := GlobalPathUtils.NormalizePath(path)
	if err != nil {
		return err
	}

	var testPath string
	if isDir {
		// 在目录中创建临时文件测试写权限
		testPath, err = GlobalPathUtils.JoinPath(normalizedPath, ".pixly_write_test")
		if err != nil {
			return err
		}
	} else {
		// 对于文件，测试其父目录的写权限
		parentDir := GlobalPathUtils.GetDirName(normalizedPath)
		testPath, err = GlobalPathUtils.JoinPath(parentDir, ".pixly_write_test")
		if err != nil {
			return err
		}
	}

	file, err := os.Create(testPath)
	if err != nil {
		return err
	}

	// 关闭并删除临时文件
	if err := file.Close(); err != nil {
		psc.logger.Warn("Failed to close temporary file during write permission check",
			zap.String("testPath", testPath),
			zap.Error(err))
	}
	if err := os.Remove(testPath); err != nil {
		psc.logger.Warn("Failed to remove temporary file during write permission check",
			zap.String("testPath", testPath),
			zap.Error(err))
	}
	return nil
}
