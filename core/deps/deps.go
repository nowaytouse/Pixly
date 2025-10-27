package deps

import (
	"fmt"
	"os/exec"
	"strings"
)

// ToolInfo 工具信息
type ToolInfo struct {
	Name         string
	Path         string
	Version      string
	Required     bool
	Installed    bool
	ErrorMessage string
	Features     []string // 新增：工具特性列表
}

// DependencyManager 依赖管理器
type DependencyManager struct {
	tools map[string]*ToolInfo
}

// NewDependencyManager 创建依赖管理器
func NewDependencyManager() *DependencyManager {
	dm := &DependencyManager{
		tools: make(map[string]*ToolInfo),
	}

	// 初始化所需的工具
	dm.tools["ffmpeg"] = &ToolInfo{
		Name:     "FFmpeg",
		Path:     "ffmpeg",
		Required: true,
	}
	dm.tools["ffprobe"] = &ToolInfo{
		Name:     "FFprobe",
		Path:     "ffprobe",
		Required: true,
	}
	dm.tools["cjxl"] = &ToolInfo{
		Name:     "JPEG XL Encoder",
		Path:     "cjxl",
		Required: true,
	}
	dm.tools["avifenc"] = &ToolInfo{
		Name:     "AVIF Encoder",
		Path:     "avifenc",
		Required: true,
	}
	dm.tools["exiftool"] = &ToolInfo{
		Name:     "ExifTool",
		Path:     "exiftool",
		Required: true,
	}

	return dm
}

// CheckDependencies 检查所有依赖
func (dm *DependencyManager) CheckDependencies() error {
	for _, tool := range dm.tools {
		if err := dm.checkTool(tool); err != nil {
			tool.ErrorMessage = err.Error()
			tool.Installed = false
		} else {
			tool.Installed = true
		}
	}
	return nil
}

// checkTool 检查单个工具
func (dm *DependencyManager) checkTool(tool *ToolInfo) error {
	// 检查工具是否存在
	_, err := exec.LookPath(tool.Path)
	if err != nil {
		return fmt.Errorf("工具未找到: %s", tool.Path)
	}

	// 获取版本信息
	version, err := dm.getToolVersion(tool.Path)
	if err != nil {
		return fmt.Errorf("无法获取版本信息: %v", err)
	}

	tool.Version = version

	// 检查特定功能
	switch tool.Path {
	case "ffmpeg":
		return dm.checkFFmpegFeatures(tool)
	case "avifenc":
		return dm.checkAvifencFeatures(tool)
	}

	return nil
}

// checkFFmpegFeatures 检查FFmpeg特性
func (dm *DependencyManager) checkFFmpegFeatures(tool *ToolInfo) error {
	// 检查AVIF支持
	cmd := exec.Command("ffmpeg", "-h", "muxer=avif")
	output, err := cmd.CombinedOutput()
	if err == nil && strings.Contains(string(output), "avif") {
		tool.Features = append(tool.Features, "AVIF支持")
	}

	// 检查libaom编码器
	cmd = exec.Command("ffmpeg", "-encoders")
	output, err = cmd.CombinedOutput()
	if err == nil && strings.Contains(string(output), "libaom-av1") {
		tool.Features = append(tool.Features, "AV1编码器(libaom)")
	}

	return nil
}

// checkAvifencFeatures 检查avifenc特性
func (dm *DependencyManager) checkAvifencFeatures(tool *ToolInfo) error {
	// 检查版本和基本功能
	cmd := exec.Command("avifenc", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	version := strings.Split(string(output), "\n")[0]
	tool.Version = version

	// 检查是否支持-q参数（修复后的参数）
	cmd = exec.Command("avifenc", "-h")
	output, err = cmd.CombinedOutput()
	if err == nil && strings.Contains(string(output), "-q,--qcolor") {
		tool.Features = append(tool.Features, "质量参数(-q)")
	}

	return nil
}

// getToolVersion 获取工具版本
func (dm *DependencyManager) getToolVersion(toolPath string) (string, error) {
	var cmd *exec.Cmd

	// 不同工具使用不同的版本参数
	switch toolPath {
	case "ffmpeg", "ffprobe":
		cmd = exec.Command(toolPath, "-version")
	case "cjxl", "avifenc", "exiftool":
		cmd = exec.Command(toolPath, "--version")
	default:
		cmd = exec.Command(toolPath, "--version")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	// 解析版本信息
	version := strings.Split(string(output), "\n")[0]
	return version, nil
}

// GetTool 获取工具信息
func (dm *DependencyManager) GetTool(name string) *ToolInfo {
	return dm.tools[name]
}

// GetAllTools 获取所有工具信息
func (dm *DependencyManager) GetAllTools() map[string]*ToolInfo {
	return dm.tools
}

// IsAllRequiredInstalled 检查所有必需工具是否已安装
func (dm *DependencyManager) IsAllRequiredInstalled() bool {
	for _, tool := range dm.tools {
		if tool.Required && !tool.Installed {
			return false
		}
	}
	return true
}

// GetMissingRequiredTools 获取缺失的必需工具
func (dm *DependencyManager) GetMissingRequiredTools() []*ToolInfo {
	var missing []*ToolInfo
	for _, tool := range dm.tools {
		if tool.Required && !tool.Installed {
			missing = append(missing, tool)
		}
	}
	return missing
}
