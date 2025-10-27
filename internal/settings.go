package internal

import (
    "fmt"

    "pixly/config"
    "pixly/core/converter"
    "pixly/internal/i18n"
    "pixly/internal/ui"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

// 添加主题设置相关命令
var themeCmd = &cobra.Command{
    Use:   "theme",
    Short: i18n.T(i18n.TextThemeSettingsOption),
    Long:  i18n.T(i18n.TextThemeSettingsOption) + "，" + i18n.T(i18n.TextThemeSettingsDescription),
    Run: func(cmd *cobra.Command, args []string) {
        manageThemeSettings()
    },
}

// manageThemeSettings 管理主题设置
// 修改：使用方向键导航替代数字键输入
func manageThemeSettings() {
    ui.ClearScreen()
    ui.DisplayBanner(i18n.T(i18n.TextThemeSettingsOption), "info")

    // 获取当前配置
    cfg := ui.GetGlobalConfig()

    // 显示当前主题模式
    fmt.Printf(i18n.T(i18n.TextCurrentMode)+": %s\n", cfg.Theme.Mode)

    // 使用方向键菜单选择主题模式
    themeOptions := []ui.ArrowMenuOption{
        {
            Icon:        "☀️",
            Text:        i18n.T(i18n.TextLightMode),
            Description: "",
            Enabled:     true,
        },
        {
            Icon:        "🌙",
            Text:        i18n.T(i18n.TextDarkMode),
            Description: "",
            Enabled:     true,
        },
        {
            Icon:        "🔄",
            Text:        i18n.T(i18n.TextAutoMode),
            Description: "",
            Enabled:     true,
        },
    }

    result, err := ui.DisplayArrowMenu(i18n.T(i18n.TextPleaseSelectThemeMode), themeOptions)
    if err != nil {
        ui.DisplayError(fmt.Errorf("%s: %v", i18n.T(i18n.TextError), err))
        ui.WaitForKeyPress("")
        return
    }

    if result.Cancelled {
        return
    }

    switch result.SelectedIndex {
    case 0:
        viper.Set("theme.mode", "light")
        ui.DisplaySuccess(i18n.T(i18n.TextSetToThemeMode) + ": " + i18n.T(i18n.TextLightMode))
    case 1:
        viper.Set("theme.mode", "dark")
        ui.DisplaySuccess(i18n.T(i18n.TextSetToThemeMode) + ": " + i18n.T(i18n.TextDarkMode))
    case 2:
        viper.Set("theme.mode", "auto")
        ui.DisplaySuccess(i18n.T(i18n.TextSetToThemeMode) + ": " + i18n.T(i18n.TextAutoMode))
    }

    // 保存配置
    if err := saveConfig(); err != nil {
        ui.DisplayError(fmt.Errorf("%s: %v", i18n.T(i18n.TextError), err))
        ui.WaitForKeyPress("")
        return
    }

    ui.DisplaySuccess(i18n.T(i18n.TextThemeSettings) + " " + i18n.T(i18n.TextSuccess) + "!")
    fmt.Println()
    fmt.Println("📊 " + i18n.T(i18n.TextUpdatedThemeSettings) + ":")
    showCurrentThemeSettings(cfg)

    ui.WaitForKeyPress("")
}

// showCurrentThemeSettings 显示当前主题设置
func showCurrentThemeSettings(cfg *config.Config) {
    fmt.Printf(i18n.T(i18n.TextThemeMode)+": %s\n", cfg.Theme.Mode)
}

// 添加语言设置相关命令
var languageCmd = &cobra.Command{
    Use:   "language",
    Short: i18n.T(i18n.TextLanguageSettingsOption),
    Long:  i18n.T(i18n.TextLanguageSettingsOption) + "，" + i18n.T(i18n.TextLanguageSettingsDescription),
    Run: func(cmd *cobra.Command, args []string) {
        manageLanguageSettings()
    },
}

// manageLanguageSettings 管理语言设置 - 增强版本
// 修改：使用方向键导航替代数字键输入
func manageLanguageSettings() {
    ui.ClearScreen()
    ui.DisplayBanner(i18n.T(i18n.TextLanguageSettingsOption), "info")

    // 获取当前语言设置
    i18nManager := i18n.GetGlobalI18nManager()
    currentLang := i18nManager.GetCurrentLanguage()

    fmt.Printf(i18n.T(i18n.TextCurrentLanguage)+": %s\n", getCurrentLanguageName(currentLang))
    fmt.Println()

    // 显示支持的语言列表
    supportedLanguages := i18nManager.GetSupportedLanguages()
    
    // 创建方向键菜单选项
    languageOptions := make([]ui.ArrowMenuOption, len(supportedLanguages))
    for i, lang := range supportedLanguages {
        languageOptions[i] = ui.ArrowMenuOption{
            Icon:        "🌐",
            Text:        fmt.Sprintf("%s (%s)", getLanguageName(lang), string(lang)),
            Description: "",
            Enabled:     true,
        }
    }

    result, err := ui.DisplayArrowMenu(i18n.T(i18n.TextSupportedLanguages), languageOptions)
    if err != nil {
        ui.DisplayError(fmt.Errorf("%s: %v", i18n.T(i18n.TextError), err))
        ui.WaitForKeyPress("")
        return
    }

    if result.Cancelled {
        return
    }

    selectedLang := supportedLanguages[result.SelectedIndex]
    if err := i18nManager.SetLanguage(selectedLang); err != nil {
        ui.DisplayError(fmt.Errorf("%s: %v", i18n.T(i18n.TextError), err))
        ui.WaitForKeyPress("")
        return
    }

    ui.DisplaySuccess(i18n.T(i18n.TextSetLanguageTo) + ": " + getLanguageName(selectedLang))

    ui.WaitForKeyPress("")
}

// getLanguageName 获取语言名称
func getLanguageName(lang i18n.Language) string {
    switch lang {
    case i18n.LanguageChinese:
        return i18n.T(i18n.TextChinese)
    case i18n.LanguageEnglish:
        return i18n.T(i18n.TextEnglish)
    default:
        return string(lang)
    }
}

// getCurrentLanguageName 获取当前语言名称
func getCurrentLanguageName(lang i18n.Language) string {
    switch lang {
    case i18n.LanguageChinese:
        return i18n.T(i18n.TextChinese) + " (zh)"
    case i18n.LanguageEnglish:
        return i18n.T(i18n.TextEnglish) + " (en)"
    default:
        return string(lang) + " (" + i18n.T(i18n.TextUnknown) + ")"
    }
}

// saveConfig 保存配置到文件
func saveConfig() error {
    // 获取配置文件路径 - 使用统一的路径处理工具
    configPath, err := converter.GlobalPathUtils.NormalizePath("~/.pixly.yaml")
    if err != nil {
        return err
    }

    // 写入配置文件
    return viper.WriteConfigAs(configPath)
}