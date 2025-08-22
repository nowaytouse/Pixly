package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

// --- Quality Level Enum ---
type QualityLevel int

const (
	QualityExtremeHigh QualityLevel = iota
	QualityHigh
	QualityMedium
	QualityLow
	QualityExtremeLow
	QualityUnknown
)

func (ql QualityLevel) String() string {
	switch ql {
	case QualityExtremeHigh:
		return "极高质量"
	case QualityHigh:
		return "高质量"
	case QualityMedium:
		return "中质量"
	case QualityLow:
		return "低质量"
	case QualityExtremeLow:
		return "极低质量"
	default:
		return "未知质量"
	}
}

// --- Combined Discovery and Assessment Stage ---

// discoveryAndAssessmentStage walks the directory, classifies, assesses, and creates tasks.
func assessmentStage(ctx context.Context, app *AppContext, pathChan <-chan string, taskChan chan<- *FileTask, lowQualityChan chan<- *FileTask) error {
	g, gCtx := errgroup.WithContext(ctx)
	workerCount := runtime.NumCPU()
	if workerCount > 8 {
		workerCount = 8 // Limit concurrency to avoid resource exhaustion
	}
	g.SetLimit(workerCount)

	for path := range pathChan {
		path := path // Local copy for the goroutine
		g.Go(func() error {
			info, err := os.Stat(path)
			if err != nil {
				app.Logger.Warn("Failed to get file info", "path", path, "error", err)
				return nil
			}

			mime, err := getMimeType(gCtx, path)
			if err != nil || !isSupportedMedia(mime) {
				return nil // Skip unsupported files
			}

			mediaType, err := determineMediaType(gCtx, path, mime)
			if err != nil {
				return nil // Skip if type can't be determined
			}

			quality, err := assessFileQuality(gCtx, path, mime, info.Size(), app.Config.QualityConfig)
			if err != nil {
				app.Logger.Warn("Failed to assess quality", "path", path, "error", err)
				quality = QualityUnknown // Assign unknown quality on failure
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
				Path:     path,
				Ext:      strings.ToLower(filepath.Ext(path)),
				Size:     info.Size(),
				MimeType: mime,
				Type:     mediaType,
				Quality:  quality,
				Logger:   app.Logger.With("file", filepath.Base(path)),
				TempDir:  app.TempDir,
			}

			if quality == QualityExtremeLow {
				select {
				case lowQualityChan <- task:
				case <-gCtx.Done():
					return gCtx.Err()
				}
			} else {
				select {
				case taskChan <- task:
				case <-gCtx.Done():
					return gCtx.Err()
				}
			}
			return nil
		})
	}

	return g.Wait()
}

// determineMediaType classifies a file as static, animated, or video.
func determineMediaType(ctx context.Context, path, mime string) (MediaType, error) {
	if strings.HasPrefix(mime, "video/") {
		return Video, nil
	}
	if strings.HasPrefix(mime, "image/") {
		// For GIFs, WebPs, etc., check the frame count to determine if animated.
		// First try ffprobe for more reliable detection
		cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", 
			"-show_entries", "stream=nb_frames", "-of", "default=nokey=1:noprint_wrappers=1", path)
		out, err := cmd.Output()
		if err == nil {
			numFrames, err := strconv.Atoi(strings.TrimSpace(string(out)))
			if err == nil && numFrames > 1 {
				return Animated, nil
			}
		}
		
		// Fallback to ImageMagick
		cmd = exec.CommandContext(ctx, "magick", "identify", "-format", "%n", path)
		out, err = cmd.Output()
		if err != nil {
			// If both fail, assume it's a static image.
			return Static, nil
		}
		numFrames, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		if numFrames > 1 {
			return Animated, nil
		}
		return Static, nil
	}
	return "", fmt.Errorf("unsupported MIME type: %s", mime)
}

// assessFileQuality evaluates the quality of a given file.
func assessFileQuality(ctx context.Context, path, mime string, size int64, qc QualityConfig) (QualityLevel, error) {
	if size < 5*1024 { // 5KB threshold for extremely low quality
		return QualityExtremeLow, nil
	}

	if strings.HasPrefix(mime, "image/") {
		// More accurate assessment using ImageMagick
		var out []byte
		var err error
		cmd := exec.CommandContext(ctx, "magick", "identify", "-format", "%w %h %Q", path)
		out, err = cmd.Output()
		if err != nil {
			// Fallback to ffprobe for more formats
			cmd = exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", 
				"-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", path)
			out, err = cmd.Output()
			if err != nil {
				return QualityUnknown, err
			}
		}
		
		parts := strings.Fields(string(out))
		if len(parts) < 2 {
			return QualityUnknown, fmt.Errorf("could not parse image dimensions")
		}
		
		w, err1 := strconv.ParseFloat(parts[0], 64)
		h, err2 := strconv.ParseFloat(parts[1], 64)
		
		if err1 != nil || err2 != nil {
			return QualityUnknown, fmt.Errorf("could not parse image dimensions")
		}
		
		pixels := w * h
		if pixels == 0 {
			return QualityExtremeLow, nil
		}

		bpp := (float64(size) * 8) / pixels

		if bpp > qc.ExtremeHighThreshold {
			return QualityExtremeHigh, nil
		}
		if bpp > qc.HighThreshold {
			return QualityHigh, nil
		}
		if bpp > qc.MediumThreshold {
			return QualityMedium, nil
		}
		if bpp > qc.LowThreshold {
			return QualityLow, nil
		}
		return QualityExtremeLow, nil

	} else if strings.HasPrefix(mime, "video/") {
		// More accurate assessment based on bitrate and resolution
		cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", 
			"format=bit_rate:stream=width,height", "-of", "default=noprint_wrappers=1:nokey=0", path)
		out, err := cmd.Output()
		if err != nil {
			return QualityUnknown, err
		}
		
		// Parse output to get bitrate and resolution
		lines := strings.Split(string(out), "\n")
		var bitrate, width, height float64
		
		for _, line := range lines {
			if strings.Contains(line, "bit_rate=") {
				parts := strings.Split(line, "=")
				if len(parts) == 2 {
					bitrate, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				}
			} else if strings.Contains(line, "width=") {
				parts := strings.Split(line, "=")
				if len(parts) == 2 {
					width, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				}
			} else if strings.Contains(line, "height=") {
				parts := strings.Split(line, "=")
				if len(parts) == 2 {
					height, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				}
			}
		}
		
		// Calculate bitrate per pixel for more accurate quality assessment
		pixels := width * height
		if pixels > 0 {
			bpp := bitrate / pixels
			if bpp > 0.5 { // High quality threshold
				return QualityExtremeHigh, nil
			} else if bpp > 0.2 {
				return QualityHigh, nil
			} else if bpp > 0.1 {
				return QualityMedium, nil
			} else if bpp > 0.05 {
				return QualityLow, nil
			}
			return QualityExtremeLow, nil
		}
		
		// Fallback to simple bitrate assessment
		if bitrate > 10000000 { // 10 Mbps
			return QualityExtremeHigh, nil
		} else if bitrate > 5000000 { // 5 Mbps
			return QualityHigh, nil
		} else if bitrate > 2000000 { // 2 Mbps
			return QualityMedium, nil
		} else if bitrate > 500000 { // 0.5 Mbps
			return QualityLow, nil
		}
		return QualityExtremeLow, nil
	}

	return QualityUnknown, nil
}