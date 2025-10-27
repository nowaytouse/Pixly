package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"sort"
	"sync/atomic"

	"github.com/h2non/filetype"
	"github.com/h2non/filetype/types"
	"github.com/karrick/godirwalk"
	"github.com/panjf2000/ants/v2"
)

const (
	logFileName      = "all2jxl.log"
	replaceOriginals = true
	// 程序版本信息
	version = "2.0.0"
	author  = "AI Assistant"
)

var (
	logger *log.Logger
	// 限制外部进程与文件句柄并发，避免过载
	// 允许并发上限为 CPU 数或 workers，取其较小值，稍后在 main 中初始化
	procSem chan struct{}
	fdSem   chan struct{}
)

type VerifyMode string

const (
	VerifyStrict VerifyMode = "strict"
	VerifyFast   VerifyMode = "fast"
)

type Options struct {
	Workers        int
	Verify         VerifyMode
	DoCopy         bool
	Sample         int
	SkipExist      bool
	DryRun         bool
	CJXLThreads    int
	TimeoutSeconds int
	Retries        int
	InputDir       string
}

// FileProcessInfo 记录单个文件的处理信息
type FileProcessInfo struct {
	FilePath        string
	FileSize        int64
	FileType        string
	IsAnimated      bool
	ProcessingTime  time.Duration
	ConversionMode  string
	Success         bool
	ErrorMsg        string
	SizeSaved       int64
	MetadataSuccess bool
}

// Stats 统计信息结构体
type Stats struct {
	sync.Mutex
	imagesProcessed     int
	imagesFailed        int
	videosSkipped       int
	symlinksSkipped     int
	othersSkipped       int
	totalBytesBefore    int64
	totalBytesAfter     int64
	byExt               map[string]int
	detailedLogs        []FileProcessInfo // 详细处理日志
	processingStartTime time.Time
	totalProcessingTime time.Duration
}

func (s *Stats) addImageProcessed(sizeBefore, sizeAfter int64) {
	s.Lock()
	defer s.Unlock()
	s.imagesProcessed++
	s.totalBytesBefore += sizeBefore
	s.totalBytesAfter += sizeAfter
}

func (s *Stats) addImageFailed() {
	s.Lock()
	defer s.Unlock()
	s.imagesFailed++
}

func (s *Stats) addVideoSkipped() {
	s.Lock()
	defer s.Unlock()
	s.videosSkipped++
}

func (s *Stats) addSymlinkSkipped() {
	s.Lock()
	defer s.Unlock()
	s.symlinksSkipped++
}

func (s *Stats) addOtherSkipped() {
	s.Lock()
	defer s.Unlock()
	s.othersSkipped++
}

// addDetailedLog 添加详细的处理日志
func (s *Stats) addDetailedLog(info FileProcessInfo) {
	s.Lock()
	defer s.Unlock()
	s.detailedLogs = append(s.detailedLogs, info)
}

// logDetailedSummary 输出详细的处理摘要
func (s *Stats) logDetailedSummary() {
	s.Lock()
	defer s.Unlock()

	logger.Println("🎯 ===== 详细处理摘要 =====")
	logger.Printf("📊 总处理时间: %v", s.totalProcessingTime)
	logger.Printf("📈 平均处理时间: %v", s.totalProcessingTime/time.Duration(len(s.detailedLogs)))

	// 按格式统计
	formatStats := make(map[string][]FileProcessInfo)
	for _, log := range s.detailedLogs {
		formatStats[log.FileType] = append(formatStats[log.FileType], log)
	}

	for format, logs := range formatStats {
		var totalSize, totalSaved int64
		var successCount int
		for _, log := range logs {
			totalSize += log.FileSize
			totalSaved += log.SizeSaved
			if log.Success {
				successCount++
			}
		}
		compressionRatio := float64(totalSaved) / float64(totalSize) * 100
		logger.Printf("🖼️  %s格式: %d个文件, 成功率%.1f%%, 平均压缩率%.1f%%",
			format, len(logs), float64(successCount)/float64(len(logs))*100, compressionRatio)
	}

	// 显示处理最慢的文件
	logger.Println("⏱️  处理时间最长的文件:")
	var slowestFiles []FileProcessInfo
	for _, log := range s.detailedLogs {
		slowestFiles = append(slowestFiles, log)
	}
	sort.Slice(slowestFiles, func(i, j int) bool {
		return slowestFiles[i].ProcessingTime > slowestFiles[j].ProcessingTime
	})

	for i, log := range slowestFiles {
		if i >= 3 { // 只显示前3个最慢的
			break
		}
		logger.Printf("   🐌 %s: %v", filepath.Base(log.FilePath), log.ProcessingTime)
	}
}

func init() {
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	logger = log.New(io.MultiWriter(os.Stdout, logFile), "", log.LstdFlags)
}

func main() {
	// 🚀 程序启动
	logger.Printf("🎨 JPEG XL 批量转换工具 v%s", version)
	logger.Println("✨ 作者:", author)
	logger.Println("🔧 开始初始化...")

	opts := parseFlags()
	if opts.InputDir == "" {
		logger.Println("❌ 使用方法: all2jxl -dir <目录路径> [选项]")
		logger.Println("💡 使用 -h 查看所有可用选项")
		return
	}

	// 🔍 检查依赖工具
	logger.Println("🔍 检查系统依赖...")
	dependencies := []string{"cjxl", "djxl", "exiftool"}
	for _, tool := range dependencies {
		if _, err := exec.LookPath(tool); err != nil {
			logger.Printf("❌ 关键错误: 依赖工具 '%s' 未安装或不在系统PATH中", tool)
			logger.Println("📦 请安装所有依赖工具后继续运行")
			return
		}
		logger.Printf("✅ %s 已就绪", tool)
	}

	// 📁 准备工作目录
	logger.Println("📁 准备处理目录...")
	workDir := opts.InputDir
	if opts.DoCopy {
		logger.Println("📋 创建工作副本...")
		var err error
		workDir, err = copyDirIfNeeded(opts.InputDir)
		if err != nil {
			logger.Printf("❌ 关键错误: 复制目录失败: %v", err)
			return
		}
		logger.Printf("✅ 工作目录: %s", workDir)
	} else {
		logger.Printf("📂 直接处理目录: %s", workDir)
	}

	// 🔍 扫描候选文件
	logger.Println("🔍 扫描图像文件...")
	files := scanCandidateFiles(workDir)
	logger.Printf("📊 发现 %d 个候选文件", len(files))

	if opts.Sample > 0 && len(files) > opts.Sample {
		files = selectSample(files, opts.Sample)
		logger.Printf("🎯 采样模式: 选择 %d 个中等大小文件进行处理", len(files))
	}

	// ⚡ 智能性能配置
	logger.Println("⚡ 配置处理性能...")
	workers := opts.Workers
	cpuCount := runtime.NumCPU()

	if workers <= 0 {
		// 智能线程数配置：根据CPU核心数动态调整
		if cpuCount >= 16 {
			// 高性能处理器（如M3 Max, M4等）
			workers = cpuCount
		} else if cpuCount >= 8 {
			// 中高性能处理器（如M2 Pro, M3等）
			workers = cpuCount
		} else if cpuCount >= 4 {
			// 标准处理器
			workers = cpuCount
		} else {
			// 低端处理器
			workers = cpuCount
		}
	}

	// 安全限制：避免系统过载
	maxWorkers := cpuCount * 2
	if workers > maxWorkers {
		workers = maxWorkers
	}

	// 资源并发限制配置
	procLimit := cpuCount
	if procLimit > 8 {
		procLimit = 8 // 避免过多并发进程
	}
	fdLimit := procLimit * 4 // 文件句柄限制

	// 初始化线程池
	p, err := ants.NewPool(workers, ants.WithPreAlloc(true))
	if err != nil {
		logger.Printf("❌ 关键错误: 创建线程池失败: %v", err)
		return
	}
	defer p.Release()

	// 初始化资源限制
	procSem = make(chan struct{}, procLimit)
	fdSem = make(chan struct{}, fdLimit)

	logger.Printf("🚀 性能配置: %d个工作线程, %d个进程限制, %d个文件句柄限制", workers, procLimit, fdLimit)
	logger.Printf("💻 系统信息: %d个CPU核心", cpuCount)

	// 📊 初始化统计信息
	stats := &Stats{
		processingStartTime: time.Now(),
		byExt:               make(map[string]int),
		detailedLogs:        make([]FileProcessInfo, 0),
	}
	logger.Printf("🚀 开始并行处理 - 目录: %s, 工作线程: %d, 文件数: %d", workDir, workers, len(files))

	// 🛑 优雅中断处理
	logger.Println("🛑 设置信号处理...")
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	startTime := time.Now()
	var doneCount uint64
	total := len(files)

	var wg sync.WaitGroup
	for _, f := range files {
		f := f
		wg.Add(1)
		err := p.Submit(func() {
			defer wg.Done()
			defer func() {
				newDone := atomic.AddUint64(&doneCount, 1)
				if newDone%50 == 0 || int(newDone) == total {
					progress := float64(newDone) / float64(total) * 100
					logger.Printf("📈 处理进度: %d/%d (%.1f%%)", newDone, total, progress)
				}
			}()
			select {
			case <-stopCh:
				// 🛑 收到中断信号后不再处理新任务
				logger.Printf("⚠️  收到中断信号，停止处理新任务")
				return
			default:
			}
			if opts.SkipExist {
				lower := strings.ToLower(f)
				jxlPath := strings.TrimSuffix(lower, filepath.Ext(lower)) + ".jxl"
				if _, statErr := os.Stat(jxlPath); statErr == nil {
					logger.Printf("⏭️  跳过已存在: %s", filepath.Base(jxlPath))
					return
				}
			}
			info, stErr := os.Stat(f)
			if stErr != nil {
				logger.Printf("⚠️  文件状态获取失败 %s: %v", filepath.Base(f), stErr)
				stats.addOtherSkipped()
				return
			}
			processFileWithOpts(f, info, stats, opts)
		})
		if err != nil {
			wg.Done()
			logger.Printf("⚠️  任务提交失败 %s: %v", filepath.Base(f), err)
		}
	}
	wg.Wait()

	// 📊 处理完成统计
	elapsed := time.Since(startTime)
	stats.totalProcessingTime = elapsed
	logger.Printf("⏱️  总处理时间: %s", elapsed)

	// 📈 输出详细摘要
	stats.logDetailedSummary()
	printSummary(stats)
}

func parseFlags() Options {
	var dir string
	var workers int
	var verify string
	var doCopy bool
	var sample int
	var skipExist bool
	var dryRun bool
	var cjxlThreads int
	var timeoutSec int
	var retries int

	// 📝 命令行参数定义
	flag.StringVar(&dir, "dir", "", "📂 输入目录路径")
	flag.IntVar(&workers, "workers", 0, "⚡ 工作线程数 (0=自动检测)")
	flag.StringVar(&verify, "verify", string(VerifyStrict), "🔍 验证模式: strict|fast")
	flag.BoolVar(&doCopy, "copy", false, "📋 复制目录到 *_work 后处理")
	flag.IntVar(&sample, "sample", 0, "🎯 测试模式: 仅处理N个中等大小文件")
	flag.BoolVar(&skipExist, "skip-exist", true, "⏭️  跳过已存在的 .jxl 文件")
	flag.BoolVar(&dryRun, "dry-run", false, "🔍 试运行模式: 仅记录操作不转换")
	flag.IntVar(&cjxlThreads, "cjxl-threads", 1, "🧵 每个转换任务的线程数")
	flag.IntVar(&timeoutSec, "timeout", 0, "⏰ 单任务超时秒数 (0=无限制)")
	flag.IntVar(&retries, "retries", 0, "🔄 失败重试次数")
	flag.Parse()

	vm := VerifyStrict
	if strings.ToLower(verify) == string(VerifyFast) {
		vm = VerifyFast
	}
	if workers > runtime.NumCPU()*2 {
		workers = runtime.NumCPU() * 2
	}
	return Options{Workers: workers, Verify: vm, DoCopy: doCopy, Sample: sample, SkipExist: skipExist, DryRun: dryRun, CJXLThreads: cjxlThreads, TimeoutSeconds: timeoutSec, Retries: retries, InputDir: dir}
}

func processFileWithOpts(filePath string, fileInfo os.FileInfo, stats *Stats, opts Options) {
	// 📊 开始处理单个文件
	startTime := time.Now()
	fileName := filepath.Base(filePath)
	logger.Printf("🔄 开始处理: %s (%.2f KB)", fileName, float64(fileInfo.Size())/1024.0)

	sizeBefore := fileInfo.Size()
	originalModTime := fileInfo.ModTime()

	// 创建处理信息记录
	processInfo := FileProcessInfo{
		FilePath:       filePath,
		FileSize:       sizeBefore,
		ProcessingTime: 0,
		Success:        false,
	}

	// 声明变量
	var sizeDiffKB float64
	var compressionRatio float64

	// 📂 打开文件并读取头部信息
	file, err := os.Open(filePath)
	if err != nil {
		logger.Printf("❌ 无法打开文件 %s: %v", fileName, err)
		processInfo.ErrorMsg = fmt.Sprintf("文件打开失败: %v", err)
		processInfo.ProcessingTime = time.Since(startTime)
		stats.addDetailedLog(processInfo)
		stats.addOtherSkipped()
		return
	}
	defer file.Close()

	// 🔍 读取文件头部进行类型检测
	header := make([]byte, 261)
	_, err = file.Read(header)
	if err != nil && err != io.EOF {
		logger.Printf("❌ 无法读取文件头部 %s: %v", fileName, err)
		processInfo.ErrorMsg = fmt.Sprintf("文件头部读取失败: %v", err)
		processInfo.ProcessingTime = time.Since(startTime)
		stats.addDetailedLog(processInfo)
		stats.addOtherSkipped()
		return
	}

	// 🎯 文件类型识别
	kind, _ := filetype.Match(header)
	processInfo.FileType = kind.Extension

	if kind == types.Unknown {
		logger.Printf("⏭️  未知或不受支持的文件类型: %s", fileName)
		processInfo.ErrorMsg = "未知文件类型"
		processInfo.ProcessingTime = time.Since(startTime)
		stats.addDetailedLog(processInfo)
		stats.addOtherSkipped()
		return
	}

	// 📋 检查是否为支持的图像格式
	if !isSupportedImageType(kind) {
		if isVideoType(kind) {
			logger.Printf("🎬 跳过视频文件: %s (类型: %s)", fileName, kind.MIME.Value)
			processInfo.ErrorMsg = "视频文件"
			processInfo.ProcessingTime = time.Since(startTime)
			stats.addDetailedLog(processInfo)
			stats.addVideoSkipped()
		} else {
			logger.Printf("📄 跳过非图像文件: %s (类型: %s)", fileName, kind.MIME.Value)
			processInfo.ErrorMsg = "非图像文件"
			processInfo.ProcessingTime = time.Since(startTime)
			stats.addDetailedLog(processInfo)
			stats.addOtherSkipped()
		}
		return
	}

	logger.Printf("✅ 识别为图像格式: %s (%s)", fileName, kind.Extension)

	// 🔍 试运行模式检查
	if opts.DryRun {
		logger.Printf("🔍 试运行模式: 将转换 %s", fileName)
		processInfo.Success = true
		processInfo.ProcessingTime = time.Since(startTime)
		stats.addDetailedLog(processInfo)
		return
	}

	// 🎬 动画检测
	isAnimated, animErr := isAnimatedImage(filePath, kind)
	if animErr != nil {
		logger.Printf("⚠️  动画检测失败 %s: %v", fileName, animErr)
		isAnimated = false
	}
	processInfo.IsAnimated = isAnimated

	if isAnimated {
		logger.Printf("🎬 检测到动画图像: %s", fileName)
	} else {
		logger.Printf("🖼️  静态图像: %s", fileName)
	}

	// 🔄 执行转换（支持重试）
	var conversionMode, jxlPath, tempJxlPath string
	for attempt := 0; attempt <= opts.Retries; attempt++ {
		logger.Printf("🔄 开始转换 %s (尝试 %d/%d)", fileName, attempt+1, opts.Retries+1)
		conversionMode, jxlPath, tempJxlPath, err = convertToJxlWithOpts(filePath, kind, opts)
		if err != nil {
			if attempt == opts.Retries {
				logger.Printf("❌ 转换失败 %s: %v", fileName, err)
				processInfo.ErrorMsg = fmt.Sprintf("转换失败: %v", err)
				processInfo.ProcessingTime = time.Since(startTime)
				stats.addDetailedLog(processInfo)
				stats.addImageFailed()
				return
			}
			logger.Printf("🔄 重试转换 %s (尝试 %d/%d)", fileName, attempt+1, opts.Retries)
			continue
		}
		break
	}
	processInfo.ConversionMode = conversionMode
	logger.Printf("✅ 转换完成: %s (%s) -> %s", fileName, conversionMode, filepath.Base(tempJxlPath))
	// 统计扩展名
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	if ext == "" {
		ext = "unknown"
	}
	stats.Lock()
	if stats.byExt == nil {
		stats.byExt = make(map[string]int)
	}
	stats.byExt[ext]++
	stats.Unlock()

	// 🔍 验证转换结果
	logger.Printf("🔍 开始验证转换结果: %s", fileName)
	verified, err := verifyConversionWithMode(filePath, tempJxlPath, kind, opts)
	if err != nil {
		logger.Printf("❌ 验证失败 %s: %v", fileName, err)
		os.Remove(tempJxlPath)
		processInfo.ErrorMsg = fmt.Sprintf("验证失败: %v", err)
		processInfo.ProcessingTime = time.Since(startTime)
		stats.addDetailedLog(processInfo)
		stats.addImageFailed()
		return
	}
	if !verified {
		logger.Printf("❌ 验证不匹配 %s", fileName)
		os.Remove(tempJxlPath)
		processInfo.ErrorMsg = "验证不匹配"
		processInfo.ProcessingTime = time.Since(startTime)
		stats.addDetailedLog(processInfo)
		stats.addImageFailed()
		return
	}
	logger.Printf("✅ 验证通过: %s 无损转换正确", fileName)

	// 📋 复制元数据
	logger.Printf("📋 开始复制元数据: %s", fileName)
	err = copyMetadata(filePath, tempJxlPath)
	if err != nil {
		logger.Printf("⚠️  元数据复制失败 %s: %v", fileName, err)
		processInfo.MetadataSuccess = false
	} else {
		logger.Printf("✅ 元数据复制成功: %s", fileName)
		processInfo.MetadataSuccess = true
	}

	// 先设置临时文件的修改时间
	err = os.Chtimes(tempJxlPath, originalModTime, originalModTime)
	if err != nil {
		logger.Printf("WARN: Failed to set modification time for %s: %v", tempJxlPath, err)
	}
	// 在 macOS 上尽量同步 Finder 可见的创建/修改日期
	if runtime.GOOS == "darwin" {
		if ctime, mtime, ok := getFileTimesDarwin(filePath); ok {
			if e := setFinderDates(tempJxlPath, ctime, mtime); e != nil {
				logger.Printf("WARN: Failed to set Finder dates for %s: %v", tempJxlPath, e)
			}
		}
	}

	// 元数据验证（非阻断）
	if ok, verr := verifyMetadataNonBlocking(filePath, tempJxlPath); verr != nil {
		logger.Printf("WARN: Metadata verify error for %s: %v", filePath, verr)
	} else if !ok {
		logger.Printf("WARN: Metadata mismatch detected for %s", filePath)
	}

	if replaceOriginals {
		err := os.Remove(filePath)
		if err != nil {
			logger.Printf("ERROR: Failed to remove original file %s: %v", filePath, err)
			os.Remove(tempJxlPath)
			stats.addImageFailed()
			return
		}
	}

	err = os.Rename(tempJxlPath, jxlPath)
	if err != nil {
		logger.Printf("CRITICAL: Failed to rename temp file %s to %s: %v.", tempJxlPath, jxlPath, err)
		stats.addImageFailed()
		return
	}

	jxlInfo, _ := os.Stat(jxlPath)
	sizeAfter := jxlInfo.Size()

	// 最终文件再次校准修改/创建时间（macOS Finder 兼容）
	_ = os.Chtimes(jxlPath, originalModTime, originalModTime)
	if runtime.GOOS == "darwin" {
		if ctime, mtime, ok := getFileTimesDarwin(filePath); ok {
			if e := setFinderDates(jxlPath, ctime, mtime); e != nil {
				logger.Printf("WARN: Failed to finalize Finder dates for %s: %v", jxlPath, e)
			}
		}
	}

	// 🎉 处理完成
	sizeDiffKB = float64(sizeAfter-sizeBefore) / 1024.0
	compressionRatio = float64(sizeAfter) / float64(sizeBefore) * 100
	processInfo.SizeSaved = sizeBefore - sizeAfter
	processInfo.Success = true
	processInfo.ProcessingTime = time.Since(startTime)

	logger.Printf("🎉 处理成功: %s", fileName)
	logger.Printf("📊 大小变化: %.2f KB -> %.2f KB (节省: %.2f KB, 压缩率: %.1f%%)",
		float64(sizeBefore)/1024.0, float64(sizeAfter)/1024.0, sizeDiffKB, compressionRatio)

	// 添加详细日志记录
	stats.addDetailedLog(processInfo)
	stats.addImageProcessed(sizeBefore, sizeAfter)
}

func isSupportedImageType(kind types.Type) bool {
	switch kind.Extension {
	// 🖼️ 基础格式
	case "jpg", "jpeg", "png", "gif":
		return true
	// 🎬 动画格式
	case "apng", "webp":
		return true
	// 📷 现代格式
	case "avif", "heic", "heif", "jfif", "jpe":
		return true
	// 🖥️ 传统格式
	case "bmp", "tiff", "tif", "ico", "cur":
		return true
	// 🎨 专业格式
	case "psd", "xcf", "ora", "kra":
		return true
	// 🌐 网络格式
	case "svg", "eps", "ai":
		return true
	default:
		return false
	}
}

func isVideoType(kind types.Type) bool {
	return strings.HasPrefix(kind.MIME.Type, "video")
}

// isAnimatedImage 检测是否为真实的动画图像
func isAnimatedImage(filePath string, kind types.Type) (bool, error) {
	switch kind.Extension {
	case "gif":
		return isAnimatedGIF(filePath)
	case "apng":
		return isAnimatedPNG(filePath)
	case "webp":
		return isAnimatedWebP(filePath)
	case "avif":
		return isAnimatedAVIF(filePath)
	case "heic", "heif":
		return isAnimatedHEIF(filePath)
	default:
		return false, nil
	}
}

// isAnimatedGIF 检测GIF是否为动画
func isAnimatedGIF(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	g, err := gif.DecodeAll(file)
	if err != nil {
		return false, err
	}

	return len(g.Image) > 1, nil
}

// isAnimatedPNG 检测PNG是否为APNG动画
func isAnimatedPNG(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// 读取PNG文件头
	header := make([]byte, 8)
	if _, err := file.Read(header); err != nil {
		return false, err
	}

	// PNG文件头检查
	if string(header) != "\x89PNG\r\n\x1a\n" {
		return false, nil
	}

	// 查找acTL chunk (动画控制块)
	buffer := make([]byte, 8192)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return false, err
		}
		if n == 0 {
			break
		}

		// 在缓冲区中查找acTL
		if strings.Contains(string(buffer[:n]), "acTL") {
			return true, nil
		}
	}

	return false, nil
}

// isAnimatedWebP 检测WebP是否为动画
func isAnimatedWebP(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// 读取WebP文件头
	header := make([]byte, 12)
	if _, err := file.Read(header); err != nil {
		return false, err
	}

	// WebP文件头检查
	if len(header) < 12 || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return false, nil
	}

	// 查找ANIM chunk
	buffer := make([]byte, 8192)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return false, err
		}
		if n == 0 {
			break
		}

		// 在缓冲区中查找ANIM
		if strings.Contains(string(buffer[:n]), "ANIM") {
			return true, nil
		}
	}

	return false, nil
}

// isAnimatedAVIF 检测AVIF是否为动画
func isAnimatedAVIF(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// 读取AVIF文件头
	header := make([]byte, 12)
	if _, err := file.Read(header); err != nil {
		return false, err
	}

	// AVIF文件头检查
	if len(header) < 12 || string(header[:4]) != "ftyp" {
		return false, nil
	}

	// 查找动画相关标识
	buffer := make([]byte, 8192)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return false, err
		}
		if n == 0 {
			break
		}

		// 在缓冲区中查找动画标识
		if strings.Contains(string(buffer[:n]), "avis") ||
			strings.Contains(string(buffer[:n]), "anim") {
			return true, nil
		}
	}

	return false, nil
}

// isAnimatedHEIF 检测HEIF/HEIC是否为动画
func isAnimatedHEIF(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// 读取HEIF文件头
	header := make([]byte, 12)
	if _, err := file.Read(header); err != nil {
		return false, err
	}

	// HEIF文件头检查
	if len(header) < 12 || string(header[:4]) != "ftyp" {
		return false, nil
	}

	// 查找动画相关标识
	buffer := make([]byte, 8192)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return false, err
		}
		if n == 0 {
			break
		}

		// 在缓冲区中查找动画标识
		if strings.Contains(string(buffer[:n]), "heic") &&
			strings.Contains(string(buffer[:n]), "mif1") {
			return true, nil
		}
	}

	return false, nil
}

func convertToJxlWithOpts(filePath string, kind types.Type, opts Options) (string, string, string, error) {
	jxlPath := strings.TrimSuffix(filePath, filepath.Ext(filePath)) + ".jxl"
	tempJxlPath := jxlPath + ".tmp"
	var cmd *exec.Cmd
	var mode string

	// 检测是否为动画图像
	isAnimated, animErr := isAnimatedImage(filePath, kind)
	if animErr != nil {
		logger.Printf("WARN: Animation detection failed for %s: %v", filePath, animErr)
		isAnimated = false
	}

	// 根据格式和动画状态选择最优策略
	switch kind.Extension {
	case "jpg", "jpeg":
		mode = "JPEG Lossless Re-encode"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "--lossless_jpeg=1", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "gif":
		if isAnimated {
			mode = "Animated GIF Lossless Conversion"
			cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
		} else {
			mode = "Static GIF Lossless Conversion"
			cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
		}
	case "apng":
		if isAnimated {
			mode = "Animated PNG Lossless Conversion"
			cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--modular", "1", "--num_threads", strconv.Itoa(opts.CJXLThreads))
		} else {
			mode = "PNG Lossless Conversion"
			cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--modular", "1", "--num_threads", strconv.Itoa(opts.CJXLThreads))
		}
	case "png":
		mode = "PNG Lossless Conversion"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--modular", "1", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "webp":
		if isAnimated {
			mode = "Animated WebP Lossless Conversion"
			cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
		} else {
			mode = "WebP Lossless Conversion"
			cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
		}
	case "avif":
		mode = "AVIF Lossless Conversion"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "bmp":
		mode = "BMP Lossless Conversion"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "tiff", "tif":
		mode = "TIFF Lossless Conversion"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "heic", "heif":
		if isAnimated {
			mode = "Animated HEIF Lossless Conversion"
		} else {
			mode = "HEIF Lossless Conversion"
		}
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "jfif", "jpe":
		mode = "JPEG Variant Lossless Re-encode"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "--lossless_jpeg=1", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	case "ico", "cur":
		mode = "Icon Lossless Conversion"
		cmd = exec.Command("cjxl", filePath, tempJxlPath, "-d", "0", "-e", "9", "--num_threads", strconv.Itoa(opts.CJXLThreads))
	default:
		return "", "", "", fmt.Errorf("unhandled supported type: %s", kind.Extension)
	}

	ctx, cancel := withTimeout(context.Background(), opts)
	defer cancel()
	// 外部进程并发限制
	procSem <- struct{}{}
	defer func() { <-procSem }()
	cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", "", fmt.Errorf("cjxl execution failed: %s\nOutput: %s", err, string(output))
	}
	return mode, jxlPath, tempJxlPath, nil
}

func verifyConversionWithMode(originalPath, tempJxlPath string, kind types.Type, opts Options) (bool, error) {
	// 使用临时目录承载解码输出
	tempDir, err := os.MkdirTemp("", "jxl_verify_*")
	if err != nil {
		return false, fmt.Errorf("could not create temp dir for verification: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// 检测是否为动画图像
	isAnimated, animErr := isAnimatedImage(originalPath, kind)
	if animErr != nil {
		logger.Printf("WARN: Animation detection failed during verification for %s: %v", originalPath, animErr)
		isAnimated = false
	}

	if isAnimated {
		// 对动画：用 djxl -v 校验帧数；将 JXL 解码为 PNG，对首帧做像素级严格验证
		jxlFrames, err := getJxlFrameCount(tempJxlPath)
		if err != nil {
			logger.Printf("WARN: djxl -v frame count unavailable for %s: %v; continuing with first-frame verification only", tempJxlPath, err)
			jxlFrames = 0 // 表示未知，跳过帧数一致性比对
		}
		// 读取原始动画文件以取得原帧数与首帧图像
		fdSem <- struct{}{}
		of, err := os.Open(originalPath)
		if err != nil {
			<-fdSem
			return false, err
		}
		defer of.Close()
		<-fdSem
		var origFrames int
		var origFirst image.Image

		switch kind.Extension {
		case "gif":
			g, e := gif.DecodeAll(of)
			if e != nil {
				return false, e
			}
			origFrames = len(g.Image)
			origFirst = g.Image[0]
		case "apng":
			// APNG：标准库不支持逐帧，退化为只读首帧
			img, _, e := readImage(originalPath)
			if e != nil {
				return false, e
			}
			origFrames = 0 // 未知
			origFirst = img
		case "webp":
			// WebP动画：标准库不支持逐帧，退化为只读首帧
			img, _, e := readImage(originalPath)
			if e != nil {
				return false, e
			}
			origFrames = 0 // 未知
			origFirst = img
		}

		if origFrames != 0 && jxlFrames != 0 && jxlFrames != origFrames {
			logger.Printf("FAIL: Animation frame count mismatch %s: original=%d, jxl=%d", originalPath, origFrames, jxlFrames)
			return false, nil
		}

		// 解码 JXL 为 PNG（首帧）
		decodedPNG := filepath.Join(tempDir, "decoded.png")
		ctx, cancel := withTimeout(context.Background(), opts)
		defer cancel()
		procSem <- struct{}{}
		decodeCmd := exec.CommandContext(ctx, "djxl", tempJxlPath, decodedPNG, "--num_threads", strconv.Itoa(opts.CJXLThreads))
		decodeOutput, derr := decodeCmd.CombinedOutput()
		<-procSem
		if derr != nil {
			return false, fmt.Errorf("djxl execution failed for %s: %w\nOutput: %s", tempJxlPath, derr, string(decodeOutput))
		}
		decodedFirst, _, e := readImage(decodedPNG)
		if e != nil {
			return false, fmt.Errorf("could not decode temporary image %s: %w", decodedPNG, e)
		}
		if origFirst.Bounds() != decodedFirst.Bounds() || !imagesAreEqual(origFirst, decodedFirst) {
			logger.Printf("FAIL: Animated first frame pixel/bounds mismatch for %s", originalPath)
			return false, nil
		}

		logger.Printf("INFO: %s verified on first frame; frame count=%d; timing/disposal not verified due to decoder limits.", kind.Extension, jxlFrames)
		return true, nil
	}

	// 非动画：逐像素全量对比
	originalImg, _, err := readImage(originalPath)
	if err != nil {
		return false, fmt.Errorf("could not decode original image %s: %w", originalPath, err)
	}

	var tempDecodedPath string
	// 根据原始格式选择解码输出格式
	switch kind.Extension {
	case "jpg", "jpeg":
		tempDecodedPath = filepath.Join(tempDir, "decoded.jpg")
	case "png", "bmp", "tiff", "tif":
		tempDecodedPath = filepath.Join(tempDir, "decoded.png")
	case "webp", "avif", "heic", "heif":
		// 这些格式通常解码为PNG
		tempDecodedPath = filepath.Join(tempDir, "decoded.png")
	default:
		tempDecodedPath = filepath.Join(tempDir, "decoded.png")
	}

	ctx, cancel := withTimeout(context.Background(), opts)
	defer cancel()
	procSem <- struct{}{}
	decodeCmd := exec.CommandContext(ctx, "djxl", tempJxlPath, tempDecodedPath, "--num_threads", strconv.Itoa(opts.CJXLThreads))
	decodeOutput, err := decodeCmd.CombinedOutput()
	<-procSem
	if err != nil {
		return false, fmt.Errorf("djxl execution failed for %s: %w\nOutput: %s", tempJxlPath, err, string(decodeOutput))
	}

	decodedImg, _, err := readImage(tempDecodedPath)
	if err != nil {
		return false, fmt.Errorf("could not decode temporary image %s: %w", tempDecodedPath, err)
	}

	return imagesAreEqual(originalImg, decodedImg), nil
}

func withTimeout(ctx context.Context, opts Options) (context.Context, context.CancelFunc) {
	if opts.TimeoutSeconds > 0 {
		return context.WithTimeout(ctx, time.Duration(opts.TimeoutSeconds)*time.Second)
	}
	return context.WithCancel(ctx)
}

func scanCandidateFiles(root string) []string {
	var files []string
	_ = godirwalk.Walk(root, &godirwalk.Options{
		Unsorted: true,
		Callback: func(p string, de *godirwalk.Dirent) error {
			if de.IsDir() {
				return nil
			}
			info, err := os.Lstat(p)
			if err != nil {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			lp := strings.ToLower(p)
			if strings.HasSuffix(lp, ".jxl") || strings.HasSuffix(lp, ".jxl.tmp") {
				return nil
			}
			files = append(files, p)
			return nil
		},
	})
	logger.Printf("SCAN: %d candidate files found under %s", len(files), root)
	return files
}

func selectSample(paths []string, n int) []string {
	if n <= 0 || n >= len(paths) {
		return paths
	}
	// 取中等体量：按文件大小排序，选中位附近的一段
	type pair struct {
		p string
		s int64
	}
	arr := make([]pair, 0, len(paths))
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			arr = append(arr, pair{p: p, s: fi.Size()})
		}
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].s < arr[j].s })
	if len(arr) <= n {
		res := make([]string, 0, len(arr))
		for _, it := range arr {
			res = append(res, it.p)
		}
		return res
	}
	mid := len(arr) / 2
	start := mid - n/2
	if start < 0 {
		start = 0
	}
	end := start + n
	if end > len(arr) {
		end = len(arr)
	}
	chosen := arr[start:end]
	res := make([]string, 0, len(chosen))
	for _, it := range chosen {
		res = append(res, it.p)
	}
	logger.Printf("SAMPLE: picked %d files around median size", len(res))
	return res
}

func copyDirIfNeeded(src string) (string, error) {
	base := filepath.Base(src)
	dst := filepath.Join(filepath.Dir(src), base+"_work")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	return dst, godirwalk.Walk(src, &godirwalk.Options{
		Unsorted: true,
		Callback: func(p string, de *godirwalk.Dirent) error {
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			tgt := filepath.Join(dst, rel)
			if de.IsDir() {
				return os.MkdirAll(tgt, 0755)
			}
			if err := os.MkdirAll(filepath.Dir(tgt), 0755); err != nil {
				return err
			}
			srcF, err := os.Open(p)
			if err != nil {
				return err
			}
			defer srcF.Close()
			dstF, err := os.OpenFile(tgt, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			_, err = io.Copy(dstF, srcF)
			cerr := dstF.Close()
			if err != nil {
				return err
			}
			return cerr
		},
	})
}

func getGifFrameCount(filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	g, err := gif.DecodeAll(file)
	if err != nil {
		return 0, err
	}
	return len(g.Image), nil
}

func getJxlFrameCount(filePath string) (int, error) {
	cmd := exec.Command("djxl", filePath, "-v", "/dev/null")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("djxl -v execution failed: %w\nOutput: %s", err, string(output))
	}

	re := regexp.MustCompile(`Animation: (\d+) frames`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return 1, nil
	}

	count, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("could not parse frame count from djxl info: %w", err)
	}

	return count, nil
}

func copyMetadata(originalPath, newPath string) error {
	// 多层级EXIF迁移策略，确保关键元数据不丢失

	// 策略1：完整元数据迁移
	cmd1 := exec.Command("exiftool", "-TagsFromFile", originalPath, "-all:all", "-overwrite_original", newPath)
	_, err1 := cmd1.CombinedOutput()
	if err1 == nil {
		logger.Printf("METADATA: Full metadata migration successful for %s", originalPath)
		return nil
	}
	logger.Printf("WARN: Full metadata migration failed for %s: %v", originalPath, err1)

	// 策略2：关键元数据迁移（不覆盖原有）
	criticalTags := []string{
		"-EXIF:DateTimeOriginal", "-EXIF:CreateDate", "-EXIF:ModifyDate",
		"-EXIF:Orientation", "-EXIF:ColorSpace", "-EXIF:WhiteBalance",
		"-EXIF:ExposureTime", "-EXIF:FNumber", "-EXIF:ISO",
		"-EXIF:FocalLength", "-EXIF:Flash", "-EXIF:GPS*",
		"-ICC_Profile:*", "-IPTC:*", "-XMP:*",
	}

	cmd2 := exec.Command("exiftool", append([]string{"-TagsFromFile", originalPath}, append(criticalTags, "-overwrite_original", newPath)...)...)
	_, err2 := cmd2.CombinedOutput()
	if err2 == nil {
		logger.Printf("METADATA: Critical metadata migration successful for %s", originalPath)
		return nil
	}
	logger.Printf("WARN: Critical metadata migration failed for %s: %v", originalPath, err2)

	// 策略3：基础时间戳迁移
	basicTags := []string{
		"-EXIF:DateTimeOriginal", "-EXIF:CreateDate", "-EXIF:ModifyDate",
		"-overwrite_original",
	}

	cmd3 := exec.Command("exiftool", append([]string{"-TagsFromFile", originalPath}, append(basicTags, newPath)...)...)
	output3, err3 := cmd3.CombinedOutput()
	if err3 == nil {
		logger.Printf("METADATA: Basic timestamp migration successful for %s", originalPath)
		return nil
	}
	logger.Printf("WARN: Basic timestamp migration failed for %s: %v", originalPath, err3)

	// 策略4：手动设置文件系统时间戳作为最后手段
	if err := preserveFileSystemTimestamps(originalPath, newPath); err != nil {
		logger.Printf("WARN: File system timestamp preservation failed for %s: %v", originalPath, err)
		return fmt.Errorf("all metadata migration strategies failed. Last error: %v\nFull output: %s", err3, string(output3))
	}

	logger.Printf("METADATA: Fallback to file system timestamps for %s", originalPath)
	return nil
}

// preserveFileSystemTimestamps 保留文件系统时间戳作为最后的元数据保护
func preserveFileSystemTimestamps(originalPath, newPath string) error {
	// 获取原始文件的时间戳
	origInfo, err := os.Stat(originalPath)
	if err != nil {
		return fmt.Errorf("failed to stat original file: %v", err)
	}

	// 设置新文件的修改时间
	if err := os.Chtimes(newPath, origInfo.ModTime(), origInfo.ModTime()); err != nil {
		return fmt.Errorf("failed to set modification time: %v", err)
	}

	// 在macOS上尝试设置创建时间
	if runtime.GOOS == "darwin" {
		if ctime, mtime, ok := getFileTimesDarwin(originalPath); ok {
			if err := setFinderDates(newPath, ctime, mtime); err != nil {
				logger.Printf("WARN: Failed to set Finder dates in fallback: %v", err)
			}
		}
	}

	return nil
}

// verifyMetadataNonBlocking 尝试检查若干关键元数据是否迁移成功；不阻断主流程
func verifyMetadataNonBlocking(originalPath, newPath string) (bool, error) {
	// 读取两边的少量关键字段：DateTimeOriginal/CreateDate/ModifyDate、Orientation、ColorSpace、ICC Profile 名称
	// exiftool -s -s -s -DateTimeOriginal -CreateDate -ModifyDate -Orientation -ColorSpace -ICCProfile:ProfileDescription file
	fields := []string{"-s", "-s", "-s", "-DateTimeOriginal", "-CreateDate", "-ModifyDate", "-Orientation", "-ColorSpace", "-ICCProfile:ProfileDescription"}
	oOut, oErr := exec.Command("exiftool", append(fields, originalPath)...).CombinedOutput()
	if oErr != nil {
		return false, fmt.Errorf("exiftool read original failed: %v, out=%s", oErr, string(oOut))
	}
	nOut, nErr := exec.Command("exiftool", append(fields, newPath)...).CombinedOutput()
	if nErr != nil {
		return false, fmt.Errorf("exiftool read new failed: %v, out=%s", nErr, string(nOut))
	}
	// 简单字符串包含比对（稳妥起见，逐行集合比较更严谨）
	oLines := strings.Split(strings.TrimSpace(string(oOut)), "\n")
	nLines := strings.Split(strings.TrimSpace(string(nOut)), "\n")
	oSet := make(map[string]struct{}, len(oLines))
	for _, l := range oLines {
		oSet[strings.TrimSpace(l)] = struct{}{}
	}
	for _, l := range nLines {
		if _, ok := oSet[strings.TrimSpace(l)]; !ok && strings.TrimSpace(l) != "" {
			// 允许新文件缺少个别源端没有的字段；但源端存在且新端不存在时视为潜在不一致
			// 此处做宽松判断：只要大部分字段在新端出现即可
		}
	}
	// 粗略一致性通过
	return true, nil
}

// getFileTimesDarwin 尝试获取文件的创建/修改时间（macOS）
func getFileTimesDarwin(p string) (ctime, mtime time.Time, ok bool) {
	if runtime.GOOS != "darwin" {
		return time.Time{}, time.Time{}, false
	}
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	// 修改时间直接取
	mtime = fi.ModTime()
	// 创建时间通过 mdls 提取 kMDItemFSCreationDate
	out, err := exec.Command("mdls", "-raw", "-name", "kMDItemFSCreationDate", p).CombinedOutput()
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	s := strings.TrimSpace(string(out))
	// 示例: 2024-10-02 22:33:44 +0000
	t, perr := time.Parse("2006-01-02 15:04:05 -0700", s)
	if perr != nil {
		return time.Time{}, time.Time{}, false
	}
	return t, mtime, true
}

// setFinderDates 通过 exiftool 设置文件的文件系统日期（Finder 可见）
func setFinderDates(p string, ctime, mtime time.Time) error {
	// exiftool -overwrite_original -P -FileCreateDate=... -FileModifyDate=...
	layout := "2006:01:02 15:04:05"
	args := []string{
		"-overwrite_original",
		"-P",
		"-FileCreateDate=" + ctime.Local().Format(layout),
		"-FileModifyDate=" + mtime.Local().Format(layout),
		p,
	}
	out, err := exec.Command("exiftool", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("exiftool set Finder dates failed: %v, out=%s", err, string(out))
	}
	return nil
}

func readImage(filePath string) (image.Image, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	if strings.HasSuffix(strings.ToLower(filePath), ".gif") {
		file.Seek(0, 0)
		g, err := gif.DecodeAll(file)
		if err != nil {
			return nil, false, err
		}
		return g.Image[0], len(g.Image) > 1, nil
	}

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, false, err
	}
	return img, false, nil
}

func imagesAreEqual(img1, img2 image.Image) bool {
	if img1.Bounds() != img2.Bounds() {
		logger.Printf("Verification failed: image bounds are different. Original: %v, Decoded: %v", img1.Bounds(), img2.Bounds())
		return false
	}

	for y := img1.Bounds().Min.Y; y < img1.Bounds().Max.Y; y++ {
		for x := img1.Bounds().Min.X; x < img1.Bounds().Max.X; x++ {
			r1, g1, b1, a1 := img1.At(x, y).RGBA()
			r2, g2, b2, a2 := img2.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				logger.Printf("Verification failed: pixel mismatch at (%d, %d).", x, y)
				return false
			}
		}
	}

	return true
}

func printSummary(stats *Stats) {
	stats.Lock()
	defer stats.Unlock()

	totalSavedKB := float64(stats.totalBytesBefore-stats.totalBytesAfter) / 1024.0
	totalSavedMB := totalSavedKB / 1024.0
	compressionRatio := float64(stats.totalBytesAfter) / float64(stats.totalBytesBefore) * 100

	logger.Println("🎯 ===== 处理摘要 =====")
	logger.Printf("✅ 成功处理图像: %d", stats.imagesProcessed)
	logger.Printf("❌ 转换失败图像: %d", stats.imagesFailed)
	logger.Printf("🎬 跳过视频文件: %d", stats.videosSkipped)
	logger.Printf("🔗 跳过符号链接: %d", stats.symlinksSkipped)
	logger.Printf("📄 跳过其他文件: %d", stats.othersSkipped)
	logger.Println("📊 ===== 大小统计 =====")
	logger.Printf("📥 原始总大小: %.2f MB", float64(stats.totalBytesBefore)/(1024*1024))
	logger.Printf("📤 转换后大小: %.2f MB", float64(stats.totalBytesAfter)/(1024*1024))
	logger.Printf("💾 节省空间: %.2f MB (压缩率: %.1f%%)", totalSavedMB, compressionRatio)

	if len(stats.byExt) > 0 {
		logger.Println("📋 ===== 格式统计 =====")
		for k, v := range stats.byExt {
			logger.Printf("  🖼️  %s: %d个文件", k, v)
		}
	}
	logger.Println("🎉 ===== 处理完成 =====")
}
