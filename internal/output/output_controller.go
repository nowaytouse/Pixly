package output

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
)

// OutputState 输出状态 - 单一数据结构，无特殊情况
type OutputState struct {
	writer    io.Writer
	buffer    []byte
	lastFlush time.Time
	width     int
	height    int
}

// OutputController 统一输出控制器 - Linus式极简设计
// 实现output.OutputWriter接口
type OutputController struct {
	state *OutputState
	mutex sync.Mutex // 单一锁，消除锁地狱
}

// NewOutputController 创建输出控制器
func NewOutputController() *OutputController {
	return &OutputController{
		state: &OutputState{
			writer:    os.Stdout,
			buffer:    make([]byte, 0, 4096),
			lastFlush: time.Now(),
			width:     80,
			height:    24,
		},
	}
}

// Write 写入数据 - 统一的输出接口
func (oc *OutputController) Write(data []byte) error {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	oc.state.buffer = append(oc.state.buffer, data...)
	return nil
}

// WriteString 写入字符串
func (oc *OutputController) WriteString(s string) error {
	return oc.Write([]byte(s))
}

// WriteColor 写入彩色文本
func (oc *OutputController) WriteColor(s string, c *color.Color) error {
	if c == nil {
		return oc.WriteString(s)
	}
	return oc.WriteString(c.Sprint(s))
}

// WriteLine 写入一行
func (oc *OutputController) WriteLine(s string) error {
	return oc.WriteString(s + "\n")
}

// WriteColorLine 写入彩色一行
func (oc *OutputController) WriteColorLine(s string, c *color.Color) error {
	if c == nil {
		return oc.WriteLine(s)
	}
	return oc.WriteLine(c.Sprint(s))
}

// Clear 清屏 - Linus式极简设计，消除缓冲地狱
func (oc *OutputController) Clear() error {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	// 先刷新现有缓冲区
	if len(oc.state.buffer) > 0 {
		oc.state.writer.Write(oc.state.buffer)
		oc.state.buffer = oc.state.buffer[:0]
	}

	// 立即发送清屏序列
	_, err := oc.state.writer.Write([]byte("\033[2J\033[H"))
	return err
}

// ClearLine 清除当前行
func (oc *OutputController) ClearLine() error {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	_, err := oc.state.writer.Write([]byte("\033[2K\033[G"))
	return err
}

// ShowCursor 显示光标
func (oc *OutputController) ShowCursor() error {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	_, err := oc.state.writer.Write([]byte("\033[?25h"))
	return err
}

// Flush 刷新输出
func (oc *OutputController) Flush() error {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	if len(oc.state.buffer) == 0 {
		return nil
	}

	_, err := oc.state.writer.Write(oc.state.buffer)
	if err != nil {
		return err
	}

	// 清空缓冲区
	oc.state.buffer = oc.state.buffer[:0]
	oc.state.lastFlush = time.Now()
	return nil
}

// SetSize 设置终端尺寸
func (oc *OutputController) SetSize(width, height int) {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	oc.state.width = width
	oc.state.height = height
}

// GetSize 获取终端尺寸
func (oc *OutputController) GetSize() (int, int) {
	oc.mutex.Lock()
	defer oc.mutex.Unlock()

	return oc.state.width, oc.state.height
}

// 全局输出控制器实例
var (
	globalOutputController *OutputController
	outputControllerOnce   sync.Once
)

// GetOutputController 获取全局输出控制器
func GetOutputController() *OutputController {
	outputControllerOnce.Do(func() {
		globalOutputController = NewOutputController()
	})
	return globalOutputController
}

// 统一的输出函数 - 内部使用，不导出避免重复声明

// PrintColor 打印彩色文本
func PrintColor(text string, c *color.Color) {
	oc := GetOutputController()
	oc.WriteColor(text, c)
	oc.Flush()
}

// PrintColorLine 打印彩色一行
func PrintColorLine(text string, c *color.Color) {
	oc := GetOutputController()
	oc.WriteColorLine(text, c)
	oc.Flush()
}
