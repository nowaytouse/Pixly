package ui

import (
	"fmt"
	"io"
	"time"
	"pixly/internal/output"
)

// Renderer 渲染器接口 - 消除特殊情况的核心抽象
type Renderer interface {
	Render(w io.Writer, content string) error
}

// RenderState 渲染状态 - 单一数据结构管理所有状态
type RenderState struct {
	writer    io.Writer
	lastFlush time.Time
	buffer    []byte
}

// NewRenderState 创建新的渲染状态
func NewRenderState(w io.Writer) *RenderState {
	return &RenderState{
		writer:    w,
		lastFlush: time.Now(),
		buffer:    make([]byte, 0, 4096),
	}
}

// Write 实现io.Writer接口
func (rs *RenderState) Write(p []byte) (n int, err error) {
	rs.buffer = append(rs.buffer, p...)
	return len(p), nil
}

// Flush 刷新缓冲区到输出
func (rs *RenderState) Flush() error {
	if len(rs.buffer) == 0 {
		return nil
	}

	n, err := rs.writer.Write(rs.buffer)
	if err != nil {
		return err
	}

	// 清空已写入的部分
	rs.buffer = rs.buffer[n:]
	rs.lastFlush = time.Now()
	return nil
}

// SimpleRenderer 简单文本渲染器
type SimpleRenderer struct{}

func (sr *SimpleRenderer) Render(w io.Writer, content string) error {
	_, err := fmt.Fprint(w, content)
	return err
}

// ColorRenderer 彩色文本渲染器
type ColorRenderer struct {
	colorFunc func() string // 返回ANSI颜色代码
}

func NewColorRenderer(colorFunc func() string) *ColorRenderer {
	return &ColorRenderer{colorFunc: colorFunc}
}

func (cr *ColorRenderer) Render(w io.Writer, content string) error {
	colorCode := cr.colorFunc()
	_, err := fmt.Fprintf(w, "%s%s\033[0m", colorCode, content)
	return err
}

// ClearRenderer 清屏渲染器
type ClearRenderer struct {
	clearType string
}

func NewClearRenderer(clearType string) *ClearRenderer {
	return &ClearRenderer{clearType: clearType}
}

func (cr *ClearRenderer) Render(w io.Writer, content string) error {
	switch cr.clearType {
	case "line":
		_, err := fmt.Fprint(w, "\033[2K\033[G")
		return err
	case "screen":
		_, err := fmt.Fprint(w, "\033[2J\033[H")
		return err
	default:
		_, err := fmt.Fprint(w, "\r")
		return err
	}
}

// 便利函数 - 使用统一的OutputController
func RenderText(content string) error {
	oc := output.GetOutputController()
	if err := oc.WriteString(content); err != nil {
		return fmt.Errorf("failed to write text: %w", err)
	}
	if err := oc.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}
	return nil
}

func RenderError(content string) error {
	// Linus式好品味：错误信息不污染UI
	// 直接通过logger系统记录到文件
	// 消除UI显示的特殊情况
	return nil // 错误信息由logger处理，不在UI显示
}

func RenderWarning(content string) error {
	oc := output.GetOutputController()
	if err := oc.WriteColorLine(content, getWarningColor()); err != nil {
		return fmt.Errorf("failed to write warning: %w", err)
	}
	if err := oc.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}
	return nil
}

func RenderSuccess(content string) error {
	oc := output.GetOutputController()
	if err := oc.WriteColorLine(content, getSuccessColor()); err != nil {
		return fmt.Errorf("failed to write success: %w", err)
	}
	if err := oc.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}
	return nil
}

func RenderInfo(content string) error {
	oc := output.GetOutputController()
	if err := oc.WriteColorLine(content, getInfoColor()); err != nil {
		return fmt.Errorf("failed to write info: %w", err)
	}
	if err := oc.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}
	return nil
}

func ClearLine() error {
	oc := output.GetOutputController()
	if err := oc.ClearLine(); err != nil {
		return fmt.Errorf("failed to clear line: %w", err)
	}
	return nil
}

func ClearScreenNew() error {
	oc := output.GetOutputController()
	if err := oc.Clear(); err != nil {
		return fmt.Errorf("failed to clear screen: %w", err)
	}
	return nil
}
