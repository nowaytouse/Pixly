package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func discoveryStage(ctx context.Context, app *AppContext, pathChan chan<- string) error {
	defer close(pathChan)
	if err := checkDirectoryPermissions(app.Config.TargetDir); err != nil {
		return fmt.Errorf("目标目录权限检查失败: %w", err)
	}
	err := filepath.Walk(app.Config.TargetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			app.Logger.Warn("遍历目录时出错", "path", path, "error", err)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if base == ".backups" || base == ".media_conversion_results" || base == ".logs" {
				return filepath.SkipDir
			}
			return nil
		}
		app.filesFoundCount.Add(1)
		// 仅当明确要求覆盖或结果文件不存在时才处理
		if !app.Config.Overwrite {
			resultPath := getResultFilePath(app.ResultsDir, path)
			if fileExists(resultPath) {
				// 检查结果文件是否表示成功转换
				content, err := os.ReadFile(resultPath)
				if err == nil {
					parts := strings.Split(string(content), "|")
					// 仅当结果以"SUCCESS"开头时才视为已处理
					if len(parts) >= 1 && parts[0] == "SUCCESS" {
						app.resumedCount.Add(1)
						return nil
					}
				}
			}
		}
		if shouldSkipEarly(path) {
			app.skipCount.Add(1)
			return nil
		}
		select {
		case pathChan <- path:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	return err
}

func checkDirectoryPermissions(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在: %w", err)
	}
	if !info.IsDir() {
		return errors.New("路径不是目录")
	}
	testFile := filepath.Join(dir, ".permission_test_"+fmt.Sprintf("%d", time.Now().Unix()))
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("目录无写入权限: %w", err)
	}
	if err := os.Remove(testFile); err != nil {
		return fmt.Errorf("无法清理测试文件: %w", err)
	}
	return nil
}