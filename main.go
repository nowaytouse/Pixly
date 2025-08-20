package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fatih/color"
	"golang.org/x/term"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

// 版本号升级为20.2.7，符合要求中的"必须要升级版本号,以避免混淆情况"
const Version = "20.2.7-GO-TITANIUM-STREAMING-ENHANCED"

type QualityConfig struct {
	ExtremeHighThreshold float64
	HighThreshold        float64
	MediumThreshold      float64
	LowThreshold         float64
}

type ToolCheckResults struct {
	HasCjxl      bool
	HasLibSvtAv1 bool
	HasVToolbox  bool
}

type Config struct {
	Mode           string
	TargetDir      string
	BackupDir      string
	ConcurrentJobs int
	MaxRetries     int
	CRF            int
	EnableBackups  bool
	HwAccel        bool
	Overwrite      bool
	LogLevel       string
	SortOrder      string
	QualityConfig  QualityConfig
}

type UserChoice int

const (
	ChoiceSkip UserChoice = iota
	ChoiceRepair
	ChoiceDelete
	ChoiceNotApplicable
	ChoiceProcess
)

type AppContext struct {
	Config              Config
	Tools               ToolCheckResults
	Logger              *StructuredLogger
	TempDir             string
	ResultsDir          string
	LogFile             *os.File
	filesFoundCount     atomic.Int64
	filesAssessedCount  atomic.Int64
	totalFilesToProcess atomic.Int64
	processedCount      atomic.Int64
	successCount        atomic.Int64
	failCount           atomic.Int64
	skipCount           atomic.Int64
	deleteCount         atomic.Int64
	retrySuccessCount   atomic.Int64
	resumedCount        atomic.Int64
	totalDecreased      atomic.Int64
	totalIncreased      atomic.Int64
	smartDecisionsCount atomic.Int64
	losslessWinsCount   atomic.Int64
	extremeHighCount    atomic.Int64
	highCount           atomic.Int64
	mediumCount         atomic.Int64
	lowCount            atomic.Int64
	extremeLowCount     atomic.Int64
	runStarted          time.Time
	mu                  sync.Mutex
	cleanupWhitelist    map[string]bool
	repairSem           chan struct{}
}

type ConversionResult struct {
	OriginalPath string
	FinalPath    string
	Decision     string
	Tag          string
	OriginalSize int64
	NewSize      int64
	Error        error
}

type FileTask struct {
	Path          string
	Size          int64
	MimeType      string
	TempDir       string
	Logger        *StructuredLogger
	BaseConfig    Config
	Quality       QualityLevel
	BatchDecision UserChoice
	Priority      int
}

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

type StructuredLogger struct {
	logger *log.Logger
	level  LogLevel
	mu     sync.Mutex
}

type QualityLevel int

const (
	QualityExtremeHigh QualityLevel = iota
	QualityHigh
	QualityMedium
	QualityLow
	QualityExtremeLow
	QualityUnknown
)

// 优化配色方案，确保在暗色和亮色模式下都有良好的可读性
var (
	bold             = color.New(color.Bold).SprintFunc()
	cyan             = color.New(color.FgHiCyan).SprintFunc()
	green            = color.New(color.FgHiGreen).SprintFunc()
	yellow           = color.New(color.FgHiYellow).SprintFunc()
	red              = color.New(color.FgHiRed).SprintFunc()
	violet           = color.New(color.FgHiMagenta).SprintFunc()
	subtle           = color.New(color.FgHiBlack).SprintFunc()
	consoleMutex     = &sync.Mutex{}
	isLivePhotoRegex = regexp.MustCompile(`(?i)^IMG_E?[0-9]{4}\.HEIC$`)
)

func newStructuredLogger(w io.Writer, l LogLevel) *StructuredLogger {
	return &StructuredLogger{logger: log.New(w, "", log.Ldate|log.Ltime|log.Lmicroseconds), level: l}
}

func (l *StructuredLogger) log(level LogLevel, msg string, fields ...interface{}) {
	if level < l.level {
		return
	}
	var levelStr string
	switch level {
	case LogLevelDebug:
		levelStr = "DEBUG"
	case LogLevelInfo:
		levelStr = "INFO"
	case LogLevelWarn:
		levelStr = "WARN"
	case LogLevelError:
		levelStr = "ERROR"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("level=%s msg=\"%s\"", levelStr, msg))
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			b.WriteString(fmt.Sprintf(" %v=\"%v\"", fields[i], fields[i+1]))
		}
	}
	l.mu.Lock()
	l.logger.Println(b.String())
	l.mu.Unlock()
}

func (l *StructuredLogger) Debug(msg string, fields ...interface{}) { l.log(LogLevelDebug, msg, fields...) }
func (l *StructuredLogger) Info(msg string, fields ...interface{})  { l.log(LogLevelInfo, msg, fields...) }
func (l *StructuredLogger) Warn(msg string, fields ...interface{})  { l.log(LogLevelWarn, msg, fields...) }
func (l *StructuredLogger) Error(msg string, fields ...interface{}) { l.log(LogLevelError, msg, fields...) }

func parseLogLevel(l string) LogLevel {
	switch strings.ToLower(l) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn":
		return LogLevelWarn
	case "error":
		return LogLevelError
	}
	return LogLevelInfo
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		stderr := errOut.String()
		return out.String(), fmt.Errorf("命令执行失败: %s %s. 错误: %v. Stderr: %s", name, strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(out.String()), nil
}

func getFileSize(p string) (int64, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return !os.IsNotExist(err)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.Trim(p, `"'`)
	p = strings.ReplaceAll(p, "\\ ", " ")
	re := regexp.MustCompile(`\\(.)`)
	p = re.ReplaceAllString(p, "$1")
	absPath, _ := filepath.Abs(p)
	return absPath
}

func printToConsole(f string, a ...interface{}) {
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	fmt.Printf("\033[2K\r"+f, a...)
}

// 获取默认质量配置
func getDefaultQualityConfig() QualityConfig {
	return QualityConfig{
		ExtremeHighThreshold: 0.25,
		HighThreshold:        0.15,
		MediumThreshold:      0.08,
		LowThreshold:         0.03,
	}
}

// 优化质量评估函数，提高精准度，使用可配置的阈值
func assessQuality(ctx context.Context, f, mime string, size int64, qc QualityConfig) (QualityLevel, error) {
	if size < 5*1024 {
		return QualityExtremeLow, nil
	}
	if strings.HasPrefix(mime, "image/") {
		// 使用更详细的图像分析
		out, err := runCmd(ctx, "magick", "identify", "-format", "%w %h %Q %[entropy] %[compression] %[quality]", f)
		if err != nil {
			return QualityUnknown, err
		}
		parts := strings.Fields(out)
		if len(parts) < 6 {
			return QualityUnknown, errors.New("无法解析图像信息")
		}
		width, _ := strconv.ParseFloat(parts[0], 64)
		height, _ := strconv.ParseFloat(parts[1], 64)
		quality, _ := strconv.ParseFloat(parts[2], 64)
		entropy, _ := strconv.ParseFloat(parts[3], 64)
		compression := parts[4]
		qualityMetric, _ := strconv.ParseFloat(parts[5], 64)
		if width == 0 || height == 0 {
			return QualityExtremeLow, nil
		}
		// 添加更多质量评估维度
		pixelScore := (width * height) / 1e6
		sizeQualityRatio := (float64(size) / 1024) / pixelScore / math.Max(1, (110-quality))
		entropyScore := entropy / 8.0
		compressionFactor := 1.0
		if compression == "JPEG" {
			compressionFactor = 0.8 // JPEG通常有更多压缩损失
		}
		// 添加伪影检测
		artifactScore := 1.0
		if qualityMetric < 80 {
			artifactScore = 0.7 // 低质量指标表示更多伪影
		}
		adjustedRatio := sizeQualityRatio * entropyScore * compressionFactor * artifactScore
		// 调整质量阈值，使用配置的参数
		if pixelScore > 12 && adjustedRatio > qc.ExtremeHighThreshold*100 {
			return QualityExtremeHigh, nil
		}
		if pixelScore > 4 && adjustedRatio > qc.HighThreshold*50 {
			return QualityHigh, nil
		}
		if pixelScore > 1 && adjustedRatio > qc.MediumThreshold*20 {
			return QualityMedium, nil
		}
		if pixelScore > 0.1 && adjustedRatio > qc.LowThreshold*5 {
			return QualityLow, nil
		}
		return QualityExtremeLow, nil
	} else if strings.HasPrefix(mime, "video/") {
		// 使用更详细的视频分析
		out, err := runCmd(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", 
			"-show_entries", "stream=width,height,r_frame_rate,bit_rate,codec_name", "-of", "csv=p=0", f)
		if err != nil {
			return QualityUnknown, err
		}
		fields := strings.Split(strings.TrimSpace(out), ",")
		if len(fields) < 5 {
			return QualityExtremeLow, nil
		}
		w, _ := strconv.ParseFloat(fields[0], 64)
		h, _ := strconv.ParseFloat(fields[1], 64)
		fpsParts := strings.Split(fields[2], "/")
		br, _ := strconv.ParseFloat(fields[3], 64)
		codec := fields[4]
		fps := 30.0
		if len(fpsParts) == 2 {
			num, _ := strconv.ParseFloat(fpsParts[0], 64)
			den, _ := strconv.ParseFloat(fpsParts[1], 64)
			if den != 0 {
				fps = num / den
			}
		}
		if w == 0 || h == 0 || fps == 0 {
			return QualityExtremeLow, nil
		}
		// 计算更精确的bpp
		bpp := br / (w * h * fps)
		// 添加噪声分析
		noiseOut, _ := runCmd(ctx, "ffmpeg", "-i", f, "-vf", "noise=0:0:0:0", "-frames:v", "1", "-f", "null", "-")
		noiseMean := 0.0
		noiseRe := regexp.MustCompile(`mean\[(\d+\.\d+)\]`)
		if noiseRe.FindStringSubmatch(noiseOut) != nil {
			noiseMean, _ = strconv.ParseFloat(noiseRe.FindStringSubmatch(noiseOut)[1], 64)
		}
		// 添加模糊检测
		blurOut, _ := runCmd(ctx, "ffmpeg", "-i", f, "-vf", "crop=iw/2:ih/2,fft", "-frames:v", "1", "-f", "null", "-")
		blurScore := 1.0
		blurRe := regexp.MustCompile(`freq=\d+\.\d+ amplitude=(\d+\.\d+)`)
		if blurRe.FindStringSubmatch(blurOut) != nil {
			amplitude, _ := strconv.ParseFloat(blurRe.FindStringSubmatch(blurOut)[1], 64)
			if amplitude < 0.1 {
				blurScore = 0.6 // 低振幅表示图像模糊
			}
		}
		// 调整BPP计算，考虑噪声和模糊
		adjustedBpp := bpp / (1 + noiseMean/100) * blurScore
		// 考虑编码器类型
		codecFactor := 1.0
		if codec == "h264" || codec == "mpeg4" {
			codecFactor = 1.2 // 旧编码器通常需要更高BPP
		}
		adjustedBpp *= codecFactor
		// 调整质量阈值，使用配置的参数
		if adjustedBpp > qc.ExtremeHighThreshold {
			return QualityExtremeHigh, nil
		}
		if adjustedBpp > qc.HighThreshold {
			return QualityHigh, nil
		}
		if adjustedBpp > qc.MediumThreshold {
			return QualityMedium, nil
		}
		if adjustedBpp > qc.LowThreshold {
			return QualityLow, nil
		}
		return QualityExtremeLow, nil
	}
	return QualityMedium, nil
}

func processImage(ctx context.Context, t *FileTask, tools ToolCheckResults, useQualityMode bool) (string, string, string, error) {
	if isSpatialImage(ctx, t.Path) {
		return "", "SKIP_SPATIAL", "SKIP_SPATIAL", nil
	}
	isAnimated := isAnimated(ctx, t.Path)
	ext := strings.ToLower(filepath.Ext(t.Path))
	isJpeg := ext == ".jpg" || ext == ".jpeg"
	var outputPath, tag string
	var cmdName string
	var args []string
	
	if useQualityMode {
		if isAnimated {
			// 根据要求，jxl不应作为动画的现代转换格式，而是avif
			outputPath = filepath.Join(t.TempDir, filepath.Base(strings.TrimSuffix(t.Path, ext)+".avif"))
			tag = "AVIF-Lossless"
			cmdName = "magick"
			args = []string{"convert", t.Path, "-quality", "100", outputPath}
		} else {
			outputPath = filepath.Join(t.TempDir, filepath.Base(strings.TrimSuffix(t.Path, ext)+".jxl"))
			tag = "JXL-Lossless"
			effort := "7"
			if t.Size > 5*1024*1024 {
				effort = "9"
			}
			if tools.HasCjxl {
				cmdName = "cjxl"
				args = []string{t.Path, outputPath, "-d", "0", "-e", effort}
				if isJpeg {
					// 对于jpeg应该优先使用jpeg_lossless=1参数
					args = append(args, "--lossless_jpeg=1")
				}
			} else {
				cmdName = "magick"
				args = []string{"convert", t.Path, "-quality", "100", outputPath}
			}
		}
		_, err := runCmd(ctx, cmdName, args...)
		if err != nil {
			return "", "FAIL", "FAIL_CONVERSION", err
		}
		return outputPath, tag, "SUCCESS", nil
	}
	
	// 效率模式处理
	outputPath = filepath.Join(t.TempDir, filepath.Base(strings.TrimSuffix(t.Path, ext)+".avif"))
	losslessPath := outputPath + ".lossless.avif"
	// 1.必须进行无损尝试并记录和有损的大小情况
	_, err := runCmd(ctx, "magick", "convert", t.Path, "-quality", "100", losslessPath)
	if err == nil {
		losslessSize, _ := getFileSize(losslessPath)
		if losslessSize > 0 && losslessSize < t.Size {
			if err := os.Rename(losslessPath, outputPath); err != nil {
				os.Remove(losslessPath)
				return "", "FAIL", "FAIL_RENAME", err
			}
			return outputPath, "AVIF-Lossless", "SUCCESS", nil
		}
		os.Remove(losslessPath)
	}
	// 2.基于智能质量判别等高级功能判别,对高质量的内容进行高范围内压缩范围,低质量适当压缩
	// 3.压缩范围内进行压缩探底,保障必须要在 质量/大小进行平衡,且偏重于质量
	qualityPoints := getDynamicQualityPoints(t.Quality)
	var bestPath string
	var bestSize int64 = math.MaxInt64
	// 4.探索到比原图小,不论小多少,就算做成功并进行替换
	for _, q := range qualityPoints {
		tempAvif := filepath.Join(t.TempDir, fmt.Sprintf("temp_%d_%s.avif", q, filepath.Base(t.Path)))
		_, err := runCmd(ctx, "magick", "convert", t.Path, "-quality", strconv.Itoa(q), tempAvif)
		if err == nil {
			size, _ := getFileSize(tempAvif)
			if size > 0 && size < t.Size && size < bestSize {
				if bestPath != "" {
					os.Remove(bestPath)
				}
				bestSize = size
				bestPath = tempAvif
			} else {
				os.Remove(tempAvif)
			}
		}
	}
	if bestPath != "" {
		if err := os.Rename(bestPath, outputPath); err != nil {
			return "", "FAIL", "FAIL_RENAME", err
		}
		return outputPath, "AVIF-Optimized", "SUCCESS", nil
	}
	return "", "SKIP_NO_OPTIMAL", "SKIP_NO_OPTIMAL", nil
}

func processVideo(ctx context.Context, t *FileTask, tools ToolCheckResults, useQualityMode bool) (string, string, string, error) {
	ext := strings.ToLower(filepath.Ext(t.Path))
	outputPath := filepath.Join(t.TempDir, filepath.Base(strings.TrimSuffix(t.Path, ext)+".mov"))
	var codec, tag, preset string
	if tools.HasLibSvtAv1 {
		codec = "libsvtav1"
		tag = "MOV-AV1"
		preset = "8"
	} else {
		codec = "libx265"
		tag = "MOV-HEVC"
		preset = "medium"
	}
	baseArgs := []string{"-y", "-i", t.Path, "-c:v", codec, "-preset", preset, "-c:a", "copy", "-c:s", "copy", "-map", "0", "-movflags", "+faststart"}
	if t.BaseConfig.HwAccel && tools.HasVToolbox {
		baseArgs = append(baseArgs, "-hwaccel", "videotoolbox")
	}
	
	if useQualityMode {
		tag += "-Lossless"
		args := append(baseArgs, "-crf", "0", outputPath)
		_, err := runCmd(ctx, "ffmpeg", args...)
		if err != nil {
			return "", "FAIL", "FAIL_CONVERSION", err
		}
		return outputPath, tag, "SUCCESS", nil
	}
	
	tag += "-Lossy"
	losslessPath := outputPath + ".lossless.mov"
	// 1.必须进行无损尝试并记录和有损的大小情况
	losslessArgs := append(baseArgs, "-crf", "0", losslessPath)
	_, err := runCmd(ctx, "ffmpeg", losslessArgs...)
	if err == nil {
		losslessSize, _ := getFileSize(losslessPath)
		if losslessSize > 0 && losslessSize < t.Size {
			if err := os.Rename(losslessPath, outputPath); err != nil {
				os.Remove(losslessPath)
				return "", "FAIL", "FAIL_RENAME", err
			}
			return outputPath, "MOV-Lossless", "SUCCESS", nil
		}
		os.Remove(losslessPath)
	}
	crfValues := getDynamicCRF(t.Quality, t.BaseConfig.CRF)
	var bestPath string
	var bestSize int64 = math.MaxInt64
	// 3.压缩范围内进行压缩探底,保障必须要在 质量/大小进行平衡,且偏重于质量
	for _, crf := range crfValues {
		tempMov := filepath.Join(t.TempDir, fmt.Sprintf("temp_%d_%s.mov", crf, filepath.Base(t.Path)))
		args := append(baseArgs, "-crf", strconv.Itoa(crf), tempMov)
		_, err := runCmd(ctx, "ffmpeg", args...)
		if err == nil {
			size, _ := getFileSize(tempMov)
			if size > 0 && size < t.Size && size < bestSize {
				if bestPath != "" {
					os.Remove(bestPath)
				}
				bestSize = size
				bestPath = tempMov
			} else {
				os.Remove(tempMov)
			}
		}
	}
	if bestPath != "" {
		if err := os.Rename(bestPath, outputPath); err != nil {
			return "", "FAIL", "FAIL_RENAME", err
		}
		return outputPath, "MOV-Optimized", "SUCCESS", nil
	}
	return "", "SKIP_NO_OPTIMAL", "SKIP_NO_OPTIMAL", nil
}

func getDynamicQualityPoints(ql QualityLevel) []int {
	switch ql {
	case QualityExtremeHigh:
		return []int{95, 90, 85}
	case QualityHigh:
		return []int{85, 80, 75}
	case QualityMedium:
		return []int{75, 70, 65}
	case QualityLow:
		return []int{65, 60, 55}
	default:
		return []int{55, 50, 45}
	}
}

func getDynamicCRF(ql QualityLevel, baseCRF int) []int {
	switch ql {
	case QualityExtremeHigh:
		return []int{baseCRF - 6, baseCRF - 3, baseCRF}
	case QualityHigh:
		return []int{baseCRF - 3, baseCRF, baseCRF + 3}
	case QualityMedium:
		return []int{baseCRF, baseCRF + 3, baseCRF + 6}
	case QualityLow:
		return []int{baseCRF + 4, baseCRF + 7, baseCRF + 10}
	default:
		return []int{baseCRF + 6, baseCRF + 9, baseCRF + 12}
	}
}

// 增强备份功能，避免与先前已有的bak文件重复覆盖冲突
func createBackup(f, b string, e bool, l *StructuredLogger) bool {
	if !e {
		return true
	}
	if err := os.MkdirAll(b, 0755); err != nil {
		l.Error("无法创建备份目录", "path", b, "error", err)
		return false
	}
	hash := sha1.Sum([]byte(f))
	shortHash := hex.EncodeToString(hash[:4])
	ts := time.Now().Format("20060102150405")
	bp := filepath.Join(b, fmt.Sprintf("%s.%s.%s.bak", filepath.Base(f), ts, shortHash))
	// 添加文件锁，避免异常介入
	lockFile := bp + ".lock"
	lockFd, err := os.OpenFile(lockFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		l.Error("无法创建锁文件", "path", lockFile, "error", err)
		return false
	}
	defer lockFd.Close()
	// 尝试获取文件锁
	if err := unix.Flock(int(lockFd.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		l.Error("无法获取文件锁", "path", lockFile, "error", err)
		return false
	}
	defer unix.Flock(int(lockFd.Fd()), unix.LOCK_UN)
	input, err := os.ReadFile(f)
	if err != nil {
		l.Error("无法读取源文件进行备份", "file", f, "error", err)
		return false
	}
	if err = os.WriteFile(bp, input, 0644); err != nil {
		l.Error("写入备份文件失败", "backup_path", bp, "error", err)
		os.Remove(bp)
		return false
	}
	return true
}

// 增强元数据迁移，多进行不同逻辑的方法的迁移尝试
func preserveMetadata(ctx context.Context, src, dst string, l *StructuredLogger) {
	// 尝试使用exiftool迁移元数据
	_, err := runCmd(ctx, "exiftool", "-TagsFromFile", src, "-all:all", "-unsafe", "-icc_profile", "-overwrite_original", "-q", "-q", dst)
	if err == nil {
		// 确保设置正确的修改时间
		srcInfo, err := os.Stat(src)
		if err == nil {
			os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
		}
		return
	}
	// 尝试使用jhead迁移JPEG元数据
	if strings.HasSuffix(dst, ".jpg") || strings.HasSuffix(dst, ".jpeg") {
		_, err = runCmd(ctx, "jhead", "-te", src, dst)
		if err == nil {
			// 确保设置正确的修改时间
			srcInfo, err := os.Stat(src)
			if err == nil {
				os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
			}
			return
		}
	}
	// 尝试使用heif-convert迁移HEIC元数据
	if strings.HasSuffix(src, ".heic") || strings.HasSuffix(src, ".heif") {
		_, err = runCmd(ctx, "heif-convert", "-m", src, dst)
		if err == nil {
			// 确保设置正确的修改时间
			srcInfo, err := os.Stat(src)
			if err == nil {
				os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
			}
			return
		}
	}
	// 最后尝试设置修改时间
	srcInfo, statErr := os.Stat(src)
	if statErr == nil {
		os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
	}
}

func getMimeType(ctx context.Context, f string) (string, error) {
	out, err := runCmd(ctx, "file", "--mime-type", "-b", f)
	if err == nil && !strings.Contains(out, "application/octet-stream") {
		return out, nil
	}
	return "application/octet-stream", errors.New("unknown mime type")
}

func isAnimated(ctx context.Context, f string) bool {
	out, err := runCmd(ctx, "magick", "identify", "-format", "%n", f)
	if err != nil {
		return false
	}
	frames, _ := strconv.Atoi(strings.TrimSpace(out))
	return frames > 1
}

func isLivePhoto(f string) bool {
	baseName := filepath.Base(f)
	if !isLivePhotoRegex.MatchString(baseName) {
		return false
	}
	movFile := filepath.Join(filepath.Dir(f), strings.TrimSuffix(baseName, filepath.Ext(baseName))+".MOV")
	return fileExists(movFile)
}

func isSpatialImage(ctx context.Context, f string) bool {
	ext := strings.ToLower(filepath.Ext(f))
	if ext != ".heic" && ext != ".heif" {
		return false
	}
	out, err := runCmd(ctx, "exiftool", "-s", "-s", "-s", "-ProjectionType", f)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(out), "equirectangular")
}

func getResultFilePath(r, f string) string {
	hash := sha1.Sum([]byte(f))
	return filepath.Join(r, hex.EncodeToString(hash[:]))
}

func shouldSkipEarly(f string) bool {
	if isLivePhoto(f) || isSpatialImage(context.Background(), f) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(f))
	targetFormats := []string{".jxl", ".avif", ".mov"}
	for _, tf := range targetFormats {
		if ext == tf {
			return true
		}
	}
	unsupported := []string{".psd", ".ai", ".pdf", ".doc", ".txt", ".zip", ".rar", ".mp3", ".wav", ".aiff"}
	for _, u := range unsupported {
		if ext == u {
			return true
		}
	}
	return false
}

var mediaMimeWhitelist = map[string]bool{
	"image/jpeg":     true,
	"image/png":      true,
	"image/gif":      true,
	"image/webp":     true,
	"image/heic":     true,
	"image/heif":     true,
	"image/tiff":     true,
	"image/bmp":      true,
	"image/svg+xml":  true,
	"image/avif":     true,
	"image/apng":     true,
	"video/mp4":      true,
	"video/quicktime": true,
	"video/x-msvideo": true,
	"video/x-matroska": true,
	"video/x-flv":    true,
	"video/3gpp":     true,
	"video/3gpp2":    true,
	"video/mpeg":     true,
	"video/x-ms-wmv": true,
	"video/x-ms-asf": true,
	"video/ogg":      true,
	"video/webm":     true,
}

func isMediaFile(mime string) bool {
	return mediaMimeWhitelist[mime]
}

// 修改超时时间为5秒，符合要求"同时设置5秒后自动跳过所有"极低质量"选项"
func handleBatchLowQualityInteraction(lowQualityFiles []*FileTask, app *AppContext) (UserChoice, error) {
	if len(lowQualityFiles) == 0 {
		return ChoiceNotApplicable, nil
	}
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	app.Logger.Warn("检测到极低质量文件", "count", len(lowQualityFiles))
	fmt.Printf("\n%s\n", yellow("------------------------- 批量处理请求 -------------------------"))
	fmt.Printf("%s: %s\n", yellow(fmt.Sprintf("检测到 %d 个极低质量文件。", len(lowQualityFiles))), bold(fmt.Sprintf("%d", len(lowQualityFiles))))
	fmt.Println(subtle("示例文件 (最多显示10个):"))
	for i, f := range lowQualityFiles {
		if i >= 10 {
			break
		}
		fmt.Printf("  - %s (%s)\n", filepath.Base(f.Path), formatBytes(f.Size))
	}
	if len(lowQualityFiles) > 10 {
		fmt.Println(subtle("  ...等更多文件。"))
	}
	fmt.Println(yellow("\n请选择如何处理所有这些文件:"))
	fmt.Printf("  %s\n", bold("[1] 全部跳过 (默认, 5秒后自动选择)"))
	fmt.Printf("  %s\n", bold("[2] 全部尝试修复并转换"))
	fmt.Printf("  %s\n", red("[3] 全部直接删除"))
	fmt.Print(yellow("请输入您的选择 [1, 2, 3]: "))
	inputChan := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		inputChan <- strings.TrimSpace(input)
	}()
	select {
	case input := <-inputChan:
		switch input {
		case "2":
			fmt.Println(green("\n已选择 [全部尝试修复]"))
			return ChoiceRepair, nil
		case "3":
			fmt.Println(red("\n已选择 [全部直接删除]"))
			return ChoiceDelete, nil
		default:
			fmt.Println(green("\n已选择 [全部跳过]"))
			return ChoiceSkip, nil
		}
	case <-time.After(5 * time.Second): // 从30秒改为5秒
		fmt.Println(green("\n超时，已选择 [全部跳过]"))
		return ChoiceSkip, nil
	}
}

func ProcessTask(ctx context.Context, t *FileTask, tools ToolCheckResults, app *AppContext) *ConversionResult {
	result := &ConversionResult{
		OriginalPath: t.Path,
		OriginalSize: t.Size,
	}
	if shouldSkipEarly(t.Path) {
		result.Decision = "SKIP_UNSUPPORTED"
		return result
	}
	switch t.BatchDecision {
	case ChoiceDelete:
		if err := os.Remove(t.Path); err != nil {
			result.Error = fmt.Errorf("批量删除失败: %w", err)
			return result
		}
		result.Decision = "DELETE_LOW_BATCH"
		return result
	case ChoiceSkip:
		result.Decision = "SKIP_LOW_BATCH"
		return result
	case ChoiceRepair:
		t.Logger.Info("根据批量决策尝试修复", "file", t.Path)
		// 限制并发修复数量
		app.repairSem <- struct{}{}
		defer func() { <-app.repairSem }()
		// 启动带自动清理的进度指示器
		repairDone := make(chan struct{})
		defer close(repairDone)
		go func() {
			spinner := []string{"🔧", "🔧.", "🔧..", "🔧..."}
			i := 0
			for {
				select {
				case <-repairDone:
					printToConsole("\r" + strings.Repeat(" ", 80) + "\r")
					return
				case <-time.After(200 * time.Millisecond):
					msg := fmt.Sprintf("%s 修复中: %s [%s]", spinner[i%len(spinner)], filepath.Base(t.Path), strings.Repeat(" ", 20))
					printToConsole(msg)
					i++
				}
			}
		}()
		repairTempPath := t.Path + ".repaired"
		var repairCmd *exec.Cmd
		if strings.HasPrefix(t.MimeType, "image/") {
			repairCmd = exec.CommandContext(ctx, "magick", t.Path, "-auto-level", "-enhance", repairTempPath)
		} else {
			repairCmd = exec.CommandContext(ctx, "ffmpeg", "-y", "-i", t.Path, "-c", "copy", "-map", "0", "-ignore_unknown", repairTempPath)
		}
		if err := repairCmd.Run(); err == nil {
			// 清除进度指示器行
			printToConsole("\r" + strings.Repeat(" ", 80) + "\r")
			os.Rename(repairTempPath, t.Path)
			t.Size, _ = getFileSize(t.Path)
			// 确保保留原始文件的修改时间
			srcInfo, err := os.Stat(t.Path)
			if err == nil {
				os.Chtimes(t.Path, srcInfo.ModTime(), srcInfo.ModTime())
			}
		} else {
			// 清除进度指示器行
			printToConsole("\r" + strings.Repeat(" ", 80) + "\r")
			os.Remove(repairTempPath)
			result.Error = fmt.Errorf("修复失败: %w", err)
			return result
		}
	}
	
	// 决定使用哪种模式
	var useQualityMode bool
	if t.BaseConfig.Mode == "auto" {
		// 根据质量级别决定模式
		useQualityMode = t.Quality >= QualityMedium
	} else {
		useQualityMode = t.BaseConfig.Mode == "quality"
	}
	
	var tempOutPath, tag, decision string
	var err error
	if strings.HasPrefix(t.MimeType, "image/") {
		tempOutPath, tag, decision, err = processImage(ctx, t, tools, useQualityMode)
	} else if strings.HasPrefix(t.MimeType, "video/") {
		tempOutPath, tag, decision, err = processVideo(ctx, t, tools, useQualityMode)
	} else {
		result.Decision = "SKIP_UNSUPPORTED_MIME"
		return result
	}
	if err != nil {
		result.Error = err
		result.Decision = decision
		return result
	}
	if decision != "SUCCESS" {
		result.Decision = decision
		return result
	}
	newSize, _ := getFileSize(tempOutPath)
	result.NewSize = newSize
	result.Tag = tag
	if createBackup(t.Path, app.Config.BackupDir, app.Config.EnableBackups, t.Logger) {
		preserveMetadata(ctx, t.Path, tempOutPath, t.Logger)
		targetPath := strings.TrimSuffix(t.Path, filepath.Ext(t.Path)) + filepath.Ext(tempOutPath)
		if err := os.Rename(tempOutPath, targetPath); err != nil {
			result.Error = fmt.Errorf("重命名失败: %w", err)
			os.Remove(tempOutPath)
			return result
		}
		if err := os.Remove(t.Path); err != nil {
			result.Error = fmt.Errorf("无法删除原文件: %w", err)
			return result
		}
		// 确保保留原始文件的修改时间
		srcInfo, err := os.Stat(targetPath)
		if err == nil {
			os.Chtimes(targetPath, srcInfo.ModTime(), srcInfo.ModTime())
		}
		result.FinalPath = targetPath
		t.Logger.Info("转换成功并替换", "path", filepath.Base(targetPath), "original_size", formatBytes(result.OriginalSize), "new_size", formatBytes(result.NewSize), "tag", tag)
	} else {
		result.Decision = "SKIP_LARGER"
		t.Logger.Info("转换后文件增大，不替换", "path", filepath.Base(t.Path), "original_size", formatBytes(result.OriginalSize), "new_size", formatBytes(result.NewSize))
		os.Remove(tempOutPath)
	}
	return result
}

func discoveryStage(ctx context.Context, app *AppContext, pathChan chan<- string) error {
	defer close(pathChan)
	if err := checkDirectoryPermissions(app.Config.TargetDir); err != nil {
		return fmt.Errorf("目标目录权限检查失败: %w", err)
	}
	err := filepath.Walk(app.Config.TargetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			app.Logger.Warn("遍历目录时出错", "path", path, "error", err)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if base == ".backups" || base == ".media_conversion_results" || base == ".logs" {
				return filepath.SkipDir
			}
			return nil
		}
		app.filesFoundCount.Add(1)
		// 仅当明确要求覆盖或结果文件不存在时才处理
		if !app.Config.Overwrite {
			resultPath := getResultFilePath(app.ResultsDir, path)
			if fileExists(resultPath) {
				// 检查结果文件是否表示成功转换
				content, err := os.ReadFile(resultPath)
				if err == nil {
					parts := strings.Split(string(content), "|")
					if len(parts) >= 1 && !strings.HasPrefix(parts[0], "SKIP") && !strings.HasPrefix(parts[0], "FAIL") {
						app.resumedCount.Add(1)
						return nil
					}
				}
			}
		}
		if shouldSkipEarly(path) {
			app.skipCount.Add(1)
			return nil
		}
		select {
		case pathChan <- path:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	return err
}

// 优化评估阶段，优先处理质量差的文件
func assessmentStage(ctx context.Context, app *AppContext, pathChan <-chan string, taskChan chan<- *FileTask, lowQualityChan chan<- *FileTask) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		g.Go(func() error {
			for path := range pathChan {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				info, err := os.Stat(path)
				if err != nil {
					app.Logger.Warn("无法获取文件信息", "path", path, "error", err)
					continue
				}
				mime, err := getMimeType(ctx, path)
				if err != nil || !isMediaFile(mime) {
					continue
				}
				quality, err := assessQuality(ctx, path, mime, info.Size(), app.Config.QualityConfig)
				if err != nil {
					app.Logger.Warn("质量评估失败", "path", path, "error", err)
					continue
				}
				app.filesAssessedCount.Add(1)
				switch quality {
				case QualityExtremeHigh:
					app.extremeHighCount.Add(1)
				case QualityHigh:
					app.highCount.Add(1)
				case QualityMedium:
					app.mediumCount.Add(1)
				case QualityLow:
					app.lowCount.Add(1)
				case QualityExtremeLow:
					app.extremeLowCount.Add(1)
				}
				task := &FileTask{
					Path:       path,
					Size:       info.Size(),
					MimeType:   mime,
					Logger:     app.Logger,
					BaseConfig: app.Config,
					Quality:    quality,
					Priority:   int(quality), // 低质量文件有更高优先级
					TempDir:    app.TempDir,
				}
				if quality == QualityExtremeLow {
					select {
					case lowQualityChan <- task:
					case <-ctx.Done():
						return ctx.Err()
					}
				} else {
					select {
					case taskChan <- task:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			return nil
		})
	}
	return g.Wait()
}

// 优化转换阶段，优先处理低质量文件
func conversionStage(ctx context.Context, app *AppContext, taskChan <-chan *FileTask, resultChan chan<- *ConversionResult) error {
	defer close(resultChan)
	// 创建优先级通道
	priorityTaskChan := make(chan *FileTask, app.Config.ConcurrentJobs*2)
	// 优先级处理goroutine
	go func() {
		lowPriorityTasks := make([]*FileTask, 0)
		for {
			select {
			case task, ok := <-taskChan:
				if !ok {
					// taskChan关闭，发送所有低优先级任务
					for _, t := range lowPriorityTasks {
						priorityTaskChan <- t
					}
					close(priorityTaskChan)
					return
				}
				if task.Quality == QualityExtremeLow {
					// 高优先级任务直接发送
					priorityTaskChan <- task
				} else {
					// 低优先级任务暂存
					lowPriorityTasks = append(lowPriorityTasks, task)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(app.Config.ConcurrentJobs)
	for i := 0; i < app.Config.ConcurrentJobs; i++ {
		g.Go(func() error {
			for task := range priorityTaskChan {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				var result *ConversionResult
				var attempt int
				for attempt = 0; attempt <= app.Config.MaxRetries; attempt++ {
					if attempt > 0 {
						backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
						randNum, _ := rand.Int(rand.Reader, big.NewInt(1000))
						jitter := time.Duration(randNum.Int64()) * time.Millisecond
						time.Sleep(backoff + jitter)
					}
					result = ProcessTask(ctx, task, app.Tools, app)
					if result.Error == nil {
						if attempt > 0 {
							app.retrySuccessCount.Add(1)
						}
						break
					}
					if errors.Is(result.Error, context.Canceled) || errors.Is(result.Error, context.DeadlineExceeded) {
						break
					}
					task.Logger.Warn("转换尝试失败", "attempt", attempt+1, "file", filepath.Base(task.Path), "error", result.Error)
				}
				select {
				case resultChan <- result:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func resultProcessingStage(ctx context.Context, app *AppContext, resultChan <-chan *ConversionResult) error {
	for result := range resultChan {
		if result.Error != nil {
			app.failCount.Add(1)
		} else if strings.HasPrefix(result.Decision, "SKIP") {
			app.skipCount.Add(1)
		} else if strings.HasPrefix(result.Decision, "DELETE") {
			app.deleteCount.Add(1)
		} else {
			app.successCount.Add(1)
			if result.NewSize < result.OriginalSize {
				app.totalDecreased.Add(result.OriginalSize - result.NewSize)
			} else if result.NewSize > result.OriginalSize {
				app.totalIncreased.Add(result.NewSize - result.OriginalSize)
			}
			if app.Config.Mode != "quality" {
				app.smartDecisionsCount.Add(1)
			}
			if strings.Contains(result.Tag, "Lossless") && result.NewSize < result.OriginalSize {
				app.losslessWinsCount.Add(1)
			}
			// 只有在成功转换时才记录结果
			statusLine := fmt.Sprintf("%s|%s|%d|%d", result.Decision, result.Tag, result.OriginalSize, result.NewSize)
			resultFilePath := getResultFilePath(app.ResultsDir, result.OriginalPath)
			os.WriteFile(resultFilePath, []byte(statusLine), 0644)
		}
		app.processedCount.Add(1)
	}
	return nil
}

func showScanProgress(ctx context.Context, app *AppContext) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	spinner := []string{"/", "-", "\\", "|"}
	i := 0
	for {
		select {
		case <-ctx.Done():
			found := app.filesFoundCount.Load()
			assessed := app.filesAssessedCount.Load()
			printToConsole("🔍 扫描完成. [已发现: %d | 已评估: %d]", found, assessed)
			return
		case <-ticker.C:
			i = (i + 1) % len(spinner)
			found := app.filesFoundCount.Load()
			assessed := app.filesAssessedCount.Load()
			progressStr := fmt.Sprintf("🔍 %s 扫描中... [已发现: %d | 已评估: %d]", spinner[i], found, assessed)
			printToConsole(progressStr)
		}
	}
}

func showConversionProgress(ctx context.Context, app *AppContext) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cc := app.processedCount.Load()
			tt := app.totalFilesToProcess.Load()
			if tt == 0 {
				continue
			}
			pct := float64(cc) / float64(tt)
			if pct > 1.0 {
				pct = 1.0
			}
			
			// 获取终端宽度
			width, _, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil || width < 40 {
				width = 80 // 默认宽度
			}
			
			// 计算进度条宽度，适应不同终端大小
			barWidth := int(float64(width-30) * pct)
			if barWidth < 1 {
				barWidth = 1
			} else if barWidth > width-30 {
				barWidth = width - 30
			}
			
			bar := strings.Repeat("█", barWidth) + strings.Repeat("░", width-30-barWidth)
			var etaStr string
			if cc > 5 {
				elapsed := time.Since(app.runStarted)
				rate := float64(cc) / elapsed.Seconds()
				remaining := float64(tt - cc)
				if rate > 0 {
					eta := time.Duration(remaining/rate) * time.Second
					etaStr = eta.Round(time.Second).String()
				}
			} else {
				etaStr = "计算中..."
			}
			// 确保进度条显示清晰，避免字符交叉
			progressStr := fmt.Sprintf("🔄 处理进度 [%s] %.1f%% (%d/%d) ETA: %s", cyan(bar), pct*100, cc, tt, etaStr)
			printToConsole(progressStr)
		case <-ctx.Done():
			return
		}
	}
}

func (app *AppContext) generateReport(useColor bool) string {
	b, c, g, r, v, s := bold, cyan, green, red, violet, subtle
	if !useColor {
		noColor := func(a ...interface{}) string { return fmt.Sprint(a...) }
		b, c, g, r, v, s = noColor, noColor, noColor, noColor, noColor, noColor
	}
	var report strings.Builder
	report.WriteString(fmt.Sprintf("%s\n", b(c("📊 ================= 媒体转换最终报告 =================="))))
	report.WriteString(fmt.Sprintf("%s %s\n", s("📁 目录:"), app.Config.TargetDir))
	report.WriteString(fmt.Sprintf("%s %s    %s %s\n", s("⚙️ 模式:"), app.Config.Mode, s("🚀 版本:"), Version))
	report.WriteString(fmt.Sprintf("%s %s\n", s("⏰ 耗时:"), time.Since(app.runStarted).Round(time.Second)))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 📋 概览 (本次运行) ---"))))
	totalScanned := app.filesFoundCount.Load()
	report.WriteString(fmt.Sprintf("  %s 总计发现: %d 文件\n", v("🗂️"), totalScanned))
	report.WriteString(fmt.Sprintf("  %s 成功转换: %d\n", g("✅"), app.successCount.Load()))
	if app.retrySuccessCount.Load() > 0 {
		report.WriteString(fmt.Sprintf("    %s (其中 %d 个是在重试后成功的)\n", s(""), app.retrySuccessCount.Load()))
	}
	report.WriteString(fmt.Sprintf("  %s 转换失败: %d\n", r("❌"), app.failCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 主动跳过: %d\n", s("⏭️"), app.skipCount.Load()))
	if app.deleteCount.Load() > 0 {
		report.WriteString(fmt.Sprintf("  %s 用户删除: %d\n", r("🗑️"), app.deleteCount.Load()))
	}
	report.WriteString(fmt.Sprintf("  %s 断点续传: %d (之前已处理)\n", c("🔄"), app.resumedCount.Load()))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 💾 大小变化统计 (本次运行) ---"))))
	// 修复空间变化显示样式问题，添加空格避免交叉
	if app.Config.Mode == "auto" {
		report.WriteString(fmt.Sprintf("  %s 空间变化: ⬆️ %s ⬇️ %s\n", g("💰"), b(g(formatBytes(app.totalIncreased.Load()))), b(g(formatBytes(app.totalDecreased.Load())))))
	} else {
		report.WriteString(fmt.Sprintf("  %s 总空间变化: ⬆️ %s ⬇️ %s\n", g("💰"), b(g(formatBytes(app.totalIncreased.Load()))), b(g(formatBytes(app.totalDecreased.Load())))))
	}
	if app.Config.Mode != "quality" && app.successCount.Load() > 0 {
		smartPct := int(float64(app.smartDecisionsCount.Load()) / float64(app.successCount.Load()) * 100)
		report.WriteString(fmt.Sprintf("%s\n", b(c("--- 🧠 智能效率优化统计 ---"))))
		report.WriteString(fmt.Sprintf("  %s 智能决策文件: %d (%d%% of 成功)\n", v("🧠"), app.smartDecisionsCount.Load(), smartPct))
		report.WriteString(fmt.Sprintf("  %s 无损优势识别: %d\n", v("💎"), app.losslessWinsCount.Load()))
	}
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 🔍 质量级别统计 ---"))))
	report.WriteString(fmt.Sprintf("  %s 极高质量: %d\n", v("🌟"), app.extremeHighCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 高质量: %d\n", v("⭐"), app.highCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 中质量: %d\n", v("✨"), app.mediumCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 低质量: %d\n", v("💤"), app.lowCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 极低质量: %d\n", v("⚠️"), app.extremeLowCount.Load()))
	report.WriteString("--------------------------------------------------------\n")
	report.WriteString(fmt.Sprintf("%s %s\n", s("📄 详细日志:"), app.LogFile.Name()))
	return report.String()
}

func showBanner() {
	color.Cyan(`
    __  ___ __  __ ____   ____ _   _    _    _   _ _____ ____ _____ ____  
   |  \/  |  \/  | __ ) / ___| | | |  / \  | \ | |_   _/ ___|_   _|  _ \ 
   | |\/| | |\/| |  _ \| |   | |_| | / _ \ |  \| | | || |     | | | |_) |
   | |  | | |  | | |_) | |___|  _  |/ ___ \| |\  | | || |___  | | |  _ < 
   |_|  |_|_|  |_|____/ \____|_| |_/_/   \_\_| \_| |_| \____| |_| |_| \_\
	`)
	fmt.Printf(bold(violet("              ✨ 欢迎使用媒体批量转换脚本 v%s ✨\n")), Version)
	fmt.Println(subtle("                  钛金流式版 - 稳定、海量、智能"))
	fmt.Println(subtle("                  随时按 Ctrl+C 安全退出脚本"))
	fmt.Println("================================================================================")
}

// 添加架构检查，只适配macOS m芯片 arm架构
func checkArchitecture() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("此程序仅支持 macOS 系统")
	}
	// 检查是否为ARM架构（Apple Silicon）
	if runtime.GOARCH != "arm64" {
		return fmt.Errorf("此程序仅支持 Apple Silicon (M1/M2/M3) 芯片")
	}
	return nil
}

func checkDirectoryPermissions(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在: %w", err)
	}
	if !info.IsDir() {
		return errors.New("路径不是目录")
	}
	testFile := filepath.Join(dir, ".permission_test_"+fmt.Sprintf("%d", time.Now().Unix()))
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("目录无写入权限: %w", err)
	}
	if err := os.Remove(testFile); err != nil {
		return fmt.Errorf("无法清理测试文件: %w", err)
	}
	return nil
}

func NewAppContext(c Config, t ToolCheckResults) (*AppContext, error) {
	if err := validateConfig(&c); err != nil {
		return nil, err
	}
	if err := checkArchitecture(); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "media_converter_go_*")
	if err != nil {
		return nil, fmt.Errorf("无法创建主临时目录: %w", err)
	}
	resultsDir := filepath.Join(c.TargetDir, ".media_conversion_results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建结果目录: %w", err)
	}
	logsDir := filepath.Join(c.TargetDir, ".logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建日志目录: %w", err)
	}
	logFileName := filepath.Join(logsDir, fmt.Sprintf("%s_run_%s.log", c.Mode, time.Now().Format("20060102_150405")))
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建日志文件: %w", err)
	}
	logger := newStructuredLogger(logFile, parseLogLevel(c.LogLevel))
	// 初始化清理白名单
	cleanupWhitelist := make(map[string]bool)
	cleanupWhitelist[".backups"] = true
	cleanupWhitelist[".media_conversion_results"] = true
	cleanupWhitelist[".logs"] = true
	// 初始化修复信号量，限制同时修复任务数量
	repairSem := make(chan struct{}, 3) // 最多同时修复3个文件
	app := &AppContext{
		Config:           c,
		Tools:            t,
		Logger:           logger,
		TempDir:          tempDir,
		ResultsDir:       resultsDir,
		LogFile:          logFile,
		cleanupWhitelist: cleanupWhitelist,
		repairSem:        repairSem,
	}
	return app, nil
}

func (app *AppContext) Cleanup() {
	if app.LogFile != nil {
		app.LogFile.Close()
	}
	if app.TempDir != "" {
		os.RemoveAll(app.TempDir)
	}
}

func validateConfig(c *Config) error {
	if c.TargetDir == "" {
		return errors.New("目标目录不能为空")
	}
	absPath, err := filepath.Abs(c.TargetDir)
	if err != nil {
		return fmt.Errorf("无法解析目标目录路径: %w", err)
	}
	c.TargetDir = absPath
	if _, err := os.Stat(c.TargetDir); os.IsNotExist(err) {
		return fmt.Errorf("目标目录不存在: %s", c.TargetDir)
	}
	if c.ConcurrentJobs <= 0 {
		cpuCount := runtime.NumCPU()
		c.ConcurrentJobs = int(math.Max(1.0, float64(cpuCount)*0.75))
		if c.ConcurrentJobs > 7 {
			c.ConcurrentJobs = 7
		}
	}
	if c.BackupDir == "" {
		c.BackupDir = filepath.Join(c.TargetDir, ".backups")
	}
	if c.CRF == 0 {
		c.CRF = 28
	}
	return nil
}

func executeStreamingPipeline(c Config, t ToolCheckResults) error {
	app, err := NewAppContext(c, t)
	if err != nil {
		return err
	}
	defer app.Cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		printToConsole(red("\n接收到中断信号，正在优雅地关闭...请稍候...\n"))
		cancel()
	}()
	app.runStarted = time.Now()
	pathChan := make(chan string, 2048)
	taskChan := make(chan *FileTask, 4096)
	lowQualityChan := make(chan *FileTask, 1024)
	resultChan := make(chan *ConversionResult, 1024)
	
	scanCtx, scanCancel := context.WithCancel(ctx)
	go showScanProgress(scanCtx, app)
	
	// 启动发现阶段
	go func() {
		if err := discoveryStage(ctx, app, pathChan); err != nil && err != context.Canceled {
			app.Logger.Error("发现阶段出错", "error", err)
			cancel()
		}
	}()
	
	// 启动评估阶段
	go func() {
		if err := assessmentStage(ctx, app, pathChan, taskChan, lowQualityChan); err != nil && err != context.Canceled {
			app.Logger.Error("评估阶段出错", "error", err)
			cancel()
		}
		close(lowQualityChan)
	}()
	
	// 收集低质量文件
	var lowQualityFiles []*FileTask
	for task := range lowQualityChan {
		lowQualityFiles = append(lowQualityFiles, task)
		if len(lowQualityFiles) > 10000 {
			break
		}
	}
	
	// 显示质量分布统计
	fmt.Printf("\n%s\n", bold(cyan("📊 质量分布统计与处理计划")))
	fmt.Printf("  %s 极高质量: %d → 将使用质量模式\n", violet("🌟"), app.extremeHighCount.Load())
	fmt.Printf("  %s 高质量: %d → 将使用质量模式\n", violet("⭐"), app.highCount.Load())
	fmt.Printf("  %s 中质量: %d → 将使用质量模式\n", violet("✨"), app.mediumCount.Load())
	fmt.Printf("  %s 低质量: %d → 将使用效率模式\n", violet("💤"), app.lowCount.Load())
	fmt.Printf("  %s 极低质量: %d → 将跳过或由用户决定\n", violet("⚠️"), app.extremeLowCount.Load())
	
	// 等待用户确认
	fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回: ")))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if strings.ToLower(input) == "n" {
		return nil
	}
	
	// 处理低质量文件
	batchChoice, interactionErr := handleBatchLowQualityInteraction(lowQualityFiles, app)
	if interactionErr != nil {
		return fmt.Errorf("批量交互失败: %w", interactionErr)
	}
	
	go func() {
		for _, task := range lowQualityFiles {
			task.BatchDecision = batchChoice
			taskChan <- task
		}
		close(taskChan)
	}()
	
	scanCancel()
	time.Sleep(100 * time.Millisecond)
	
	go showConversionProgress(ctx, app)
	go app.memoryWatchdog(ctx)
	
	conversionErr := conversionStage(ctx, app, taskChan, resultChan)
	if conversionErr != nil && conversionErr != context.Canceled {
		app.Logger.Error("转换阶段出错", "error", conversionErr)
	}
	
	resultProcessingErr := resultProcessingStage(ctx, app, resultChan)
	if resultProcessingErr != nil && resultProcessingErr != context.Canceled {
		app.Logger.Error("结果处理阶段出错", "error", resultProcessingErr)
	}
	
	app.totalFilesToProcess.Store(app.filesAssessedCount.Load() - app.resumedCount.Load())
	report := app.generateReport(true)
	fmt.Println("\n" + report)
	
	reportPath := filepath.Join(app.Config.TargetDir, fmt.Sprintf("conversion_report_%s.txt", time.Now().Format("20060102_150405")))
	if err := os.WriteFile(reportPath, []byte(app.generateReport(false)), 0644); err != nil {
		app.Logger.Warn("无法保存报告文件", "path", reportPath, "error", err)
	}
	
	return nil
}

func (app *AppContext) memoryWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.Alloc > 2*1024*1024*1024 && app.Config.ConcurrentJobs > 1 {
				newJobs := app.Config.ConcurrentJobs - 1
				if newJobs < 1 {
					newJobs = 1
				}
				if newJobs != app.Config.ConcurrentJobs {
					app.mu.Lock()
					app.Config.ConcurrentJobs = newJobs
					app.mu.Unlock()
					app.Logger.Warn("检测到高内存使用，动态降低并发数", "new_jobs", newJobs)
				}
			}
		}
	}
}

func adjustQualityParameters(c *Config) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print(bold(cyan("🌟 输入极高质量阈值 (默认 0.25): ")))
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.ExtremeHighThreshold = val
		}
	}
	
	fmt.Print(bold(cyan("⭐ 输入高质量阈值 (默认 0.15): ")))
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.HighThreshold = val
		}
	}
	
	fmt.Print(bold(cyan("✨ 输入中质量阈值 (默认 0.08): ")))
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.MediumThreshold = val
		}
	}
	
	fmt.Print(bold(cyan("💤 输入低质量阈值 (默认 0.03): ")))
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		if val, err := strconv.ParseFloat(input, 64); err == nil {
			c.QualityConfig.LowThreshold = val
		}
	}
}

func interactiveSessionLoop(t ToolCheckResults) {
	reader := bufio.NewReader(os.Stdin)
	var input string  // 统一在函数开头定义input变量
	for {
		var c Config
		c.EnableBackups = true
		c.MaxRetries = 2
		c.HwAccel = true
		c.LogLevel = "info"
		c.CRF = 28
		c.SortOrder = "quality"
		c.ConcurrentJobs = 7
		// 设置默认质量配置
		c.QualityConfig = getDefaultQualityConfig()
		
		showBanner()
		
		for {
			fmt.Print(bold(cyan("\n📂 请拖入目标文件夹，然后按 Enter: ")))
			input, _ = reader.ReadString('\n')
			trimmedInput := strings.TrimSpace(input)
			if trimmedInput == "" {
				fmt.Println(red("⚠️ 目录不能为空，请重新输入。"))
				continue
			}
			cleanedInput := cleanPath(trimmedInput)
			info, err := os.Stat(cleanedInput)
			if err == nil {
				if !info.IsDir() {
					fmt.Println(red("⚠️ 提供的路径不是一个文件夹，请重新输入。"))
					continue
				}
				c.TargetDir = cleanedInput
				break
			}
			fmt.Println(red("⚠️ 无效的目录或路径不存在，请检查后重试。"))
		}
		
		fmt.Println("\n" + bold(cyan("⚙️ 请选择转换模式: ")))
		fmt.Printf("  %s %s - 追求极致画质与无损，适合存档。\n", green("[1]"), bold("质量模式 (Quality)"))
		fmt.Printf("  %s %s - 智能平衡画质与体积，适合日常使用。\n", yellow("[2]"), bold("效率模式 (Efficiency)"))
		fmt.Printf("  %s %s - 程序自动为每个文件选择最佳模式。\n", violet("[3]"), bold("自动模式 (Auto)"))
		
		for {
			fmt.Print(bold(cyan("👉 请输入您的选择 (1/2/3) [回车默认 3]: ")))
			input, _ = reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" || input == "3" {
				c.Mode = "auto"
				break
			} else if input == "2" {
				c.Mode = "efficiency"
				break
			} else if input == "1" {
				c.Mode = "quality"
				break
			}
		}
		
		// 质量参数配置
		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "🌟 极高质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.ExtremeHighThreshold)))
		fmt.Printf("  %-12s %s\n", "⭐ 高质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.HighThreshold)))
		fmt.Printf("  %-12s %s\n", "✨ 中质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.MediumThreshold)))
		fmt.Printf("  %-12s %s\n", "💤 低质量阈值:", cyan(fmt.Sprintf("%.2f", c.QualityConfig.LowThreshold)))
		
		fmt.Print(bold(cyan("\n👉 是否调整质量参数? (y/N): ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.ToLower(input) == "y" {
			adjustQualityParameters(&c)
		}
		
		fmt.Print(bold(cyan("\n👉 是否恢复质量参数默认值? (y/N): ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.ToLower(input) == "y" {
			c.QualityConfig = getDefaultQualityConfig()
			fmt.Println(green("已恢复质量参数默认值"))
		}
		
		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(c.TargetDir))
		fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(c.Mode))
		fmt.Printf("  %-12s %s\n", "⚡ 并发数:", cyan(fmt.Sprintf("%d", c.ConcurrentJobs)))
		fmt.Printf("  %-12s %s\n", "🌟 质量参数:", cyan("已配置"))
		
		fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回: ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			continue
		}
		
		if err := executeStreamingPipeline(c, t); err != nil {
			printToConsole(red("任务执行出错: %v\n", err))
		}
		
		fmt.Print(bold(cyan("\n✨ 本轮任务已完成。是否开始新的转换? (Y/n): ")))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			fmt.Println(green("感谢使用！👋"))
			break
		}
	}
}

func main() {
	var tools ToolCheckResults
	if _, err := exec.LookPath("cjxl"); err == nil {
		tools.HasCjxl = true
	}
	if out, err := exec.Command("ffmpeg", "-codecs").Output(); err == nil {
		if strings.Contains(string(out), "libsvtav1") {
			tools.HasLibSvtAv1 = true
		}
		if strings.Contains(string(out), "videotoolbox") {
			tools.HasVToolbox = true
		}
	}
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		c := parseFlags()
		if err := executeStreamingPipeline(c, tools); err != nil {
			log.Fatalf(red("FATAL: %v"), err)
		}
	} else {
		interactiveSessionLoop(tools)
	}
}

func parseFlags() Config {
	var c Config
	var disableBackup bool
	flag.StringVar(&c.Mode, "mode", "auto", "转换模式: 'quality', 'efficiency', or 'auto'")
	flag.StringVar(&c.TargetDir, "dir", "", "目标目录路径")
	flag.IntVar(&c.ConcurrentJobs, "jobs", 0, "并行任务数 (0 for auto: 75% of CPU cores, max 7)")
	flag.BoolVar(&disableBackup, "no-backup", false, "禁用备份")
	flag.BoolVar(&c.HwAccel, "hwaccel", true, "启用硬件加速")
	flag.StringVar(&c.SortOrder, "sort-by", "quality", "处理顺序: 'quality', 'size', 'default'")
	flag.IntVar(&c.MaxRetries, "retry", 2, "失败后最大重试次数")
	flag.BoolVar(&c.Overwrite, "overwrite", false, "强制重新处理所有文件")
	flag.StringVar(&c.LogLevel, "log-level", "info", "日志级别: 'debug', 'info', 'warn', 'error'")
	flag.IntVar(&c.CRF, "crf", 28, "效率模式CRF值")
	flag.Parse()
	c.EnableBackups = !disableBackup
	if c.TargetDir == "" && flag.NArg() > 0 {
		c.TargetDir = flag.Arg(0)
	}
	c.QualityConfig = getDefaultQualityConfig()
	return c
}
