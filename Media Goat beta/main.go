// Filename: main.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fatih/color"
)

// --- Script Configuration & Globals ---
const Version = "15.0.0-GO-DIAMOND"

// Config 存储了单次运行的配置
type Config struct {
	Mode           string
	TargetDir      string
	ConcurrentJobs int
	EnableBackups  bool
	SortOrder      string
	HwAccel        bool
	MaxRetries     int
	Overwrite      bool
}

// 全局状态和计数器
var (
	logFile    *os.File
	reportFile string
	resultsDir string
	tempDir    string

	runStarted        time.Time
	totalFiles        int64
	processedCount    int64
	successCount      int64
	failCount         int64
	skipCount         int64
	resumedCount      int64
	totalSaved        int64
	retrySuccessCount int64

	hasLibSvtAv1 bool
	hasCjxl      bool

	consoleMutex = &sync.Mutex{}
	lastProgress string

	bold   = color.New(color.Bold).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	violet = color.New(color.FgHiMagenta).SprintFunc()
	subtle = color.New(color.Faint).SprintFunc()
)

// --- 日志与控制台输出 ---

func initLogging(cfg Config) error {
	logDir := cfg.TargetDir
	timestamp := time.Now().Format("20060102_150405")
	logFileName := filepath.Join(logDir, fmt.Sprintf("%s_conversion_%s.txt", cfg.Mode, timestamp))
	reportFile = filepath.Join(logDir, fmt.Sprintf("%s_conversion_report_%s.txt", cfg.Mode, timestamp))

	var err error
	logFile, err = os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("无法创建日志文件: %v", err)
	}

	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime)

	header := fmt.Sprintf(`📜 媒体转换日志 - %s
=================================================
  - Go 版本: %s
  - 模式: %s
  - 目标: %s
  - 并发: %d
  - 备份: %t
  - 硬件加速: %t
  - 失败重试: %d 次
  - 覆盖模式: %t
=================================================`,
		time.Now().Format(time.RFC1123), Version, cfg.Mode, cfg.TargetDir, cfg.ConcurrentJobs, cfg.EnableBackups, cfg.HwAccel, cfg.MaxRetries, cfg.Overwrite)

	log.Println(header)
	_, err = fmt.Fprintln(logFile, header)
	return err
}

func logMessage(level, message string) {
	log.Printf("[%s] %s\n", level, message)
}

func printToConsole(format string, a ...interface{}) {
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	fmt.Print("\r\033[K")
	fmt.Printf(format, a...)
	if lastProgress != "" {
		fmt.Print(lastProgress)
	}
}

// --- 核心工具函数 ---

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("命令执行失败: %s %s. 错误: %v. Stderr: %s", name, strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

func getFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func getMimeType(file string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "file", "--mime-type", "-b", file)
	if err == nil && !strings.Contains(out, "application/octet-stream") {
		return out
	}
	ext := strings.ToLower(filepath.Ext(file))
	switch ext {
	case ".webm", ".mp4", ".avi", ".mov", ".mkv", ".flv", ".wmv", ".m4v":
		return "video/" + strings.TrimPrefix(ext, ".")
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".webp", ".heic", ".heif", ".jxl", ".avif":
		return "image/" + strings.TrimPrefix(ext, ".")
	default:
		return "application/octet-stream"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func createBackup(file, backupDir string, enabled bool) bool {
	if !enabled {
		return true
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		logMessage("ERROR", fmt.Sprintf("无法创建备份目录 %s: %v", backupDir, err))
		return false
	}
	base := filepath.Base(file)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%d.bak%s", name, time.Now().Unix(), ext))
	sourceFile, err := os.Open(file)
	if err != nil {
		logMessage("ERROR", fmt.Sprintf("无法打开源文件进行备份 %s: %v", file, err))
		return false
	}
	defer sourceFile.Close()
	destFile, err := os.Create(backupPath)
	if err != nil {
		logMessage("ERROR", fmt.Sprintf("无法创建备份文件 %s: %v", backupPath, err))
		return false
	}
	defer destFile.Close()
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		os.Remove(backupPath)
		logMessage("ERROR", fmt.Sprintf("备份文件失败 %s: %v", file, err))
		return false
	}
	logMessage("INFO", fmt.Sprintf("已创建备份: %s", filepath.Base(backupPath)))
	return true
}

func preserveMetadata(ctx context.Context, src, dst string) {
	srcInfo, err := os.Stat(src)
	if err == nil {
		os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
	}
	_, err = runCmd(ctx, "exiftool", "-TagsFromFile", src, "-all:all", "-unsafe", "-icc_profile", "-overwrite_original", "-preserve", dst)
	if err != nil {
		logMessage("WARN", fmt.Sprintf("元数据迁移可能不完整: %s -> %s. 原因: %v", filepath.Base(src), filepath.Base(dst), err))
	}
}

func getResultFilePath(filePath string) string {
	hash := sha1.Sum([]byte(filePath))
	return filepath.Join(resultsDir, hex.EncodeToString(hash[:]))
}

// --- 媒体分析 ---

func isAnimated(file string) bool {
	mime := getMimeType(file)
	if !strings.Contains(mime, "gif") && !strings.Contains(mime, "webp") && !strings.Contains(mime, "avif") {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "ffprobe", "-v", "quiet", "-select_streams", "v:0", "-show_entries", "stream=nb_frames", "-of", "csv=p=0", file)
	if err != nil {
		return false
	}
	frames, _ := strconv.Atoi(out)
	return frames > 1
}

// [更新] isLivePhoto 使用更可靠的正则和大小写不敏感检查
var isLivePhotoRegex = regexp.MustCompile(`(?i)^IMG_E?[0-9]{4}\.HEIC$`)

func isLivePhoto(file string) bool {
	baseName := filepath.Base(file)
	if !isLivePhotoRegex.MatchString(baseName) {
		return false
	}
	movFile := filepath.Join(filepath.Dir(file), strings.TrimSuffix(baseName, filepath.Ext(baseName))+".MOV")
	return fileExists(movFile)
}

func isSpatialImage(file string) bool {
	ext := strings.ToLower(filepath.Ext(file))
	if ext != ".heic" && ext != ".heif" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "exiftool", "-s", "-s", "-s", "-ProjectionType", file)
	if err != nil {
		return false
	}
	return strings.Contains(out, "equirectangular") || strings.Contains(out, "cubemap")
}

func shouldSkipFile(file string, mode string) (bool, string) {
	base := filepath.Base(file)
	if isLivePhoto(file) {
		return true, fmt.Sprintf("⏭️ 跳过特殊文件 (Live Photo): %s", base)
	}
	if isSpatialImage(file) {
		return true, fmt.Sprintf("⏭️ 跳过特殊文件 (Spatial Photo): %s", base)
	}
	mime := getMimeType(file)
	if !strings.HasPrefix(mime, "image/") && !strings.HasPrefix(mime, "video/") {
		return true, fmt.Sprintf("⏭️ 跳过不支持的MIME类型: %s (%s)", base, mime)
	}
	var targetExt string
	if strings.HasPrefix(mime, "image/") {
		finalMode := mode
		if mode == "auto" {
			finalMode = analyzeFileForAutoMode(file)
		}
		if finalMode == "quality" && hasCjxl {
			targetExt = ".jxl"
		} else {
			targetExt = ".avif"
		}
	} else {
		targetExt = ".mov"
	}
	if strings.EqualFold(filepath.Ext(file), targetExt) {
		return true, fmt.Sprintf("文件已是目标格式: %s", base)
	}
	targetFilename := strings.TrimSuffix(file, filepath.Ext(file)) + targetExt
	if fileExists(targetFilename) {
		return true, fmt.Sprintf("⏭️ 跳过，目标文件已存在: %s", filepath.Base(targetFilename))
	}
	return false, ""
}

// --- 转换逻辑 ---

type conversionResult struct {
	FilePath     string
	Tag          string
	Decision     string
	OriginalSize int64
	NewSize      int64
	Error        error
}

func processFile(ctx context.Context, filePath, mode string, cfg Config) conversionResult {
	logMessage("INFO", fmt.Sprintf("开始处理: %s (模式: %s)", filepath.Base(filePath), mode))
	result := conversionResult{FilePath: filePath, OriginalSize: getFileSize(filePath)}
	if skip, reason := shouldSkipFile(filePath, mode); skip {
		logMessage("INFO", reason)
		result.Decision = "SKIP"
		return result
	}
	fileTempDir, err := os.MkdirTemp(tempDir, "conv_*")
	if err != nil {
		result.Error = fmt.Errorf("无法创建临时目录: %v", err)
		return result
	}
	defer os.RemoveAll(fileTempDir)
	mime := getMimeType(filePath)
	var tempOutPath, tag, decision string
	if strings.HasPrefix(mime, "image/") {
		tempOutPath, tag, decision, err = processImage(ctx, filePath, fileTempDir, result.OriginalSize, mode)
	} else if strings.HasPrefix(mime, "video/") {
		tempOutPath, tag, decision, err = processVideo(ctx, filePath, fileTempDir, mode, cfg.HwAccel)
	} else {
		result.Decision = "SKIP"
		logMessage("INFO", fmt.Sprintf("跳过未知类型文件: %s", filepath.Base(filePath)))
		return result
	}
	if err != nil {
		result.Error = err
		logMessage("ERROR", fmt.Sprintf("核心转换过程失败: %s. 原因: %v", filepath.Base(filePath), err))
		return result
	}
	result.NewSize = getFileSize(tempOutPath)
	result.Tag = tag
	result.Decision = decision
	if result.NewSize <= 0 {
		result.Error = fmt.Errorf("转换后文件大小无效")
		return result
	}
	shouldReplace := false
	if mode == "quality" || (result.NewSize < result.OriginalSize) {
		shouldReplace = true
	}
	if shouldReplace {
		backupDir := filepath.Join(cfg.TargetDir, ".backups")
		if !createBackup(filePath, backupDir, cfg.EnableBackups) {
			result.Error = fmt.Errorf("创建备份失败，中止替换")
			return result
		}
		preserveMetadata(ctx, filePath, tempOutPath)
		targetPath := strings.TrimSuffix(filePath, filepath.Ext(filePath)) + filepath.Ext(tempOutPath)
		if err := os.Rename(tempOutPath, targetPath); err != nil {
			result.Error = fmt.Errorf("无法移动转换后的文件: %v", err)
			return result
		}
		if !strings.EqualFold(filePath, targetPath) {
			os.Remove(filePath)
		}
		logMessage("SUCCESS", fmt.Sprintf("%s | %s -> %s | %s", filepath.Base(targetPath), formatBytes(result.OriginalSize), formatBytes(result.NewSize), tag))
	} else {
		result.Decision = "SKIP_LARGER"
		logMessage("WARN", fmt.Sprintf("转换后文件增大，不替换: %s (%s -> %s)", filepath.Base(filePath), formatBytes(result.OriginalSize), formatBytes(result.NewSize)))
	}
	return result
}

func processImage(ctx context.Context, input, tempDir string, originalSize int64, mode string) (string, string, string, error) {
	isAnim := isAnimated(input)
	if mode == "quality" {
		var losslessPath, tag string
		var err error
		if isAnim {
			losslessPath = filepath.Join(tempDir, "lossless.avif")
			tag, err = generateLosslessImage(ctx, input, losslessPath, isAnim)
		} else if hasCjxl {
			losslessPath = filepath.Join(tempDir, "lossless.jxl")
			tag, err = generateLosslessImage(ctx, input, losslessPath, isAnim)
		} else {
			losslessPath = filepath.Join(tempDir, "lossless.avif")
			tag, err = generateLosslessImage(ctx, input, losslessPath, false)
		}
		return losslessPath, tag, "QUALITY_LOSSLESS", err
	}
	var wg sync.WaitGroup
	var losslessPath, lossyPath, losslessTag, lossyTag string
	var losslessErr, lossyErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		var ext string
		if isAnim {
			ext = "avif"
		} else if hasCjxl {
			ext = "jxl"
		} else {
			ext = "avif"
		}
		path := filepath.Join(tempDir, "lossless."+ext)
		losslessTag, losslessErr = generateLosslessImage(ctx, input, path, isAnim)
		if losslessErr == nil {
			losslessPath = path
		}
	}()
	go func() {
		defer wg.Done()
		path := filepath.Join(tempDir, "lossy.avif")
		lossyTag, lossyErr = generateLossyImage(ctx, input, path, isAnim, 80)
		if lossyErr == nil {
			lossyPath = path
		}
	}()
	wg.Wait()
	losslessSize := getFileSize(losslessPath)
	lossySize := getFileSize(lossyPath)
	if losslessSize > 0 && losslessSize < lossySize && float64(losslessSize) < float64(originalSize)*0.8 {
		os.Remove(lossyPath)
		return losslessPath, losslessTag, "SMART_LOSSLESS", nil
	}
	if lossySize > 0 && float64(lossySize) < float64(originalSize)*0.8 {
		os.Remove(losslessPath)
		return lossyPath, lossyTag, "SMART_LOSSY", nil
	}
	if lossySize > 0 && float64(lossySize) >= float64(originalSize)*0.8 {
		os.Remove(losslessPath)
		return exploreFurtherLossyImage(ctx, input, tempDir, originalSize, isAnim, lossyPath, lossyTag)
	}
	if lossySize > 0 && (losslessSize == 0 || lossySize < losslessSize) {
		os.Remove(losslessPath)
		return lossyPath, lossyTag, "LOSSY_DEFAULT", nil
	}
	if losslessSize > 0 {
		os.Remove(lossyPath)
		return losslessPath, losslessTag, "LOSSLESS_DEFAULT", nil
	}
	return "", "", "", fmt.Errorf("所有图片转换尝试均失败")
}

func exploreFurtherLossyImage(ctx context.Context, input, tempDir string, originalSize int64, isAnim bool, firstAttemptPath, firstAttemptTag string) (string, string, string, error) {
	qualityLevels := []int{65, 50}
	bestPath, bestTag, bestSize := firstAttemptPath, firstAttemptTag, getFileSize(firstAttemptPath)
	for _, q := range qualityLevels {
		testPath := filepath.Join(tempDir, fmt.Sprintf("lossy_q%d.avif", q))
		tag, err := generateLossyImage(ctx, input, testPath, isAnim, q)
		if err != nil {
			continue
		}
		testSize := getFileSize(testPath)
		if testSize > 0 && testSize < bestSize {
			os.Remove(bestPath)
			bestPath, bestTag, bestSize = testPath, tag, testSize
		} else {
			os.Remove(testPath)
		}
	}
	if bestSize < originalSize {
		return bestPath, bestTag, "SMART_LOSSY_EXPLORED", nil
	}
	return firstAttemptPath, firstAttemptTag, "SMART_LOSSY_EXPLORED", nil
}

func generateLosslessImage(ctx context.Context, input, output string, isAnim bool) (string, error) {
	ext := filepath.Ext(output)
	if isAnim {
		if !hasLibSvtAv1 {
			return "", fmt.Errorf("ffmpeg 不支持 libsvtav1")
		}
		_, err := runCmd(ctx, "ffmpeg", "-hide_banner", "-v", "error", "-y", "-i", input, "-c:v", "libsvtav1", "-qp", "0", "-preset", "8", "-pix_fmt", "yuv420p", "-f", "avif", output)
		return "AVIF-Lossless-Anim", err
	}
	if ext == ".jxl" && hasCjxl {
		_, err := runCmd(ctx, "cjxl", input, output, "-d", "0", "-e", "9")
		if err != nil {
			_, err = runCmd(ctx, "magick", input, "-quality", "100", output)
			return "JXL-Lossless(fallback)", err
		}
		return "JXL-Lossless", err
	}
	_, err := runCmd(ctx, "magick", input, "-quality", "100", output)
	return "AVIF-Lossless-Static", err
}

func generateLossyImage(ctx context.Context, input, output string, isAnim bool, quality int) (string, error) {
	qStr := strconv.Itoa(quality)
	if isAnim {
		if !hasLibSvtAv1 {
			return "", fmt.Errorf("ffmpeg 不支持 libsvtav1")
		}
		crf := 30 + (100-quality)/4
		_, err := runCmd(ctx, "ffmpeg", "-hide_banner", "-v", "error", "-y", "-i", input, "-c:v", "libsvtav1", "-crf", strconv.Itoa(crf), "-preset", "7", "-pix_fmt", "yuv420p", "-f", "avif", output)
		return "AVIF-Anim-CRF" + strconv.Itoa(crf), err
	}
	_, err := runCmd(ctx, "magick", input, "-quality", qStr, output)
	return "AVIF-Q" + qStr, err
}

func ensureEvenDimensions(ctx context.Context, input, tempDir string) (string, bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := runCmd(probeCtx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", input)
	if err != nil {
		return input, false, fmt.Errorf("无法获取视频尺寸: %v", err)
	}
	parts := strings.Split(out, "x")
	if len(parts) != 2 {
		return input, false, fmt.Errorf("无效的尺寸输出: %s", out)
	}
	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])
	if width%2 == 0 && height%2 == 0 {
		return input, false, nil
	}
	logMessage("INFO", fmt.Sprintf("修正奇数分辨率: %dx%d -> %s", width, height, filepath.Base(input)))
	output := filepath.Join(tempDir, "even_dim_"+filepath.Base(input))
	ffmpegCtx, ffmpegCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer ffmpegCancel()
	_, err = runCmd(ffmpegCtx, "ffmpeg", "-i", input, "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-c:a", "copy", output)
	if err != nil {
		return input, false, fmt.Errorf("修正视频尺寸失败: %v", err)
	}
	return output, true, nil
}

func processVideo(ctx context.Context, input, tempDir string, mode string, hwAccel bool) (string, string, string, error) {
	processedInput, wasProcessed, err := ensureEvenDimensions(ctx, input, tempDir)
	if err != nil {
		logMessage("WARN", fmt.Sprintf("无法修正视频尺寸，将使用原文件继续: %v", err))
		processedInput = input
	}
	if wasProcessed {
		defer os.Remove(processedInput)
	}
	base := filepath.Base(input)
	tempOut := filepath.Join(tempDir, strings.TrimSuffix(base, filepath.Ext(base))+".mov")
	var attempts []struct {
		name, tag string
		args      []string
	}
	if mode == "quality" {
		attempts = []struct{ name, tag string; args []string }{
			{"HEVC Lossless", "HEVC-Quality", []string{"-c:v", "libx265", "-x265-params", "lossless=1", "-c:a", "aac", "-b:a", "192k"}},
			{"AV1 Lossless", "AV1-Lossless-Fallback", []string{"-c:v", "libsvtav1", "-qp", "0", "-preset", "8", "-c:a", "copy"}},
			{"Remux", "REMUX-Fallback", []string{"-c", "copy", "-map", "0"}},
		}
	} else {
		attempts = []struct{ name, tag string; args []string }{
			{"HEVC Lossy", "HEVC-CRF28", []string{"-c:v", "libx265", "-crf", "28", "-preset", "medium", "-c:a", "aac", "-b:a", "128k"}},
			{"AV1 Lossy", "AV1-CRF35-Fallback", []string{"-c:v", "libsvtav1", "-crf", "35", "-preset", "7", "-c:a", "aac", "-b:a", "128k"}},
			{"Remux", "REMUX-Fallback", []string{"-c", "copy", "-map", "0"}},
		}
	}
	var hwArgs []string
	if hwAccel && runtime.GOOS == "darwin" {
		hwArgs = []string{"-hwaccel", "videotoolbox"}
	}
	commonArgs := append(hwArgs, []string{"-hide_banner", "-v", "error", "-y", "-i", processedInput}...)
	finalArgs := []string{"-movflags", "+faststart", "-avoid_negative_ts", "make_zero", tempOut}
	for _, attempt := range attempts {
		if strings.Contains(attempt.name, "AV1") && !hasLibSvtAv1 {
			continue
		}
		logMessage("INFO", fmt.Sprintf("视频尝试: [%s] for %s", attempt.name, base))
		args := append(commonArgs, attempt.args...)
		args = append(args, finalArgs...)
		_, err := runCmd(ctx, "ffmpeg", args...)
		if err == nil && getFileSize(tempOut) > 0 {
			logMessage("INFO", fmt.Sprintf("视频成功: [%s]", attempt.name))
			return tempOut, attempt.tag, "VIDEO_CONVERTED", nil
		}
		logMessage("WARN", fmt.Sprintf("视频失败: [%s]. Error: %v", attempt.name, err))
	}
	return "", "", "", fmt.Errorf("所有视频转换尝试均失败: %s", base)
}

// --- 主逻辑与界面 ---

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

type FileTask struct {
	Path string
	Size int64
	Mode string
}

func main() {
	if err := checkDependencies(); err != nil {
		fmt.Println(red("错误: " + err.Error()))
		fmt.Println(yellow("请确保已安装所有必需的依赖项。在 macOS 上，可以尝试使用 Homebrew 安装:"))
		fmt.Println(cyan("brew install ffmpeg imagemagick jpeg-xl exiftool"))
		os.Exit(1)
	}
	var cfg Config
	var disableBackup bool
	flag.StringVar(&cfg.Mode, "mode", "", "转换模式: 'quality', 'efficiency', or 'auto'")
	flag.StringVar(&cfg.TargetDir, "dir", "", "目标目录路径")
	flag.IntVar(&cfg.ConcurrentJobs, "jobs", 0, "并发任务数 (0 for auto)")
	flag.BoolVar(&disableBackup, "no-backup", false, "禁用备份")
	flag.BoolVar(&cfg.HwAccel, "hwaccel", false, "启用硬件加速 (主要针对视频)")
	flag.StringVar(&cfg.SortOrder, "sort-by", "size", "处理顺序: 'size' (从小到大) or 'default'")
	flag.IntVar(&cfg.MaxRetries, "retry", 2, "失败后最大重试次数")
	flag.BoolVar(&cfg.Overwrite, "overwrite", false, "强制重新处理所有文件")
	flag.Parse()
	cfg.EnableBackups = !disableBackup
	if cfg.TargetDir == "" || cfg.Mode == "" {
		interactiveSessionLoop()
	} else {
		if err := executeConversionTask(cfg); err != nil {
			fmt.Println(red("错误: " + err.Error()))
			os.Exit(1)
		}
	}
}

func executeConversionTask(cfg Config) error {
	resetGlobalCounters()
	if err := validateConfig(cfg); err != nil {
		return err
	}
	if cfg.ConcurrentJobs == 0 {
		cfg.ConcurrentJobs = int(float64(runtime.NumCPU()) * 0.75)
		if cfg.ConcurrentJobs < 1 {
			cfg.ConcurrentJobs = 1
		}
	}
	showBanner()
	fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(cfg.TargetDir))
	fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(cfg.Mode))
	fmt.Printf("  %-12s %s\n", "⚡ 并发:", cyan(strconv.Itoa(cfg.ConcurrentJobs)))
	fmt.Printf("  %-12s %s\n", "🛡️ 备份:", cyan(fmt.Sprintf("%t", cfg.EnableBackups)))
	fmt.Printf("  %-12s %s\n", "⚙️ 硬件加速:", cyan(fmt.Sprintf("%t", cfg.HwAccel)))
	fmt.Printf("  %-12s %s\n", "🔁 重试次数:", cyan(strconv.Itoa(cfg.MaxRetries)))
	fmt.Printf("  %-12s %s\n", " FORCE:", cyan(fmt.Sprintf("%t", cfg.Overwrite)))
	fmt.Println(subtle("-------------------------------------------------"))
	var err error
	tempDir, err = os.MkdirTemp("", "media_converter_go")
	if err != nil {
		return fmt.Errorf("无法创建主临时目录: %v", err)
	}
	defer os.RemoveAll(tempDir)
	resultsDir = filepath.Join(cfg.TargetDir, ".media_conversion_results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("无法创建结果目录: %v", err)
	}
	if err := initLogging(cfg); err != nil {
		return err
	}
	defer logFile.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		printToConsole(red("\n👋 用户中断，正在取消所有任务并清理...\n"))
		cancel()
		time.Sleep(2 * time.Second)
		os.RemoveAll(tempDir)
		os.Exit(1)
	}()
	runStarted = time.Now()
	printToConsole(bold("🔎 [1/3] 并行扫描媒体文件并建立索引...\n"))
	tasks, err := findFilesParallel(cfg)
	if err != nil {
		return err
	}
	totalFiles = int64(len(tasks))
	if totalFiles == 0 {
		printToConsole(yellow("⚠️ 未发现需要处理的媒体文件。\n"))
		return nil
	}
	printToConsole("  ✨ 发现 %s 个待处理文件 (%s 个文件之前已处理过)\n", violet(strconv.FormatInt(totalFiles, 10)), violet(strconv.FormatInt(resumedCount, 10)))
	printToConsole(bold("⚙️ [2/3] 开始转换 (并发数: %s)...\n"), cyan(cfg.ConcurrentJobs))
	jobs := make(chan FileTask, totalFiles)
	results := make(chan conversionResult, totalFiles)
	var wg sync.WaitGroup
	for i := 0; i < cfg.ConcurrentJobs; i++ {
		wg.Add(1)
		go worker(&wg, ctx, jobs, results, cfg)
	}
	for _, task := range tasks {
		jobs <- task
	}
	close(jobs)
	progressDone := make(chan bool)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if atomic.LoadInt64(&processedCount) >= totalFiles {
					showProgress(totalFiles, totalFiles, "完成")
					progressDone <- true
					return
				}
				showProgress(atomic.LoadInt64(&processedCount), totalFiles, "转换中")
			case <-ctx.Done():
				return
			}
		}
	}()
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		for res := range results {
			if res.Error != nil {
				atomic.AddInt64(&failCount, 1)
			} else if res.Decision == "SKIP" || res.Decision == "SKIP_LARGER" {
				atomic.AddInt64(&skipCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
				atomic.AddInt64(&totalSaved, res.OriginalSize-res.NewSize)
			}
			resultFilePath := getResultFilePath(res.FilePath)
			statusLine := fmt.Sprintf("%s|%s|%d|%d", res.Decision, res.Tag, res.OriginalSize, res.NewSize)
			os.WriteFile(resultFilePath, []byte(statusLine), 0644)
			atomic.AddInt64(&processedCount, 1)
		}
	}()
	wg.Wait()
	close(results)
	resultWg.Wait()
	<-progressDone
	fmt.Print("\r\033[K")
	printToConsole("\n" + bold("📊 [3/3] 正在汇总结果并生成报告...\n"))
	reportContentColored := generateReport(cfg, true)
	fmt.Println("\n" + reportContentColored)
	reportContentPlain := generateReport(cfg, false)
	os.WriteFile(reportFile, []byte(reportContentPlain), 0644)
	return nil
}

// [更新] worker 包含失败重试逻辑
func worker(wg *sync.WaitGroup, ctx context.Context, jobs <-chan FileTask, results chan<- conversionResult, cfg Config) {
	defer wg.Done()
	for task := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			var result conversionResult
			for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
				mode := cfg.Mode
				if task.Mode != "" {
					mode = task.Mode
				}
				result = processFile(ctx, task.Path, mode, cfg)
				if result.Error == nil {
					if attempt > 0 {
						atomic.AddInt64(&retrySuccessCount, 1)
						logMessage("INFO", fmt.Sprintf("成功重试 (%d/%d): %s", attempt, cfg.MaxRetries, filepath.Base(task.Path)))
					}
					break
				}
				logMessage("WARN", fmt.Sprintf("尝试 %d/%d 失败: %s. 错误: %v", attempt+1, cfg.MaxRetries, filepath.Base(task.Path), result.Error))
				if attempt < cfg.MaxRetries {
					time.Sleep(time.Second * time.Duration(attempt+1))
				}
			}
			results <- result
		}
	}
}

// [极致性能] findFilesParallel 并行化文件扫描
func findFilesParallel(cfg Config) ([]FileTask, error) {
	var tasks []FileTask
	var taskMutex sync.Mutex
	var wg sync.WaitGroup
	taskChan := make(chan FileTask, 1000)
	dirChan := make(chan string, 100)

	wg.Add(1)
	dirChan <- cfg.TargetDir

	for i := 0; i < runtime.NumCPU()*2; i++ { // 使用更多 goroutine 来处理 IO 密集型任务
		go func() {
			for dir := range dirChan {
				entries, err := os.ReadDir(dir)
				if err != nil {
					logMessage("ERROR", fmt.Sprintf("无法读取目录 %s: %v", dir, err))
					wg.Done()
					continue
				}
				for _, entry := range entries {
					path := filepath.Join(dir, entry.Name())
					if entry.IsDir() {
						if entry.Name() == ".backups" || entry.Name() == ".media_conversion_results" {
							continue
						}
						wg.Add(1)
						dirChan <- path
					} else {
						if !cfg.Overwrite && fileExists(getResultFilePath(path)) {
							atomic.AddInt64(&resumedCount, 1)
							continue
						}
						info, err := entry.Info()
						if err != nil {
							continue
						}
						task := FileTask{Path: path, Size: info.Size()}
						if cfg.Mode == "auto" {
							task.Mode = analyzeFileForAutoMode(path)
						}
						taskChan <- task
					}
				}
				wg.Done()
			}
		}()
	}

	go func() {
		wg.Wait()
		close(dirChan)
		close(taskChan)
	}()

	for task := range taskChan {
		taskMutex.Lock()
		tasks = append(tasks, task)
		taskMutex.Unlock()
	}

	if cfg.SortOrder == "size" {
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].Size < tasks[j].Size })
	}
	return tasks, nil
}

func analyzeFileForAutoMode(file string) string {
	mime := getMimeType(file)
	switch {
	case strings.HasPrefix(mime, "image/png"), strings.HasPrefix(mime, "image/bmp"), strings.HasPrefix(mime, "image/tiff"):
		return "quality"
	default:
		return "efficiency"
	}
}

func validateConfig(cfg Config) error {
	if cfg.TargetDir == "" {
		return fmt.Errorf("目标目录未指定")
	}
	if _, err := os.Stat(cfg.TargetDir); os.IsNotExist(err) {
		return fmt.Errorf("目标目录不存在: %s", cfg.TargetDir)
	}
	if cfg.Mode != "quality" && cfg.Mode != "efficiency" && cfg.Mode != "auto" {
		return fmt.Errorf("无效的模式: %s", cfg.Mode)
	}
	return nil
}

func cleanPath(path string) string {
	p := strings.TrimSpace(path)
	p = strings.Trim(p, `"'`)
	p = strings.ReplaceAll(p, "\\ ", " ")
	p = strings.ReplaceAll(p, "\\(", "(")
	p = strings.ReplaceAll(p, "\\)", ")")
	p = strings.ReplaceAll(p, "\\[", "[")
	p = strings.ReplaceAll(p, "\\]", "]")
	return p
}

func interactiveSetup(cfg *Config) {
	reader := bufio.NewReader(os.Stdin)
	showBanner()
	for {
		fmt.Print(bold(cyan("📂 请输入或拖入目标文件夹, 然后按 Enter: ")))
		input, _ := reader.ReadString('\n')
		cleanedInput := cleanPath(input)
		if _, err := os.Stat(cleanedInput); err == nil {
			cfg.TargetDir, _ = filepath.Abs(cleanedInput)
			break
		}
		fmt.Println(yellow("⚠️ 无效的目录，请重新输入。"))
	}
	fmt.Println("\n" + bold(cyan("⚙️ 请选择转换模式: ")))
	fmt.Printf("  %s %s - 追求极致画质，适合存档。\n", green("[1]"), bold("质量模式 (Quality)"))
	fmt.Printf("  %s %s - 平衡画质与体积，适合日常。\n", yellow("[2]"), bold("效率模式 (Efficiency)"))
	fmt.Printf("  %s %s - %s\n", violet("[3]"), bold("自动模式 (Auto)"), bold(subtle("强烈推荐!")))
	for {
		fmt.Print(bold(cyan("👉 请输入您的选择 (1/2/3) [回车默认 3]: ")))
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" || input == "3" {
			cfg.Mode = "auto"
			break
		} else if input == "1" {
			cfg.Mode = "quality"
			break
		} else if input == "2" {
			cfg.Mode = "efficiency"
			break
		}
	}
}

func interactiveSessionLoop() {
	for {
		var cfg Config
		cfg.EnableBackups = true
		cfg.MaxRetries = 2
		interactiveSetup(&cfg)
		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(cfg.TargetDir))
		fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(cfg.Mode))
		fmt.Print(bold(cyan("👉 按 Enter 键开始转换，或输入 'n' 返回主菜单: ")))
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			fmt.Println(yellow("操作已取消。"))
			continue
		}
		if err := executeConversionTask(cfg); err != nil {
			printToConsole(red("任务执行出错: %v\n", err))
		}
		fmt.Print(bold(cyan("\n✨ 本轮任务已完成。是否开始新的转换任务? (Y/n): ")))
		input, _ = reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			fmt.Println(green("感谢使用！👋"))
			break
		}
		fmt.Println("\n")
	}
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
	fmt.Println(subtle("                  Go 语言重构版 - 极致性能、稳定与安全"))
	fmt.Println("================================================================================\n")
}

func showProgress(current, total int64, taskName string) {
	if total == 0 {
		return
	}
	pct := float64(current) / float64(total) * 100
	barWidth := 40
	filledWidth := int(float64(barWidth) * pct / 100.0)
	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
	progressStr := fmt.Sprintf("\r%s [%s] %.0f%% (%d/%d)", taskName, cyan(bar), pct, current, total)
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	fmt.Print(progressStr)
	lastProgress = progressStr
}

func generateReport(cfg Config, useColor bool) string {
	b, c, g, r, v, s := bold, cyan, green, red, violet, subtle
	if !useColor {
		noColor := func(a ...interface{}) string { return fmt.Sprint(a...) }
		b, c, g, r, v, s = noColor, noColor, noColor, noColor, noColor, noColor
	}
	var report strings.Builder
	report.WriteString(fmt.Sprintf("%s\n", b(c("📊 ================= 媒体转换最终报告 =================="))))
	report.WriteString(fmt.Sprintf("%s %s\n", s("📁 目录:"), cfg.TargetDir))
	report.WriteString(fmt.Sprintf("%s %s    %s %s\n", s("⚙️ 模式:"), cfg.Mode, s("🚀 Go 版本:"), Version))
	report.WriteString(fmt.Sprintf("%s %s\n\n", s("⏰ 耗时:"), time.Since(runStarted).Round(time.Second)))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 📋 概览 (本次运行) ---"))))
	report.WriteString(fmt.Sprintf("  %s 总计扫描: %d 文件\n", v("🗂️"), totalFiles+resumedCount))
	report.WriteString(fmt.Sprintf("  %s 成功转换: %d\n", g("✅"), successCount))
	if retrySuccessCount > 0 {
		report.WriteString(fmt.Sprintf("    %s (其中 %d 个是在重试后成功的)\n", s(""), retrySuccessCount))
	}
	report.WriteString(fmt.Sprintf("  %s 转换失败: %d\n", r("❌"), failCount))
	report.WriteString(fmt.Sprintf("  %s 主动跳过: %d\n", s("⏭️"), skipCount))
	report.WriteString(fmt.Sprintf("  %s 断点续传: %d (之前已处理)\n\n", c("🔄"), resumedCount))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 💾 大小变化统计 (本次运行) ---"))))
	report.WriteString(fmt.Sprintf("  %s 总空间节省: %s\n\n", g("💰"), b(g(formatBytes(totalSaved)))))
	report.WriteString("--------------------------------------------------------\n")
	report.WriteString(fmt.Sprintf("%s %s\n", s("📄 详细日志:"), logFile.Name()))
	return report.String()
}

func checkDependencies() error {
	deps := []string{"ffmpeg", "magick", "exiftool", "ffprobe", "file"}
	var missingDeps []string
	for _, dep := range deps {
		if _, err := exec.LookPath(dep); err != nil {
			missingDeps = append(missingDeps, dep)
		}
	}
	if _, err := exec.LookPath("cjxl"); err == nil {
		hasCjxl = true
	} else {
		fmt.Println(yellow("⚠️  警告: 未找到 'cjxl' 命令。JXL 无损图片转换将不可用。"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "ffmpeg", "-encoders")
	if err == nil && strings.Contains(out, "libsvtav1") {
		hasLibSvtAv1 = true
	} else {
		fmt.Println(yellow("⚠️  警告: 当前 ffmpeg 版本不支持 'libsvtav1' 编码器。AV1 转换将不可用。"))
	}
	if len(missingDeps) > 0 {
		return fmt.Errorf("缺少以下核心依赖: %s", strings.Join(missingDeps, ", "))
	}
	return nil
}

func resetGlobalCounters() {
	totalFiles = 0
	processedCount = 0
	successCount = 0
	failCount = 0
	skipCount = 0
	resumedCount = 0
	totalSaved = 0
	retrySuccessCount = 0
	lastProgress = ""
}
