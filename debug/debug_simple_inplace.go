//go:build demo_debug
// +build demo_debug

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"pixly/config"
	"pixly/core/converter"

	"go.uber.org/zap"
)

func main() {
	// 创建日志记录器
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatal("创建日志记录器失败:", err)
	}
	defer logger.Sync()

	// 创建配置
	cfg, err := config.NewConfig("", logger)
	if err != nil {
		log.Fatal("创建配置失败:", err)
	}

	// 设置测试目录（更新到 test_副本4 的 test_images）
	testDir := "/Users/nameko_1/Downloads/test_副本4/test_images"

	// 检查测试目录是否存在
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		log.Fatal("测试目录不存在:", testDir)
	}

	fmt.Printf("=== Pixly 媒体转换引擎简单原地转换测试（静态 GIF -> JXL） ===\n")
	fmt.Printf("测试目录: %s\n\n", testDir)

	// 创建一个新的测试目录用于原地转换测试
	inPlaceTestDir := filepath.Join(testDir, "simple_inplace_test_gif")

	// 创建测试目录
	err = os.MkdirAll(inPlaceTestDir, 0755)
	if err != nil {
		log.Printf("创建原地转换测试目录失败: %v", err)
		return
	}

	// 复制一个测试文件到原地转换目录（目标：静态 GIF）
	testFile := "test3.gif"
	srcPath := filepath.Join(testDir, testFile)
	dstPath := filepath.Join(inPlaceTestDir, testFile)

	// 检查源文件是否存在
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		log.Printf("源文件不存在: %s", srcPath)
		return
	}

	// 获取原始文件信息
	originalInfo, err := os.Stat(srcPath)
	if err != nil {
		log.Printf("获取源文件信息失败: %v", err)
		return
	}

	// 复制文件
	input, err := os.ReadFile(srcPath)
	if err != nil {
		log.Printf("读取源文件失败 %s: %v", srcPath, err)
		return
	}

	err = os.WriteFile(dstPath, input, 0644)
	if err != nil {
		log.Printf("写入目标文件失败 %s: %v", dstPath, err)
		return
	}

	fmt.Printf("原始文件: %s (%.2f KB)\n", testFile, float64(originalInfo.Size())/1024)

	// 设置原地转换（DirectoryTemplate为空，KeepOriginal=false）
	cfg.Output.DirectoryTemplate = "" // 关键：设置为空以启用原地转换
	cfg.Output.KeepOriginal = false   // 关键：不保留原始文件

	fmt.Printf("配置 DirectoryTemplate: '%s'\n", cfg.Output.DirectoryTemplate)
	fmt.Printf("配置 KeepOriginal: %t\n", cfg.Output.KeepOriginal)

	// 创建转换器（使用 quality 模式触发平衡优化策略）
	conv, err := converter.NewConverter(cfg, logger, "quality")
	if err != nil {
		log.Printf("创建转换器失败: %v", err)
		return
	}

	// 执行转换（对包含单个 GIF 的目录进行处理）
	err = conv.Convert(inPlaceTestDir)
	if err != nil {
		log.Printf("转换失败: %v", err)
		conv.Close()
		return
	}

	// 关闭转换器
	conv.Close()

	// 检查转换结果
	fmt.Printf("\n转换后文件:\n")

	// 检查是否还存在原始文件（.gif）
	originalFilesExist := 0
	convertedJXLFiles := 0

	filepath.Walk(inPlaceTestDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		fileName := filepath.Base(path)
		ext := strings.ToLower(filepath.Ext(path))

		// 检查是否为原始文件（.gif）
		if ext == ".gif" {
			originalFilesExist++
			fmt.Printf("  原始文件仍存在: %s (%.2f KB)\n", fileName, float64(info.Size())/1024)
		}

		// 检查是否为转换后的文件（.jxl）
		if ext == ".jxl" {
			convertedJXLFiles++
			fmt.Printf("  转换后文件: %s (%.2f KB)\n", fileName, float64(info.Size())/1024)
		}

		return nil
	})

	fmt.Printf("\n结果分析:\n")
	if originalFilesExist == 0 && convertedJXLFiles > 0 {
		fmt.Printf("  ✓ 原地转换成功：静态 GIF 已被替换为 JXL 文件\n")
	} else if originalFilesExist > 0 && convertedJXLFiles > 0 {
		fmt.Printf("  ⚠ 部分原地转换：既存在原始 GIF 又存在 JXL，请检查策略选择\n")
	} else if originalFilesExist > 0 && convertedJXLFiles == 0 {
		fmt.Printf("  ✗ 原地转换失败：仅存在原始 GIF，未生成 JXL\n")
	} else {
		fmt.Printf("  ✗ 原地转换失败：未找到任何文件\n")
	}

	// 清理测试目录
	os.RemoveAll(inPlaceTestDir)
}
