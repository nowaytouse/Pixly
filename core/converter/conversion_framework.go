package converter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ConversionConfig 统一的转换配置
type ConversionConfig struct {
	OutputExtension string
	ToolPath        string
	ArgsBuilder     func(input, output string, quality int) []string
	PreProcessor    func(inputPath string) (processedPath string, cleanup func(), err error)
	PostProcessor   func(outputPath string) error
}

// ConversionFramework 统一的转换框架，消除重复代码
type ConversionFramework struct {
	converter *Converter
}

// NewConversionFramework 创建转换框架
func NewConversionFramework(converter *Converter) *ConversionFramework {
	return &ConversionFramework{
		converter: converter,
	}
}

// Execute 执行统一的转换流程，消除所有特殊情况
func (cf *ConversionFramework) Execute(file *MediaFile, config ConversionConfig, quality int) (string, error) {
	// 1. 计算输出路径（统一逻辑）
	outputPath := cf.converter.getOutputPath(file, config.OutputExtension)

	// 2. 处理原地转换（统一逻辑）
	isInPlace := cf.converter.config.Output.DirectoryTemplate == ""
	actualOutputPath := cf.prepareOutputPath(outputPath, isInPlace)

	// 3. 预处理输入文件（可选）
	inputPath := file.Path
	var cleanup func()
	if config.PreProcessor != nil {
		processedPath, cleanupFunc, err := config.PreProcessor(file.Path)
		if err != nil {
			return "", cf.converter.errorHandler.WrapError("preprocessing failed", err)
		}
		inputPath = processedPath
		cleanup = cleanupFunc
	}

	// 4. 清理函数（统一逻辑）
	defer func() {
		if cleanup != nil {
			cleanup()
		}
		cf.cleanupTempFile(actualOutputPath, outputPath)
	}()

	// 5. 构建命令参数
	args := config.ArgsBuilder(inputPath, actualOutputPath, quality)

	// 6. 执行转换命令（统一逻辑）
	output, err := cf.converter.toolManager.ExecuteWithPathValidation(config.ToolPath, args...)
	if err != nil {
		return "", cf.converter.errorHandler.WrapErrorWithOutput("conversion failed", err, output)
	}

	// 7. 后处理（可选）
	if config.PostProcessor != nil {
		if err := config.PostProcessor(actualOutputPath); err != nil {
			return "", cf.converter.errorHandler.WrapError("post-processing failed", err)
		}
	}

	// 8. 验证临时文件（在移动之前验证）
	if !cf.converter.verifyOutputFile(actualOutputPath, file.Size) {
		var errorBuilder strings.Builder
		errorBuilder.WriteString("temp file verification failed: path: ")
		errorBuilder.WriteString(actualOutputPath)
		return "", cf.converter.errorHandler.WrapError(errorBuilder.String(), nil)
	}

	// 9. 移动临时文件到最终位置（统一逻辑）
	if err := cf.finalizeTempFile(actualOutputPath, outputPath); err != nil {
		return "", err
	}

	// Conversion completed

	return outputPath, nil
}

// prepareOutputPath 准备输出路径（统一逻辑）
func (cf *ConversionFramework) prepareOutputPath(outputPath string, isInPlace bool) string {
	actualOutputPath := outputPath + ".tmp"

	if !isInPlace {
		// 确保输出目录存在
		os.MkdirAll(filepath.Dir(outputPath), 0755)
	}
	// 对于原地转换，我们仍然需要确保目录存在
	os.MkdirAll(filepath.Dir(outputPath), 0755)

	return actualOutputPath
}

// cleanupTempFile 清理临时文件（统一逻辑）
func (cf *ConversionFramework) cleanupTempFile(actualOutputPath, outputPath string) {
	if actualOutputPath != outputPath {
		// 检查最终文件是否存在，如果不存在说明转换失败，需要清理临时文件
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			os.Remove(actualOutputPath)
		}
		// 注意：对于原地转换成功的情况，原始文件的清理由finalizeTempFile处理
	}
}

// finalizeTempFile 完成临时文件处理（统一逻辑）
func (cf *ConversionFramework) finalizeTempFile(actualOutputPath, outputPath string) error {
	if actualOutputPath != outputPath {
		// 检查是否为原地转换
		isInPlace := cf.converter.config.Output.DirectoryTemplate == ""

		if isInPlace {
			// 原地转换：删除原文件，重命名临时文件
			// 注意：这里oldPath应该是原文件路径，outputPath是新文件路径
			// 在原地转换中，actualOutputPath是临时文件，outputPath是最终文件
			if err := cf.converter.fileOpHandler.AtomicFileReplace(actualOutputPath, outputPath, true); err != nil {
				return err
			}
		} else {
			// 非原地转换：直接重命名临时文件
			if err := os.Rename(actualOutputPath, outputPath); err != nil {
				return cf.converter.errorHandler.WrapError("failed to move temp file to final location", err)
			}
		}
	}
	return nil
}

// 预定义的转换配置，消除重复的参数构建逻辑

// JXLConfig JXL转换配置
func (cf *ConversionFramework) JXLConfig() ConversionConfig {
	return ConversionConfig{
		OutputExtension: ".jxl",
		ToolPath:        cf.converter.config.Tools.CjxlPath,
		ArgsBuilder: func(input, output string, quality int) []string {
			// 使用无损参数进行JXL转换
			return []string{
				input,
				output,
				"--distance=0",  // 无损转换
				"-e", "9",       // 最高压缩效率
			}
		},
		PreProcessor: cf.universalToJXLPreProcessor,
	}
}

// AVIFConfig AVIF转换配置
func (cf *ConversionFramework) AVIFConfig() ConversionConfig {
	return ConversionConfig{
		OutputExtension: ".avif",
		ToolPath:        cf.converter.config.Tools.AvifencPath,
		ArgsBuilder: func(input, output string, quality int) []string {
			return []string{
				// 修复参数：使用--qcolor而不是-q，并调整参数顺序
				"--qcolor", strconv.Itoa(quality),
				"-s", "4",
				"-j", "all",
				input,
				output,
			}
		},
		PreProcessor: cf.universalToAVIFPreProcessor,
	}
}

// universalToAVIFPreProcessor 通用AVIF预处理器，处理avifenc不兼容的格式
func (cf *ConversionFramework) universalToAVIFPreProcessor(inputPath string) (string, func(), error) {
	ext := strings.ToLower(filepath.Ext(inputPath))

	// 需要预处理的格式列表
	incompatibleFormats := map[string]bool{
		".gif":  true,
		".webp": true,
		".bmp":  true,
		".tiff": true,
		".tif":  true,
	}

	if !incompatibleFormats[ext] {
		return inputPath, nil, nil
	}

	// 创建临时PNG文件
	tempFile := inputPath + ".temp.png"
	os.Remove(tempFile) // 清理可能存在的临时文件

	// 使用FFmpeg将不兼容格式转换为PNG
	args := []string{
		"-i", inputPath,
	}

	// 对于GIF文件，只取第一帧
	if ext == ".gif" {
		args = append(args, "-vframes", "1")
	}

	args = append(args, "-c:v", "png", "-y", tempFile)

	output, err := cf.converter.toolManager.ExecuteWithPathValidation(cf.converter.config.Tools.FFmpegPath, args...)
	if err != nil {
		os.Remove(tempFile)
		var errorBuilder strings.Builder
		errorBuilder.WriteString(strings.ToUpper(ext[1:]))
		errorBuilder.WriteString(" to PNG conversion failed")
		return "", nil, cf.converter.errorHandler.WrapErrorWithOutput(errorBuilder.String(), err, output)
	}

	cleanup := func() {
		os.Remove(tempFile)
	}

	return tempFile, cleanup, nil
}

// universalToJXLPreProcessor 通用JXL预处理器：处理JXL编码器不直接支持的静态GIF（取第一帧转PNG）
func (cf *ConversionFramework) universalToJXLPreProcessor(inputPath string) (string, func(), error) {
	ext := strings.ToLower(filepath.Ext(inputPath))

	// 目前仅对 GIF 进行预处理（提取第一帧为 PNG）
	if ext != ".gif" {
		return inputPath, nil, nil
	}

	// 生成临时 PNG 文件路径
	tempFile := inputPath + ".temp.png"
	_ = os.Remove(tempFile) // 预清理

	// 使用 FFmpeg 提取第一帧为 PNG
	args := []string{
		"-i", inputPath,
		"-vframes", "1",
		"-c:v", "png",
		"-y", tempFile,
	}

	output, err := cf.converter.toolManager.ExecuteWithPathValidation(cf.converter.config.Tools.FFmpegPath, args...)
	if err != nil {
		_ = os.Remove(tempFile)
		return "", nil, cf.converter.errorHandler.WrapErrorWithOutput("GIF to PNG conversion failed", err, output)
	}

	cleanup := func() { _ = os.Remove(tempFile) }
	return tempFile, cleanup, nil
}

// validateConversionResult 验证转换结果完整性
func (cf *ConversionFramework) validateConversionResult(inputPath, outputPath string) error {
	// 检查输出文件是否存在
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return fmt.Errorf("转换输出文件不存在: %s", outputPath)
	}

	// 检查文件大小
	outputInfo, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("无法获取输出文件信息: %v", err)
	}

	// 输出文件不能为0字节
	if outputInfo.Size() == 0 {
		return fmt.Errorf("转换输出文件为空")
	}

	// 使用ffprobe验证AVIF文件完整性
	if strings.HasSuffix(strings.ToLower(outputPath), ".avif") {
		cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,codec_name", "-of", "csv=p=0", outputPath)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("AVIF文件验证失败: %v", err)
		}
	}

	return nil
}

// completeConversionWithValidation 完成转换并验证结果
func (cf *ConversionFramework) completeConversionWithValidation(inputPath, outputPath string, conversionResult error) error {
	if conversionResult != nil {
		return conversionResult
	}

	// 验证转换结果
	if err := cf.validateConversionResult(inputPath, outputPath); err != nil {
		return cf.converter.errorHandler.WrapError("转换结果验证失败", err)
	}

	// 记录转换统计
	cf.converter.mutex.Lock()
	cf.converter.stats.SuccessfulFiles++
	cf.converter.mutex.Unlock()

	return nil
}
