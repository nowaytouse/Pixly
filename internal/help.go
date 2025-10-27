package internal

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"pixly/internal/cmd"
)

// helpCmd 定义help命令
var helpCmd = &cobra.Command{
	Use:   "help [command]",
	Short: "显示命令的帮助信息",
	Long: `显示指定命令的详细帮助信息。

如果不指定命令，将显示所有可用命令的列表。`,
	Example: `  # 显示所有命令
  pixly help

  # 显示convert命令的帮助
  pixly help convert

  # 显示settings命令的帮助
  pixly help settings`,
	RunE: runHelpCommand,
}

// helpTopicsCmd 定义help topics子命令
var helpTopicsCmd = &cobra.Command{
	Use:   "topics",
	Short: "显示帮助主题列表",
	Long:  `显示所有可用的帮助主题，包括概念、配置和故障排除指南。`,
	RunE:  runHelpTopicsCommand,
}

// helpFormatsCmd 定义help formats子命令
var helpFormatsCmd = &cobra.Command{
	Use:   "formats",
	Short: "显示支持的文件格式",
	Long:  `显示Pixly支持的所有输入和输出文件格式的详细信息。`,
	RunE:  runHelpFormatsCommand,
}

// helpModesCmd 定义help modes子命令
var helpModesCmd = &cobra.Command{
	Use:   "modes",
	Short: "显示转换模式说明",
	Long:  `显示所有可用转换模式的详细说明和使用场景。`,
	RunE:  runHelpModesCommand,
}

func init() {
	// 添加子命令
	helpCmd.AddCommand(helpTopicsCmd)
	helpCmd.AddCommand(helpFormatsCmd)
	helpCmd.AddCommand(helpModesCmd)

	// 添加到根命令
	cmd.AddCommand(helpCmd)
}

func runHelpCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		// 显示根命令帮助
		return cmd.Help()
	}

	// 查找指定的命令
	targetCmd, _, err := cmd.Root().Find(args)
	if err != nil {
		return fmt.Errorf("未找到命令 '%s': %v", strings.Join(args, " "), err)
	}

	// 显示目标命令的帮助
	return targetCmd.Help()
}

func runHelpTopicsCommand(cmd *cobra.Command, args []string) error {
	fmt.Println("📚 Pixly 帮助主题")
	fmt.Println("====================")
	fmt.Println()

	fmt.Println("🎯 核心概念:")
	fmt.Println("  配置管理     - 如何配置和自定义Pixly")
	fmt.Println("  转换模式     - 不同转换模式的选择指南")
	fmt.Println("  文件格式     - 支持的输入和输出格式")
	fmt.Println("  性能优化     - 提高转换速度的技巧")
	fmt.Println()

	fmt.Println("🔧 配置主题:")
	fmt.Println("  主题设置     - 自定义界面外观")
	fmt.Println("  语言设置     - 多语言支持")
	fmt.Println("  并发设置     - 调整处理性能")
	fmt.Println("  输出设置     - 配置输出目录和选项")
	fmt.Println()

	fmt.Println("🚨 故障排除:")
	fmt.Println("  常见错误     - 解决常见问题")
	fmt.Println("  依赖问题     - 外部工具安装指南")
	fmt.Println("  性能问题     - 优化转换性能")
	fmt.Println("  文件问题     - 处理损坏或不兼容文件")
	fmt.Println()

	fmt.Println("💡 使用 'pixly help <topic>' 查看具体主题的详细信息")
	return nil
}

func runHelpFormatsCommand(cmd *cobra.Command, args []string) error {
	fmt.Println("📁 支持的文件格式")
	fmt.Println("==================")
	fmt.Println()

	fmt.Println("🖼️  图像格式:")
	fmt.Println("  输入: JPEG, PNG, GIF, WebP, TIFF, BMP, HEIC, AVIF, JXL")
	fmt.Println("  输出: JXL (推荐), AVIF, WebP, PNG")
	fmt.Println()

	fmt.Println("🎬 视频格式:")
	fmt.Println("  输入: MP4, AVI, MOV, MKV, WebM, FLV, WMV")
	fmt.Println("  输出: MOV (重包装), MP4, WebM")
	fmt.Println()

	fmt.Println("🎵 音频格式:")
	fmt.Println("  输入: MP3, WAV, FLAC, AAC, OGG, M4A")
	fmt.Println("  输出: FLAC (无损), AAC, OGG")
	fmt.Println()

	fmt.Println("📄 文档格式:")
	fmt.Println("  输入: PDF, DOC, DOCX, PPT, PPTX")
	fmt.Println("  输出: PDF (优化), WebP (图像提取)")
	fmt.Println()

	fmt.Println("⭐ 推荐格式组合:")
	fmt.Println("  照片归档: JPEG → JXL (无损压缩)")
	fmt.Println("  网页图片: PNG → AVIF (高压缩比)")
	fmt.Println("  动图优化: GIF → AVIF (大幅减小体积)")
	fmt.Println("  视频存储: MP4 → MOV (无损重包装)")

	return nil
}

func runHelpModesCommand(cmd *cobra.Command, args []string) error {
	fmt.Println("⚙️  转换模式详解")
	fmt.Println("==================")
	fmt.Println()

	fmt.Println("🤖 auto+ 模式 (智能自动):")
	fmt.Println("  • 适用场景: 日常使用，平衡质量和体积")
	fmt.Println("  • 处理策略: 智能分析文件质量，自动选择最佳转换方案")
	fmt.Println("  • 高品质文件 → 无损压缩 (质量模式)")
	fmt.Println("  • 中等品质文件 → 平衡优化算法")
	fmt.Println("  • 推荐用户: 普通用户，追求便利性")
	fmt.Println()

	fmt.Println("🔥 quality 模式 (品质优先):")
	fmt.Println("  • 适用场景: 专业归档，最大保真度")
	fmt.Println("  • 处理策略: 优先使用无损压缩，保持原始质量")
	fmt.Println("  • 静态图像 → JXL 无损压缩")
	fmt.Println("  • 动态图像 → AVIF 无损压缩")
	fmt.Println("  • 视频文件 → MOV 无损重包装")
	fmt.Println("  • 推荐用户: 摄影师，设计师，内容创作者")
	fmt.Println()

	fmt.Println("🚀 emoji 模式 (表情包优化):")
	fmt.Println("  • 适用场景: 社交媒体，即时通讯")
	fmt.Println("  • 处理策略: 针对小尺寸图像优化，追求最小体积")
	fmt.Println("  • 静态表情 → AVIF 高压缩")
	fmt.Println("  • 动态表情 → AVIF 动画优化")
	fmt.Println("  • 推荐用户: 社交媒体用户，表情包制作者")
	fmt.Println()

	fmt.Println("💡 选择建议:")
	fmt.Println("  • 不确定选择 → auto+ 模式")
	fmt.Println("  • 专业工作 → quality 模式")
	fmt.Println("  • 社交分享 → emoji 模式")
	fmt.Println("  • 批量处理 → auto+ 模式 + 高并发")

	return nil
}