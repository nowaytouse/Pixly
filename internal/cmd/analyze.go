package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"pixly/config"
	"pixly/core/converter"
	"pixly/internal/output"
	"pixly/internal/ui"
)

// analyzeCmd represents the analyze command
var analyzeCmd = &cobra.Command{
	Use:   "analyze [directory]",
	Short: "分析指定目录的媒体文件，不进行转换",
	Long: `分析指定目录的媒体文件，生成详细的分析报告，包括：
- 文件格式统计
- 文件大小分布
- 媒体质量分析
- 转换建议

示例：
  pixly analyze /path/to/media/files
  pixly analyze --verbose ./images`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAnalyze,
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	targetDir := "."
	if len(args) > 0 {
		targetDir = args[0]
	}

	log.Info("开始分析媒体文件",
		zap.String("target", targetDir))

	analyzer := &MediaAnalyzer{
		config: cfg,
		logger: log,
	}

	return analyzer.AnalyzeDirectory(targetDir)
}

// MediaAnalyzer 媒体文件分析器
type MediaAnalyzer struct {
	config *config.Config
	logger *zap.Logger
}

// AnalyzeDirectory 分析目录
func (a *MediaAnalyzer) AnalyzeDirectory(targetDir string) error {
	ui.Println("🔍 正在扫描和分析媒体文件...")

	// 扫描媒体文件
	files, err := a.scanMediaFiles(targetDir)
	if err != nil {
		return fmt.Errorf("扫描文件失败: %w", err)
	}

	if len(files) == 0 {
		ui.Println("❌ 未找到媒体文件")
		return nil
	}

	ui.Printf("📊 找到 %d 个媒体文件\n\n", len(files))

	// 生成分析报告
	report := a.generateAnalysisReport(files)

	// 显示报告
	a.displayReport(report)

	// 保存详细报告
	if err := a.saveAnalysisReport(report); err != nil {
		a.logger.Error("保存分析报告失败", zap.Error(err))
	}

	return nil
}

// AnalysisReport 分析报告
type AnalysisReport struct {
	TotalFiles       int                   `json:"total_files"`
	TotalSize        int64                 `json:"total_size"`
	FormatStats      map[string]FormatInfo `json:"format_stats"`
	TypeStats        map[string]int        `json:"type_stats"`
	SizeDistribution map[string]int        `json:"size_distribution"`
	LargestFiles     []FileInfo            `json:"largest_files"`
}

// FormatInfo 格式信息
type FormatInfo struct {
	Count     int   `json:"count"`
	TotalSize int64 `json:"total_size"`
	AvgSize   int64 `json:"avg_size"`
}

// FileInfo 文件信息
type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// scanMediaFiles 扫描媒体文件
func (a *MediaAnalyzer) scanMediaFiles(targetDir string) ([]*converter.MediaFile, error) {
	var files []*converter.MediaFile

	err := converter.GlobalPathUtils.WalkPath(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			a.logger.Warn("访问文件时出错", zap.String("path", path), zap.Error(err))
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// 检查是否为媒体文件
		if a.isMediaFile(path) {
			ext := strings.ToLower(converter.GlobalPathUtils.GetExtension(path))
			mediaFile := &converter.MediaFile{
				Path:      path,
				Name:      info.Name(),
				Size:      info.Size(),
				Extension: ext,
				ModTime:   info.ModTime(),
				Type:      a.getFileType(ext),
			}
			files = append(files, mediaFile)
		}

		return nil
	})

	return files, err
}

// isMediaFile 检查是否为媒体文件
func (a *MediaAnalyzer) isMediaFile(path string) bool {
	ext := strings.ToLower(converter.GlobalPathUtils.GetExtension(path))

	// 使用配置文件中的支持扩展名列表
	for _, supportedExt := range a.config.Conversion.SupportedExtensions {
		if ext == strings.ToLower(supportedExt) {
			return true
		}
	}

	return false
}

// getFileType 获取文件类型
func (a *MediaAnalyzer) getFileType(ext string) converter.MediaType {
	ext = strings.ToLower(ext)

	// 检查是否为图片格式
	for _, imgExt := range a.config.Conversion.ImageExtensions {
		if ext == strings.ToLower(imgExt) {
			return converter.TypeImage
		}
	}

	// 检查是否为视频格式
	for _, vidExt := range a.config.Conversion.VideoExtensions {
		if ext == strings.ToLower(vidExt) {
			return converter.TypeVideo
		}
	}

	return converter.TypeUnknown
}

// generateAnalysisReport 生成分析报告
func (a *MediaAnalyzer) generateAnalysisReport(files []*converter.MediaFile) *AnalysisReport {
	report := &AnalysisReport{
		TotalFiles:       len(files),
		FormatStats:      make(map[string]FormatInfo),
		TypeStats:        make(map[string]int),
		SizeDistribution: make(map[string]int),
		LargestFiles:     make([]FileInfo, 0),
	}

	// 统计格式和类型
	for _, file := range files {
		report.TotalSize += file.Size

		// 格式统计
		format := file.Extension
		formatInfo := report.FormatStats[format]
		formatInfo.Count++
		formatInfo.TotalSize += file.Size
		formatInfo.AvgSize = formatInfo.TotalSize / int64(formatInfo.Count)
		report.FormatStats[format] = formatInfo

		// 类型统计
		report.TypeStats[string(file.Type)]++

		// 大小分布
		sizeCategory := a.getSizeCategory(file.Size)
		report.SizeDistribution[sizeCategory]++

		// 记录大文件
		if file.Size > 10*1024*1024 { // 大于10MB
			report.LargestFiles = append(report.LargestFiles, FileInfo{
				Path: file.Path,
				Size: file.Size,
				Type: string(file.Type),
			})
		}
	}

	// 不再生成建议
	return report
}

// getSizeCategory 获取大小类别
func (a *MediaAnalyzer) getSizeCategory(size int64) string {
	mb := size / (1024 * 1024)

	switch {
	case mb < 1:
		return "< 1MB"
	case mb < 5:
		return "1-5MB"
	case mb < 10:
		return "5-10MB"
	case mb < 50:
		return "10-50MB"
	case mb < 100:
		return "50-100MB"
	default:
		return "> 100MB"
	}
}

// generateRecommendations 生成转换建议（已废弃）

// displayReport 显示报告
func (a *MediaAnalyzer) displayReport(report *AnalysisReport) {
	oc := output.GetOutputController()

	oc.WriteLine("📊 == 媒体文件分析报告 ==")
	oc.WriteString(fmt.Sprintf("总文件数: %d\n", report.TotalFiles))
	oc.WriteString(fmt.Sprintf("总大小: %.2f MB\n\n", float64(report.TotalSize)/(1024*1024)))
	oc.Flush()

	oc.WriteLine("📈 文件类型分布:")
	for fileType, count := range report.TypeStats {
		oc.WriteString(fmt.Sprintf("  %s: %d 个文件\n", fileType, count))
	}
	oc.WriteLine("")
	oc.Flush()

	oc.WriteLine("📊 格式统计 (Top 10):")
	type FormatPair struct {
		Format string
		Info   FormatInfo
	}

	var sortedFormats []FormatPair
	for format, info := range report.FormatStats {
		sortedFormats = append(sortedFormats, FormatPair{format, info})
	}

	// 简单排序（按文件数）
	for i := 0; i < len(sortedFormats)-1; i++ {
		for j := i + 1; j < len(sortedFormats); j++ {
			if sortedFormats[i].Info.Count < sortedFormats[j].Info.Count {
				sortedFormats[i], sortedFormats[j] = sortedFormats[j], sortedFormats[i]
			}
		}
	}

	for i, pair := range sortedFormats {
		if i >= 10 {
			break
		}
		oc.WriteString(fmt.Sprintf("  %s: %d 个文件, 总大小: %.2f MB, 平均大小: %.2f MB\n",
			pair.Format, pair.Info.Count,
			float64(pair.Info.TotalSize)/(1024*1024),
			float64(pair.Info.AvgSize)/(1024*1024)))
	}
	oc.WriteLine("")
	oc.Flush()

	oc.WriteLine("📏 大小分布:")
	for size, count := range report.SizeDistribution {
		oc.WriteString(fmt.Sprintf("  %s: %d 个文件\n", size, count))
	}
	oc.WriteLine("")
	oc.Flush()

	if len(report.LargestFiles) > 0 {
		oc.WriteLine("🗂️ 最大的文件 (前5个):")
		for i, file := range report.LargestFiles {
			if i >= 5 {
				break
			}
			oc.WriteString(fmt.Sprintf("  %s (%.2f MB) - %s\n",
				converter.GlobalPathUtils.GetBaseName(file.Path),
				float64(file.Size)/(1024*1024),
				file.Type))
		}
		oc.WriteLine("")
		oc.Flush()
	}

	// 不再显示转换建议

}

// saveAnalysisReport 保存分析报告
func (a *MediaAnalyzer) saveAnalysisReport(report *AnalysisReport) error {
	// 确保目录存在
	if err := os.MkdirAll("reports/analysis", 0755); err != nil {
		return fmt.Errorf("创建reports/analysis目录失败: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	var builder strings.Builder
	builder.WriteString("pixly_analysis_")
	builder.WriteString(timestamp)
	builder.WriteString(".json")
	filename, err := converter.GlobalPathUtils.JoinPath("reports", "analysis", builder.String())
	if err != nil {
		return fmt.Errorf("构建文件路径失败: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化报告失败: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("保存分析报告失败: %w", err)
	}

	ui.Printf("📄 详细分析报告已保存到: %s\n", filename)
	return nil
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
}
