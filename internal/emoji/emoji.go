package emoji

import (
	"os"
	"runtime"
	"strings"

	"pixly/internal/theme"

	"github.com/fatih/color"
)

// EmojiFont emoji字体接口
type EmojiFont interface {
	Apply(text string) string
	GetName() string
	IsSupported() bool
}

// UnicodeEmoji 标准Unicode emoji
type UnicodeEmoji struct {
	name string
}

// NewUnicodeEmoji 创建Unicode emoji实例
func NewUnicodeEmoji() *UnicodeEmoji {
	return &UnicodeEmoji{name: "unicode"}
}

// Apply 应用emoji到文本
func (ue *UnicodeEmoji) Apply(text string) string {
	// 如果不支持Unicode，使用ASCII fallback
	if !detectUnicodeSupport() {
		return ue.getFallbackText(text)
	}
	return text
}

// getFallbackText 获取emoji的ASCII fallback
func (ue *UnicodeEmoji) getFallbackText(text string) string {
	// 智能emoji到ASCII映射
	fallbackMap := map[string]string{
		"📋":  "[*]",
		"📝":  "[+]",
		"🎮":  "[>]",
		"🎯":  "[o]",
		"🔄":  "[~]",
		"📁":  "[D]",
		"🔒":  "[#]",
		"❌":  "[X]",
		"⚙️": "[*]",
		"🤖":  "[A]",
		"🎨":  "[C]",
		"📊":  "[=]",
		"🔧":  "[T]",
		"📄":  "[F]",
		"🌍":  "[G]",
		"ℹ️": "[i]",
		"🚀":  "[^]",
		"✅":  "[v]",
		"⭐":  "[*]",
		"🔍":  "[?]",
		"💾":  "[S]",
		"🎵":  "[♪]",
		"🖼️": "[P]",
		"📹":  "[V]",
		"🔊":  "[♫]",
	}

	// 替换文本中的所有emoji
	result := text
	for emoji, fallback := range fallbackMap {
		result = strings.ReplaceAll(result, emoji, fallback)
	}
	return result
}

// GetName 获取字体名称
func (ue *UnicodeEmoji) GetName() string {
	return ue.name
}

// IsSupported 检查是否支持
func (ue *UnicodeEmoji) IsSupported() bool {
	return detectUnicodeSupport()
}

// CustomEmoji 自定义emoji实现
type CustomEmoji struct {
	symbol string
}

// NewCustomEmoji 创建自定义emoji实例
func NewCustomEmoji() *CustomEmoji {
	return &CustomEmoji{
		symbol: "",
	}
}

// Apply 应用emoji到文本
func (ce *CustomEmoji) Apply(text string) string {
	var builder strings.Builder
	builder.WriteString(ce.symbol)
	builder.WriteString(" ")
	builder.WriteString(text)
	builder.WriteString(" ")
	builder.WriteString(ce.symbol)
	return builder.String()
}

// GetName 获取字体名称
func (ce *CustomEmoji) GetName() string {
	return "custom"
}

// IsSupported 检查是否支持
func (ce *CustomEmoji) IsSupported() bool {
	return true // 自定义emoji总是支持
}

// AnimatedEmoji 动效emoji
type AnimatedEmoji struct {
	isAnimated bool
	name       string
}

// NewAnimatedEmoji 创建新的动效emoji字体
func NewAnimatedEmoji() *AnimatedEmoji {
	return &AnimatedEmoji{
		isAnimated: true,
		name:       "animated",
	}
}

// Apply 应用emoji到文本
func (ae *AnimatedEmoji) Apply(text string) string {
	// 简单的动画效果：添加闪烁符号
	var builder strings.Builder
	builder.WriteString("✨ ")
	builder.WriteString(text)
	builder.WriteString(" ✨")
	return builder.String()
}

// GetName 获取字体名称
func (ae *AnimatedEmoji) GetName() string {
	return ae.name
}

// IsSupported 检查是否支持
func (ae *AnimatedEmoji) IsSupported() bool {
	return true // 动效emoji总是支持
}

// EmojiManager emoji管理器
type EmojiManager struct {
	config    *EmojiConfig
	theme     *theme.ThemeManager
	fonts     map[string]EmojiFont
	supported bool
}

// EmojiConfig emoji配置
type EmojiConfig struct {
	Enabled bool   `mapstructure:"enable_emoji"`
	Style   string `mapstructure:"emoji_style"`
	Size    int    `mapstructure:"emoji_size"`

	Menu struct {
		Enabled        bool   `mapstructure:"enabled"`
		Style          string `mapstructure:"style"`
		NumberStyle    string `mapstructure:"number_style"`
		BorderStyle    string `mapstructure:"border_style"`
		OptionSurround bool   `mapstructure:"option_surround"`
	} `mapstructure:"menu"`

	AsciiArt struct {
		DotEffect            bool   `mapstructure:"dot_effect"`
		GradientEffect       bool   `mapstructure:"gradient_effect"`
		GradientStyle        string `mapstructure:"gradient_style"`
		PrimaryGradientColor string `mapstructure:"primary_gradient_color"`
	} `mapstructure:"ascii_art"`

	Status struct {
		Success string `mapstructure:"success"`
		Failed  string `mapstructure:"failed"`
		Skipped string `mapstructure:"skipped"`
		Warning string `mapstructure:"warning"`
	} `mapstructure:"status"`

	Progress struct {
		Start          string   `mapstructure:"start"`
		Middle         string   `mapstructure:"middle"`
		End            string   `mapstructure:"end"`
		Complete       string   `mapstructure:"complete"`
		GradientColors []string `mapstructure:"gradient_colors"`
	} `mapstructure:"progress"`

	Animation struct {
		StartupAnimation    bool `mapstructure:"startup_animation"`
		CompletionAnimation bool `mapstructure:"completion_animation"`
		SkipFirstRun        bool `mapstructure:"skip_first_run"`
	} `mapstructure:"animation"`

	Font struct {
		OptimizeRendering bool `mapstructure:"optimize_rendering"`
		EnableStyles      bool `mapstructure:"enable_styles"`
		FallbackEnabled   bool `mapstructure:"fallback_enabled"`
	} `mapstructure:"font"`
}

// NewEmojiManager 创建新的emoji管理器
func NewEmojiManager(config *EmojiConfig, theme *theme.ThemeManager) *EmojiManager {
	em := &EmojiManager{
		config:    config,
		theme:     theme,
		fonts:     make(map[string]EmojiFont),
		supported: detectEmojiSupport(),
	}

	em.initFonts()
	return em
}

// initFonts 初始化字体
func (em *EmojiManager) initFonts() {
	em.fonts["unicode"] = NewUnicodeEmoji()
	em.fonts["custom"] = NewCustomEmoji()
	em.fonts["animated"] = NewAnimatedEmoji()
}

// GetEmojiFont 获取指定类型的emoji字体
func (em *EmojiManager) GetEmojiFont(fontType string) EmojiFont {
	if font, exists := em.fonts[fontType]; exists {
		return font
	}
	return em.fonts["unicode"] // 默认返回Unicode字体
}

// ApplyEmoji 应用emoji到文本
func (em *EmojiManager) ApplyEmoji(text, emojiType string) string {
	if !em.config.Enabled {
		return text
	}

	font := em.GetEmojiFont(emojiType)
	if font.IsSupported() {
		return font.Apply(text)
	}

	// 回退到文本替代
	return em.GetFallbackText(text)
}

// IsEmojiSupported 检查终端是否支持emoji
func (em *EmojiManager) IsEmojiSupported() bool {
	return em.supported
}

// GetFallbackText 获取emoji的文本回退
func (em *EmojiManager) GetFallbackText(emoji string) string {
	// 智能emoji到ASCII映射
	fallbackMap := map[string]string{
		"📋":  "[*]",
		"📝":  "[+]",
		"🎮":  "[>]",
		"🎯":  "[o]",
		"🔄":  "[~]",
		"📁":  "[D]",
		"🔒":  "[#]",
		"❌":  "[X]",
		"⚙️": "[*]",
		"🤖":  "[A]",
		"🎨":  "[C]",
		"📊":  "[=]",
		"🔧":  "[T]",
		"📄":  "[F]",
		"🌍":  "[G]",
		"ℹ️": "[i]",
		"🚀":  "[^]",
		"✅":  "[v]",
		"⭐":  "[*]",
		"🔍":  "[?]",
		"💾":  "[S]",
		"🎵":  "[♪]",
		"🖼️": "[P]",
		"📹":  "[V]",
		"🔊":  "[♫]",
	}

	if fallback, exists := fallbackMap[emoji]; exists {
		return fallback
	}
	return emoji
}

// detectEmojiSupport 检测emoji支持
func detectEmojiSupport() bool {
	// 简单的检测实现
	// 在实际应用中，可能需要更复杂的检测逻辑
	switch runtime.GOOS {
	case "windows":
		// Windows支持情况复杂，保守返回
		return false
	case "darwin":
		// macOS通常支持emoji
		return true
	case "linux":
		// Linux需要检查终端支持
		return checkLinuxTerminalSupport()
	default:
		return true
	}
}

// detectUnicodeSupport 检测Unicode支持
func detectUnicodeSupport() bool {
	// 检查LANG环境变量是否支持UTF-8
	lang := os.Getenv("LANG")
	if !strings.Contains(strings.ToUpper(lang), "UTF") {
		return false
	}

	// 检查TERM环境变量
	term := strings.ToLower(os.Getenv("TERM"))
	if term == "" {
		return false
	}

	// 已知不支持Unicode的终端
	unsupportedTerms := []string{
		"dumb", "vt52", "vt100", "vt102", "vt220",
		"linux", "cons25", "cygwin",
	}

	for _, unsupported := range unsupportedTerms {
		if strings.Contains(term, unsupported) {
			return false
		}
	}

	// 对于xterm-256color等现代终端，进一步检查
	if strings.Contains(term, "xterm") || strings.Contains(term, "screen") {
		// 检查是否在SSH会话中，SSH可能不支持emoji
		if os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" {
			return false
		}
	}

	return true
}

// checkLinuxTerminalSupport 检查Linux终端支持
func checkLinuxTerminalSupport() bool {
	// 检查环境变量
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if term == "" {
		return false
	}

	// 常见支持emoji的终端
	supportedTerms := []string{"xterm-256color", "xterm-color", "screen-256color"}
	for _, supported := range supportedTerms {
		if strings.Contains(term, supported) {
			return true
		}
	}

	return false
}

// AsciiArtEnhancer 字符画增强器
type AsciiArtEnhancer struct {
	config *EmojiConfig
	theme  *theme.ThemeManager
}

// NewAsciiArtEnhancer 创建新的字符画增强器
func NewAsciiArtEnhancer(config *EmojiConfig, theme *theme.ThemeManager) *AsciiArtEnhancer {
	return &AsciiArtEnhancer{
		config: config,
		theme:  theme,
	}
}

// CreateDottedEffect 创建点点材质效果
func (aae *AsciiArtEnhancer) CreateDottedEffect(text string, isDarkMode bool) string {
	if !aae.config.AsciiArt.DotEffect {
		return text
	}

	// 获取点状字符
	dotChar := aae.getDotCharacter(isDarkMode, false)

	// 添加点状装饰
	return dotChar + " " + text + " " + dotChar
}

// CreateEnhancedDottedEffect 创建增强点点材质效果
func (aae *AsciiArtEnhancer) CreateEnhancedDottedEffect(text string, isDarkMode bool) string {
	if !aae.config.AsciiArt.DotEffect {
		return text
	}

	// 获取增强点状字符
	doubleDotChar := aae.getDotCharacter(isDarkMode, true)

	// 添加增强点状装饰
	return doubleDotChar + " " + text + " " + doubleDotChar
}

// CreateGradientEffect 创建渐变效果
func (aae *AsciiArtEnhancer) CreateGradientEffect(text string, colorType string) string {
	if !aae.config.AsciiArt.GradientEffect {
		return text
	}

	// 根据配置创建渐变效果
	switch aae.config.AsciiArt.GradientStyle {
	case "single":
		return aae.createSingleColorGradient(text, aae.config.AsciiArt.PrimaryGradientColor)
	case "multi":
		return aae.createMultiColorGradient(text)
	default:
		return text
	}
}

// getDotCharacter 获取点状字符
func (aae *AsciiArtEnhancer) getDotCharacter(isDarkMode bool, isEnhanced bool) string {
	if isDarkMode {
		if isEnhanced {
			return color.New(color.FgHiWhite, color.Faint).Sprint("··")
		}
		return color.New(color.FgHiWhite, color.Faint).Sprint("·")
	}

	if isEnhanced {
		return color.New(color.FgHiBlack, color.Faint).Sprint("··")
	}
	return color.New(color.FgHiBlack, color.Faint).Sprint("·")
}

// CreateCompatibleAsciiArt 创建兼容性字符画
func (aae *AsciiArtEnhancer) CreateCompatibleAsciiArt(art string) string {
	// 替换可能导致编码问题的特殊字符
	art = strings.ReplaceAll(art, "y", "v") // 避免y字母显示问题
	// 可以添加更多字符替换规则
	return art
}

// OptimizeFontRendering 优化字体渲染
func (aae *AsciiArtEnhancer) OptimizeFontRendering(text string) string {
	// 实现字体渲染优化逻辑
	// 可以添加字体平滑处理、抗锯齿等效果
	return text
}

// ApplyFontStyles 应用字体样式
func (aae *AsciiArtEnhancer) ApplyFontStyles(text string, styles []string) string {
	// 应用字体样式如粗体、斜体等
	for _, style := range styles {
		switch style {
		case "bold":
			text = color.New(color.Bold).Sprint(text)
		case "italic":
			text = color.New(color.Italic).Sprint(text)
		case "underline":
			text = color.New(color.Underline).Sprint(text)
		}
	}
	return text
}

// createSingleColorGradient 创建单色渐变效果
func (aae *AsciiArtEnhancer) createSingleColorGradient(text string, colorName string) string {
	// 根据颜色名称创建渐变效果
	var gradientColor *color.Color
	switch colorName {
	case "yellow":
		gradientColor = color.New(color.FgYellow)
	case "purple":
		gradientColor = color.New(color.FgMagenta)
	case "red":
		gradientColor = color.New(color.FgRed)
	case "green":
		gradientColor = color.New(color.FgGreen)
	case "blue":
		gradientColor = color.New(color.FgBlue)
	default:
		gradientColor = color.New(color.FgYellow) // 默认黄色
	}

	return gradientColor.Sprint(text)
}

// createMultiColorGradient 创建多色渐变效果
func (aae *AsciiArtEnhancer) createMultiColorGradient(text string) string {
	// 创建多色渐变效果
	colors := []*color.Color{
		color.New(color.FgRed),
		color.New(color.FgYellow),
		color.New(color.FgGreen),
		color.New(color.FgCyan),
		color.New(color.FgBlue),
		color.New(color.FgMagenta),
	}

	var result strings.Builder
	colorCount := len(colors)

	for i, char := range text {
		colorIndex := i % colorCount
		result.WriteString(colors[colorIndex].Sprint(string(char)))
	}

	return result.String()
}
