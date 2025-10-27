package converter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pixly/config"

	"go.uber.org/zap"
)

// MetadataManager 元数据管理器
type MetadataManager struct {
	logger       *zap.Logger
	config       *config.Config
	errorHandler *ErrorHandler
}

// NewMetadataManager 创建元数据管理器实例
func NewMetadataManager(logger *zap.Logger, config *config.Config, errorHandler *ErrorHandler) *MetadataManager {
	return &MetadataManager{
		logger:       logger,
		config:       config,
		errorHandler: errorHandler,
	}
}

// MigrateMetadata 使用exiftool迁移元数据
func (mm *MetadataManager) MigrateMetadata(sourcePath, targetPath string) error {
	// 开始元数据迁移

	// 检查exiftool是否可用
	if _, err := exec.LookPath(mm.config.Tools.ExiftoolPath); err != nil {
		return mm.errorHandler.WrapError("exiftool不可用", err)
	}

	// 使用exiftool迁移所有元数据
	// 命令: exiftool -TagsFromFile source -all:all target
	args := []string{
		"-TagsFromFile", sourcePath,
		"-all:all",
		"-overwrite_original",
		targetPath,
	}

	cmd := exec.Command(mm.config.Tools.ExiftoolPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return mm.errorHandler.WrapErrorWithOutput("exiftool元数据迁移失败", err, output)
	}

	// 元数据迁移完成

	return nil
}

// CopyMetadata 复制元数据（高级选项）
func (mm *MetadataManager) CopyMetadata(sourcePath, targetPath string, options ...MetadataCopyOption) error {
	// 开始复制元数据

	// 检查exiftool是否可用
	if _, err := exec.LookPath(mm.config.Tools.ExiftoolPath); err != nil {
		return mm.errorHandler.WrapError("exiftool不可用", err)
	}

	// 构建命令参数
	args := []string{
		"-TagsFromFile", sourcePath,
	}

	// 应用选项
	for _, option := range options {
		args = append(args, option.Args()...)
	}

	// 添加目标文件
	args = append(args, targetPath)

	cmd := exec.Command(mm.config.Tools.ExiftoolPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return mm.errorHandler.WrapErrorWithOutput("exiftool元数据复制失败", err, output)
	}

	// 元数据复制完成

	return nil
}

// MetadataCopyOption 元数据复制选项接口
type MetadataCopyOption interface {
	Args() []string
}

// MetadataType 元数据类型枚举 - 消除特殊情况的好品味设计
type MetadataType int

const (
	MetadataAll MetadataType = iota
	MetadataEXIF
	MetadataIPTC
	MetadataXMP
)

// metadataArgs 元数据类型到参数的映射 - 数据驱动，无条件分支
var metadataArgs = map[MetadataType][]string{
	MetadataAll:  {"-all:all"},
	MetadataEXIF: {"-EXIF:all"},
	MetadataIPTC: {"-IPTC:all"},
	MetadataXMP:  {"-XMP:all"},
}

// MetadataOption 统一的元数据选项 - 替代4个重复结构体
type MetadataOption struct {
	Type MetadataType
}

func (o MetadataOption) Args() []string {
	return metadataArgs[o.Type]
}

// 便利构造函数 - 保持API简洁
func CopyAllOption() MetadataOption  { return MetadataOption{MetadataAll} }
func CopyEXIFOption() MetadataOption { return MetadataOption{MetadataEXIF} }
func CopyIPTCOption() MetadataOption { return MetadataOption{MetadataIPTC} }
func CopyXMPOption() MetadataOption  { return MetadataOption{MetadataXMP} }

// ExcludeOption 排除特定元数据选项
type ExcludeOption struct {
	Tags []string
}

func (o ExcludeOption) Args() []string {
	var args []string
	for _, tag := range o.Tags {
		args = append(args, "-"+tag+"=")
	}
	return args
}

// OverwriteOption 覆盖选项
type OverwriteOption struct {
	Overwrite bool
}

func (o OverwriteOption) Args() []string {
	if o.Overwrite {
		return []string{"-overwrite_original"}
	}
	return []string{}
}

// PreserveDatesOption 保留日期选项
type PreserveDatesOption struct {
	Preserve bool
}

func (o PreserveDatesOption) Args() []string {
	if o.Preserve {
		return []string{"-FileModifyDate<DateTimeOriginal"}
	}
	return []string{}
}

// GetMetadata 获取文件元数据
func (mm *MetadataManager) GetMetadata(filePath string) (map[string]string, error) {
	// 获取文件元数据

	// 检查exiftool是否可用
	if _, err := exec.LookPath(mm.config.Tools.ExiftoolPath); err != nil {
		return nil, mm.errorHandler.WrapError("exiftool不可用", err)
	}

	// 使用exiftool获取元数据
	// 命令: exiftool -j -all:all file (JSON格式输出)
	args := []string{
		"-j",       // JSON输出格式
		"-all:all", // 所有元数据
		filePath,
	}

	cmd := exec.Command(mm.config.Tools.ExiftoolPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, mm.errorHandler.WrapError("获取元数据失败", err)
	}

	// 解析JSON输出
	var jsonData []map[string]interface{}
	if err := json.Unmarshal(output, &jsonData); err != nil {
		return nil, mm.errorHandler.WrapError("解析元数据JSON失败", err)
	}

	// 转换为字符串映射
	metadata := make(map[string]string)
	if len(jsonData) > 0 {
		for key, value := range jsonData[0] {
			metadata[key] = fmt.Sprint(value)
		}
	}

	// 元数据获取完成
	return metadata, nil
}

// ValidateMetadata 验证元数据完整性
func (mm *MetadataManager) ValidateMetadata(filePath string) error {
	// 验证元数据完整性

	// 检查exiftool是否可用
	if _, err := exec.LookPath(mm.config.Tools.ExiftoolPath); err != nil {
		return mm.errorHandler.WrapError("exiftool不可用", err)
	}

	// 使用exiftool验证文件
	// 命令: exiftool -validate file
	args := []string{
		"-validate",
		filePath,
	}

	cmd := exec.Command(mm.config.Tools.ExiftoolPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 检查是否是元数据问题而不是执行问题
		if strings.Contains(string(output), "Warning") || strings.Contains(string(output), "Error") {
			return mm.errorHandler.WrapErrorWithOutput("元数据验证失败", mm.errorHandler.WrapError("validation failed", nil), output)
		}
		// 如果只是执行错误，返回原始错误
		return mm.errorHandler.WrapErrorWithOutput("exiftool执行失败", err, output)
	}

	// 元数据验证完成
	return nil
}

// FileInfo 增强的文件信息结构
type FileInfo struct {
	Path         string            `json:"path"`
	Size         int64             `json:"size"`
	ModTime      time.Time         `json:"mod_time"`
	MimeType     string            `json:"mime_type"`
	Width        int               `json:"width,omitempty"`
	Height       int               `json:"height,omitempty"`
	ColorSpace   string            `json:"color_space,omitempty"`
	BitDepth     int               `json:"bit_depth,omitempty"`
	Compression  string            `json:"compression,omitempty"`
	Orientation  int               `json:"orientation,omitempty"`
	CreateDate   *time.Time        `json:"create_date,omitempty"`
	CameraModel  string            `json:"camera_model,omitempty"`
	LensModel    string            `json:"lens_model,omitempty"`
	ISO          int               `json:"iso,omitempty"`
	ExposureTime string            `json:"exposure_time,omitempty"`
	FNumber      string            `json:"f_number,omitempty"`
	GPSLatitude  string            `json:"gps_latitude,omitempty"`
	GPSLongitude string            `json:"gps_longitude,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ExtractFileInfo 提取增强的文件信息
func (mm *MetadataManager) ExtractFileInfo(filePath string) (*FileInfo, error) {
	// 提取文件信息

	// 获取基本文件信息
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, mm.errorHandler.WrapError("获取文件状态失败", err)
	}

	fileInfo := &FileInfo{
		Path:    filePath,
		Size:    stat.Size(),
		ModTime: stat.ModTime(),
	}

	// 获取详细元数据
	metadata, err := mm.GetMetadata(filePath)
	if err != nil {
		mm.logger.Warn("获取元数据失败，使用基本信息",
			zap.String("file", filePath),
			zap.Error(err))
		return fileInfo, nil
	}

	fileInfo.Metadata = metadata

	// 解析关键元数据字段
	mm.parseMetadataFields(fileInfo, metadata)

	// 文件信息提取完成

	return fileInfo, nil
}

// parseMetadataFields 解析元数据字段
func (mm *MetadataManager) parseMetadataFields(fileInfo *FileInfo, metadata map[string]string) {
	// MIME类型
	if mimeType, ok := metadata["MIMEType"]; ok {
		fileInfo.MimeType = mimeType
	}

	// 图像尺寸
	if width, ok := metadata["ImageWidth"]; ok {
		if w, err := strconv.Atoi(width); err == nil {
			fileInfo.Width = w
		}
	}
	if height, ok := metadata["ImageHeight"]; ok {
		if h, err := strconv.Atoi(height); err == nil {
			fileInfo.Height = h
		}
	}

	// 颜色空间
	if colorSpace, ok := metadata["ColorSpace"]; ok {
		fileInfo.ColorSpace = colorSpace
	}

	// 位深度
	if bitDepth, ok := metadata["BitsPerSample"]; ok {
		if bd, err := strconv.Atoi(strings.Split(bitDepth, " ")[0]); err == nil {
			fileInfo.BitDepth = bd
		}
	}

	// 压缩方式
	if compression, ok := metadata["Compression"]; ok {
		fileInfo.Compression = compression
	}

	// 方向
	if orientation, ok := metadata["Orientation"]; ok {
		if o, err := strconv.Atoi(orientation); err == nil {
			fileInfo.Orientation = o
		}
	}

	// 创建日期
	if createDate, ok := metadata["CreateDate"]; ok {
		if cd, err := time.Parse("2006:01:02 15:04:05", createDate); err == nil {
			fileInfo.CreateDate = &cd
		}
	}

	// 相机信息
	if camera, ok := metadata["Model"]; ok {
		fileInfo.CameraModel = camera
	}
	if lens, ok := metadata["LensModel"]; ok {
		fileInfo.LensModel = lens
	}

	// 拍摄参数
	if iso, ok := metadata["ISO"]; ok {
		if i, err := strconv.Atoi(iso); err == nil {
			fileInfo.ISO = i
		}
	}
	if exposureTime, ok := metadata["ExposureTime"]; ok {
		fileInfo.ExposureTime = exposureTime
	}
	if fNumber, ok := metadata["FNumber"]; ok {
		fileInfo.FNumber = fNumber
	}

	// GPS信息
	if gpsLat, ok := metadata["GPSLatitude"]; ok {
		fileInfo.GPSLatitude = gpsLat
	}
	if gpsLon, ok := metadata["GPSLongitude"]; ok {
		fileInfo.GPSLongitude = gpsLon
	}
}

// PreserveTimestamp 保留文件时间戳（增强版）
func (mm *MetadataManager) PreserveTimestamp(sourcePath, targetPath string) error {
	// 保留文件时间戳

	// 获取源文件信息
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return mm.errorHandler.WrapError("获取源文件信息失败", err)
	}

	// 验证目标文件存在
	if _, err := os.Stat(targetPath); err != nil {
		return mm.errorHandler.WrapError("目标文件不存在", err)
	}

	// 记录原始时间戳
	originalModTime := sourceInfo.ModTime()
	originalAccessTime := sourceInfo.ModTime() // 使用修改时间作为访问时间

	// 设置目标文件时间戳（使用高精度时间戳）
	if err := os.Chtimes(targetPath, originalAccessTime, originalModTime); err != nil {
		return mm.errorHandler.WrapError("设置文件时间戳失败", err)
	}

	// 验证时间戳设置是否成功
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return mm.errorHandler.WrapError("验证目标文件信息失败", err)
	}

	// 检查时间戳精度（允许1秒误差）
	timeDiff := targetInfo.ModTime().Unix() - originalModTime.Unix()
	if timeDiff > 1 || timeDiff < -1 {
		mm.logger.Warn("时间戳精度警告",
			zap.String("target", targetPath),
			zap.Time("expected", originalModTime),
			zap.Time("actual", targetInfo.ModTime()),
			zap.Int64("diff_seconds", timeDiff))
	}

	// 文件时间戳保留成功

	return nil
}

// PreserveTimestampWithValidation 带验证的时间戳保留
func (mm *MetadataManager) PreserveTimestampWithValidation(sourcePath, targetPath string, tolerance time.Duration) error {
	// 带验证的时间戳保留

	// 执行基本时间戳保留
	if err := mm.PreserveTimestamp(sourcePath, targetPath); err != nil {
		return err
	}

	// 获取源文件和目标文件信息进行验证
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return mm.errorHandler.WrapError("获取源文件信息失败", err)
	}

	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return mm.errorHandler.WrapError("获取目标文件信息失败", err)
	}

	// 精确验证时间戳
	timeDiff := targetInfo.ModTime().Sub(sourceInfo.ModTime())
	if timeDiff > tolerance || timeDiff < -tolerance {
		return mm.errorHandler.WrapError("时间戳验证失败",
			fmt.Errorf("时间差超出容忍范围: %v (容忍: %v)", timeDiff, tolerance))
	}

	// 时间戳验证通过

	return nil
}

// CompareMetadata 比较两个文件的元数据（增强版）
func (mm *MetadataManager) CompareMetadata(file1, file2 string) (map[string]interface{}, error) {
	// 比较文件元数据

	// 获取两个文件的基本信息
	info1, err := os.Stat(file1)
	if err != nil {
		return nil, mm.errorHandler.WrapError("获取文件1信息失败", err)
	}

	info2, err := os.Stat(file2)
	if err != nil {
		return nil, mm.errorHandler.WrapError("获取文件2信息失败", err)
	}

	// 获取两个文件的元数据
	metadata1, err := mm.GetMetadata(file1)
	if err != nil {
		return nil, mm.errorHandler.WrapError("获取文件1元数据失败", err)
	}

	metadata2, err := mm.GetMetadata(file2)
	if err != nil {
		return nil, mm.errorHandler.WrapError("获取文件2元数据失败", err)
	}

	// 比较结果
	comparison := map[string]interface{}{
		"file1":            filepath.Base(file1),
		"file2":            filepath.Base(file2),
		"identical":        true,
		"differences":      make(map[string]map[string]string),
		"missing_in_file1": make([]string, 0),
		"missing_in_file2": make([]string, 0),
		"file_info": map[string]interface{}{
			"file1": map[string]interface{}{
				"size":           info1.Size(),
				"mod_time":       info1.ModTime(),
				"metadata_count": len(metadata1),
			},
			"file2": map[string]interface{}{
				"size":           info2.Size(),
				"mod_time":       info2.ModTime(),
				"metadata_count": len(metadata2),
			},
		},
		"timestamp_comparison": map[string]interface{}{
			"timestamps_match":  info1.ModTime().Equal(info2.ModTime()),
			"time_diff_seconds": info2.ModTime().Sub(info1.ModTime()).Seconds(),
		},
	}

	differences := comparison["differences"].(map[string]map[string]string)
	missingInFile1 := comparison["missing_in_file1"].([]string)
	missingInFile2 := comparison["missing_in_file2"].([]string)

	// 检查file1中的字段
	for key, value1 := range metadata1 {
		if value2, exists := metadata2[key]; exists {
			if value1 != value2 {
				differences[key] = map[string]string{
					"file1": value1,
					"file2": value2,
				}
				comparison["identical"] = false
			}
		} else {
			missingInFile2 = append(missingInFile2, key)
			comparison["identical"] = false
		}
	}

	// 检查file2中独有的字段
	for key := range metadata2 {
		if _, exists := metadata1[key]; !exists {
			missingInFile1 = append(missingInFile1, key)
			comparison["identical"] = false
		}
	}

	comparison["missing_in_file1"] = missingInFile1
	comparison["missing_in_file2"] = missingInFile2

	// 检查时间戳是否匹配
	timestampsMatch := comparison["timestamp_comparison"].(map[string]interface{})["timestamps_match"].(bool)
	if !timestampsMatch {
		comparison["identical"] = false
	}

	// 元数据比较完成

	return comparison, nil
}

// ValidateTimestampPreservation 验证时间戳保留功能
func (mm *MetadataManager) ValidateTimestampPreservation(originalPath, processedPath string, tolerance time.Duration) (bool, error) {
	// 验证时间戳保留

	// 获取原始文件信息
	originalInfo, err := os.Stat(originalPath)
	if err != nil {
		return false, mm.errorHandler.WrapError("获取原始文件信息失败", err)
	}

	// 获取处理后文件信息
	processedInfo, err := os.Stat(processedPath)
	if err != nil {
		return false, mm.errorHandler.WrapError("获取处理后文件信息失败", err)
	}

	// 计算时间差
	timeDiff := processedInfo.ModTime().Sub(originalInfo.ModTime())
	isPreserved := timeDiff <= tolerance && timeDiff >= -tolerance

	// 时间戳保留验证结果

	return isPreserved, nil
}
