package main

import (
	"context"
	"errors"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

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

func assessQuality(ctx context.Context, f, mime string, size int64, qc QualityConfig) (QualityLevel, error) {
	// 快速路径：极小文件直接标记为极低质量
	if size < 5*1024 {
		return QualityExtremeLow, nil
	}

	// 快速路径：极小尺寸图像直接标记为低质量
	if strings.HasPrefix(mime, "image/") {
		out, err := runCmd(ctx, "magick", "identify", "-format", "%w %h", f)
		if err == nil {
			parts := strings.Fields(out)
			if len(parts) >= 2 {
				width, _ := strconv.Atoi(parts[0])
				height, _ := strconv.Atoi(parts[1])
				if width < 100 && height < 100 {
					return QualityExtremeLow, nil
				}
			}
		}
	}

	// 快速路径：极小分辨率视频直接标记为低质量
	if strings.HasPrefix(mime, "video/") {
		out, err := runCmd(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
			"-show_entries", "stream=width,height", "-of", "csv=p=0", f)
		if err == nil {
			parts := strings.Split(strings.TrimSpace(out), ",")
			if len(parts) >= 2 {
				width, _ := strconv.Atoi(parts[0])
				height, _ := strconv.Atoi(parts[1])
				if width < 240 || height < 240 {
					return QualityExtremeLow, nil
				}
			}
		}
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
		// 使用更详细的视频分析 - 仅分析前几帧，提高性能
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
		// 添加噪声分析 - 仅分析关键帧，提高性能
		noiseOut, _ := runCmd(ctx, "ffmpeg", "-i", f, "-vframes", "1", "-vf", "noise=0:0:0:0", "-f", "null", "-")
		noiseMean := 0.0
		noiseRe := regexp.MustCompile(`mean\[(\d+\.\d+)\]`)
		if noiseRe.FindStringSubmatch(noiseOut) != nil {
			noiseMean, _ = strconv.ParseFloat(noiseRe.FindStringSubmatch(noiseOut)[1], 64)
		}
		// 添加模糊检测 - 仅分析中心区域，提高性能
		blurOut, _ := runCmd(ctx, "ffmpeg", "-i", f, "-vframes", "1", "-vf", "crop=iw/2:ih/2,fft", "-f", "null", "-")
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

func assessmentStage(ctx context.Context, app *AppContext, pathChan <-chan string, taskChan chan<- *FileTask, lowQualityChan chan<- *FileTask) error {
	g, ctx := errgroup.WithContext(ctx)
	// 动态调整并发数，避免内存压力
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8 // 限制最大并发数，避免内存压力
	}
	g.SetLimit(numWorkers)
	for i := 0; i < numWorkers; i++ {
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
