package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

// --- Enums for Conversion Logic ---

type MediaType string

const (
	Static   MediaType = "Static"
	Animated MediaType = "Animated"
	Video    MediaType = "Video"
)

type TargetFormat string

const (
	TargetFormatJXL  TargetFormat = "jxl"
	TargetFormatAVIF TargetFormat = "avif"
	TargetFormatMOV  TargetFormat = "mov"
)

type ConversionType string

const (
	ConversionTypeLossless ConversionType = "Lossless"
	ConversionTypeLossy    ConversionType = "Lossy"
)

type Action int

const (
	ActionConvert Action = iota
	ActionSkip
	ActionDelete
)

// --- Main Conversion Stage ---

// conversionStage manages the pool of workers that process file tasks.
func conversionStage(ctx context.Context, app *AppContext, taskChan <-chan *FileTask, resultChan chan<- *ConversionResult) error {
	app.Logger.Info("转换阶段开始", "channel_open", taskChan != nil)
	
	g, gCtx := errgroup.WithContext(ctx)
	// Limit concurrency based on config, but allow some flexibility
	workerCount := app.Config.ConcurrentJobs
	if workerCount <= 0 {
		workerCount = 4 // Default to 4 if not set
	}
	g.SetLimit(workerCount)
	
	// Count tasks received
	taskCount := 0

	for task := range taskChan {
		taskCount++
		app.Logger.Info("收到转换任务", "file", filepath.Base(task.Path), "target_format", task.TargetFormat, "conversion_type", task.ConversionType)
		
		// Check if context has been cancelled before starting a new task
		if gCtx.Err() != nil {
			app.Logger.Warn("上下文已取消，停止处理任务")
			break
		}

		task := task // Local copy for the goroutine
		g.Go(func() error {
			app.Logger.Info("开始处理单个任务", "file", filepath.Base(task.Path))
			result := processSingleTask(gCtx, app, task)
			app.Logger.Info("任务处理完成", "file", filepath.Base(task.Path), "decision", result.Decision)

			// Send result to the next stage, unless context is cancelled
			select {
			case resultChan <- result:
			case <-gCtx.Done():
				return gCtx.Err()
			}
			return nil
		})
	}
	
	app.Logger.Info("转换阶段结束", "processed_tasks", taskCount)

	return g.Wait()
}

// processSingleTask handles the logic for a single file, including retries.
func processSingleTask(ctx context.Context, app *AppContext, task *FileTask) *ConversionResult {
	result := &ConversionResult{
		OriginalPath: task.Path,
		OriginalSize: task.Size,
		Task:         task,
	}

	// Handle actions that don't require conversion (Delete/Skip)
	if task.Action == ActionDelete {
		if err := os.Remove(task.Path); err != nil {
			result.Error = fmt.Errorf("failed to delete file: %w", err)
			result.Decision = "FAIL_DELETE"
		} else {
			result.Decision = "DELETE_SUCCESS"
			app.deleteCount.Add(1)
		}
		return result
	}
	if task.Action == ActionSkip {
		result.Decision = "SKIP"
		return result
	}

	// ---
	// Conversion Logic
	// ---
	var finalPath string
	var err error

	// Retry loop
	for i := 0; i <= app.Config.MaxRetries; i++ {
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			return result
		}

		finalPath, err = convertFile(ctx, app, task)
		if err == nil {
			if i > 0 {
				app.retrySuccessCount.Add(1)
			}
			break // Success
		}
		task.Logger.Warn("Conversion attempt failed", "file", filepath.Base(task.Path), "attempt", i+1, "error", err)
	}

	if err != nil {
		result.Error = err
		result.Decision = "FAIL_CONVERSION"
		return result
	}

	if finalPath == "" {
		result.Decision = "SKIP_NO_IMPROVEMENT"
		return result
	}

	// ---
	// Post-Conversion
	// ---
	newSize, _ := getFileSize(finalPath)
	result.NewSize = newSize
	result.FinalPath = finalPath
	result.Decision = "SUCCESS"

	// Create backup and replace original file
	if createBackup(task.Path, app.Config.BackupDir, app.Config.EnableBackups, task.Logger) {
		// Preserve metadata from original file to the new file
		if err := preserveMetadata(ctx, task.Path, finalPath, task.Logger); err != nil {
			task.Logger.Warn("Failed to preserve metadata", "error", err)
		}

		// Construct final destination path
		destPath := strings.TrimSuffix(task.Path, task.Ext) + "." + string(task.TargetFormat)

		// Replace original file
		if err := os.Rename(finalPath, destPath); err != nil {
			result.Error = fmt.Errorf("failed to replace original file: %w", err)
			result.Decision = "FAIL_REPLACE"
			os.Remove(finalPath) // cleanup temp file
			return result
		}
		result.FinalPath = destPath
	} else {
		result.Decision = "FAIL_BACKUP"
		os.Remove(finalPath)
	}

	return result
}

// convertFile contains the core logic for calling the correct command-line tool.
func convertFile(ctx context.Context, app *AppContext, task *FileTask) (string, error) {
	outputPath := filepath.Join(app.TempDir, fmt.Sprintf("%s.%s", generateRandomString(12), task.TargetFormat))

	var cmd *exec.Cmd

	switch task.TargetFormat {
	case TargetFormatJXL:
		// For static images, use libjxl (cjxl) as priority
		if task.Type == Static {
			args := []string{task.Path, outputPath, "-e", "7"} // Default effort
			if task.ConversionType == ConversionTypeLossless {
				args = append(args, "-d", "0")
				if strings.EqualFold(task.Ext, ".jpg") || strings.EqualFold(task.Ext, ".jpeg") {
					args = append(args, "--lossless_jpeg=1")
				}
			} else {
				// Lossy JXL - requires probing for best quality
				// For now, using a fixed quality. Probing can be added here.
				args = append(args, "-q", "85")
			}
			cmd = exec.CommandContext(ctx, "cjxl", args...)
		} else {
			// For animated images, fall back to ffmpeg
			cmd = buildFfmpegCommand(ctx, app, task, outputPath)
		}

	case TargetFormatAVIF:
		// For animated AVIF, use ffmpeg as priority
		// For static AVIF, use avifenc for latest features
		if task.Type == Animated {
			cmd = buildFfmpegCommand(ctx, app, task, outputPath)
		} else {
			// Static AVIF with avifenc
			args := []string{"--speed", "6"}
			
			// Quality settings
			if task.ConversionType == ConversionTypeLossless {
				args = append(args, "--lossless")
			} else {
				// For sticker mode, use aggressive compression
				if task.IsStickerMode {
					args = append(args, "--min", "40", "--max", "63")
				} else if app.Config.Mode == "efficiency" {
					args = append(args, "--min", "20", "--max", "40")
				} else {
					args = append(args, "--min", "25", "--max", "35")
				}
			}
			
			args = append(args, task.Path, outputPath)
			cmd = exec.CommandContext(ctx, "avifenc", args...)
		}

	case TargetFormatMOV:
		cmd = buildFfmpegCommand(ctx, app, task, outputPath)

	default:
		return "", fmt.Errorf("unsupported target format: %s", task.TargetFormat)
	}

	if cmd == nil {
		return "", fmt.Errorf("failed to build command for task")
	}

	// Run the command
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cmd failed: %s\n%s", err, stderr.String())
	}

	// For lossy conversions, check if the new file is smaller
	if task.ConversionType == ConversionTypeLossy {
		newSize, err := getFileSize(outputPath)
		if err != nil {
			return "", fmt.Errorf("failed to get size of new file: %w", err)
		}
		if newSize >= task.Size {
			os.Remove(outputPath)
			return "", nil // Return nil error but empty path to indicate skipping
		}
	}

	return outputPath, nil
}

// buildFfmpegCommand constructs the correct ffmpeg command for videos and animated images.
func buildFfmpegCommand(ctx context.Context, app *AppContext, task *FileTask, outputPath string) *exec.Cmd {
	args := []string{"-y", "-i", task.Path} // Removed extra space

	if task.TargetFormat == TargetFormatAVIF {
		// Animated AVIF with guaranteed animation support
		args = append(args, "-c:v", "libsvtav1")
		crf := 35 // Default for efficiency
		if task.ConversionType == ConversionTypeLossless {
			crf = 0
		} else if task.IsStickerMode {
			crf = 50 // Aggressive compression for stickers
		} else if app.Config.Mode == "efficiency" {
			// Efficiency mode compression settings
			crf = app.Config.CRF
		}
		args = append(args, "-crf", strconv.Itoa(crf), "-preset", "8")

		// Preserve FPS and frame count exactly
		fps, err := getSourceFPS(ctx, task.Path)
		if err == nil {
			args = append(args, "-r", fps)
		}
		
		// Ensure all frames are copied to preserve animation
		args = append(args, "-pix_fmt", "yuv420p")

	} else if task.TargetFormat == TargetFormatMOV {
		// Video to MOV
		args = append(args, "-c:v", "libx265", "-preset", "medium", "-c:a", "copy", "-c:s", "copy")
		crf := 28 // Default
		if task.ConversionType == ConversionTypeLossless {
			crf = 0
		}
		args = append(args, "-crf", strconv.Itoa(crf))
	}

	args = append(args, outputPath)
	return exec.CommandContext(ctx, "ffmpeg", args...)
}

// getSourceFPS uses ffprobe to get the frame rate of a video or animated image.
func getSourceFPS(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=r_frame_rate", "-of", "default=noprint_wrappers=1:nokey=1", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	fps := strings.TrimSpace(out.String())
	// ffprobe returns fps as a fraction e.g. "30/1", check for this
	if !strings.Contains(fps, "/") {
		return "", fmt.Errorf("unexpected fps format: %s", fps)
	}
	return fps, nil
}

// preserveMetadata copies metadata from the source file to the destination file.
func preserveMetadata(ctx context.Context, src, dest string, logger Logger) error {
	// First, attempt to extract ICC profile
	iccPath := dest + ".icc"
	iccCmd := exec.CommandContext(ctx, "exiftool", "-icc_profile", "-b", src)
	iccOut, err := iccCmd.Output()
	if err == nil && len(iccOut) > 0 {
		if err := os.WriteFile(iccPath, iccOut, 0644); err == nil {
			defer os.Remove(iccPath)
			// Apply ICC profile using exiftool
			applyCmd := exec.CommandContext(ctx, "exiftool", "-overwrite_original", fmt.Sprintf("-icc_profile<=%s", iccPath), dest)
			if err := applyCmd.Run(); err != nil {
				logger.Warn("Failed to apply ICC profile", "file", filepath.Base(dest), "error", err)
			}
		}
	} else {
		// Log failure to extract but don't stop the process
		logger.Info("No ICC profile found or failed to extract", "file", filepath.Base(src))
	}

	// Copy other tags
	cmd := exec.CommandContext(ctx, "exiftool", "-tagsfromfile", src, "-all:all", "-overwrite_original", dest)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exiftool failed to copy tags: %w", err)
	}
	return nil
}