package ui

import (
	"fmt"
	"pixly/internal/i18n"
	"strings"
)

// LanguageOption 语言选项结构
type LanguageOption struct {
	Emoji string
	Code  string
	Name  string
}

// SwitchLanguage 切换语言
func SwitchLanguage(langCode string) error {
	var lang i18n.Language

	switch strings.ToLower(langCode) {
	case "zh", "chinese", "中文":
		lang = i18n.LanguageChinese
	case "en", "english", "英文":
		lang = i18n.LanguageEnglish
	default:
		return fmt.Errorf("unsupported language: %s", langCode)
	}

	// 切换语言
	err := i18n.GetGlobalI18nManager().SetLanguage(lang)
	if err != nil {
		return err
	}

	// 更新UI颜色变量
	UpdateUIColors()

	// 显示切换成功消息
	successMsg := CreateSymmetricBanner("✅", "✅", i18n.T(i18n.TextLanguageSwitched))
	DisplayBanner(successMsg, "success")

	return nil
}

// UpdateUIColors 更新UI颜色变量以适应新语言
func UpdateUIColors() {
	// 重新初始化颜色变量
	initColorVars()
}

// GetSupportedLanguages 获取支持的语言列表
func GetSupportedLanguages() []LanguageOption {
	return []LanguageOption{
		{Emoji: "🇨🇳", Code: "zh", Name: "Chinese"},
		{Emoji: "🇺🇸", Code: "en", Name: "English"},
	}
}

// DisplayLanguageSettings 显示语言设置
func DisplayLanguageSettings() {
	// 获取当前语言
	currentLang := i18n.GetGlobalI18nManager().GetCurrentLanguage()

	fmt.Println("")
	title := i18n.T(i18n.TextLanguageSettings)
	titleLen := len(title) + 4
	// 限制边框长度，防止生成过长的字符串
	if titleLen > 100 {
		titleLen = 100
	}
	border := strings.Repeat("═", titleLen)
	HeaderColor.Printf("  ╔%s╗\n", border)
	HeaderColor.Printf("  ║ %s ║\n", title)
	HeaderColor.Printf("  ╚%s╝\n", border)

	Println("")

	// 显示当前语言设置
	var currentLangText string
	switch currentLang {
	case i18n.LanguageChinese:
		currentLangText = CreateSymmetricEmojiText("🇨🇳", "中文")
	case i18n.LanguageEnglish:
		currentLangText = CreateSymmetricEmojiText("🇺🇸", "English")
	default:
		currentLangText = CreateSymmetricEmojiText("🌐", i18n.T(i18n.TextUnknown))
	}

	InfoColor.Printf("  %s: %s\n", i18n.T(i18n.TextCurrentLanguage), currentLangText)

	// 显示支持的语言
	Println("")
	InfoColor.Printf("  %s:\n", i18n.T(i18n.TextSupportedLanguages))
	supportedLanguages := GetSupportedLanguages()
	for _, lang := range supportedLanguages {
		InfoColor.Printf("    %s %s (%s)\n", lang.Emoji, lang.Name, lang.Code)
	}
}
