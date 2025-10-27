package ui

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/viper"
)

// DynamicProgressBar 动态进度条结构
type DynamicProgressBar struct {
	id         string
	total      int64
	current    int64
	message    string
	bar        *pterm.ProgressbarPrinter
	lastUpdate time.Time
	mutex      sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	finished   bool
}

// DynamicProgressManager 动态进度条管理器
type DynamicProgressManager struct {
	bars       map[string]*DynamicProgressBar
	mutex      sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	silentMode bool
	quietMode  bool
	disableUI  bool
}

// 全局动态进度管理器
var globalDynamicProgressManager *DynamicProgressManager
var dynamicProgressManagerOnce sync.Once

// GetDynamicProgressManager 获取全局动态进度管理器
func GetDynamicProgressManager() *DynamicProgressManager {
	dynamicProgressManagerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalDynamicProgressManager = &DynamicProgressManager{
			bars:       make(map[string]*DynamicProgressBar),
			ctx:        ctx,
			cancel:     cancel,
			silentMode: viper.GetBool("advanced.ui.silent_mode"),
			quietMode:  viper.GetBool("advanced.ui.quiet_mode"),
			disableUI:  viper.GetBool("advanced.ui.disable_ui"),
		}
		// 启动管理器
		go globalDynamicProgressManager.run()
	})
	return globalDynamicProgressManager
}

// StartDynamicProgress 启动默认动态进度条
func StartDynamicProgress(total int64, message string) {
	GetDynamicProgressManager().StartBar("default", total, message)
}

// StartDynamicProgressUnknownTotal 启动未知总大小的动态进度条
func StartDynamicProgressUnknownTotal(message string) {
	GetDynamicProgressManager().StartBar("default", 100, message)
}

// UpdateDynamicProgress 更新默认动态进度条
func UpdateDynamicProgress(current int64, message string) {
	GetDynamicProgressManager().UpdateBar("default", current, message)
}

// FinishDynamicProgress 完成默认动态进度条
func FinishDynamicProgress() {
	GetDynamicProgressManager().FinishBar("default")
}

// StartNamedDynamicProgress 启动命名动态进度条
func StartNamedDynamicProgress(name string, total int64, message string) {
	GetDynamicProgressManager().StartBar(name, total, message)
}

// UpdateNamedDynamicProgress 更新命名动态进度条
func UpdateNamedDynamicProgress(name string, current int64, message string) {
	GetDynamicProgressManager().UpdateBar(name, current, message)
}

// FinishNamedDynamicProgress 完成命名动态进度条
func FinishNamedDynamicProgress(name string) {
	GetDynamicProgressManager().FinishBar(name)
}

// StartBar 启动进度条
func (dpm *DynamicProgressManager) StartBar(id string, total int64, message string) {
	// 如果处于静默模式或禁用UI，直接返回
	if dpm.silentMode || dpm.disableUI {
		return
	}

	dpm.mutex.Lock()
	defer dpm.mutex.Unlock()

	// 如果已存在，先完成它
	if existingBar, exists := dpm.bars[id]; exists {
		existingBar.finish()
		delete(dpm.bars, id)
	}

	ctx, cancel := context.WithCancel(dpm.ctx)

	// 创建pterm进度条 - 使用高级配置
	progressBar, _ := pterm.DefaultProgressbar.WithTotal(int(total)).WithTitle(message).Start()

	// 自定义进度条样式 - 彩色动画效果
	progressBar.BarStyle = &pterm.Style{pterm.FgLightBlue, pterm.BgDefault}
	progressBar.TitleStyle = &pterm.Style{pterm.FgLightCyan, pterm.Bold}
	progressBar.BarCharacter = "█"
	progressBar.LastCharacter = "█"
	progressBar.ElapsedTimeRoundingFactor = time.Millisecond
	progressBar.ShowCount = true
	progressBar.ShowPercentage = false
	progressBar.ShowElapsedTime = true
	progressBar.ShowTitle = true

	dpm.bars[id] = &DynamicProgressBar{
		id:         id,
		total:      total,
		current:    0,
		message:    message,
		bar:        progressBar,
		lastUpdate: time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		finished:   false,
	}
}

// UpdateBar 更新进度条
func (dpm *DynamicProgressManager) UpdateBar(id string, current int64, message string) {
	// 如果处于静默模式或禁用UI，直接返回
	if dpm.silentMode || dpm.disableUI {
		return
	}

	dpm.mutex.RLock()
	bar, exists := dpm.bars[id]
	dpm.mutex.RUnlock()

	if !exists || bar.finished {
		return
	}

	bar.mutex.Lock()
	defer bar.mutex.Unlock()

	// 检查是否需要更新（高频更新优化）
	now := time.Now()
	shouldUpdate := time.Since(bar.lastUpdate) >= 16*time.Millisecond || // 60fps更新频率
		current != bar.current ||
		(message != "" && message != bar.message) ||
		current == 0 || // 总是显示开始
		current >= bar.total // 总是显示完成

	if !shouldUpdate {
		return
	}

	// 更新进度条数据
	bar.current = current
	if message != "" {
		bar.message = message
		// 计算精确百分比（保留2位小数）
		percentage := float64(current) / float64(bar.total) * 100
		// 添加动态指示器（当进度静止时显示旋转符号）
		spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerIndex := int(now.UnixNano()/100000000) % len(spinnerChars)

		// 构建带精度和动态指示器的标题
		var builder strings.Builder
		builder.WriteString(message)
		builder.WriteString(" ")
		builder.WriteString(spinnerChars[spinnerIndex])
		builder.WriteString(" [")
		builder.WriteString(strconv.FormatFloat(percentage, 'f', 2, 64))
		builder.WriteString("%]")
		bar.bar.UpdateTitle(builder.String())
	}
	bar.lastUpdate = now

	// 更新pterm进度条 - 支持精确小数点进度
	if bar.bar != nil {
		bar.bar.Current = int(current)
		// 不使用Increment()避免双重计数，直接触发重绘
	}
}

// FinishBar 完成进度条
func (dpm *DynamicProgressManager) FinishBar(id string) {
	// 如果处于静默模式或禁用UI，直接返回
	if dpm.silentMode || dpm.disableUI {
		return
	}

	dpm.mutex.Lock()
	defer dpm.mutex.Unlock()

	bar, exists := dpm.bars[id]
	if !exists {
		return
	}

	bar.finish()
	delete(dpm.bars, id)
}

// finish 完成单个进度条
func (dpb *DynamicProgressBar) finish() {
	dpb.mutex.Lock()
	defer dpb.mutex.Unlock()

	if dpb.finished {
		return
	}

	dpb.finished = true
	dpb.cancel()

	if dpb.bar != nil {
		// 确保显示100%完成
		dpb.current = dpb.total
		dpb.bar.Current = dpb.bar.Total
		// 更新标题显示100%完成
		completedMessage := dpb.message + " ✓ [100.00%]"
		dpb.bar.UpdateTitle(completedMessage)
		// 强制刷新显示
		time.Sleep(100 * time.Millisecond)
		dpb.bar.Stop()
	}
}

// run 运行管理器主循环
func (dpm *DynamicProgressManager) run() {
	ticker := time.NewTicker(16 * time.Millisecond) // 60fps刷新率
	defer ticker.Stop()

	for {
		select {
		case <-dpm.ctx.Done():
			return
		case <-ticker.C:
			// 定期检查和清理已完成的进度条
			dpm.cleanupFinishedBars()
		}
	}
}

// cleanupFinishedBars 清理已完成的进度条
func (dpm *DynamicProgressManager) cleanupFinishedBars() {
	dpm.mutex.Lock()
	defer dpm.mutex.Unlock()

	for id, bar := range dpm.bars {
		if bar.finished {
			delete(dpm.bars, id)
		}
	}
}

// Shutdown 关闭动态进度管理器
func (dpm *DynamicProgressManager) Shutdown() {
	dpm.mutex.Lock()
	defer dpm.mutex.Unlock()

	// 完成所有进度条
	for _, bar := range dpm.bars {
		bar.finish()
	}

	// 清空映射
	dpm.bars = make(map[string]*DynamicProgressBar)

	// 取消上下文
	dpm.cancel()
}

// CreateSpinner 创建旋转加载器
func CreateSpinner(message string) *pterm.SpinnerPrinter {
	spinner, _ := pterm.DefaultSpinner.Start(message)

	// 自定义旋转器样式
	spinner.Sequence = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinner.Style = &pterm.Style{pterm.FgCyan}
	spinner.MessageStyle = &pterm.Style{pterm.FgLightWhite}

	return spinner
}

// CreateMultiProgressBar 创建多进度条显示
func CreateMultiProgressBar(bars []ProgressBarConfig) *pterm.MultiPrinter {
	multi := pterm.DefaultMultiPrinter

	for _, config := range bars {
		progressBar, _ := pterm.DefaultProgressbar.WithTotal(int(config.Total)).WithTitle(config.Title).Start()

		// 应用彩色样式
		progressBar.BarStyle = &pterm.Style{pterm.FgLightBlue, pterm.BgDefault}
		progressBar.TitleStyle = &pterm.Style{pterm.FgLightCyan, pterm.Bold}

		multi.NewWriter()
	}

	multi.Start()
	return &multi
}

// ProgressBarConfig 进度条配置
type ProgressBarConfig struct {
	Title string
	Total int64
}

// CreateCircularProgress 创建圆环进度条（使用ASCII艺术）
func CreateCircularProgress(total int64, message string) *CircularProgress {
	return &CircularProgress{
		total:   total,
		current: 0,
		message: message,
		spinner: CreateSpinner(message),
	}
}

// CircularProgress 圆环进度条
type CircularProgress struct {
	total   int64
	current int64
	message string
	spinner *pterm.SpinnerPrinter
	mutex   sync.RWMutex
}

// Update 更新圆环进度条
func (cp *CircularProgress) Update(current int64, message string) {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	cp.current = current
	if message != "" {
		cp.message = message
		var progressText strings.Builder
		progressText.WriteString(message)
		progressText.WriteString(" (")
		progressText.WriteString(strconv.FormatFloat(float64(current)/float64(cp.total)*100, 'f', 2, 64))
		progressText.WriteString("%%)")
		cp.spinner.UpdateText(progressText.String())
	}
}

// Finish 完成圆环进度条
func (cp *CircularProgress) Finish() {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	if cp.spinner != nil {
		cp.spinner.Success(cp.message + " 完成!")
	}
}

// ShowLoadingAnimation 显示加载动画
func ShowLoadingAnimation(message string, duration time.Duration) {
	spinner := CreateSpinner(message)
	time.Sleep(duration)
	spinner.Success("完成!")
}

// CreateProgressBarWithETA 创建带ETA的进度条
func CreateProgressBarWithETA(total int64, message string) *pterm.ProgressbarPrinter {
	progressBar, _ := pterm.DefaultProgressbar.WithTotal(int(total)).WithTitle(message).Start()

	// 启用ETA显示
	progressBar.ShowElapsedTime = true
	progressBar.ShowCount = true
	progressBar.ShowPercentage = true

	// 高级样式配置
	progressBar.BarStyle = &pterm.Style{pterm.FgLightBlue, pterm.BgDefault}
	progressBar.TitleStyle = &pterm.Style{pterm.FgLightCyan, pterm.Bold}
	progressBar.BarCharacter = "█"
	progressBar.LastCharacter = "█"

	return progressBar
}
