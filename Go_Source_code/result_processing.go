package main

import (
	"context"
	"os"
	"strings"
	"fmt"
)

func resultProcessingStage(ctx context.Context, app *AppContext, resultChan <-chan *ConversionResult) error {
	for result := range resultChan {
		if result.Error != nil {
			app.failCount.Add(1)
		} else if strings.HasPrefix(result.Decision, "SKIP") {
			app.skipCount.Add(1)
		} else if strings.HasPrefix(result.Decision, "DELETE") {
			app.deleteCount.Add(1)
		} else {
			app.successCount.Add(1)
			if result.NewSize < result.OriginalSize {
				app.totalDecreased.Add(result.OriginalSize - result.NewSize)
			} else if result.NewSize > result.OriginalSize {
				app.totalIncreased.Add(result.NewSize - result.OriginalSize)
			}
			if app.Config.Mode != "quality" {
				app.smartDecisionsCount.Add(1)
			}
			if strings.Contains(result.Tag, "Lossless") && result.NewSize < result.NewSize {
				app.losslessWinsCount.Add(1)
			}
			// 只有在成功转换时才记录结果
			statusLine := fmt.Sprintf("%s|%s|%d|%d", "SUCCESS", result.Tag, result.OriginalSize, result.NewSize)
			resultFilePath := getResultFilePath(app.ResultsDir, result.OriginalPath)
			os.WriteFile(resultFilePath, []byte(statusLine), 0644)
		}
		app.processedCount.Add(1)
	}
	return nil
}