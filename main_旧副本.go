// Filename: main.go
// Version: 16.0.2-GO-TITANIUM-STABLE
// Description: A deeply refactored and stabilized media conversion tool.
// This version combines the advanced architecture of 16.0.1 with fixes for compilation,
// path handling, and usability, ensuring stability comparable to older versions.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
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

// --- Script Configuration & Constants ---
const Version = "16.0.2-GO-TITANIUM-STABLE"

// ToolCheckResults 存储了外部依赖工具的可用性
type ToolCheckResults struct {
	HasLibSvtAv1 bool
	HasCjxl      bool
	HasVToolbox  bool
}

// Config 存储了单次运行的所有配置
type Config struct {
	Mode           string
	TargetDir      string
	BackupDir      string
	ConcurrentJobs int
	EnableBackups  bool
	SortOrder      string
	HwAccel        bool
	MaxRetries     int
	Overwrite      bool
	Confirm        bool
	LogLevel       string
}

// --- 全局变量 (已最小化) ---
var (
	// 控制台输出着色
	bold   = color.New(color.Bold).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	violet = color.New(color.FgHiMagenta).SprintFunc()
	subtle = color.New(color.Faint).SprintFunc()

	// 控制台输出同步
	consoleMutex = &sync.Mutex{}
)

// --- 日志系统 ---

// StructuredLogger 提供一个简单的结构化日志记录器
type StructuredLogger struct {
	logger *log.Logger
	level  LogLevel
}

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func parseLogLevel(levelStr string) LogLevel {
	switch strings.ToLower(levelStr) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

func newStructuredLogger(w io.Writer, level LogLevel) *StructuredLogger {
	return &StructuredLogger{
		logger: log.New(w, "", log.Ldate|log.Ltime|log.Lmicroseconds),
		level:  level,
	}
}

func (l *StructuredLogger) log(level LogLevel, message string, fields ...interface{}) {
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

	var logEntry strings.Builder
	logEntry.WriteString(fmt.Sprintf("level=%s msg=\"%s\"", levelStr, message))

	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			logEntry.WriteString(fmt.Sprintf(" %v=\"%v\"", fields[i], fields[i+1]))
		}
	}

	l.logger.Println(logEntry.String())
}

func (l *StructuredLogger) Debug(msg string, fields ...interface{}) { l.log(LogLevelDebug, msg, fields...) }
func (l *StructuredLogger) Info(msg string, fields ...interface{})  { l.log(LogLevelInfo, msg, fields...) }
func (l *StructuredLogger) Warn(msg string, fields ...interface{})  { l.log(LogLevelWarn, msg, fields...) }
func (l *StructuredLogger) Error(msg string, fields ...interface{}) { l.log(LogLevelError, msg, fields...) }

// --- 核心工具与辅助函数 ---

// runCmd 接受动态超时，返回更详细的错误
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
		return out.String(), fmt.Errorf("command failed: %s %s. exit_error: %v. stderr: %s", name, strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

func getFileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func createBackup(file, backupDir string, enabled bool, logger *StructuredLogger) bool {
	if !enabled {
		return true
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		logger.Error("无法创建备份目录", "path", backupDir, "error", err)
		return false
	}

	base := filepath.Base(file)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	hash := sha1.Sum([]byte(file))
	shortHash := hex.EncodeToString(hash[:4])
	timestamp := time.Now().Format("20060102150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s_%s.bak%s", name, timestamp, shortHash, ext))

	sourceFile, err := os.Open(file)
	if err != nil {
		logger.Error("无法打开源文件进行备份", "file", file, "error", err)
		return false
	}
	defer sourceFile.Close()

	destFile, err := os.Create(backupPath)
	if err != nil {
		logger.Error("无法创建备份文件", "backup_path", backupPath, "error", err)
		return false
	}
	defer destFile.Close()

	if _, err = io.Copy(destFile, sourceFile); err != nil {
		logger.Error("备份文件时复制失败", "file", file, "error", err)
		os.Remove(backupPath) // 清理不完整的备份
		return false
	}

	logger.Info("已创建备份", "original", filepath.Base(file), "backup", filepath.Base(backupPath))
	return true
}

func preserveMetadata(ctx context.Context, src, dst string, logger *StructuredLogger) {
	srcInfo, err := os.Stat(src)
	modTime := time.Now()
	if err == nil {
		modTime = srcInfo.ModTime()
	}

	_, err = runCmd(ctx, "exiftool", "-TagsFromFile", src, "-all:all", "-unsafe", "-icc_profile", "-overwrite_original", "-preserve", dst)
	if err != nil {
		logger.Warn("使用 exiftool 迁移元数据失败，将仅保留文件修改时间", "source", src, "dest", dst, "reason", err)
		if err := os.Chtimes(dst, modTime, modTime); err != nil {
			logger.Warn("回退设置文件修改时间失败", "dest", dst, "error", err)
		}
	}
}

func getResultFilePath(resultsDir, filePath string) string {
	hash := sha1.Sum([]byte(filePath))
	return filepath.Join(resultsDir, hex.EncodeToString(hash[:]))
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

// cleanPath 使用正则表达式处理更复杂的路径清理
func cleanPath(path string) string {
	p := strings.TrimSpace(path)
	// 移除路径两端可能存在的单引号或双引号
	p = strings.Trim(p, `"'`)
	// 移除常见的 shell 转义符 (例如'\' )
	re := regexp.MustCompile(`\\(.)`)
	p = re.ReplaceAllString(p, "$1")
	return p
}

// --- 媒体分析 ---

func getMimeType(ctx context.Context, file string) (string, error) {
	out, err := runCmd(ctx, "file", "--mime-type", "-b", file)
	if err == nil && !strings.Contains(out, "application/octet-stream") {
		return out, nil
	}
	ext := strings.ToLower(filepath.Ext(file))
	videoExts := map[string]string{".webm": "video/webm", ".mp4": "video/mp4", ".avi": "video/x-msvideo", ".mov": "video/quicktime", ".mkv": "video/x-matroska"}
	if mime, ok := videoExts[ext]; ok {
		return mime, nil
	}
	imageExts := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".heic": "image/heic", ".avif": "image/avif", ".jxl": "image/jxl"}
	if mime, ok := imageExts[ext]; ok {
		return mime, nil
	}
	return "application/octet-stream", errors.New("unknown mime type")
}

func isAnimated(ctx context.Context, file string) bool {
	mime, _ := getMimeType(ctx, file)
	if !strings.Contains(mime, "gif") && !strings.Contains(mime, "webp") && !strings.Contains(mime, "avif") {
		return false
	}
	out, err := runCmd(ctx, "ffprobe", "-v", "quiet", "-select_streams", "v:0", "-show_entries", "stream=nb_frames", "-of", "csv=p=0", file)
	if err != nil {
		return false
	}
	frames, _ := strconv.Atoi(out)
	return frames > 1
}

var isLivePhotoRegex = regexp.MustCompile(`(?i)^IMG_E?[0-9]{4}\.HEIC$`)

func isLivePhoto(file string) bool {
	baseName := filepath.Base(file)
	if !isLivePhotoRegex.MatchString(baseName) {
		return false
	}
	movFile := filepath.Join(filepath.Dir(file), strings.TrimSuffix(baseName, filepath.Ext(baseName))+".MOV")
	return fileExists(movFile)
}

func isSpatialImage(ctx context.Context, file string) bool {
	ext := strings.ToLower(filepath.Ext(file))
	if ext != ".heic" && ext != ".heif" {
		return false
	}
	out, err := runCmd(ctx, "exiftool", "-s", "-s", "-s", "-ProjectionType", file)
	if err != nil {
		return false
	}
	return strings.Contains(out, "equirectangular") || strings.Contains(out, "cubemap")
}

// --- 转换逻辑 (策略模式) ---

type Converter interface {
	Process(ctx context.Context, task *FileTask, tools ToolCheckResults) (*ConversionResult, error)
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
	Path       string
	Size       int64
	MimeType   string
	TempDir    string
	Logger     *StructuredLogger
	BaseConfig Config
}

func getConverterFactory(mode string) (Converter, error) {
	switch mode {
	case "quality":
		return &QualityConverter{}, nil
	case "efficiency":
		return &EfficiencyConverter{}, nil
	default:
		return nil, fmt.Errorf("未知的转换模式: %s", mode)
	}
}

type QualityConverter struct{}

func (c *QualityConverter) Process(ctx context.Context, task *FileTask, tools ToolCheckResults) (*ConversionResult, error) {
	return processMedia(ctx, task, tools)
}

type EfficiencyConverter struct{}

func (c *EfficiencyConverter) Process(ctx context.Context, task *FileTask, tools ToolCheckResults) (*ConversionResult, error) {
	return processMedia(ctx, task, tools)
}

func processMedia(ctx context.Context, task *FileTask, tools ToolCheckResults) (*ConversionResult, error) {
	result := &ConversionResult{OriginalPath: task.Path, OriginalSize: task.Size}
	var tempOutPath, tag, decision string
	var err error

	timeout := time.Duration(task.Size/1024/1024*30)*time.Second + 60*time.Second // 每MB 30秒，基础60秒
	convCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if strings.HasPrefix(task.MimeType, "image/") {
		tempOutPath, tag, decision, err = processImage(convCtx, task, tools)
	} else if strings.HasPrefix(task.MimeType, "video/") {
		tempOutPath, tag, decision, err = processVideo(convCtx, task, tools)
	} else {
		result.Decision = "SKIP_UNSUPPORTED"
		return result, nil
	}

	if err != nil {
		result.Error = err
		return result, err
	}

	newSize, err := getFileSize(tempOutPath)
	if err != nil {
		result.Error = fmt.Errorf("无法获取转换后文件大小: %w", err)
		return result, result.Error
	}
	result.NewSize = newSize
	result.Tag = tag
	result.Decision = decision

	if result.NewSize <= 0 {
		result.Error = errors.New("转换后文件大小无效")
		return result, result.Error
	}

	shouldReplace := false
	if task.BaseConfig.Mode == "quality" || (result.NewSize < result.OriginalSize) {
		shouldReplace = true
	}

	if shouldReplace {
		if !createBackup(task.Path, task.BaseConfig.BackupDir, task.BaseConfig.EnableBackups, task.Logger) {
			result.Error = errors.New("创建备份失败，中止替换")
			os.Remove(tempOutPath) // 清理临时文件
			return result, result.Error
		}

		preserveMetadata(convCtx, task.Path, tempOutPath, task.Logger)
		targetPath := strings.TrimSuffix(task.Path, filepath.Ext(task.Path)) + filepath.Ext(tempOutPath)

		if err := os.Rename(tempOutPath, targetPath); err != nil {
			result.Error = fmt.Errorf("无法移动转换后的文件: %w", err)
			return result, result.Error
		}

		if !strings.EqualFold(task.Path, targetPath) {
			if err := os.Remove(task.Path); err != nil {
				task.Logger.Warn("删除原文件失败", "path", task.Path, "error", err)
			}
		}
		result.FinalPath = targetPath
		task.Logger.Info("转换成功并替换", "path", filepath.Base(targetPath), "original_size", formatBytes(result.OriginalSize), "new_size", formatBytes(result.NewSize), "tag", tag)
	} else {
		result.Decision = "SKIP_LARGER"
		task.Logger.Info("转换后文件增大，不替换", "path", filepath.Base(task.Path), "original_size", formatBytes(result.OriginalSize), "new_size", formatBytes(result.NewSize))
		os.Remove(tempOutPath)
	}

	return result, nil
}

func processImage(ctx context.Context, task *FileTask, tools ToolCheckResults) (string, string, string, error) {
	isAnim := isAnimated(ctx, task.Path)
	var outputPath, tag string
	var err error
	
	// 为简化演示，这里采用一个直接的逻辑。实际应用可以更复杂。
	// 规则：质量模式且有cjxl时，静态图转JXL无损。其他情况转AVIF。
	if task.BaseConfig.Mode == "quality" && tools.HasCjxl && !isAnim {
		outputPath = filepath.Join(task.TempDir, "lossless.jxl")
		tag = "JXL-Lossless"
		_, err = runCmd(ctx, "cjxl", task.Path, outputPath, "-d", "0", "-e", "9")
	} else {
		outputPath = filepath.Join(task.TempDir, "lossy.avif")
		quality := 80
		if task.BaseConfig.Mode == "quality" {
			quality = 95 // 质量模式使用更高的质量
		}
		tag = fmt.Sprintf("AVIF-Q%d", quality)
		_, err = runCmd(ctx, "magick", task.Path, "-quality", strconv.Itoa(quality), outputPath)
	}

	if err != nil {
		return "", "", "", err
	}
	return outputPath, tag, "IMAGE_CONVERTED", nil
}

// **FIX**: 合并了特定平台的 getHwAccelArgs 函数，以解决编译错误。
// 现在它在运行时检查操作系统，使代码可以在任何平台下直接编译。
func getHwAccelArgs(hwAccel bool, tools ToolCheckResults) []string {
	if !hwAccel {
		return nil
	}
	// 运行时检查操作系统
	if runtime.GOOS == "darwin" && tools.HasVToolbox {
		return []string{"-hwaccel", "videotoolbox"}
	}
	// 在此可以为其他操作系统添加硬件加速支持 (e.g., vaapi, nvdec)
	// if runtime.GOOS == "linux" && tools.HasVAAPI {
	//    return []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi"}
	// }
	return nil
}

func processVideo(ctx context.Context, task *FileTask, tools ToolCheckResults) (string, string, string, error) {
	tempOut := filepath.Join(task.TempDir, strings.TrimSuffix(filepath.Base(task.Path), filepath.Ext(task.Path))+".mov")
	var args []string
	var tag string

	if task.BaseConfig.Mode == "quality" {
		tag = "HEVC-Lossless"
		args = []string{"-c:v", "libx265", "-x25-params", "lossless=1", "-c:a", "aac", "-b:a", "192k"}
	} else {
		tag = "HEVC-CRF28"
		args = []string{"-c:v", "libx265", "-crf", "28", "-preset", "medium", "-c:a", "aac", "-b:a", "128k"}
	}

	hwArgs := getHwAccelArgs(task.BaseConfig.HwAccel, tools)
	baseArgs := append(hwArgs, []string{"-hide_banner", "-v", "error", "-y", "-i", task.Path}...)
	finalArgs := append(baseArgs, args...)
	finalArgs = append(finalArgs, "-movflags", "+faststart", tempOut)

	_, err := runCmd(ctx, "ffmpeg", finalArgs...)
	if err != nil {
		return "", "", "", err
	}
	return tempOut, tag, "VIDEO_CONVERTED", nil
}

// --- 主流程与并发控制 ---

type AppContext struct {
	Config     Config
	Tools      ToolCheckResults
	Logger     *StructuredLogger
	TempDir    string
	ResultsDir string
	LogFile    *os.File
	runStarted time.Time
	// 统计
	totalFiles        atomic.Int64
	processedCount    atomic.Int64
	successCount      atomic.Int64
	failCount         atomic.Int64
	skipCount         atomic.Int64
	resumedCount      atomic.Int64
	retrySuccessCount atomic.Int64
	totalSaved        atomic.Int64
}

func main() {
	tools, err := checkDependencies()
	if err != nil {
		fmt.Println(red("错误: " + err.Error()))
		fmt.Println(yellow("请确保已安装所有必需的依赖项 (ffmpeg, imagemagick, exiftool)。"))
		os.Exit(1)
	}

	cfg := parseFlags()

	if cfg.TargetDir == "" || cfg.Mode == "" {
		interactiveSessionLoop(tools) // 启动交互模式
	} else {
		if err := executeConversionTask(cfg, tools); err != nil {
			fmt.Fprintf(os.Stderr, red("错误: %v\n"), err)
			os.Exit(1)
		}
	}
}

func executeConversionTask(cfg Config, tools ToolCheckResults) error {
	app, err := NewAppContext(cfg, tools)
	if err != nil {
		return err
	}
	defer app.Cleanup()

	if cfg.Overwrite && cfg.Confirm {
		fmt.Print(yellow(fmt.Sprintf("⚠️  警告: 您正处于强制覆盖模式，将重新处理 %s 中的所有文件。\n    确定要继续吗? (yes/no): ", cfg.TargetDir)))
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) != "yes" {
			fmt.Println(red("操作已取消。"))
			return nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var mainWg sync.WaitGroup
	mainWg.Add(1)

	go func() {
		defer mainWg.Done()
		app.run(ctx)
	}()

	<-sigChan
	fmt.Print(red("\n👋 用户中断，正在等待当前任务完成并清理...\n"))
	cancel()

	mainWg.Wait()
	fmt.Println(green("清理完成，安全退出。"))

	return nil
}

func (app *AppContext) run(ctx context.Context) {
	app.runStarted = time.Now()

	printToConsole(bold("🔎 [1/3] 并行扫描媒体文件...\n"))
	tasks, err := findFilesParallel(ctx, app)
	if err != nil {
		app.Logger.Error("文件扫描失败", "error", err)
		return
	}
	app.totalFiles.Store(int64(len(tasks)))
	if app.totalFiles.Load() == 0 {
		printToConsole(yellow("⚠️ 未发现需要处理的媒体文件。\n"))
		return
	}
	printToConsole("  ✨ 发现 %s 个待处理文件 (%s 个文件之前已跳过)\n", violet(strconv.FormatInt(app.totalFiles.Load(), 10)), violet(strconv.FormatInt(app.resumedCount.Load(), 10)))

	printToConsole(bold("⚙️ [2/3] 开始转换 (并发数: %s)...\n"), cyan(app.Config.ConcurrentJobs))
	jobs := make(chan *FileTask, len(tasks))
	results := make(chan *ConversionResult, len(tasks))

	var workerWg sync.WaitGroup
	for i := 0; i < app.Config.ConcurrentJobs; i++ {
		workerWg.Add(1)
		go worker(ctx, &workerWg, jobs, results, app)
	}

	for i := range tasks {
		jobs <- &tasks[i]
	}
	close(jobs)

	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go app.resultProcessor(ctx, &resultWg, results)

	progressDone := make(chan bool)
	go showProgress(ctx, progressDone, &app.processedCount, &app.totalFiles)

	workerWg.Wait()
	close(results)

	resultWg.Wait()
	<-progressDone

	fmt.Print("\r\033[K")
	printToConsole("\n" + bold("📊 [3/3] 正在汇总结果并生成报告...\n"))
	reportContentColored := app.generateReport(true)
	fmt.Println("\n" + reportContentColored)
	reportContentPlain := app.generateReport(false)
	reportPath := filepath.Join(app.Config.TargetDir, fmt.Sprintf("%s_conversion_report_%s.txt", app.Config.Mode, time.Now().Format("20060102_150405")))
	os.WriteFile(reportPath, []byte(reportContentPlain), 0644)
}

func worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan *FileTask, results chan<- *ConversionResult, app *AppContext) {
	defer wg.Done()
	for task := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			var result *ConversionResult
			var err error
			for attempt := 0; attempt <= app.Config.MaxRetries; attempt++ {
				if attempt > 0 {
					backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
					task.Logger.Info("等待重试", "delay", backoff, "file", filepath.Base(task.Path))
					select {
					case <-time.After(backoff):
					case <-ctx.Done():
						return
					}
				}

				// 为每个任务创建独立的临时目录
				taskTempDir, tempErr := os.MkdirTemp(app.TempDir, "task_*")
				if tempErr != nil {
					result = &ConversionResult{OriginalPath: task.Path, Error: fmt.Errorf("无法创建任务临时目录: %w", tempErr)}
					break 
				}
				task.TempDir = taskTempDir

				converter, factoryErr := getConverterFactory(task.BaseConfig.Mode)
				if factoryErr != nil {
					result = &ConversionResult{OriginalPath: task.Path, Error: factoryErr}
					os.RemoveAll(taskTempDir)
					break
				}
				result, err = converter.Process(ctx, task, app.Tools)
				os.RemoveAll(taskTempDir)

				if err == nil {
					if attempt > 0 {
						app.retrySuccessCount.Add(1)
						task.Logger.Info("重试成功", "attempt", attempt, "file", filepath.Base(task.Path))
					}
					break
				}
				task.Logger.Warn("转换尝试失败", "attempt", attempt+1, "max_retries", app.Config.MaxRetries, "file", filepath.Base(task.Path), "error", err)
			}
			results <- result
		}
	}
}

func (app *AppContext) resultProcessor(ctx context.Context, wg *sync.WaitGroup, results <-chan *ConversionResult) {
	defer wg.Done()
	for res := range results {
		if res.Error != nil {
			app.failCount.Add(1)
		} else if strings.HasPrefix(res.Decision, "SKIP") {
			app.skipCount.Add(1)
		} else {
			app.successCount.Add(1)
			app.totalSaved.Add(res.OriginalSize - res.NewSize)
		}

		statusLine := fmt.Sprintf("%s|%s|%d|%d", res.Decision, res.Tag, res.OriginalSize, res.NewSize)
		resultFilePath := getResultFilePath(app.ResultsDir, res.OriginalPath)
		if err := os.WriteFile(resultFilePath, []byte(statusLine), 0644); err != nil {
			app.Logger.Error("写入结果文件失败", "path", resultFilePath, "error", err)
		}

		app.processedCount.Add(1)
	}
}

func findFilesParallel(ctx context.Context, app *AppContext) ([]FileTask, error) {
    var tasks []FileTask
    var taskMutex sync.Mutex
    var wg sync.WaitGroup
    pathChan := make(chan string, 100)

    // 控制并发的信号量
    sem := make(chan struct{}, runtime.NumCPU()*4) // IO密集型任务，可以适当增加goroutine数量

    // 启动goroutine来处理收集到的任务
    taskChan := make(chan FileTask, 1000)
    collectionDone := make(chan struct{})
    go func() {
        for task := range taskChan {
            taskMutex.Lock()
            tasks = append(tasks, task)
            taskMutex.Unlock()
        }
        close(collectionDone)
    }()

    // 初始路径
    wg.Add(1)
    go func() { pathChan <- app.Config.TargetDir }()

    // 遍历目录
    go func() {
        for path := range pathChan {
            select {
            case sem <- struct{}{}:
            case <-ctx.Done():
                return
            }

            go func(currentPath string) {
                defer func() { <-sem }()
                defer wg.Done()

                entries, err := os.ReadDir(currentPath)
                if err != nil {
                    app.Logger.Warn("无法读取目录", "path", currentPath, "error", err)
                    return
                }

                for _, entry := range entries {
                    if ctx.Err() != nil { return }

                    fullPath := filepath.Join(currentPath, entry.Name())
                    if entry.IsDir() {
                        if entry.Name() == ".backups" || entry.Name() == ".media_conversion_results" {
                            continue
                        }
                        wg.Add(1)
                        pathChan <- fullPath
                    } else {
                        // 预处理和过滤
						if !app.Config.Overwrite && fileExists(getResultFilePath(app.ResultsDir, fullPath)) {
							app.resumedCount.Add(1)
							continue
						}
						info, err := entry.Info()
						if err != nil {
							continue
						}
						if shouldSkipEarly(fullPath) {
							// skipCount will be handled later if needed, or just log here.
							continue
						}
						mime, _ := getMimeType(ctx, fullPath)
						if !strings.HasPrefix(mime, "image/") && !strings.HasPrefix(mime, "video/") {
							continue
						}

						task := FileTask{
							Path:       fullPath,
							Size:       info.Size(),
							MimeType:   mime,
							Logger:     app.Logger,
							BaseConfig: app.Config,
						}
						if task.BaseConfig.Mode == "auto" {
							task.BaseConfig.Mode = analyzeFileForAutoMode(task.MimeType)
						}
						taskChan <- task
                    }
                }
            }(path)
        }
    }()

    wg.Wait()
    close(pathChan)
    close(taskChan)
    <-collectionDone

    sortTasks(tasks, app.Config.SortOrder)
    return tasks, nil
}


func shouldSkipEarly(file string) bool {
	if isLivePhoto(file) {
		return true
	}
	// isSpatialImage is slow, avoid it in the scanning phase.
	return false
}

func analyzeFileForAutoMode(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/png"), strings.HasPrefix(mime, "image/bmp"), strings.HasPrefix(mime, "image/tiff"):
		return "quality"
	default:
		return "efficiency"
	}
}

func sortTasks(tasks []FileTask, order string) {
	switch order {
	case "size":
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].Size < tasks[j].Size })
	case "type":
		sort.SliceStable(tasks, func(i, j int) bool {
			isImgI := strings.HasPrefix(tasks[i].MimeType, "image/")
			isImgJ := strings.HasPrefix(tasks[j].MimeType, "image/")
			return isImgI && !isImgJ // 图片优先
		})
	default: // 默认按路径名排序，保证每次运行顺序一致
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].Path < tasks[j].Path })
	}
}

// --- UI 与交互 ---

func printToConsole(format string, a ...interface{}) {
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	fmt.Print("\r\033[K")
	fmt.Printf(format, a...)
}

func showProgress(ctx context.Context, done chan bool, current, total *atomic.Int64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c := current.Load()
			t := total.Load()
			if t == 0 {
				continue
			}
			pct := float64(c) / float64(t) * 100
			barWidth := 40
			filledWidth := int(float64(barWidth) * pct / 100.0)
			
			if filledWidth > barWidth { filledWidth = barWidth }
			if filledWidth < 0 { filledWidth = 0 }
			
			bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
			progressStr := fmt.Sprintf("\r转换中 [%s] %.0f%% (%d/%d)", cyan(bar), pct, c, t)
			consoleMutex.Lock()
			fmt.Print(progressStr)
			consoleMutex.Unlock()
			if c >= t {
				done <- true
				return
			}
		case <-ctx.Done():
			done <- true
			return
		}
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
	fmt.Println(subtle("                  钛金稳定版 - 极致性能、稳定与安全"))
	fmt.Println("================================================================================\n")
}

// --- 初始化与配置 ---

func parseFlags() Config {
	var cfg Config
	var disableBackup bool
	flag.StringVar(&cfg.Mode, "mode", "", "转换模式: 'quality', 'efficiency', or 'auto'")
	flag.StringVar(&cfg.TargetDir, "dir", "", "目标目录路径")
	flag.StringVar(&cfg.BackupDir, "backup-dir", "", "自定义备份目录 (默认在目标目录下创建 .backups)")
	flag.IntVar(&cfg.ConcurrentJobs, "jobs", 0, "并发任务数 (0 for auto: 75% of CPU cores)")
	flag.BoolVar(&disableBackup, "no-backup", false, "禁用备份")
	flag.BoolVar(&cfg.HwAccel, "hwaccel", true, "启用硬件加速 (默认启用)")
	flag.StringVar(&cfg.SortOrder, "sort-by", "default", "处理顺序: 'size', 'type', 'default'")
	flag.IntVar(&cfg.MaxRetries, "retry", 2, "失败后最大重试次数")
	flag.BoolVar(&cfg.Overwrite, "overwrite", false, "强制重新处理所有文件")
	flag.BoolVar(&cfg.Confirm, "confirm", true, "在强制覆盖模式下需要用户确认 (默认启用)")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "日志级别: 'debug', 'info', 'warn', 'error'")
	flag.Parse()
	cfg.EnableBackups = !disableBackup
	return cfg
}

func NewAppContext(cfg Config, tools ToolCheckResults) (*AppContext, error) {
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "media_converter_go_*")
	if err != nil {
		return nil, fmt.Errorf("无法创建主临时目录: %w", err)
	}

	resultsDir := filepath.Join(cfg.TargetDir, ".media_conversion_results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建结果目录: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	logFileName := filepath.Join(cfg.TargetDir, fmt.Sprintf("%s_conversion_%s.log", cfg.Mode, timestamp))
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建日志文件: %w", err)
	}

	logLevel := parseLogLevel(cfg.LogLevel)
	logger := newStructuredLogger(logFile, logLevel)

	app := &AppContext{
		Config:     cfg,
		Tools:      tools,
		Logger:     logger,
		TempDir:    tempDir,
		ResultsDir: resultsDir,
		LogFile:    logFile,
	}

	header := fmt.Sprintf(`📜 媒体转换日志 - %s
=================================================
  - Version: %s, Mode: %s, Target: %s
  - Concurrency: %d, Backups: %t, HWAccel: %t
  - Retries: %d, Overwrite: %t
=================================================`,
		time.Now().Format(time.RFC1123), Version, cfg.Mode, cfg.TargetDir, cfg.ConcurrentJobs, cfg.EnableBackups, cfg.HwAccel, cfg.MaxRetries, cfg.Overwrite)
	logFile.WriteString(header + "\n\n")
	logger.Info("应用程序上下文初始化成功")

	return app, nil
}

func (app *AppContext) Cleanup() {
	if app.LogFile != nil {
		app.LogFile.Close()
	}
	if app.TempDir != "" {
		if err := os.RemoveAll(app.TempDir); err != nil {
			fmt.Fprintf(os.Stderr, "警告: 清理临时目录 %s 失败: %v\n", app.TempDir, err)
		}
	}
}

func validateConfig(cfg *Config) error {
	if cfg.TargetDir != "" {
		absPath, err := filepath.Abs(cfg.TargetDir)
		if err != nil {
			return fmt.Errorf("无法解析目标目录路径: %w", err)
		}
		cfg.TargetDir = absPath
		if _, err := os.Stat(cfg.TargetDir); os.IsNotExist(err) {
			return fmt.Errorf("目标目录不存在: %s", cfg.TargetDir)
		}
	}

	if cfg.ConcurrentJobs <= 0 {
		cpuCount := runtime.NumCPU()
		jobs := int(math.Max(1.0, float64(cpuCount)*0.75))
		cfg.ConcurrentJobs = jobs
	}

	if cfg.BackupDir == "" && cfg.TargetDir != "" {
		cfg.BackupDir = filepath.Join(cfg.TargetDir, ".backups")
	}

	return nil
}

func checkDependencies() (ToolCheckResults, error) {
	var results ToolCheckResults
	deps := []string{"ffmpeg", "magick", "exiftool", "file"}
	var missingDeps []string
	for _, dep := range deps {
		if _, err := exec.LookPath(dep); err != nil {
			missingDeps = append(missingDeps, dep)
		}
	}
	if len(missingDeps) > 0 {
		return results, fmt.Errorf("缺少核心依赖: %s", strings.Join(missingDeps, ", "))
	}

	if _, err := exec.LookPath("cjxl"); err == nil {
		results.HasCjxl = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "ffmpeg", "-encoders")
	if err == nil && strings.Contains(out, "libsvtav1") {
		results.HasLibSvtAv1 = true
	}
	out, err = runCmd(ctx, "ffmpeg", "-hwaccels")
	if err == nil && strings.Contains(out, "videotoolbox") {
		results.HasVToolbox = true
	}
	return results, nil
}

// --- 报告生成 ---
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
	report.WriteString(fmt.Sprintf("%s %s\n\n", s("⏰ 耗时:"), time.Since(app.runStarted).Round(time.Second)))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 📋 概览 (本次运行) ---"))))
	totalScanned := app.totalFiles.Load() + app.resumedCount.Load()
	report.WriteString(fmt.Sprintf("  %s 总计扫描: %d 文件\n", v("🗂️"), totalScanned))
	report.WriteString(fmt.Sprintf("  %s 成功转换: %d\n", g("✅"), app.successCount.Load()))
	if app.retrySuccessCount.Load() > 0 {
		report.WriteString(fmt.Sprintf("    %s (其中 %d 个是在重试后成功的)\n", s(""), app.retrySuccessCount.Load()))
	}
	report.WriteString(fmt.Sprintf("  %s 转换失败: %d\n", r("❌"), app.failCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 主动跳过: %d\n", s("⏭️"), app.skipCount.Load()))
	report.WriteString(fmt.Sprintf("  %s 断点续传: %d (之前已处理)\n\n", c("🔄"), app.resumedCount.Load()))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 💾 大小变化统计 (本次运行) ---"))))
	report.WriteString(fmt.Sprintf("  %s 总空间节省: %s\n\n", g("💰"), b(g(formatBytes(app.totalSaved.Load())))))
	report.WriteString("--------------------------------------------------------\n")
	report.WriteString(fmt.Sprintf("%s %s\n", s("📄 详细日志:"), app.LogFile.Name()))
	return report.String()
}

// --- 交互式会话 ---
func interactiveSessionLoop(tools ToolCheckResults) {
    reader := bufio.NewReader(os.Stdin)
    for {
        var cfg Config
		// 设置交互模式的默认值
        cfg.EnableBackups = true
        cfg.MaxRetries = 2
        cfg.HwAccel = true
		cfg.Confirm = true
		cfg.LogLevel = "info"

        showBanner()

		// 1. 获取目标目录
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

		// 2. 选择模式
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

		// 3. 最终确认并执行
        fmt.Println(subtle("\n-------------------------------------------------"))
        fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(cfg.TargetDir))
        fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(cfg.Mode))
        fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回主菜单: ")))
        input, _ := reader.ReadString('\n')
        if strings.TrimSpace(strings.ToLower(input)) == "n" {
            fmt.Println(yellow("操作已取消。"))
            continue
        }

        if err := executeConversionTask(cfg, tools); err != nil {
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