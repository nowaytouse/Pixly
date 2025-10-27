package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Counter is a thread-safe counter for tracking statistics.
type Counter int64

func (c *Counter) Add(n int64)   { atomic.AddInt64((*int64)(c), n) }
func (c *Counter) Load() int64   { return atomic.LoadInt64((*int64)(c)) }
func (c *Counter) Store(n int64) { atomic.StoreInt64((*int64)(c), n) }

// runCmd executes a command and returns its stdout, or an error with stderr.
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
		return "", fmt.Errorf("command failed: %s %v. stderr: %s", name, args, errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// getFileSize returns the size of a file.
func getFileSize(p string) (int64, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileExists checks if a file exists at the given path.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return !os.IsNotExist(err)
}

// formatBytes converts bytes to a human-readable string.
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

// cleanPath cleans a file path provided by user drag-and-drop.
func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	return p
}

// getMimeType returns the MIME type of a file.
func getMimeType(ctx context.Context, f string) (string, error) {
	out, err := runCmd(ctx, "file", "--mime-type", "-b", f)
	if err != nil {
		return "", err
	}
	return strings.Split(out, ";")[0], nil
}

// isSupportedMedia checks if a file is a supported media type.
func isSupportedMedia(mime string) bool {
	// Check basic support
	if !(strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/")) {
		return false
	}
	
	// Exclude unsupported image formats
	unsupportedImageTypes := []string{
		"image/vnd.adobe.photoshop",  // PSD files
		"image/x-photoshop",
		"image/photoshop",
		"image/x-psd",
		"application/photoshop",
		"application/x-photoshop",
		"application/psd",
		"image/vnd.microsoft.icon",   // ICO files
		"image/x-icon",
		"image/icon",
	}
	
	for _, unsupported := range unsupportedImageTypes {
		if mime == unsupported {
			return false
		}
	}
	
	// Exclude unsupported video formats (add as needed)
	unsupportedVideoTypes := []string{
		// Currently empty, can add specific unsupported video formats if needed
	}
	
	for _, unsupported := range unsupportedVideoTypes {
		if mime == unsupported {
			return false
		}
	}
	
	return true
}

// generateRandomString creates a random string for temporary file names.
func generateRandomString(n int) string {
	const letters = "0123456789abcdefghijklmnopqrstuvwxyz"
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "randomfallback"
	}
	for i, b := range bytes {
		bytes[i] = letters[b%byte(len(letters))]
	}
	return string(bytes)
}

// createBackup creates a backup of a file in the specified backup directory.
func createBackup(f, backupDir string, enabled bool, logger Logger) bool {
	if !enabled {
		return true
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		logger.Error("Failed to create backup directory", "path", backupDir, "error", err)
		return false
	}

	backupName := fmt.Sprintf("%s.%d.bak", filepath.Base(f), time.Now().UnixNano())
	destPath := filepath.Join(backupDir, backupName)

	input, err := os.ReadFile(f)
	if err != nil {
		logger.Error("Failed to read source for backup", "file", f, "error", err)
		return false
	}

	if err = os.WriteFile(destPath, input, 0644); err != nil {
		logger.Error("Failed to write backup file", "backup_path", destPath, "error", err)
		return false
	}
	return true
}