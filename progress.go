package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// ProgressType represents different types of progress tracking
type ProgressType int

const (
	ProgressTypeScan ProgressType = iota
	ProgressTypeConvert
	ProgressTypeForce
)

// ProgressBar manages progress display and hang detection
type ProgressBar struct {
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	progressType    ProgressType
	description     string
	total           int64
	current         int64
	startTime       time.Time
	lastUpdate      time.Time
	lastChangeTime  time.Time
	updateCount     int64
	spinnerIndex    int
	spinnerChars    []string
	hangTimeout     time.Duration
	forceExitFunc   func()
	isCompleted     bool
}

// NewProgressBar creates a new progress bar with hang detection
func NewProgressBar(ctx context.Context, progressType ProgressType, description string, total int64) *ProgressBar {
	// Create a child context with cancel for this progress bar
	pbCtx, cancel := context.WithCancel(ctx)
	
	pb := &ProgressBar{
		ctx:          pbCtx,
		cancel:       cancel,
		progressType: progressType,
		description:  description,
		total:        total,
		current:      0,
		startTime:    time.Now(),
		lastUpdate:   time.Now(),
		lastChangeTime: time.Now(),
		spinnerChars: []string{"/", "-", "", "|"},
		hangTimeout:  30 * time.Second, // 30 second hang timeout
		spinnerIndex: 0,
		isCompleted:  false,
	}
	
	// Start background monitoring
	go pb.monitorProgress()
	
	return pb
}

// SetForceExitFunc sets the function to call when force exit is triggered
func (pb *ProgressBar) SetForceExitFunc(forceExitFunc func()) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.forceExitFunc = forceExitFunc
}

// Update updates the progress with current value
func (pb *ProgressBar) Update(current int64) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	if pb.isCompleted {
		return
	}
	
	pb.current = current
	pb.lastUpdate = time.Now()
	pb.updateCount++
	
	// Only update change time if there's actual progress
	if current > pb.current {
		pb.lastChangeTime = time.Now()
	}
	
	pb.displayProgress()
}

// Increment increments the progress by 1
func (pb *ProgressBar) Increment() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	if pb.isCompleted {
		return
	}
	
	pb.current++
	pb.lastUpdate = time.Now()
	pb.updateCount++
	pb.lastChangeTime = time.Now()
	
	pb.displayProgress()
}

// Complete marks the progress as complete
func (pb *ProgressBar) Complete() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	if pb.isCompleted {
		return
	}
	
	pb.current = pb.total
	pb.isCompleted = true
	pb.lastUpdate = time.Now()
	pb.displayProgress()
	
	// Cancel the context to stop monitoring
	pb.cancel()
}

// monitorProgress monitors for hangs and triggers force exit if needed
func (pb *ProgressBar) monitorProgress() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-pb.ctx.Done():
			return
		case <-ticker.C:
			pb.checkForHang()
		}
	}
}

// checkForHang checks if progress has stalled for too long
func (pb *ProgressBar) checkForHang() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	if pb.isCompleted {
		return
	}
	
	// Check if no progress has been made for hangTimeout duration
	if time.Since(pb.lastChangeTime) > pb.hangTimeout {
		pb.handleHang()
	}
}

// handleHang deals with progress hang by triggering force exit
func (pb *ProgressBar) handleHang() {
	message := fmt.Sprintf("❌ %d秒内无进度更新，疑似卡死。强制退出。", int(pb.hangTimeout.Seconds()))
	printToConsole(message)
	
	if pb.forceExitFunc != nil {
		pb.forceExitFunc()
	} else {
		// Fallback to os.Exit if no force exit function provided
		os.Exit(1)
	}
	
	// Mark as completed to prevent further updates
	pb.isCompleted = true
	pb.cancel()
}

// displayProgress shows the progress bar in the terminal
func (pb *ProgressBar) displayProgress() {
	// Clear the line and return cursor to start
	fmt.Printf("\033[2K\r")
	
	// Get terminal width
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 40 {
		width = 80
	}
	
	// Calculate progress percentage
	var pct float64
	if pb.total > 0 {
		pct = float64(pb.current) / float64(pb.total)
		if pct > 1.0 {
			pct = 1.0
		}
	}
	
	// Choose spinner character
	spinnerChar := pb.spinnerChars[pb.spinnerIndex]
	pb.spinnerIndex = (pb.spinnerIndex + 1) % len(pb.spinnerChars)
	
	// Format time information
	elapsed := time.Since(pb.startTime)
	elapsedStr := elapsed.Round(time.Second).String()
	
	// Format ETA if we have enough data
	var etaStr string
	if pb.current > 0 && pb.total > 0 && elapsed.Seconds() > 1 {
		rate := float64(pb.current) / elapsed.Seconds()
		remaining := float64(pb.total - pb.current)
		if rate > 0 {
			eta := time.Duration(remaining/rate) * time.Second
			etaStr = eta.Round(time.Second).String()
		} else {
			etaStr = "计算中..."
		}
	} else {
		etaStr = "计算中..."
	}
	
	// Display different progress formats based on type
	switch pb.progressType {
	case ProgressTypeScan:
		// Scan progress: [current/total] with spinner
		progressStr := fmt.Sprintf("%s %s 扫描中... [已发现: %d | 已评估: %d] 耗时: %s",
			cyan("🔍"), spinnerChar, pb.total, pb.current, elapsedStr)
		fmt.Print(progressStr)
		
	case ProgressTypeConvert:
		// Conversion progress: progress bar with percentage
		if pb.total > 0 {
			// Calculate bar width
			barWidth := int(float64(width-50) * pct)
			if barWidth < 1 && pct > 0 {
				barWidth = 1
			} else if barWidth > width-50 {
				barWidth = width - 50
			}
			
			// Create progress bar
			bar := strings.Repeat("█", barWidth) + strings.Repeat("░", width-50-barWidth)
			
			progressStr := fmt.Sprintf("%s %s 处理进度 [%s] %.1f%% (%d/%d) ETA: %s",
				cyan("🔄"), spinnerChar, bar, pct*100, pb.current, pb.total, etaStr)
			fmt.Print(progressStr)
		} else {
			// Indeterminate progress
			progressStr := fmt.Sprintf("%s %s 处理中... (%d 已处理) 耗时: %s",
				cyan("🔄"), spinnerChar, pb.current, elapsedStr)
			fmt.Print(progressStr)
		}
		
	case ProgressTypeForce:
		// Force mode progress: simple counter
		progressStr := fmt.Sprintf("%s %s 强制处理中... (%d/%d) 耗时: %s",
			red("⚡"), spinnerChar, pb.current, pb.total, elapsedStr)
		fmt.Print(progressStr)
		
	default:
		// Generic progress
		if pb.total > 0 {
			progressStr := fmt.Sprintf("%s %s 进行中... (%d/%d) %.1f%% 耗时: %s",
				green("✅"), spinnerChar, pb.current, pb.total, pct*100, elapsedStr)
			fmt.Print(progressStr)
		} else {
			progressStr := fmt.Sprintf("%s %s 进行中... (%d 已处理) 耗时: %s",
				green("✅"), spinnerChar, pb.current, elapsedStr)
			fmt.Print(progressStr)
		}
	}
}

// printToConsole prints to console with line clearing
// This function is already defined in ui.go, so we'll use the existing one
// func printToConsole(f string, a ...interface{}) {
// 	consoleMutex.Lock()
// 	defer consoleMutex.Unlock()
// 	// Clears the line and returns cursor to the start
// 	fmt.Printf("\033[2K\r"+f, a...)
// }

// Color functions for consistent styling across the application
// These are already defined in colors.go, so we'll use the existing ones