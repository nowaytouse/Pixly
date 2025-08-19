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
	"golang.org/x/sys/unix"
)

const Version = "16.0.3-GO-TITANIUM-ENHANCED"

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
	Confirm        bool
	LogLevel       string
	SortOrder      string
}

var bold = color.New(color.Bold).SprintFunc()
var cyan = color.New(color.FgCyan).SprintFunc()
var green = color.New(color.FgGreen).SprintFunc()
var yellow = color.New(color.FgYellow).SprintFunc()
var red = color.New(color.FgRed).SprintFunc()
var violet = color.New(color.FgHiMagenta).SprintFunc()
var subtle = color.New(color.Faint).SprintFunc()
var consoleMutex = &sync.Mutex{}

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

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

type StructuredLogger struct {
	logger *log.Logger
	level  LogLevel
}

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
	l.logger.Println(b.String())
}

func (l *StructuredLogger) Debug(msg string, fields ...interface{}) {
	l.log(LogLevelDebug, msg, fields...)
}
func (l *StructuredLogger) Info(msg string, fields ...interface{}) {
	l.log(LogLevelInfo, msg, fields...)
}
func (l *StructuredLogger) Warn(msg string, fields ...interface{}) {
	l.log(LogLevelWarn, msg, fields...)
}
func (l *StructuredLogger) Error(msg string, fields ...interface{}) {
	l.log(LogLevelError, msg, fields...)
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
		if strings.Contains(strings.ToLower(stderr), "error") || strings.Contains(strings.ToLower(stderr), "unsupported") {
			return out.String(), fmt.Errorf("command failed with error: %s %s. stderr: %s", name, strings.Join(args, " "), stderr)
		}
		return out.String(), fmt.Errorf("command failed: %s %s. exit_error: %v. stderr: %s", name, strings.Join(args, " "), err, stderr)
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

func createBackup(f, b string, e bool, l *StructuredLogger) bool {
	if !e {
		return true
	}
	if err := os.MkdirAll(b, 0755); err != nil {
		l.Error("无法创建备份目录", "path", b, "error", err)
		return false
	}
	base := filepath.Base(f)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	hash := sha1.Sum([]byte(f))
	shortHash := hex.EncodeToString(hash[:8])
	ts := time.Now().Format("20060102150405")
	r := fmt.Sprintf("%d", time.Now().UnixNano()%10000)
	bp := filepath.Join(b, fmt.Sprintf("%s_%s_%s_%s.bak%s", name, ts, shortHash, r, ext))
	sf, err := os.Open(f)
	if err != nil {
		l.Error("无法打开源文件进行备份", "file", f, "error", err)
		return false
	}
	defer sf.Close()
	df, err := os.Create(bp)
	if err != nil {
		l.Error("无法创建备份文件", "backup_path", bp, "error", err)
		return false
	}
	defer df.Close()
	if _, err = io.Copy(df, sf); err != nil {
		l.Error("备份文件时复制失败", "file", f, "error", err)
		os.Remove(bp)
		return false
	}
	l.Info("已创建备份", "original", filepath.Base(f), "backup", filepath.Base(bp))
	return true
}

func preserveMetadata(ctx context.Context, src, dst string, l *StructuredLogger) {
	srcInfo, err := os.Stat(src)
	modTime := time.Now()
	if err == nil {
		modTime = srcInfo.ModTime()
	}
	_, err = runCmd(ctx, "exiftool", "-TagsFromFile", src, "-all:all", "-unsafe", "-icc_profile", "-overwrite_original", "-preserve", dst)
	if err != nil {
		l.Warn("使用 exiftool 迁移元数据失败，将仅保留文件修改时间", "source", src, "dest", dst, "reason", err)
		printToConsole(yellow("警告: 元数据迁移失败，仅保留修改时间: %s\n"), dst)
		if err := os.Chtimes(dst, modTime, modTime); err != nil {
			l.Warn("回退设置文件修改时间失败", "dest", dst, "error", err)
		}
	}
}

func getResultFilePath(r, f string) string {
	hash := sha1.Sum([]byte(f))
	return filepath.Join(r, hex.EncodeToString(hash[:]))
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
	p = strings.Trim(p, `"'`)
	re := regexp.MustCompile(`\\(.)`)
	p = re.ReplaceAllString(p, "$1")
	return p
}

func getMimeType(ctx context.Context, f string) (string, error) {
	out, err := runCmd(ctx, "file", "--mime-type", "-b", f)
	if err == nil && !strings.Contains(out, "application/octet-stream") {
		return out, nil
	}
	ext := strings.ToLower(filepath.Ext(f))
	videoExts := map[string]string{".webm": "video/webm", ".mp4": "video/mp4", ".avi": "video/x-msvideo", ".mov": "video/quicktime", ".mkv": "video/x-matroska", ".m4v": "video/x-m4v", ".flv": "video/x-flv"}
	if mime, ok := videoExts[ext]; ok {
		return mime, nil
	}
	imageExts := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".heic": "image/heic", ".avif": "image/avif", ".jxl": "image/jxl"}
	if mime, ok := imageExts[ext]; ok {
		return mime, nil
	}
	out, err = runCmd(ctx, "ffprobe", "-v", "quiet", "-show_format", "-of", "flat", f)
	if err == nil && strings.Contains(out, "format_name") {
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "format.format_name=") {
				return strings.Trim(strings.TrimPrefix(l, "format.format_name="), `"`), nil
			}
		}
	}
	return "application/octet-stream", errors.New("unknown mime type")
}

func isAnimated(ctx context.Context, f string) bool {
	mime, _ := getMimeType(ctx, f)
	if !strings.Contains(mime, "gif") && !strings.Contains(mime, "webp") && !strings.Contains(mime, "avif") {
		return false
	}
	out, err := runCmd(ctx, "ffprobe", "-v", "quiet", "-select_streams", "v:0", "-show_entries", "stream=nb_frames", "-of", "csv=p=0", f)
	if err != nil {
		return false
	}
	frames, _ := strconv.Atoi(out)
	return frames > 1
}

var isLivePhotoRegex = regexp.MustCompile(`(?i)^IMG_E?[0-9]{4}\.HEIC$`)

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
	return strings.Contains(out, "equirectangular") || strings.Contains(out, "cubemap")
}

type Converter interface {
	Process(ctx context.Context, t *FileTask, tools ToolCheckResults) (*ConversionResult, error)
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

func getConverterFactory(m string) (Converter, error) {
	switch m {
	case "quality":
		return &QualityConverter{}, nil
	case "efficiency", "auto":
		return &EfficiencyConverter{}, nil
	default:
		return nil, fmt.Errorf("未知的转换模式: %s", m)
	}
}

type QualityConverter struct{}

func (c *QualityConverter) Process(ctx context.Context, t *FileTask, tools ToolCheckResults) (*ConversionResult, error) {
	return processMedia(ctx, t, tools)
}

type EfficiencyConverter struct{}

func (c *EfficiencyConverter) Process(ctx context.Context, t *FileTask, tools ToolCheckResults) (*ConversionResult, error) {
	return processMedia(ctx, t, tools)
}

func processMedia(ctx context.Context, t *FileTask, tools ToolCheckResults) (*ConversionResult, error) {
	result := &ConversionResult{OriginalPath: t.Path, OriginalSize: t.Size}
	var tempOutPath, tag, decision string
	var err error
	timeout := time.Duration(t.Size/1024/1024*30)*time.Second + 60*time.Second
	convCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if strings.HasPrefix(t.MimeType, "image/") {
		tempOutPath, tag, decision, err = processImage(convCtx, t, tools)
	} else if strings.HasPrefix(t.MimeType, "video/") {
		tempOutPath, tag, decision, err = processVideo(convCtx, t, tools)
	} else {
		result.Decision = "SKIP_UNSUPPORTED"
		if strings.HasPrefix(t.MimeType, "audio/") {
			t.Logger.Warn("音频文件跳过", "file", t.Path)
			printToConsole(yellow("警告: 音频文件不支持，已跳过: %s\n"), t.Path)
		}
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
	if t.BaseConfig.Mode == "quality" {
		shouldReplace = true
	} else if t.BaseConfig.Mode == "efficiency" {
		if result.NewSize < result.OriginalSize && bitrateCheck(convCtx, t.Path, tempOutPath, t.Logger) {
			shouldReplace = true
		}
	} else if t.BaseConfig.Mode == "auto" {
		if t.Size > 100*1024*1024 || strings.Contains(t.MimeType, "mkv") {
			shouldReplace = true
		} else if result.NewSize < result.OriginalSize {
			shouldReplace = true
		}
	}
	if shouldReplace {
		if !createBackup(t.Path, t.BaseConfig.BackupDir, t.BaseConfig.EnableBackups, t.Logger) {
			result.Error = errors.New("创建备份失败，中止替换")
			os.Remove(tempOutPath)
			return result, result.Error
		}
		backupPath := filepath.Join(t.BaseConfig.BackupDir, filepath.Base(t.Path)+".bak")
		os.Rename(t.Path, backupPath)
		preserveMetadata(convCtx, t.Path, tempOutPath, t.Logger)
		targetPath := strings.TrimSuffix(t.Path, filepath.Ext(t.Path)) + filepath.Ext(tempOutPath)
		if err := os.Rename(tempOutPath, targetPath); err != nil {
			result.Error = fmt.Errorf("无法移动转换后的文件: %w", err)
			os.Rename(backupPath, t.Path)
			return result, result.Error
		}
		if isLivePhoto(t.Path) {
			movFile := strings.TrimSuffix(t.Path, filepath.Ext(t.Path)) + ".MOV"
			if fileExists(movFile) {
				os.Rename(movFile, strings.TrimSuffix(targetPath, filepath.Ext(targetPath))+".MOV")
			}
		}
		result.FinalPath = targetPath
		t.Logger.Info("转换成功并替换", "path", filepath.Base(targetPath), "original_size", formatBytes(result.OriginalSize), "new_size", formatBytes(result.NewSize), "tag", tag)
	} else {
		result.Decision = "SKIP_LARGER"
		t.Logger.Info("转换后文件增大，不替换", "path", filepath.Base(t.Path), "original_size", formatBytes(result.OriginalSize), "new_size", formatBytes(result.NewSize))
		os.Remove(tempOutPath)
	}
	return result, nil
}

func bitrateCheck(ctx context.Context, orig, new string, l *StructuredLogger) bool {
	origBr, err := runCmd(ctx, "ffprobe", "-v", "quiet", "-show_format_entry", "bit_rate", "-of", "default=noprint_wrappers=1:nokey=1", orig)
	if err != nil {
		return true
	}
	newBr, err := runCmd(ctx, "ffprobe", "-v", "quiet", "-show_format_entry", "bit_rate", "-of", "default=noprint_wrappers=1:nokey=1", new)
	if err != nil {
		return true
	}
	ob, _ := strconv.ParseFloat(origBr, 64)
	nb, _ := strconv.ParseFloat(newBr, 64)
	return nb >= ob*0.8
}

func processImage(ctx context.Context, t *FileTask, tools ToolCheckResults) (string, string, string, error) {
	if isSpatialImage(ctx, t.Path) {
		return "", "SKIP_SPATIAL", "SKIP_SPATIAL", nil
	}
	isAnim := isAnimated(ctx, t.Path)
	var outputPath, tag string
	var err error
	if t.BaseConfig.Mode == "quality" && tools.HasCjxl && !isAnim {
		outputPath = filepath.Join(t.TempDir, "lossless.jxl")
		tag = "JXL-Lossless"
		_, err = runCmd(ctx, "cjxl", t.Path, outputPath, "-d", "0", "-e", "9")
	} else {
		outputPath = filepath.Join(t.TempDir, "lossy.avif")
		quality := 80
		if t.BaseConfig.Mode == "quality" || t.BaseConfig.Mode == "auto" {
			quality = 95
		}
		tag = fmt.Sprintf("AVIF-Q%d", quality)
		_, err = runCmd(ctx, "magick", t.Path, "-quality", strconv.Itoa(quality), outputPath)
	}
	if err != nil {
		return "", "", "", err
	}
	return outputPath, tag, "IMAGE_CONVERTED", nil
}

func getHwAccelArgs(h bool, tools ToolCheckResults) []string {
	if !h {
		return nil
	}
	if runtime.GOOS == "darwin" && tools.HasVToolbox {
		return []string{"-hwaccel", "videotoolbox"}
	}
	return nil
}

func processVideo(ctx context.Context, t *FileTask, tools ToolCheckResults) (string, string, string, error) {
	ext := filepath.Ext(t.Path)
	outExt := ".mov"
	if strings.Contains(t.MimeType, "webm") || strings.ToLower(ext) == ".webm" {
		outExt = ".mov"
	} else if strings.Contains(t.MimeType, "mkv") {
		outExt = ".mkv"
	}
	tempOut := filepath.Join(t.TempDir, strings.TrimSuffix(filepath.Base(t.Path), ext)+outExt)
	var args []string
	var tag string
	if t.BaseConfig.Mode == "quality" {
		tag = "HEVC-Lossless"
		args = []string{"-c:v", "libx265", "-x265-params", "lossless=1", "-c:a", "aac", "-b:a", "192k"}
	} else {
		crf := t.BaseConfig.CRF
		if crf == 0 {
			crf = 28
		}
		tag = fmt.Sprintf("HEVC-CRF%d", crf)
		args = []string{"-c:v", "libx265", "-crf", strconv.Itoa(crf), "-preset", "medium", "-c:a", "aac", "-b:a", "128k"}
	}
	hwArgs := getHwAccelArgs(t.BaseConfig.HwAccel, tools)
	baseArgs := append(hwArgs, []string{"-hide_banner", "-v", "error", "-y", "-i", t.Path}...)
	finalArgs := append(baseArgs, args...)
	finalArgs = append(finalArgs, "-movflags", "+faststart", tempOut)
	_, err := runCmd(ctx, "ffmpeg", finalArgs...)
	if err != nil {
		return "", "", "", err
	}
	if t.BaseConfig.Mode == "quality" {
		newSize, _ := getFileSize(tempOut)
		if newSize > t.Size*3/2 {
			printToConsole(yellow("警告: 无损视频体积增大超过50%%: %s\n"), t.Path)
		}
	}
	return tempOut, tag, "VIDEO_CONVERTED", nil
}

type AppContext struct {
	Config            Config
	Tools             ToolCheckResults
	Logger            *StructuredLogger
	TempDir           string
	ResultsDir        string
	LogFile           *os.File
	runStarted        time.Time
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
	tools := ToolCheckResults{HasCjxl: fileExists("/usr/bin/cjxl"), HasLibSvtAv1: true, HasVToolbox: runtime.GOOS == "darwin"}
	cfg := parseFlags()
	if cfg.TargetDir == "" || cfg.Mode == "" {
		interactiveSessionLoop(tools)
	} else {
		if err := executeConversionTask(cfg, tools); err != nil {
			fmt.Fprintf(os.Stderr, red("错误: %v\n"), err)
			os.Exit(1)
		}
	}
}

func executeConversionTask(c Config, t ToolCheckResults) error {
	app, err := NewAppContext(c, t)
	if err != nil {
		return err
	}
	defer app.Cleanup()
	if c.Overwrite && c.Confirm {
		fmt.Print(yellow(fmt.Sprintf("⚠️  警告: 您正处于强制覆盖模式，将重新处理 %s 中的所有文件。\n    确定要继续吗? (yes/no): ", c.TargetDir)))
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
	go showProgress(ctx, progressDone, &app.processedCount, &app.totalFiles, app.totalFiles.Load(), app.runStarted)
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
				taskTempDir, tempErr := os.MkdirTemp(app.TempDir, "task_*")
				if tempErr != nil {
					result = &ConversionResult{OriginalPath: task.Path, Error: fmt.Errorf("无法创建任务临时目录: %w", tempErr)}
					break
				}
				task.TempDir = taskTempDir
				converter, factoryErr := getConverterFactory(task.BaseConfig.Mode)
				if factoryErr != nil {
					result = &ConversionResult{OriginalPath: task.Path, Error: factoryErr}
					cleanupTemp(taskTempDir, 3, task.Logger)
					break
				}
				result, err = converter.Process(ctx, task, app.Tools)
				cleanupTemp(taskTempDir, 3, task.Logger)
				if err == nil {
					if attempt > 0 {
						app.retrySuccessCount.Add(1)
						task.Logger.Info("重试成功", "attempt", attempt, "file", filepath.Base(task.Path))
					}
					break
				}
				if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "resource") {
					continue
				}
				break
				task.Logger.Warn("转换尝试失败", "attempt", attempt+1, "max_retries", app.Config.MaxRetries, "file", filepath.Base(task.Path), "error", err)
			}
			results <- result
		}
	}
}

func cleanupTemp(d string, r int, l *StructuredLogger) {
	for i := 0; i < r; i++ {
		if err := os.RemoveAll(d); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	l.Warn("临时目录清理失败", "dir", d)
}

func (app *AppContext) resultProcessor(ctx context.Context, wg *sync.WaitGroup, results <-chan *ConversionResult) {
	defer wg.Done()
	var mu sync.Mutex
	for res := range results {
		mu.Lock()
		if res.Error != nil {
			app.failCount.Add(1)
		} else if strings.HasPrefix(res.Decision, "SKIP") {
			app.skipCount.Add(1)
		} else {
			app.successCount.Add(1)
			if res.OriginalSize > 0 && res.NewSize > 0 && res.OriginalSize > res.NewSize {
				savedSpace := res.OriginalSize - res.NewSize
				app.totalSaved.Add(savedSpace)
			}
		}
		statusLine := fmt.Sprintf("%s|%s|%d|%d", res.Decision, res.Tag, res.OriginalSize, res.NewSize)
		resultFilePath := getResultFilePath(app.ResultsDir, res.OriginalPath)
		if err := os.WriteFile(resultFilePath, []byte(statusLine), 0644); err != nil {
			app.Logger.Error("写入结果文件失败", "path", resultFilePath, "error", err)
		}
		app.processedCount.Add(1)
		mu.Unlock()
	}
}

func findFilesParallel(ctx context.Context, app *AppContext) ([]FileTask, error) {
	var tasks []FileTask
	var taskMutex sync.Mutex
	var wg sync.WaitGroup
	pathChan := make(chan string, 100)
	sem := make(chan struct{}, runtime.NumCPU()*2)
	taskChan := make(chan FileTask, 1000)
	collectionDone := make(chan struct{})
	go func() {
		for t := range taskChan {
			taskMutex.Lock()
			tasks = append(tasks, t)
			taskMutex.Unlock()
		}
		close(collectionDone)
	}()
	wg.Add(1)
	go func() { pathChan <- app.Config.TargetDir }()
	go func() {
		for p := range pathChan {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			go func(cp string) {
				defer func() { <-sem }()
				defer wg.Done()
				entries, err := os.ReadDir(cp)
				if err != nil {
					app.Logger.Warn("无法读取目录", "path", cp, "error", err)
					return
				}
				for _, e := range entries {
					if ctx.Err() != nil {
						return
					}
					fp := filepath.Join(cp, e.Name())
					if e.IsDir() {
						if e.Name() == ".backups" || e.Name() == ".media_conversion_results" || e.Name() == ".logs" {
							continue
						}
						wg.Add(1)
						pathChan <- fp
					} else {
						if !app.Config.Overwrite && fileExists(getResultFilePath(app.ResultsDir, fp)) {
							app.resumedCount.Add(1)
							continue
						}
						info, err := e.Info()
						if err != nil {
							continue
						}
						if shouldSkipEarly(fp) {
							continue
						}
						mime, _ := getMimeType(ctx, fp)
						if strings.ToLower(filepath.Ext(fp)) == ".webm" {
							mime = "video/webm"
						} else if !strings.HasPrefix(mime, "image/") && !strings.HasPrefix(mime, "video/") {
							continue
						}
						task := FileTask{Path: fp, Size: info.Size(), MimeType: mime, Logger: app.Logger, BaseConfig: app.Config}
						if task.BaseConfig.Mode == "auto" {
							task.BaseConfig.Mode = analyzeFileForAutoMode(task.MimeType, task.Size)
						}
						taskChan <- task
					}
				}
			}(p)
		}
	}()
	wg.Wait()
	close(pathChan)
	close(taskChan)
	<-collectionDone
	sortTasks(tasks, app.Config.SortOrder)
	return tasks, nil
}

func shouldSkipEarly(f string) bool {
	if isLivePhoto(f) {
		return true
	}
	return false
}

func analyzeFileForAutoMode(m string, s int64) string {
	if s > 100*1024*1024 || strings.Contains(m, "mkv") {
		return "quality"
	}
	switch {
	case strings.HasPrefix(m, "image/png"), strings.HasPrefix(m, "image/bmp"), strings.HasPrefix(m, "image/tiff"):
		return "quality"
	default:
		return "efficiency"
	}
}

func sortTasks(t []FileTask, o string) {
	switch o {
	case "size":
		sort.Slice(t, func(i, j int) bool { return t[i].Size < t[j].Size })
	case "type":
		sort.SliceStable(t, func(i, j int) bool {
			isImgI := strings.HasPrefix(t[i].MimeType, "image/")
			isImgJ := strings.HasPrefix(t[j].MimeType, "image/")
			return isImgI && !isImgJ
		})
	default:
		sort.Slice(t, func(i, j int) bool { return t[i].Path < t[j].Path })
	}
}

func printToConsole(f string, a ...interface{}) {
	consoleMutex.Lock()
	defer consoleMutex.Unlock()
	fmt.Print("\r\033[K")
	fmt.Printf(f, a...)
}

func showProgress(ctx context.Context, d chan bool, c, t *atomic.Int64, total int64, runStarted time.Time) {
	_, err := unix.IoctlGetWinsize(1, unix.TIOCGWINSZ)
	if err != nil {
		go func() {
			for {
				select {
				case <-time.After(5 * time.Second):
					printToConsole("处理中: %d/%d\n", c.Load(), t.Load())
					if c.Load() >= t.Load() {
						d <- true
						return
					}
				case <-ctx.Done():
					d <- true
					return
				}
			}
		}()
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cc := c.Load()
			tt := t.Load()
			if tt == 0 {
				continue
			}
			pct := float64(cc) / float64(tt) * 100
			barWidth := 40
			filledWidth := int(float64(barWidth) * pct / 100.0)
			if filledWidth > barWidth {
				filledWidth = barWidth
			}
			if filledWidth < 0 {
				filledWidth = 0
			}
			bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
			var eta time.Duration
			if cc > 0 {
				eta = time.Duration((tt - cc) * int64(time.Since(runStarted).Seconds() / float64(cc+1)))
			} else {
				eta = 0
			}
			progressStr := fmt.Sprintf("\r转换中 [%s] %.0f%% (%d/%d) ETA: %s", cyan(bar), pct, cc, tt, eta)
			consoleMutex.Lock()
			fmt.Print(progressStr)
			consoleMutex.Unlock()
			if cc >= tt {
				d <- true
				return
			}
		case <-ctx.Done():
			d <- true
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
	fmt.Println(subtle("                  钛金增强版 - 智能、稳定与高效"))
	fmt.Println("================================================================================\n")
}

func parseFlags() Config {
	var c Config
	var disableBackup bool
	flag.StringVar(&c.Mode, "mode", "", "转换模式: 'quality', 'efficiency', or 'auto'")
	flag.StringVar(&c.TargetDir, "dir", "", "目标目录路径")
	flag.StringVar(&c.BackupDir, "backup-dir", "", "自定义备份目录 (默认在目标目录下创建 .backups)")
	flag.IntVar(&c.ConcurrentJobs, "jobs", 0, "并发任务数 (0 for auto: 75% of CPU cores)")
	flag.BoolVar(&disableBackup, "no-backup", false, "禁用备份")
	flag.BoolVar(&c.HwAccel, "hwaccel", true, "启用硬件加速 (默认启用)")
	flag.StringVar(&c.SortOrder, "sort-by", "default", "处理顺序: 'size', 'type', 'default'")
	flag.IntVar(&c.MaxRetries, "retry", 2, "失败后最大重试次数")
	flag.BoolVar(&c.Overwrite, "overwrite", false, "强制重新处理所有文件")
	flag.BoolVar(&c.Confirm, "confirm", true, "在强制覆盖模式下需要用户确认 (默认启用)")
	flag.StringVar(&c.LogLevel, "log-level", "info", "日志级别: 'debug', 'info', 'warn', 'error'")
	flag.IntVar(&c.CRF, "crf", 28, "效率模式CRF值 (默认28)")
	flag.Parse()
	c.EnableBackups = !disableBackup
	return c
}

func NewAppContext(c Config, t ToolCheckResults) (*AppContext, error) {
	if err := validateConfig(&c); err != nil {
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
	os.MkdirAll(logsDir, 0755)
	timestamp := time.Now().Format("20060102_150405")
	logFileName := filepath.Join(logsDir, fmt.Sprintf("%s_conversion_%s.log", c.Mode, timestamp))
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("无法创建日志文件: %w", err)
	}
	logLevel := parseLogLevel(c.LogLevel)
	logger := newStructuredLogger(logFile, logLevel)
	app := &AppContext{Config: c, Tools: t, Logger: logger, TempDir: tempDir, ResultsDir: resultsDir, LogFile: logFile}
	header := fmt.Sprintf(`📜 媒体转换日志 - %s
=================================================
  - Version: %s, Mode: %s, Target: %s
  - Concurrency: %d, Backups: %t, HWAccel: %t
  - Retries: %d, Overwrite: %t
=================================================`, time.Now().Format(time.RFC1123), Version, c.Mode, c.TargetDir, c.ConcurrentJobs, c.EnableBackups, c.HwAccel, c.MaxRetries, c.Overwrite)
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

func validateConfig(c *Config) error {
	if c.TargetDir != "" {
		absPath, err := filepath.Abs(c.TargetDir)
		if err != nil {
			return fmt.Errorf("无法解析目标目录路径: %w", err)
		}
		c.TargetDir = absPath
		if _, err := os.Stat(c.TargetDir); os.IsNotExist(err) {
			return fmt.Errorf("目标目录不存在: %s", c.TargetDir)
		}
		f, err := os.OpenFile(filepath.Join(c.TargetDir, ".test"), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("目录无写权限: %s", c.TargetDir)
		}
		f.Close()
		os.Remove(filepath.Join(c.TargetDir, ".test"))
		var totalSize int64
		filepath.Walk(c.TargetDir, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				totalSize += info.Size()
			}
			return nil
		})
		st := syscall.Statfs_t{}
		err = syscall.Statfs(c.TargetDir, &st)
		if err != nil {
			return fmt.Errorf("无法检查磁盘空间: %w", err)
		}
		free := int64(st.Bavail) * int64(st.Bsize)
		if free < totalSize/10 {
			return fmt.Errorf("磁盘空间不足: 需至少 %s 可用", formatBytes(totalSize/10))
		}
	}
	if c.ConcurrentJobs <= 0 {
		cpuCount := runtime.NumCPU()
		jobs := int(math.Max(1.0, float64(cpuCount)*0.75))
		c.ConcurrentJobs = jobs
	}
	if c.BackupDir == "" && c.TargetDir != "" {
		c.BackupDir = filepath.Join(c.TargetDir, ".backups")
	}
	return nil
}

func (app *AppContext) generateReport(u bool) string {
	b, c, g, r, v, s := bold, cyan, green, red, violet, subtle
	if !u {
		noColor := func(a ...interface{}) string { return fmt.Sprint(a...) }
		b, c, g, r, v, s = noColor, noColor, noColor, noColor, noColor, noColor
	}
	var report strings.Builder
	report.WriteString(fmt.Sprintf("%s\n", b(c("📊 ================= 媒体转换最终报告 =================="))))
	report.WriteString(fmt.Sprintf("%s %s\n", s("📁 目录:"), app.Config.TargetDir))
	report.WriteString(fmt.Sprintf("%s %s    %s %s\n", s("⚙️ 模式:"), app.Config.Mode, s("🚀 版本:"), Version))
	report.WriteString(fmt.Sprintf("%s %s\n\n", s("⏰ 耗时:"), time.Since(app.runStarted).Round(time.Second)))
	report.WriteString(fmt.Sprintf("%s\n", b(c("--- 📋 概览 (本次运行) ---"))))
	totalScanned := app.totalFiles.Load()
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

func interactiveSessionLoop(t ToolCheckResults) {
	reader := bufio.NewReader(os.Stdin)
	for {
		var c Config
		c.EnableBackups = true
		c.MaxRetries = 2
		c.HwAccel = true
		c.Confirm = true
		c.LogLevel = "info"
		c.CRF = 28
		showBanner()
		for {
			fmt.Print(bold(cyan("📂 请输入或拖入目标文件夹，然后按 Enter: ")))
			input, _ := reader.ReadString('\n')
			cleanedInput := cleanPath(input)
			if _, err := os.Stat(cleanedInput); err == nil {
				c.TargetDir, _ = filepath.Abs(cleanedInput)
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
				c.Mode = "auto"
				break
			} else if input == "1" {
				c.Mode = "quality"
				break
			} else if input == "2" {
				c.Mode = "efficiency"
				break
			}
		}
		fmt.Println(subtle("\n-------------------------------------------------"))
		fmt.Printf("  %-12s %s\n", "📁 目标:", cyan(c.TargetDir))
		fmt.Printf("  %-12s %s\n", "🚀 模式:", cyan(c.Mode))
		fmt.Print(bold(cyan("\n👉 按 Enter 键开始转换，或输入 'n' 返回主菜单: ")))
		input, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) == "n" {
			fmt.Println(yellow("操作已取消。"))
			continue
		}
		if err := executeConversionTask(c, t); err != nil {
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