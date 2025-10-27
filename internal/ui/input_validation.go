package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidateInput 验证输入并返回清理后的结果
func ValidateInput(input string, validator func(string) bool, errorMsg string) (string, error) {
	// 清理输入
	cleaned := SanitizeInput(input)

	// 检查是否为空
	if cleaned == "" && errorMsg != "" {
		return "", fmt.Errorf("%s", errorMsg)
	}

	// 使用自定义验证器
	if validator != nil && !validator(cleaned) {
		if errorMsg != "" {
			return "", fmt.Errorf("%s", errorMsg)
		}
		return "", fmt.Errorf("输入验证失败")
	}

	return cleaned, nil
}

// ValidateNumericInput 验证数值输入
func ValidateNumericInput(input string, min, max float64) (float64, error) {
	// 清理输入
	cleaned := SanitizeInput(input)

	// 解析为浮点数
	value, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, fmt.Errorf("无效的数值格式")
	}

	// 检查范围
	if value < min || value > max {
		return 0, fmt.Errorf("数值超出范围 %.2f-%.2f", min, max)
	}

	return value, nil
}

// ValidateIntegerInput 验证整数输入
func ValidateIntegerInput(input string, min, max int) (int, error) {
	// 清理输入
	cleaned := SanitizeInput(input)

	// 解析为整数
	value, err := strconv.Atoi(cleaned)
	if err != nil {
		return 0, fmt.Errorf("无效的整数格式")
	}

	// 检查范围
	if value < min || value > max {
		return 0, fmt.Errorf("整数超出范围 %d-%d", min, max)
	}

	return value, nil
}

// ValidateYesNoInput 验证是/否输入
func ValidateYesNoInput(input string) (bool, error) {
	// 清理输入
	cleaned := strings.ToLower(SanitizeInput(input))

	// 检查各种是的表示
	switch cleaned {
	case "y", "yes", "是", "1":
		return true, nil
	case "n", "no", "否", "0", "":
		return false, nil
	default:
		return false, fmt.Errorf("无效的是/否输入")
	}
}

// SanitizeInput 清理输入字符串
func SanitizeInput(input string) string {
	// 去除首尾空白
	input = strings.TrimSpace(input)

	// 移除危险字符
	dangerousChars := []string{"\x00", "\n", "\r", "\t", "\x0B", "\x0C"}
	for _, char := range dangerousChars {
		input = strings.ReplaceAll(input, char, "")
	}

	// 限制长度
	const maxLength = 1024
	if len(input) > maxLength {
		// 确保在UTF-8字符边界截断
		if utf8.ValidString(input[:maxLength]) {
			input = input[:maxLength]
		} else {
			// 找到最后一个有效的UTF-8字符位置
			for i := maxLength - 1; i >= 0; i-- {
				if utf8.ValidString(input[:i]) {
					input = input[:i]
					break
				}
			}
		}
	}

	return input
}
