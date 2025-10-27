package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// 版本号升级为20.2.8，符合要求中的"必须要升级版本号,以避免混淆情况"
const Version = "20.2.8-GO-TITANIUM-STREAMING-ENHANCED"

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
	"image/jpeg":       true,
	"image/png":        true,
	"image/gif":        true,
	"image/webp":       true,
	"image/heic":       true,
	"image/heif":       true,
	"image/tiff":       true,
	"image/bmp":        true,
	"image/svg+xml":    true,
	"image/avif":       true,
	"image/apng":       true,
	"video/mp4":        true,
	"video/quicktime":  true,
	"video/x-msvideo":  true,
	"video/x-matroska": true,
	"video/x-flv":      true,
	"video/3gpp":       true,
	"video/3gpp2":      true,
	"video/mpeg":       true,
	"video/x-ms-wmv":   true,
	"video/x-ms-asf":   true,
	"video/ogg":        true,
	"video/webm":       true,
}

func isMediaFile(mime string) bool {
	return mediaMimeWhitelist[mime]
}

func getMimeType(ctx context.Context, f string) (string, error) {
	out, err := runCmd(ctx, "file", "--mime-type", "-b", f)
	if err == nil && !strings.Contains(out, "application/octet-stream") {
		return out, nil
	}
	return "application/octet-stream", errors.New("unknown mime type")
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
