package deps

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Installer 安装器
type Installer struct {
	dm *DependencyManager
}

// NewInstaller 创建安装器
func NewInstaller(dm *DependencyManager) *Installer {
	return &Installer{
		dm: dm,
	}
}

// InstallTool 安装指定工具
func (i *Installer) InstallTool(name string) error {
	tool := i.dm.GetTool(name)
	if tool == nil {
		return fmt.Errorf("未知工具: %s", name)
	}

	switch name {
	case "ffmpeg", "ffprobe":
		return i.installFFmpeg()
	case "cjxl":
		return i.installCjxl()
	case "avifenc":
		return i.installAvifenc()
	case "exiftool":
		return i.installExiftool()
	default:
		return fmt.Errorf("不支持的工具: %s", name)
	}
}

// InstallAllRequired 安装所有必需工具
func (i *Installer) InstallAllRequired() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("当前仅支持macOS系统")
	}

	missing := i.dm.GetMissingRequiredTools()
	if len(missing) == 0 {
		return nil // 所有工具已安装
	}

	fmt.Println("正在安装缺失的依赖工具...")

	for _, tool := range missing {
		fmt.Printf("正在安装 %s...\n", tool.Name)
		if err := i.InstallTool(tool.Path); err != nil {
			return fmt.Errorf("安装 %s 失败: %v", tool.Name, err)
		}
		fmt.Printf("✅ %s 安装成功\n", tool.Name)
	}

	return nil
}

// InteractiveInstall 交互式安装
func (i *Installer) InteractiveInstall() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("当前仅支持macOS系统")
	}

	// 检查所有依赖
	if err := i.dm.CheckDependencies(); err != nil {
		return err
	}

	// 显示当前状态
	i.showDependencyStatus()

	// 获取所有工具
	allTools := i.dm.GetAllTools()

	// 询问用户选择安装方式
	fmt.Println("\n请选择安装方式:")
	fmt.Println("1. 安装所有缺失的必需工具")
	fmt.Println("2. 选择性安装工具")
	fmt.Println("3. 退出")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("请输入选项 (1-3): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取输入失败: %v", err)
	}
	choice := strings.TrimSpace(input)

	switch choice {
	case "1":
		return i.installMissingRequired()
	case "2":
		return i.selectiveInstall(allTools, reader)
	case "3":
		fmt.Println("退出安装程序")
		return nil
	default:
		fmt.Println("无效选项，退出安装程序")
		return nil
	}
}

// showDependencyStatus 显示依赖状态
func (i *Installer) showDependencyStatus() {
	fmt.Println("\n📦 依赖组件状态:")
	fmt.Println("==================")

	allTools := i.dm.GetAllTools()
	for name, tool := range allTools {
		status := "❌ 未安装"
		if tool.Installed {
			status = "✅ 已安装"
		}

		required := ""
		if tool.Required {
			required = " (必需)"
		}

		fmt.Printf("%-10s %-20s %s%s\n", name+":", tool.Name, status, required)
	}
}

// installMissingRequired 安装缺失的必需工具
func (i *Installer) installMissingRequired() error {
	missing := i.dm.GetMissingRequiredTools()
	if len(missing) == 0 {
		fmt.Println("🎉 所有必需依赖组件均已安装!")
		return nil
	}

	fmt.Printf("发现 %d 个缺失的必需工具:\n", len(missing))
	for _, tool := range missing {
		fmt.Printf("  - %s (%s)\n", tool.Name, tool.Path)
	}

	fmt.Print("\n是否继续安装? (y/N): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取输入失败: %v", err)
	}
	confirm := strings.TrimSpace(strings.ToLower(input))

	if confirm != "y" && confirm != "yes" {
		fmt.Println("取消安装")
		return nil
	}

	return i.InstallAllRequired()
}

// selectiveInstall 选择性安装
func (i *Installer) selectiveInstall(tools map[string]*ToolInfo, reader *bufio.Reader) error {
	// 创建工具列表
	var toolList []string
	fmt.Println("\n可用工具:")
	for name, tool := range tools {
		status := "未安装"
		if tool.Installed {
			status = "已安装"
		}
		fmt.Printf("%d. %s (%s) - %s\n", len(toolList)+1, tool.Name, name, status)
		toolList = append(toolList, name)
	}

	fmt.Print("\n请输入要安装的工具编号 (多个编号用逗号分隔，如: 1,3,5): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取输入失败: %v", err)
	}
	selection := strings.TrimSpace(input)

	if selection == "" {
		fmt.Println("未选择任何工具")
		return nil
	}

	// 解析选择
	selectedIndices := strings.Split(selection, ",")
	var selectedTools []string

	for _, indexStr := range selectedIndices {
		index, err := strconv.Atoi(strings.TrimSpace(indexStr))
		if err != nil || index < 1 || index > len(toolList) {
			fmt.Printf("无效编号: %s\n", indexStr)
			continue
		}
		selectedTools = append(selectedTools, toolList[index-1])
	}

	if len(selectedTools) == 0 {
		fmt.Println("未选择有效工具")
		return nil
	}

	// 确认安装
	fmt.Println("\n将安装以下工具:")
	for _, name := range selectedTools {
		tool := tools[name]
		fmt.Printf("  - %s (%s)\n", tool.Name, name)
	}

	fmt.Print("\n确认安装? (y/N): ")
	input, err = reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取输入失败: %v", err)
	}
	confirm := strings.TrimSpace(strings.ToLower(input))

	if confirm != "y" && confirm != "yes" {
		fmt.Println("取消安装")
		return nil
	}

	// 执行安装
	fmt.Println("\n开始安装...")
	for _, name := range selectedTools {
		tool := tools[name]
		fmt.Printf("正在安装 %s...\n", tool.Name)
		if err := i.InstallTool(name); err != nil {
			fmt.Printf("❌ 安装 %s 失败: %v\n", tool.Name, err)
		} else {
			fmt.Printf("✅ %s 安装成功\n", tool.Name)
		}
	}

	fmt.Println("\n安装完成!")
	return nil
}

// installFFmpeg 安装FFmpeg (使用Homebrew)
func (i *Installer) installFFmpeg() error {
	return i.installViaBrew("ffmpeg")
}

// installCjxl 安装cjxl (使用Homebrew)
func (i *Installer) installCjxl() error {
	// 先安装libjxl
	if err := i.installViaBrew("libjxl"); err != nil {
		return err
	}
	return nil
}

// installAvifenc 安装avifenc (使用Homebrew)
func (i *Installer) installAvifenc() error {
	// 先安装libavif
	if err := i.installViaBrew("libavif"); err != nil {
		return err
	}
	return nil
}

// installExiftool 安装exiftool (使用Homebrew)
func (i *Installer) installExiftool() error {
	return i.installViaBrew("exiftool")
}

// installViaBrew 通过Homebrew安装
func (i *Installer) installViaBrew(formula string) error {
	// 检查Homebrew是否已安装
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("请先安装Homebrew: https://brew.sh")
	}

	// 执行安装命令
	cmd := exec.Command("brew", "install", formula)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// CheckAndInstall 检查并安装缺失的依赖
func (i *Installer) CheckAndInstall() error {
	// 检查所有依赖
	if err := i.dm.CheckDependencies(); err != nil {
		return err
	}

	// 检查是否所有必需工具都已安装
	if i.dm.IsAllRequiredInstalled() {
		fmt.Println("✅ 所有必需依赖已安装")
		return nil
	}

	// 显示缺失的工具
	missing := i.dm.GetMissingRequiredTools()
	fmt.Println("❌ 缺失以下必需工具:")
	for _, tool := range missing {
		fmt.Printf("  - %s (%s)\n", tool.Name, tool.Path)
	}

	// 询问用户是否安装
	fmt.Print("\n是否自动安装缺失的工具? (y/N): ")
	var input string
	fmt.Scanln(&input)

	if input == "y" || input == "Y" {
		return i.InstallAllRequired()
	}

	return fmt.Errorf("用户取消安装")
}
