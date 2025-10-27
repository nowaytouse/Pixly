package theme

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/viper"
)

// ThemeMode 主题模式枚举
type ThemeMode string

const (
	ThemeModeLight    ThemeMode = "light"
	ThemeModeDark     ThemeMode = "dark"
	ThemeModeAuto     ThemeMode = "auto"
	ThemeModeNeon     ThemeMode = "neon"
	ThemeModeContrast ThemeMode = "contrast"
)

// ThemeConfig 主题配置
type ThemeConfig struct {
	// 主题模式：light, dark, auto
	Mode             ThemeMode         `mapstructure:"mode"`
	CustomColors     map[string]string `mapstructure:"customcolors"`
	GradientEnabled  bool              `mapstructure:"gradientenabled"`
	AnimationEnabled bool              `mapstructure:"animationenabled"`
	Transparency     float64           `mapstructure:"transparency"`
	Brightness       float64           `mapstructure:"brightness"`
	Contrast         float64           `mapstructure:"contrast"`
	AutoSwitchTime   string            `mapstructure:"autoswitchtime"`
}

// MenuColors 菜单配色 - "好品味"：提取重复的匿名结构体
type MenuColors struct {
	Item        *color.Color
	Selected    *color.Color
	Disabled    *color.Color
	Description *color.Color
}

// StatsColors 统计信息配色
type StatsColors struct {
	Successful *color.Color
	Failed     *color.Color
	Skipped    *color.Color
	Duration   *color.Color
	SpaceSaved *color.Color
}

// PromptColors 提示配色
type PromptColors struct {
	Question *color.Color
	Input    *color.Color
	Confirm  *color.Color
}

// ColorScheme 配色方案
type ColorScheme struct {
	// 基础颜色
	Primary   *color.Color
	Secondary *color.Color
	Success   *color.Color
	Warning   *color.Color
	Error     *color.Color
	Info      *color.Color
	Progress  *color.Color
	Highlight *color.Color
	Muted     *color.Color

	// 扩展颜色
	Accent1    *color.Color
	Accent2    *color.Color
	Accent3    *color.Color
	Background *color.Color
	Foreground *color.Color
	Border     *color.Color
	Shadow     *color.Color

	// 配色组件 - 使用命名类型替代匿名结构体
	Menu   MenuColors
	Stats  StatsColors
	Prompt PromptColors

	// 主题元数据
	Name        string
	Description string
	IsDark      bool
}

// AnimationState 泛型动画状态管理
type AnimationState[T any] struct {
	data map[string]T
	mu   sync.RWMutex
}

// NewAnimationState 创建新的动画状态管理器
func NewAnimationState[T any]() *AnimationState[T] {
	return &AnimationState[T]{
		data: make(map[string]T),
	}
}

// Set 设置动画状态值
func (as *AnimationState[T]) Set(key string, value T) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.data[key] = value
}

// Get 获取动画状态值
func (as *AnimationState[T]) Get(key string) (T, bool) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	value, exists := as.data[key]
	return value, exists
}

// GetAll 获取所有动画状态
func (as *AnimationState[T]) GetAll() map[string]T {
	as.mu.RLock()
	defer as.mu.RUnlock()
	result := make(map[string]T)
	for k, v := range as.data {
		result[k] = v
	}
	return result
}

// Clear 清除所有动画状态
func (as *AnimationState[T]) Clear() {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.data = make(map[string]T)
}

// ThemeManager 主题管理器
type ThemeManager struct {
	config          *ThemeConfig
	colorScheme     *ColorScheme
	currentMode     ThemeMode
	availableThemes map[ThemeMode]*ColorScheme
	gradientCache   map[string]string
	animationState  *AnimationState[any]
	lastSwitchTime  time.Time
	mu              sync.RWMutex
}

// NewThemeManager 创建新的主题管理器
func NewThemeManager() *ThemeManager {
	tm := &ThemeManager{
		config: &ThemeConfig{
			Mode:             ThemeModeAuto,
			CustomColors:     make(map[string]string),
			GradientEnabled:  false,
			AnimationEnabled: false,
			Transparency:     1.0,
			Brightness:       1.0,
			Contrast:         1.0,
			AutoSwitchTime:   "18:00",
		},
		availableThemes: make(map[ThemeMode]*ColorScheme),
		gradientCache:   make(map[string]string),
		animationState:  NewAnimationState[any](),
		lastSwitchTime:  time.Now(),
	}

	tm.loadConfig()
	tm.initializeAllThemes()
	tm.currentMode = tm.detectThemeMode()
	tm.initializeColorScheme()

	return tm
}

// loadConfig 加载主题配置
func (tm *ThemeManager) loadConfig() {
	// 尝试从配置文件加载
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	configPath := filepath.Join(home, ".pixly.yaml")
	if _, err := os.Stat(configPath); err == nil {
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err == nil {
			if err := viper.UnmarshalKey("theme", tm.config); err != nil {
				// 使用默认配置
				return
			}
		}
	}
}

// initializeAllThemes 初始化所有可用主题
func (tm *ThemeManager) initializeAllThemes() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.availableThemes[ThemeModeLight] = tm.createLightColorScheme()
	tm.availableThemes[ThemeModeDark] = tm.createDarkColorScheme()
	tm.availableThemes[ThemeModeNeon] = tm.createNeonColorScheme()
	tm.availableThemes[ThemeModeContrast] = tm.createContrastColorScheme()
}

// initializeColorScheme 初始化配色方案
func (tm *ThemeManager) initializeColorScheme() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	tm.initializeColorSchemeUnsafe()
}

// initializeColorSchemeUnsafe 不加锁的版本，用于已经持有锁的情况
func (tm *ThemeManager) initializeColorSchemeUnsafe() {
	// 检测当前应该使用的主题模式
	tm.currentMode = tm.detectThemeMode()

	// 根据模式初始化配色
	switch tm.currentMode {
	case ThemeModeLight, ThemeModeDark, ThemeModeNeon, ThemeModeContrast:
		if scheme, exists := tm.availableThemes[tm.currentMode]; exists {
			tm.colorScheme = scheme
		} else {
			// 如果主题不存在，使用availableThemes中的暗色主题
			tm.colorScheme = tm.availableThemes[ThemeModeDark]
		}
	case ThemeModeAuto:
		// 自动模式根据系统主题决定
		systemMode := tm.detectSystemTheme()
		if systemMode == ThemeModeDark {
			tm.colorScheme = tm.availableThemes[ThemeModeDark]
		} else {
			tm.colorScheme = tm.availableThemes[ThemeModeLight]
		}
	default:
		// 默认使用暗色模式
		tm.colorScheme = tm.availableThemes[ThemeModeDark]
	}
}

// detectThemeMode 检测主题模式
func (tm *ThemeManager) detectThemeMode() ThemeMode {
	switch tm.config.Mode {
	case ThemeModeLight:
		return ThemeModeLight
	case ThemeModeDark:
		return ThemeModeDark
	case ThemeModeNeon:
		return ThemeModeNeon
	case ThemeModeContrast:
		return ThemeModeContrast
	case ThemeModeAuto:
		// 真正的系统主题检测
		return tm.detectSystemTheme()
	default:
		return ThemeModeAuto
	}
}

// detectSystemTheme 检测系统主题模式
func (tm *ThemeManager) detectSystemTheme() ThemeMode {
	// macOS 系统主题检测
	if runtime.GOOS == "darwin" {
		return tm.detectMacOSTheme()
	}

	// Linux 系统主题检测 - 已删除具体实现，统一返回默认暗色模式
	// 其他系统默认暗色模式
	return ThemeModeDark
}

// detectMacOSTheme 检测 macOS 系统主题
func (tm *ThemeManager) detectMacOSTheme() ThemeMode {
	cmd := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle")
	output, err := cmd.Output()
	if err != nil {
		// 如果读取失败，默认为亮色模式
		return ThemeModeLight
	}

	if strings.TrimSpace(string(output)) == "Dark" {
		return ThemeModeDark
	}

	return ThemeModeLight
}

// createLightColorScheme 创建亮色配色方案
func (tm *ThemeManager) createLightColorScheme() *ColorScheme {
	return &ColorScheme{
		Primary:   color.New(color.FgBlue),
		Secondary: color.New(color.FgCyan),
		Success:   color.New(color.FgGreen),
		Warning:   color.New(color.FgYellow),
		Error:     color.New(color.FgRed),
		Info:      color.New(color.FgBlue),
		Progress:  color.New(color.FgGreen),
		Highlight: color.New(color.FgMagenta),
		Muted:     color.New(color.FgBlack),

		// 扩展颜色
		Accent1:    color.New(color.FgHiBlue),
		Accent2:    color.New(color.FgHiMagenta),
		Accent3:    color.New(color.FgHiCyan),
		Background: color.New(color.FgWhite),
		Foreground: color.New(color.FgBlack),
		Border:     color.New(color.FgBlack),
		Shadow:     color.New(color.FgBlack),

		// 配色组件 - 使用命名类型替代匿名结构体
		Menu: MenuColors{
			Item:        color.New(color.FgBlack),
			Selected:    color.New(color.FgBlue, color.Bold),
			Disabled:    color.New(color.FgBlack),
			Description: color.New(color.FgBlack),
		},
		Stats: StatsColors{
			Successful: color.New(color.FgGreen),
			Failed:     color.New(color.FgRed),
			Skipped:    color.New(color.FgYellow),
			Duration:   color.New(color.FgBlue),
			SpaceSaved: color.New(color.FgMagenta),
		},
		Prompt: PromptColors{
			Question: color.New(color.FgBlue),
			Input:    color.New(color.FgCyan),
			Confirm:  color.New(color.FgGreen),
		},

		// 主题元数据
		Name:        "Light",
		Description: "Light theme with blue accents",
		IsDark:      false,
	}
}

// createDarkColorScheme 创建暗色配色方案
func (tm *ThemeManager) createDarkColorScheme() *ColorScheme {
	return &ColorScheme{
		Primary:   color.New(color.FgCyan),
		Secondary: color.New(color.FgBlue),
		Success:   color.New(color.FgGreen),
		Warning:   color.New(color.FgYellow),
		Error:     color.New(color.FgRed),
		Info:      color.New(color.FgCyan),
		Progress:  color.New(color.FgGreen),
		Highlight: color.New(color.FgMagenta),
		Muted:     color.New(color.FgWhite),

		// 扩展颜色
		Accent1:    color.New(color.FgHiCyan),
		Accent2:    color.New(color.FgHiMagenta),
		Accent3:    color.New(color.FgHiBlue),
		Background: color.New(color.FgBlack),
		Foreground: color.New(color.FgWhite),
		Border:     color.New(color.FgWhite),
		Shadow:     color.New(color.FgBlack),

		// 配色组件 - 使用命名类型替代匿名结构体
		Menu: MenuColors{
			Item:        color.New(color.FgWhite),
			Selected:    color.New(color.FgCyan, color.Bold),
			Disabled:    color.New(color.FgWhite),
			Description: color.New(color.FgWhite),
		},
		Stats: StatsColors{
			Successful: color.New(color.FgGreen),
			Failed:     color.New(color.FgRed),
			Skipped:    color.New(color.FgYellow),
			Duration:   color.New(color.FgCyan),
			SpaceSaved: color.New(color.FgMagenta),
		},
		Prompt: PromptColors{
			Question: color.New(color.FgCyan),
			Input:    color.New(color.FgBlue),
			Confirm:  color.New(color.FgGreen),
		},

		// 主题元数据
		Name:        "Dark",
		Description: "Dark theme with cyan accents",
		IsDark:      true,
	}
}

// createNeonColorScheme 创建霓虹配色方案
func (tm *ThemeManager) createNeonColorScheme() *ColorScheme {
	return &ColorScheme{
		Primary:   color.New(color.FgHiMagenta),
		Secondary: color.New(color.FgHiCyan),
		Success:   color.New(color.FgHiGreen),
		Warning:   color.New(color.FgHiYellow),
		Error:     color.New(color.FgHiRed),
		Info:      color.New(color.FgHiBlue),
		Progress:  color.New(color.FgHiGreen),
		Highlight: color.New(color.FgHiMagenta, color.Bold),
		Muted:     color.New(color.FgHiBlack),
		Menu: MenuColors{
			Item:        color.New(color.FgHiWhite),
			Selected:    color.New(color.FgHiMagenta, color.Bold),
			Disabled:    color.New(color.FgHiBlack),
			Description: color.New(color.FgHiCyan),
		},
		Stats: StatsColors{
			Successful: color.New(color.FgHiGreen),
			Failed:     color.New(color.FgHiRed),
			Skipped:    color.New(color.FgHiYellow),
			Duration:   color.New(color.FgHiBlue),
			SpaceSaved: color.New(color.FgHiMagenta),
		},
		Prompt: PromptColors{
			Question: color.New(color.FgHiMagenta),
			Input:    color.New(color.FgHiCyan),
			Confirm:  color.New(color.FgHiGreen),
		},
		Name:        "Neon",
		Description: "Bright neon colors for a futuristic look",
		IsDark:      true,
	}
}

// createContrastColorScheme 创建高对比度配色方案
func (tm *ThemeManager) createContrastColorScheme() *ColorScheme {
	return &ColorScheme{
		Primary:   color.New(color.FgWhite, color.BgBlack, color.Bold),
		Secondary: color.New(color.FgBlack, color.BgWhite, color.Bold),
		Success:   color.New(color.FgHiGreen, color.Bold),
		Warning:   color.New(color.FgHiYellow, color.Bold),
		Error:     color.New(color.FgHiRed, color.Bold),
		Info:      color.New(color.FgHiBlue, color.Bold),
		Progress:  color.New(color.FgHiGreen, color.Bold),
		Highlight: color.New(color.FgHiWhite, color.BgBlack, color.Bold),
		Muted:     color.New(color.FgHiBlack),
		// 扩展颜色
		Accent1:    color.New(color.FgHiCyan, color.Bold),
		Accent2:    color.New(color.FgHiMagenta, color.Bold),
		Accent3:    color.New(color.FgHiYellow, color.Bold),
		Background: color.New(color.FgBlack),
		Foreground: color.New(color.FgHiWhite, color.Bold),
		Border:     color.New(color.FgHiWhite, color.Bold),
		Shadow:     color.New(color.FgHiBlack),
		Menu: MenuColors{
			Item:        color.New(color.FgWhite, color.Bold),
			Selected:    color.New(color.FgBlack, color.BgWhite, color.Bold),
			Disabled:    color.New(color.FgHiBlack),
			Description: color.New(color.FgHiWhite),
		},
		Stats: StatsColors{
			Successful: color.New(color.FgHiGreen, color.Bold),
			Failed:     color.New(color.FgHiRed, color.Bold),
			Skipped:    color.New(color.FgHiYellow, color.Bold),
			Duration:   color.New(color.FgHiBlue, color.Bold),
			SpaceSaved: color.New(color.FgHiMagenta, color.Bold),
		},
		Prompt: PromptColors{
			Question: color.New(color.FgHiWhite, color.Bold),
			Input:    color.New(color.FgHiCyan, color.Bold),
			Confirm:  color.New(color.FgHiGreen, color.Bold),
		},
		Name:        "Contrast",
		Description: "High contrast theme for accessibility",
		IsDark:      true,
	}
}

// GetColorScheme 获取当前配色方案
func (tm *ThemeManager) GetColorScheme() *ColorScheme {
	return tm.colorScheme
}

// GetCurrentMode 获取当前主题模式
func (tm *ThemeManager) GetCurrentMode() ThemeMode {
	return tm.currentMode
}

// SwitchTheme 切换主题
func (tm *ThemeManager) SwitchTheme(mode ThemeMode) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.config.Mode = mode
	tm.currentMode = tm.detectThemeMode()
	tm.initializeColorSchemeUnsafe() // 使用不加锁的版本
	tm.lastSwitchTime = time.Now()
	return tm.saveConfig()
}

// saveConfig 保存配置
func (tm *ThemeManager) saveConfig() error {
	viper.Set("theme", tm.config)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(home, ".pixly.yaml")
	return viper.WriteConfigAs(configPath)
}

// GetThemeInfo 获取当前主题信息
func (tm *ThemeManager) GetThemeInfo() map[string]interface{} {
	scheme := tm.GetColorScheme()
	return map[string]interface{}{
		"mode":             tm.config.Mode,
		"name":             scheme.Name,
		"description":      scheme.Description,
		"isDark":           scheme.IsDark,
		"gradientEnabled":  tm.config.GradientEnabled,
		"animationEnabled": tm.config.AnimationEnabled,
		"transparency":     tm.config.Transparency,
		"brightness":       tm.config.Brightness,
		"contrast":         tm.config.Contrast,
	}
}

// GetAvailableThemes 获取所有可用主题列表
func (tm *ThemeManager) GetAvailableThemes() map[ThemeMode]*ColorScheme {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.availableThemes
}

// ApplyCustomColors 应用自定义颜色配置
func (tm *ThemeManager) ApplyCustomColors(customColors map[string]string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.config.CustomColors == nil {
		tm.config.CustomColors = make(map[string]string)
	}

	for key, value := range customColors {
		tm.config.CustomColors[key] = value
	}

	return tm.saveConfig()
}

// EnableGradient 启用或禁用渐变效果
func (tm *ThemeManager) EnableGradient(enabled bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.config.GradientEnabled = enabled
	return tm.saveConfig()
}

// EnableAnimation 启用或禁用动画效果
func (tm *ThemeManager) EnableAnimation(enabled bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.config.AnimationEnabled = enabled
	if enabled {
		tm.animationState.Set("status", "active")
	} else {
		tm.animationState.Set("status", "inactive")
	}
	return tm.saveConfig()
}

// SetTransparency 设置透明度 (0.0-1.0)
func (tm *ThemeManager) SetTransparency(transparency float64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if transparency < 0.0 || transparency > 1.0 {
		return fmt.Errorf("transparency must be between 0.0 and 1.0")
	}

	tm.config.Transparency = transparency
	return tm.saveConfig()
}

// SetBrightness 设置亮度 (0.0-2.0)
func (tm *ThemeManager) SetBrightness(brightness float64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if brightness < 0.0 || brightness > 2.0 {
		return fmt.Errorf("brightness must be between 0.0 and 2.0")
	}

	tm.config.Brightness = brightness
	return tm.saveConfig()
}

// SetContrast 设置对比度 (0.0-2.0)
func (tm *ThemeManager) SetContrast(contrast float64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if contrast < 0.0 || contrast > 2.0 {
		return fmt.Errorf("contrast must be between 0.0 and 2.0")
	}

	tm.config.Contrast = contrast
	return tm.saveConfig()
}

// SetAutoSwitchTime 设置自动切换时间
func (tm *ThemeManager) SetAutoSwitchTime(switchTime string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.config.AutoSwitchTime = switchTime
	return tm.saveConfig()
}

// GetGradientEffect 获取渐变效果
func (tm *ThemeManager) GetGradientEffect(colorType string) *color.Color {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.config.GradientEnabled {
		return nil
	}

	if _, exists := tm.gradientCache[colorType]; exists {
		// 返回缓存的渐变颜色
		return color.New(color.FgHiCyan, color.Bold)
	}

	// 创建渐变效果（简化版本）
	scheme := tm.GetColorScheme()
	var gradientColor *color.Color

	switch colorType {
	case "primary":
		gradientColor = color.New(color.FgHiBlue, color.Bold)
	case "secondary":
		gradientColor = color.New(color.FgHiMagenta, color.Bold)
	case "success":
		gradientColor = color.New(color.FgHiGreen, color.Bold)
	default:
		gradientColor = scheme.Primary
	}

	tm.gradientCache[colorType] = "cached"
	return gradientColor
}

// GetAnimationState 获取当前动画状态
func (tm *ThemeManager) GetAnimationState() map[string]any {
	return tm.animationState.GetAll()
}

// UpdateAnimationState 更新动画状态
func (tm *ThemeManager) UpdateAnimationState(key string, value any) {
	tm.animationState.Set(key, value)
}

// ClearGradientCache 清除渐变缓存
func (tm *ThemeManager) ClearGradientCache() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.gradientCache = make(map[string]string)
}

// 全局主题管理器
var globalThemeManager *ThemeManager

// GetGlobalThemeManager 获取全局主题管理器
func GetGlobalThemeManager() *ThemeManager {
	if globalThemeManager == nil {
		globalThemeManager = NewThemeManager()
	}
	return globalThemeManager
}

// InitializeGlobalTheme 初始化全局主题
func InitializeGlobalTheme() {
	globalThemeManager = NewThemeManager()
}

// 兼容性函数：保持现有代码能正常工作
// CreateEnhancedGradientEffect 简化版本，不再支持渐变特效
func (tm *ThemeManager) CreateEnhancedGradientEffect(text string, colorType string) string {
	return tm.FormatWithTheme(text, colorType)
}

// CreatePulsingEffect 简化版本，不再支持脉冲特效
func (tm *ThemeManager) CreatePulsingEffect(text string, pulseIntensity float64) string {
	return text // 直接返回原文本
}

// FormatWithTheme 使用主题格式化文本
func (tm *ThemeManager) FormatWithTheme(text string, colorType string) string {
	if tm.colorScheme == nil {
		return text
	}

	var c *color.Color
	switch colorType {
	case "primary":
		c = tm.colorScheme.Primary
	case "secondary":
		c = tm.colorScheme.Secondary
	case "success":
		c = tm.colorScheme.Success
	case "warning":
		c = tm.colorScheme.Warning
	case "error":
		c = tm.colorScheme.Error
	case "info":
		c = tm.colorScheme.Info
	case "progress":
		c = tm.colorScheme.Progress
	case "highlight":
		c = tm.colorScheme.Highlight
	case "muted":
		c = tm.colorScheme.Muted
	default:
		return text
	}

	if c != nil {
		return c.Sprint(text)
	}
	return text
}
