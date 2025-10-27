package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/fatih/color"
)

func main() {
	// 添加这一行确保颜色支持
	color.NoColor = false

	// 添加详细的启动日志
	fmt.Println("🚀 媒体转换工具 v20.2.8-GO-TITANIUM-STREAMING-ENHANCED 启动中...")
	fmt.Printf("💻 系统信息: %s, 架构: %s\n", runtime.GOOS, runtime.GOARCH)

	// 检查架构（临时放宽检查）
	if runtime.GOOS != "darwin" {
		fmt.Println("❌ 错误: 此程序仅支持 macOS 系统")
		os.Exit(1)
	}

	// 临时放宽架构检查，支持更多ARM64变体
	if !strings.Contains(runtime.GOARCH, "arm") && !strings.Contains(runtime.GOARCH, "aarch") {
		fmt.Printf("❌ 警告: 检测到非ARM架构 (%s)，程序可能无法正常工作\n", runtime.GOARCH)
		fmt.Println("💡 提示: 本程序设计用于Apple Silicon芯片(M1/M2/M3/M4)，但将尝试继续运行")
	} else {
		fmt.Printf("✅ 检测到ARM架构: %s\n", runtime.GOARCH)
	}

	// 检查依赖
	var tools ToolCheckResults
	if _, err := exec.LookPath("cjxl"); err == nil {
		tools.HasCjxl = true
		fmt.Println("✅ cjxl 已找到 (用于JXL转换)")
	} else {
		fmt.Println("❌ 未找到 cjxl - 请安装: brew install cjxl")
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Println("❌ 未找到 ffmpeg - 请安装: brew install ffmpeg")
	} else {
		fmt.Printf("✅ ffmpeg 已找到: %s\n", ffmpegPath)
		out, err := exec.Command("ffmpeg", "-codecs").Output()
		if err != nil {
			fmt.Printf("❌ 无法执行 ffmpeg -codecs: %v\n", err)
		} else {
			if strings.Contains(string(out), "libsvtav1") {
				tools.HasLibSvtAv1 = true
				fmt.Println("✅ 找到 libsvtav1 编码器 (用于AV1)")
			} else {
				fmt.Println("⚠️ 未找到 libsvtav1 编码器 - 建议重新安装 ffmpeg")
			}
			if strings.Contains(string(out), "videotoolbox") {
				tools.HasVToolbox = true
				fmt.Println("✅ 找到 videotoolbox 硬件加速")
			} else {
				fmt.Println("⚠️ 未找到 videotoolbox 硬件加速")
			}
		}
	}

	// 检查其他依赖
	dependencies := []string{"magick", "exiftool"}
	for _, dep := range dependencies {
		if _, err := exec.LookPath(dep); err == nil {
			fmt.Printf("✅ %s 已找到\n", dep)
		} else {
			fmt.Printf("❌ 未找到 %s - 请安装: brew install %s\n", dep, dep)
		}
	}

	fmt.Println("\n🔍 正在初始化应用上下文...")

	// 尝试继续执行，即使架构检查不完全匹配
	var toolsCheck ToolCheckResults
	if _, err := exec.LookPath("cjxl"); err == nil {
		toolsCheck.HasCjxl = true
	}
	if out, err := exec.Command("ffmpeg", "-codecs").Output(); err == nil {
		if strings.Contains(string(out), "libsvtav1") {
			toolsCheck.HasLibSvtAv1 = true
		}
		if strings.Contains(string(out), "videotoolbox") {
			toolsCheck.HasVToolbox = true
		}
	}

	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		config := parseFlags()
		fmt.Println("📌 检测到命令行参数，进入非交互模式")
		if err := executeStreamingPipeline(config, toolsCheck); err != nil {
			log.Fatalf("FATAL: %v", err)
		}
	} else {
		fmt.Println("✅ 未检测到命令行参数，进入交互模式")
		interactiveSessionLoop(toolsCheck)
	}

	// 添加延迟，确保用户能看到输出
	fmt.Println("\n✅ 程序执行完成，按 Enter 键退出...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
