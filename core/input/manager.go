package input

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Manager 统一输入管理器 - 解决多重输入冲突问题
type Manager struct {
	rawMode  bool
	oldState *term.State
}

// GlobalInputManager 全局输入管理器实例
var GlobalInputManager = &Manager{}

// EnableRawMode 启用原始模式，用于单键输入
func (m *Manager) EnableRawMode() error {
	if m.rawMode {
		return nil // 已经在原始模式
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to enable raw mode: %w", err)
	}

	m.oldState = oldState
	m.rawMode = true
	return nil
}

// DisableRawMode 禁用原始模式
func (m *Manager) DisableRawMode() error {
	if !m.rawMode || m.oldState == nil {
		return nil // 不在原始模式
	}

	err := term.Restore(int(os.Stdin.Fd()), m.oldState)
	if err != nil {
		return fmt.Errorf("failed to restore terminal: %w", err)
	}

	m.rawMode = false
	m.oldState = nil
	return nil
}

// ReadKey 读取单个按键（用于菜单导航）
func (m *Manager) ReadKey() (string, error) {
	// 临时启用原始模式
	if err := m.EnableRawMode(); err != nil {
		return "", err
	}
	defer func() {
		if err := m.DisableRawMode(); err != nil {
			fmt.Printf("警告: 禁用原始模式失败: %v\n", err)
		}
	}()

	for {
		buf := make([]byte, 3)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		// 处理ESC序列（方向键）
		if n >= 3 && buf[0] == 27 && buf[1] == '[' {
			switch buf[2] {
			case 'A':
				return "up", nil
			case 'B':
				return "down", nil
			}
		}

		// 处理单字节字符
		if n >= 1 {
			b := buf[0]
			switch b {
			case 13, 10: // Enter (解决^M问题)
				return "enter", nil
			case 'q', 'Q':
				return "q", nil
			case 'w', 'W':
				return "w", nil
			case 's', 'S':
				return "s", nil
			case 27: // ESC alone
				return "q", nil
			default:
				if b >= '1' && b <= '9' {
					return string(b), nil
				}
				// 处理字母键 - 消除特殊情况
				if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
					return string(b), nil
				}
				// 忽略其他字符，继续读取
			}
		}
	}
}

// ReadLine 读取一行输入（用于文本输入）
func (m *Manager) ReadLine() (string, error) {
	// 确保不在原始模式
	if err := m.DisableRawMode(); err != nil {
		return "", err
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(line), nil
}

// 全局便捷函数

// ReadKey 全局按键读取函数
func ReadKey() (string, error) {
	return GlobalInputManager.ReadKey()
}

// ReadLine 全局行读取函数
func ReadLine() (string, error) {
	return GlobalInputManager.ReadLine()
}
