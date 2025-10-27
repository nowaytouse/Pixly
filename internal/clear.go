package internal

import (
	"fmt"
	"time"
)

// ClearScreen clears the terminal screen
func ClearScreen() {
	// 统一的清屏逻辑，适用于所有终端
	for i := 0; i < 3; i++ {
		// 使用标准ANSI转义序列清屏
		fmt.Print("\033[2J\033[H\033[0m")
		fmt.Print("\033[3J") // 清除滚动缓冲区
		fmt.Print("\033c")   // 重置终端

		// 确保光标在左上角
		fmt.Print("\033[H")
		fmt.Print("\r") // 回车确保在行首

		// 重置所有属性
		fmt.Print("\033[0m")

		// 延迟确保操作完成
		time.Sleep(10 * time.Millisecond)

		if i < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// ClearScreenWithFeedback clears the screen and provides visual feedback
func ClearScreenWithFeedback() {
	// 统一的清屏逻辑
	fmt.Print("\033[2J\033[H\033[0m")
	fmt.Print("\033[3J") // 清除滚动缓冲区
	fmt.Print("\033c")   // 重置终端
	fmt.Print("\033[H")  // 移动光标到顶部

	// 确保在行首开始
	fmt.Print("\r")
	fmt.Print("\033[0m")
	time.Sleep(10 * time.Millisecond)
}

// PositionCursor moves the cursor to the specified position
func PositionCursor(row, col int) {
	fmt.Printf("\033[%d;%dH", row, col)
}

// ResetTerminal resets the terminal to its default state
func ResetTerminal() {
	fmt.Print("\033c")   // Reset terminal
	fmt.Print("\033[0m") // Reset all attributes
	fmt.Print("\033[H")  // Move cursor to home
	fmt.Print("\033[2J") // Clear screen
}