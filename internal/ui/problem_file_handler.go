package ui

import (
	"errors"
	"strings"

	"github.com/fatih/color"
)

// ProblemFileHandler 问题文件处理器
// UI包不应依赖converter包的具体类型
type ProblemFileHandler struct {
	// 移除对converter.BatchProcessor的直接依赖
	// 问题文件处理应该通过回调函数或接口实现
}

// NewProblemFileHandler 创建新的问题文件处理器
func NewProblemFileHandler() *ProblemFileHandler {
	return &ProblemFileHandler{}
}

// HandleCorruptedFiles 处理损坏文件（强制用户进行一次性批量处理抉择）
// UI包不应直接访问文件数据，应通过回调或事件机制
func (h *ProblemFileHandler) HandleCorruptedFiles() (string, error) {
	// 文件检查和处理逻辑应在converter包中完成
	// UI包只负责用户交互界面
	// 这里返回默认行为：忽略
	return "I", nil
}

// ProcessUserDecision 处理用户决策
func (h *ProblemFileHandler) ProcessUserDecision(choice string, fileType string) error {
	// 文件处理逻辑应在converter包中完成
	// UI包不应直接操作文件
	renderChannel := GetRenderChannel()

	switch choice {
	case "D", "d":
		var msgBuilder strings.Builder
		msgBuilder.WriteString("删除")
		msgBuilder.WriteString(fileType)
		msgBuilder.WriteString("文件的请求已记录")
		renderChannel.SendMessage(UIMessage[any]{
			Type: "text",
			Data: color.RedString(msgBuilder.String()),
		})
	case "I", "i":
		var msgBuilder strings.Builder
		msgBuilder.WriteString("忽略")
		msgBuilder.WriteString(fileType)
		msgBuilder.WriteString("文件")
		renderChannel.SendMessage(UIMessage[any]{
			Type: "text",
			Data: color.BlueString(msgBuilder.String()),
		})
	case "S", "s":
		var msgBuilder strings.Builder
		msgBuilder.WriteString("跳过")
		msgBuilder.WriteString(fileType)
		msgBuilder.WriteString("文件")
		renderChannel.SendMessage(UIMessage[any]{
			Type: "text",
			Data: color.YellowString(msgBuilder.String()),
		})
	default:
		var errBuilder strings.Builder
		errBuilder.WriteString("无效的选择: ")
		errBuilder.WriteString(choice)
		return errors.New(errBuilder.String())
	}

	return nil
}
