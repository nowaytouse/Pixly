package main

import (
	"context"
	cryptorand "crypto/rand" // 使用别名避免冲突
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand" // 添加math/rand包
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

type UserChoice int

const (
	ChoiceSkip UserChoice = iota
	ChoiceRepair
	ChoiceDelete
	ChoiceNotApplicable
	ChoiceProcess
)

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
						// 修复随机数生成问题
						randNum, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1000))
						if err != nil {
							// 如果crypto/rand失败，使用math/rand作为备选
							jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
							time.Sleep(backoff + jitter)
							continue
						}
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
