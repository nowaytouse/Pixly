package converter

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// PathUtils 路径处理工具
type PathUtils struct{}

// NormalizePath 彻底的路径规范化
func (pu *PathUtils) NormalizePath(input string) (string, error) {
	// 1. 跳过URI解码 - 这是破坏中文文件名的罪魁祸首！
	// 只有当路径明确包含%编码时才进行解码
	decodedPath := input
	if strings.Contains(input, "%") {
		// 只对真正的URL编码路径进行解码
		if decoded, err := url.QueryUnescape(input); err == nil {
			// 验证解码后的路径是否合理
			if utf8.ValidString(decoded) {
				decodedPath = decoded
			}
		}
	}

	// 2. 处理 ~ 符号
	if strings.HasPrefix(decodedPath, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		decodedPath = filepath.Join(homeDir, decodedPath[1:])
	}

	// 3. 转换为绝对路径
	absPath, err := filepath.Abs(decodedPath)
	if err != nil {
		return "", err
	}

	// 4. 处理反斜杠（Windows路径）
	absPath = strings.ReplaceAll(absPath, "\\", string(filepath.Separator))

	// 5. 验证 UTF-8 编码
	if !utf8.ValidString(absPath) {
		// 尝试修复编码问题
		fixedPath := pu.detectAndFixEncoding(absPath)
		absPath = fixedPath
	}

	return absPath, nil
}

// detectAndFixEncoding 智能检测并修复路径编码问题
func (pu *PathUtils) detectAndFixEncoding(path string) string {
	// 如果已经是有效的UTF-8，直接返回
	if utf8.ValidString(path) {
		return path
	}

	// 尝试多种编码检测和转换
	encodings := []struct {
		name    string
		decoder transform.Transformer
	}{
		{"GBK", simplifiedchinese.GBK.NewDecoder()},
		{"GB18030", simplifiedchinese.GB18030.NewDecoder()},
	}

	for _, enc := range encodings {
		// 将无效UTF-8字符串的字节重新解释为指定编码
		reader := transform.NewReader(strings.NewReader(path), enc.decoder)
		decoded, err := io.ReadAll(reader)
		if err != nil {
			continue // 尝试下一种编码
		}

		decodedStr := string(decoded)
		// 检查解码结果是否合理（包含可打印字符且为有效UTF-8）
		if utf8.ValidString(decodedStr) && len(strings.TrimSpace(decodedStr)) > 0 {
			return decodedStr
		}
	}

	// 如果所有编码都失败，返回原始路径
	return path
}

// ValidatePath 验证路径是否有效
func (pu *PathUtils) ValidatePath(path string) bool {
	// 检查路径是否为空
	if path == "" {
		return false
	}

	// 检查路径是否只包含点（当前目录）
	if path == "." {
		return false
	}

	// 检查路径是否包含真正的非法字符（控制字符，除了文件系统允许的字符）
	// 只禁止真正的危险字符，如空字符和换行符
	illegalChars := []string{"\x00", "\n", "\r"}
	for _, char := range illegalChars {
		if strings.Contains(path, char) {
			return false
		}
	}

	return true
}

// IsPathSafe 检查路径是否安全（防止路径遍历攻击）
func (pu *PathUtils) IsPathSafe(basePath, targetPath string) bool {
	// 解析并清理路径
	cleanTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}

	cleanBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}

	// 检查目标路径是否在基础路径内
	relPath, err := filepath.Rel(cleanBasePath, cleanTargetPath)
	if err != nil {
		return false
	}

	// 检查相对路径是否包含 ".."
	return !strings.HasPrefix(relPath, "..")
}

// GetPathInfo 获取路径信息
func (pu *PathUtils) GetPathInfo(path string) (os.FileInfo, error) {
	// 规范化路径
	normalizedPath, err := pu.NormalizePath(path)
	if err != nil {
		return nil, err
	}

	// 获取文件信息
	info, err := os.Stat(normalizedPath)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// JoinPath 连接路径并规范化
func (pu *PathUtils) JoinPath(elem ...string) (string, error) {
	if len(elem) == 0 {
		return "", nil
	}

	// 使用filepath.Join连接路径
	joinedPath := filepath.Join(elem...)

	// 规范化连接后的路径
	return pu.NormalizePath(joinedPath)
}

// GetBaseName 获取路径的基础名称
func (pu *PathUtils) GetBaseName(path string) string {
	return filepath.Base(path)
}

// GetDirName 获取路径的目录名称
func (pu *PathUtils) GetDirName(path string) string {
	return filepath.Dir(path)
}

// GetExtension 获取文件扩展名
func (pu *PathUtils) GetExtension(path string) string {
	return filepath.Ext(path)
}

// IsAbsPath 检查路径是否为绝对路径
func (pu *PathUtils) IsAbsPath(path string) bool {
	return filepath.IsAbs(path)
}

// CleanPath 清理路径
func (pu *PathUtils) CleanPath(path string) string {
	return filepath.Clean(path)
}

// RelPath 获取相对路径
func (pu *PathUtils) RelPath(basepath, targpath string) (string, error) {
	// 先规范化两个路径
	normalizedBase, err := pu.NormalizePath(basepath)
	if err != nil {
		return "", err
	}
	normalizedTarget, err := pu.NormalizePath(targpath)
	if err != nil {
		return "", err
	}
	return filepath.Rel(normalizedBase, normalizedTarget)
}

// SplitPath 分割路径
func (pu *PathUtils) SplitPath(path string) (dir, file string) {
	return filepath.Split(path)
}

// WalkPath 遍历目录
func (pu *PathUtils) WalkPath(root string, walkFn filepath.WalkFunc) error {
	// 先规范化根路径
	normalizedRoot, err := pu.NormalizePath(root)
	if err != nil {
		return err
	}
	return filepath.Walk(normalizedRoot, walkFn)
}

// WalkDirPath 遍历目录（使用WalkDir）
func (pu *PathUtils) WalkDirPath(root string, walkDirFn func(path string, d os.DirEntry, err error) error) error {
	// 先规范化根路径
	normalizedRoot, err := pu.NormalizePath(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(normalizedRoot, walkDirFn)
}

// GlobalPathUtils 全局路径处理工具实例
var GlobalPathUtils = &PathUtils{}
