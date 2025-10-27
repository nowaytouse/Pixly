package converter

import (
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// convertToJXL 转换为JPEG XL格式
func (c *Converter) convertToJXL(file *MediaFile, quality int) (string, error) {
	framework := NewConversionFramework(c)
	return framework.Execute(file, framework.JXLConfig(), quality)
}

// convertToAVIF 转换为AVIF格式
func (c *Converter) convertToAVIF(file *MediaFile, quality int) (string, error) {
	framework := NewConversionFramework(c)
	return framework.Execute(file, framework.AVIFConfig(), quality)
}

// 辅助函数

// hasTransparency 检查图片是否有透明度
func (c *Converter) hasTransparency(path string) bool {
	// 使用FFprobe检查透明度
	args := []string{
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=pix_fmt",
		"-of", "csv=p=0",
		path,
	}

	// 使用工具管理器执行命令，支持路径验证
	output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFprobePath, args...)
	if err != nil {
		return false
	}

	pixFmt := strings.TrimSpace(string(output))
	// 常见的带透明度的像素格式
	transparentFormats := []string{"rgba", "argb", "bgra", "abgr", "yuva", "gbra"}

	for _, format := range transparentFormats {
		if strings.Contains(pixFmt, format) {
			return true
		}
	}

	return false
}

// isAnimated 检查是否为动图
func (c *Converter) isAnimated(path string) bool {
	// 使用FFprobe检查帧数
	args := []string{
		"-v", "quiet",
		"-select_streams", "v:0",
		"-count_frames",
		"-show_entries", "stream=nb_read_frames",
		"-of", "csv=p=0",
		path,
	}

	// 使用工具管理器执行命令，支持路径验证
	output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFprobePath, args...)
	if err != nil {
		return false
	}

	frames := strings.TrimSpace(string(output))
	// 如果帧数大于1，则为动图
	return frames != "1" && frames != "0" && frames != ""
}

// getOutputPath 获取输出文件路径 - 支持原地转换和指定目录输出，同时保持目录结构
func (c *Converter) getOutputPath(file *MediaFile, newExt string) string {
	// 路径计算逻辑

	// 使用GlobalPathUtils处理路径
	normalizedPath, err := GlobalPathUtils.NormalizePath(file.Path)
	if err != nil {
		return ""
	}
	
	// 获取相对于工作目录的相对路径
	workingDir, err := os.Getwd()
	if err != nil {
		return ""
	}
	
	// 计算相对于工作目录的相对路径
	relPath, err := filepath.Rel(workingDir, normalizedPath)
	if err != nil {
		return ""
	}
	
	// 获取文件名和扩展名
	baseName := GlobalPathUtils.GetBaseName(normalizedPath)
	ext := GlobalPathUtils.GetExtension(normalizedPath)
	name := strings.TrimSuffix(baseName, ext)

	var outputDir string
	
	// 核心逻辑：默认原地转换，只有指定输出目录时才使用输出目录
	if c.config.Output.DirectoryTemplate != "" {
		// 指定了输出目录，保持原始目录结构
		outputDir = filepath.Join(c.config.Output.DirectoryTemplate, filepath.Dir(relPath))
		
		// 确保输出目录存在
		if err := c.fileOpHandler.SafeCreateDir(outputDir); err != nil {
			c.logger.Warn("Failed to create output directory", zap.String("dir", outputDir), zap.Error(err))
			// 如果创建目录失败，回退到基本输出目录
			outputDir = c.config.Output.DirectoryTemplate
		}
	} else {
		// 默认原地转换：使用原文件所在目录
		outputDir = filepath.Dir(normalizedPath)
	}

	outputPath, err := GlobalPathUtils.JoinPath(outputDir, name+newExt)
	if err != nil {
		return ""
	}
	// 规范化输出路径
	normalizedOutput, err := GlobalPathUtils.NormalizePath(outputPath)
	if err != nil {
		return outputPath // 如果规范化失败，返回原路径
	}
	return normalizedOutput
}

// verifyOutputFile 验证输出文件
func (c *Converter) verifyOutputFile(outputPath string, originalSize int64) bool {
	c.logger.Debug("开始验证输出文件", zap.String("outputPath", outputPath), zap.Int64("originalSize", originalSize))

	stat, err := os.Stat(outputPath)
	if err != nil {
		c.logger.Debug("无法获取输出文件信息", zap.String("outputPath", outputPath), zap.Error(err))
		return false
	}

	// 检查文件大小是否合理（不能为0）
	if stat.Size() == 0 {
		c.logger.Debug("输出文件大小为0", zap.String("outputPath", outputPath), zap.Int64("size", stat.Size()))
		return false
	}

	// 对于WebP到JXL的转换，允许更大的文件大小
	// WebP是有损压缩格式，而JXL是无损压缩，所以文件大小增加是正常的
	// 只有当文件大小超过原文件10倍时才认为异常
	if stat.Size() > originalSize*10 {
		c.logger.Debug("输出文件大小超过原文件10倍", zap.String("outputPath", outputPath), zap.Int64("size", stat.Size()), zap.Int64("originalSize", originalSize))
		return false
	}

	c.logger.Debug("文件大小检查通过", zap.String("outputPath", outputPath), zap.Int64("size", stat.Size()))

	// 使用FFprobe验证文件的实际有效性
	if !c.verifyFileIntegrity(outputPath) {
		c.logger.Debug("文件完整性验证失败", zap.String("outputPath", outputPath))
		return false
	}

	c.logger.Debug("输出文件验证成功", zap.String("outputPath", outputPath))
	return true
}

// verifyFileIntegrity 使用FFprobe验证文件的实际有效性
func (c *Converter) verifyFileIntegrity(filePath string) bool {
	// 使用FFprobe检查文件是否为有效的媒体文件
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		filePath,
	}

	// 使用工具管理器执行命令，支持路径验证
	output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFprobePath, args...)
	if err != nil {
		// File integrity verification failed - ffprobe failed
		return false
	}

	// 检查输出是否包含有效的格式信息
	outputStr := string(output)
	if len(outputStr) < 10 || !strings.Contains(outputStr, "format") {
		// File integrity verification failed - invalid format info
		return false
	}

	return true
}

// RepackagingConfig 重新包装配置
type RepackagingConfig struct {
	SupportedExts []string
	CjxlArgs      []string
	NeedsFFmpeg   bool
	FFmpegFormat  string
}

// convertToJXLRepackaging 统一的JXL重新包装函数
func (c *Converter) convertToJXLRepackaging(file *MediaFile, config RepackagingConfig) (string, error) {
	outputPath := c.getOutputPath(file, ".jxl")
	ext := strings.ToLower(file.Extension)

	// 检查文件格式支持
	supported := false
	for _, supportedExt := range config.SupportedExts {
		if ext == supportedExt {
			supported = true
			break
		}
	}
	if !supported {
		var formatErrorBuilder strings.Builder
		formatErrorBuilder.WriteString("unsupported file format for repackaging: ")
		formatErrorBuilder.WriteString(ext)
		return "", c.errorHandler.WrapError(formatErrorBuilder.String(), nil)
	}

	// 检查是否为原地转换
	isInPlace := c.config.Output.DirectoryTemplate == ""
	var actualOutputPath string
	if isInPlace {
		actualOutputPath = outputPath + ".tmp"
	} else {
		actualOutputPath = outputPath + ".tmp"
		if err := c.fileOpHandler.SafeCreateDir(filepath.Dir(outputPath)); err != nil {
			return "", err
		}
	}

	var inputPath string
	var tempFile string

	// 如果需要FFmpeg预处理
	if config.NeedsFFmpeg {
		tempFile = actualOutputPath + ".temp.png"
		if err := c.fileOpHandler.SafeRemoveFile(tempFile); err != nil {
			// Failed to remove existing temp file
		}

		// 使用FFmpeg转换
		ffmpegArgs := []string{
			"-i", file.Path,
			"-y",
			tempFile,
		}

		output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFmpegPath, ffmpegArgs...)
		if err != nil {
			if removeErr := c.fileOpHandler.SafeRemoveFile(tempFile); removeErr != nil {
				// Failed to cleanup temp file after FFmpeg error
			}
			return "", c.errorHandler.WrapError("FFmpeg conversion", err, "output", string(output))
		}
		inputPath = tempFile
		defer func() {
			if err := c.fileOpHandler.SafeRemoveFile(tempFile); err != nil {
				// Failed to cleanup temp file
			}
		}()
	} else {
		inputPath = file.Path
	}

	// 构建cjxl参数
	args := []string{inputPath, actualOutputPath}
	args = append(args, config.CjxlArgs...)

	// cjxl 会自动处理透明度，无需额外参数

	// 执行cjxl转换
	output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.CjxlPath, args...)
	if err != nil {
		if removeErr := c.fileOpHandler.SafeRemoveFile(actualOutputPath); removeErr != nil {
			// Failed to cleanup output file after cjxl error
		}
		return "", c.errorHandler.WrapError("cjxl execution", err, "output", string(output))
	}

	// 验证输出文件
	if !c.verifyOutputFile(actualOutputPath, file.Size) {
		if removeErr := c.fileOpHandler.SafeRemoveFile(actualOutputPath); removeErr != nil {
			// Failed to cleanup invalid output file
		}
		var verifyErrorBuilder strings.Builder
		verifyErrorBuilder.WriteString("output file verification failed: ")
		verifyErrorBuilder.WriteString(actualOutputPath)
		return "", c.errorHandler.WrapError(verifyErrorBuilder.String(), nil)
	}

	// 原子性替换文件
	if err := c.fileOpHandler.AtomicFileReplace(actualOutputPath, outputPath, isInPlace); err != nil {
		return "", err
	}

	// JXL repackaging completed

	return outputPath, nil
}

// convertJPEGToJXLRepackaging JPEG到JXL的无损重新包装
// 只有jpeg才能重新包装为Jxl!
func (c *Converter) convertJPEGToJXLRepackaging(file *MediaFile) (string, error) {
	config := RepackagingConfig{
		SupportedExts: []string{".jpg", ".jpeg", ".jpe"},
		CjxlArgs:      []string{"--lossless_jpeg=1", "--effort=9"},
		NeedsFFmpeg:   false,
	}
	return c.convertToJXLRepackaging(file, config)
}

// convertPNGToJXLRepackaging PNG到JXL的无损转换
func (c *Converter) convertPNGToJXLRepackaging(file *MediaFile) (string, error) {
	config := RepackagingConfig{
		SupportedExts: []string{".png"},
		CjxlArgs:      []string{"--distance=0", "--effort=9"},
		NeedsFFmpeg:   false,
	}
	return c.convertToJXLRepackaging(file, config)
}

// convertWebPToJXLRepackaging WebP到JXL的数学无损转换
func (c *Converter) convertWebPToJXLRepackaging(file *MediaFile) (string, error) {
	// WebP动图检测
	if c.isAnimated(file.Path) {
		var animatedErrorBuilder strings.Builder
		animatedErrorBuilder.WriteString("animated WebP files are not supported for repackaging: ")
		animatedErrorBuilder.WriteString(file.Path)
		return "", c.errorHandler.WrapError(animatedErrorBuilder.String(), nil)
	}

	config := RepackagingConfig{
		SupportedExts: []string{".webp"},
		CjxlArgs:      []string{"--distance=0", "--effort=9"},
		NeedsFFmpeg:   true,
		FFmpegFormat:  "png",
	}
	return c.convertToJXLRepackaging(file, config)
}

// convertToJXLMathematicalLossless JXL数学无损压缩
func (c *Converter) convertToJXLMathematicalLossless(file *MediaFile) (string, error) {
	outputPath := c.getOutputPath(file, ".jxl")

	// 检查是否为原地转换
	isInPlace := c.config.Output.DirectoryTemplate == ""
	var actualOutputPath string
	if isInPlace {
		actualOutputPath = outputPath + ".tmp"
	} else {
		actualOutputPath = outputPath + ".tmp"
		if err := c.fileOpHandler.SafeCreateDir(filepath.Dir(outputPath)); err != nil {
			return "", err
		}
	}

	// 使用FFmpeg进行数学无损压缩（distance=0）
	args := []string{
		"-i", file.Path,
		"-c:v", "libjxl",
		"-distance", "0", // distance=0表示数学无损
		"-effort", "9", // 最高压缩效率
		"-y",
		actualOutputPath,
	}

	output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFmpegPath, args...)
	if err != nil {
		if removeErr := c.fileOpHandler.SafeRemoveFile(actualOutputPath); removeErr != nil {
			// Failed to cleanup output file after FFmpeg error
		}
		return "", c.errorHandler.WrapError("mathematical lossless JXL conversion", err, "output", string(output))
	}

	// 验证输出文件
	if !c.verifyOutputFile(actualOutputPath, file.Size) {
		if removeErr := c.fileOpHandler.SafeRemoveFile(actualOutputPath); removeErr != nil {
			// Failed to cleanup invalid output file
		}
		var mathVerifyErrorBuilder strings.Builder
		mathVerifyErrorBuilder.WriteString("output file verification failed: ")
		mathVerifyErrorBuilder.WriteString(actualOutputPath)
		return "", c.errorHandler.WrapError(mathVerifyErrorBuilder.String(), nil)
	}

	// 原地转换处理
	if isInPlace {
		if err := c.fileOpHandler.AtomicFileReplace(actualOutputPath, outputPath, true); err != nil {
			return "", err
		}
		// Mathematical lossless JXL conversion completed
		return outputPath, nil
	}

	// 非原地转换
	if err := c.fileOpHandler.AtomicFileReplace(actualOutputPath, outputPath, false); err != nil {
		return "", err
	}
	return outputPath, nil
}

// convertToJXLLossless JXL无损转换
func (c *Converter) convertToJXLLossless(file *MediaFile) (string, error) {
	outputPath := c.getOutputPath(file, ".jxl")
	ext := strings.ToLower(file.Extension)

	// 检查是否为原地转换 - 只有未指定输出目录时才是原地转换
	isInPlace := c.config.Output.DirectoryTemplate == ""

	c.logger.Debug("开始JXL无损转换",
		zap.String("file", file.Path),
		zap.String("outputPath", outputPath),
		zap.Bool("isInPlace", isInPlace))

	// 为原地转换创建临时输出文件
	var actualOutputPath string
	if isInPlace {
		actualOutputPath = outputPath + ".tmp"
		defer func() {
			if actualOutputPath != outputPath {
				if err := c.fileOpHandler.SafeRemoveFile(actualOutputPath); err != nil {
					// Failed to cleanup temp output file
				}
			}
		}()
	} else {
		actualOutputPath = outputPath + ".tmp" // 始终使用临时文件
		if err := c.fileOpHandler.SafeCreateDir(filepath.Dir(outputPath)); err != nil {
			return "", err
		}
	}

	c.logger.Debug("临时输出路径", zap.String("actualOutputPath", actualOutputPath))

	// 对于WebP格式，需要先转换为PNG然后再用cjxl处理
	var inputPath string
	var tempFile string

	if ext == ".webp" {
		// WebP动图检测
		if c.isAnimated(file.Path) {
			var animatedErrorBuilder strings.Builder
			animatedErrorBuilder.WriteString("animated WebP files are not supported for JXL conversion: ")
			animatedErrorBuilder.WriteString(file.Path)
			return "", c.errorHandler.WrapError(animatedErrorBuilder.String(), nil)
		}

		// 创建临时PNG文件
		tempFile = outputPath + ".temp.png"
		if err := c.fileOpHandler.SafeRemoveFile(tempFile); err != nil {
			// Failed to remove existing temp file
		}

		c.logger.Debug("开始WebP到PNG转换", zap.String("tempFile", tempFile))

		// 使用FFmpeg将WebP转换为PNG
		args := []string{
			"-i", file.Path,
			"-y",
			tempFile,
		}

		// 使用工具管理器执行命令，支持路径验证
		output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFmpegPath, args...)
		if err != nil {
			if removeErr := c.fileOpHandler.SafeRemoveFile(tempFile); removeErr != nil {
				// Failed to cleanup temp file after FFmpeg error
			}
			return "", c.errorHandler.WrapError("WebP to PNG conversion", err, "output", string(output))
		}

		inputPath = tempFile
		c.logger.Debug("WebP到PNG转换完成")
		// Converted WebP to PNG for cjxl processing
	} else {
		inputPath = file.Path
	}

	// 清理函数，确保临时文件被清理
	defer func() {
		if tempFile != "" {
			if err := c.fileOpHandler.SafeRemoveFile(tempFile); err != nil {
				// Failed to cleanup temp file
			}
		}
	}()

	// 构建cjxl无损命令
	args := []string{
		inputPath,
		actualOutputPath,
		"--lossless_jpeg=1", // 对于JPEG使用lossless_jpeg
		"-e", "9",           // 固定JXL压缩参数为-e 9
	}

	// 对于非JPEG文件，使用distance参数实现无损
	if ext != ".jpg" && ext != ".jpeg" {
		// 移除lossless_jpeg参数，使用distance=0实现无损
		args = []string{
			inputPath,
			actualOutputPath,
			"--distance=0", // distance=0表示无损
			"-e", "9",      // 固定JXL压缩参数为-e 9
		}
	}

	c.logger.Debug("执行cjxl命令", zap.Strings("args", args))

	// 首选cjxl工具进行无损JXL转换
	output, err := c.toolManager.ExecuteWithPathValidation(c.config.Tools.CjxlPath, args...)
	if err != nil {
		// cjxl失败，使用FFmpeg作为备选方案
		// cjxl lossless conversion failed, trying FFmpeg as fallback

		c.logger.Debug("cjxl转换失败，尝试FFmpeg作为备选方案")

		// 构建FFmpeg无损命令作为备选
		ffmpegArgs := []string{
			"-i", inputPath,
			"-c:v", "libjxl",
			"-distance", "0", // distance=0表示无损
			"-effort", "9",
			"-y",
			actualOutputPath,
		}

		// 使用FFmpeg执行无损转换
		output, err = c.toolManager.ExecuteWithPathValidation(c.config.Tools.FFmpegPath, ffmpegArgs...)
		if err != nil {
			return "", c.errorHandler.WrapError("both cjxl and FFmpeg lossless JXL conversion failed", err, "output", string(output))
		}
	}

	c.logger.Debug("cjxl转换成功")

	// 验证输出文件
	if !c.verifyOutputFile(actualOutputPath, file.Size) {
		var verifyErrorBuilder strings.Builder
		verifyErrorBuilder.WriteString("output file verification failed: ")
		verifyErrorBuilder.WriteString(actualOutputPath)
		return "", c.errorHandler.WrapError(verifyErrorBuilder.String(), nil)
	}

	c.logger.Debug("输出文件验证成功")

	// 原地转换：使用原子文件替换
	if isInPlace {
		c.logger.Debug("执行原地转换")
		// 使用原子文件替换
		if err := c.fileOpHandler.AtomicFileReplace(actualOutputPath, outputPath, true); err != nil {
			return "", err
		}

		c.logger.Debug("原子操作替换成功", zap.String("outputPath", outputPath))
		// Atomically replaced original file
		c.logger.Debug("原地转换完成，返回输出路径", zap.String("outputPath", outputPath))
		return outputPath, nil // 原地转换返回新文件路径
	} else {
		c.logger.Debug("执行非原地转换")
		// 非原地转换：重命名临时文件到最终位置
		if err := c.fileOpHandler.AtomicFileReplace(actualOutputPath, outputPath, false); err != nil {
			return "", err
		}
		c.logger.Debug("非原地转换完成", zap.String("outputPath", outputPath))
		}

	c.logger.Debug("转换完成", zap.String("outputPath", outputPath))
	return outputPath, nil
}
