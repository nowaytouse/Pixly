package ui

import (
	"pixly/core/input"
)

// ReadKey 读取单个按键（委托给pkg/input）
func ReadKey() (string, error) {
	return input.ReadKey()
}

// ReadLine 读取一行输入（委托给pkg/input）
func ReadLine() (string, error) {
	return input.ReadLine()
}

// CleanupInput 清理输入状态（委托给pkg/input）
func CleanupInput() error {
	return input.GlobalInputManager.DisableRawMode()
}
