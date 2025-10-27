package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// resultProcessingStage processes the results from the conversion stage,
// updating counters and writing result files for the resume feature.
func resultProcessingStage(ctx context.Context, app *AppContext, resultChan <-chan *ConversionResult) error {
	for {
		select {
		case result, ok := <-resultChan:
			if !ok {
				return nil // Channel is closed
			}

			// Always increment the total processed counter
			app.processedCount.Add(1)

			if result.Error != nil {
				app.failCount.Add(1)
				app.Logger.Warn("Processing failed", "file", filepath.Base(result.OriginalPath), "error", result.Error)
				continue
			}

			switch result.Decision {
			case "SUCCESS":
				app.successCount.Add(1)
				if result.NewSize < result.OriginalSize {
					app.totalDecreased.Add(result.OriginalSize - result.NewSize)
				} else {
					app.totalIncreased.Add(result.NewSize - result.OriginalSize)
				}

				// Specific counters for efficiency/auto modes - only in auto mode for quality stats
				if app.Config.Mode == "auto" {
					app.smartDecisionsCount.Add(1)
					if result.Task.ConversionType == ConversionTypeLossless && result.NewSize < result.OriginalSize {
						app.losslessWinsCount.Add(1)
					}
				} else if app.Config.Mode == "efficiency" {
					// Efficiency mode has different stats tracking
					app.smartDecisionsCount.Add(1)
				}

				// Write a result file for the resume functionality
				statusLine := fmt.Sprintf("SUCCESS|%s|%d|%d", result.Task.TargetFormat, result.OriginalSize, result.NewSize)
				resultFilePath := getResultFilePath(app.ResultsDir, result.OriginalPath)
				os.WriteFile(resultFilePath, []byte(statusLine), 0644)

			case "SKIP", "SKIP_NO_IMPROVEMENT":
				app.skipCount.Add(1)

			case "DELETE_SUCCESS":
				// This counter is already incremented in the conversion stage, but this is a safe place to do it too.
				// app.deleteCount.Add(1)

			default: // Any other decision is considered a failure
				app.failCount.Add(1)
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// getResultFilePath generates a unique path for a result file.
func getResultFilePath(resultsDir, originalPath string) string {
	hash := sha1.Sum([]byte(originalPath))
	return filepath.Join(resultsDir, hex.EncodeToString(hash[:]))
}
