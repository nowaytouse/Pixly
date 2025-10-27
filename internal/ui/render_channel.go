package ui

import (
	"time"
	"pixly/internal/output"
)

// UIMessage UI更新消息结构 - 使用泛型提供类型安全
type UIMessage[T any] struct {
	Type    string // 消息类型
	Content string // 消息内容
	Data    T      // 附加数据，使用泛型提供类型安全
}

// NewUIMessage 创建新的UI消息
func NewUIMessage[T any](msgType, content string, data T) UIMessage[T] {
	return UIMessage[T]{
		Type:    msgType,
		Content: content,
		Data:    data,
	}
}

// NewSimpleUIMessage 创建简单的UI消息（无附加数据）
func NewSimpleUIMessage(msgType, content string) UIMessage[any] {
	return UIMessage[any]{
		Type:    msgType,
		Content: content,
		Data:    nil,
	}
}

// RenderChannel 简化的渲染通道 - 直接使用OutputController
type RenderChannel struct {
	// 不再需要复杂的goroutine和channel机制
}

// GlobalRenderChannel 全局渲染通道实例
var GlobalRenderChannel *RenderChannel

// GetRenderChannel 获取全局渲染通道实例
func GetRenderChannel() *RenderChannel {
	if GlobalRenderChannel == nil {
		GlobalRenderChannel = &RenderChannel{}
	}
	return GlobalRenderChannel
}

// 不再需要复杂的startRenderer - 直接同步处理

// SendProgressMessage 发送进度消息
func (rc *RenderChannel) SendProgressMessage(current, total int) {
	progressData := map[string]interface{}{
		"current": current,
		"total":   total,
	}
	msg := UIMessage[any]{
		Type: "progress",
		Data: progressData,
	}
	rc.SendMessage(msg)
}

// SendStatusMessage 发送状态消息
func (rc *RenderChannel) SendStatusMessage(status string) {
	msg := UIMessage[any]{
		Type: "status",
		Data: status,
	}
	rc.SendMessage(msg)
}

// SendErrorMessage 发送错误消息
func (rc *RenderChannel) SendErrorMessage(errorText string) {
	msg := UIMessage[any]{
		Type: "error",
		Data: errorText,
	}
	rc.SendMessage(msg)
}

// renderMessage 渲染单个消息 - 使用统一的OutputController
func (rc *RenderChannel) renderMessage(msg UIMessage[any]) {
	oc := output.GetOutputController()

	// 根据消息类型进行不同的渲染处理
	switch msg.Type {
	case "menu":
		// 渲染菜单标题
		DisplayBanner(msg.Content, "info")
	case "arrow_menu":
		// 渲染方向键菜单
		oc.WriteString(msg.Content)
	case "banner":
		// 渲染横幅
		DisplayBanner(msg.Content, "info")
	case "progress":
		// 渲染进度信息
		oc.WriteColorLine(msg.Content, getProgressColor())
	case "error":
		// 渲染错误信息
		oc.WriteColorLine(msg.Content, getErrorColor())
	case "warning":
		// 渲染警告信息
		oc.WriteColorLine(msg.Content, getWarningColor())
	case "success":
		// 渲染成功信息
		oc.WriteColorLine(msg.Content, getSuccessColor())
	case "info":
		// 渲染信息
		oc.WriteColorLine(msg.Content, getInfoColor())
	case "animation":
		// 渲染动画文本
		CreateOptimizedPulsingText(msg.Content, 500*time.Millisecond)
	case "clear":
		// 清除当前行
		oc.ClearLine()
	case "clear_screen":
		// 清屏
		oc.Clear()
	case "welcome":
		// 渲染欢迎界面
		oc.WriteLine(msg.Content)
	case "version":
		// 渲染版本信息
		oc.WriteLine(msg.Content)
	case "text":
		// 渲染普通文本
		oc.WriteLine(msg.Content)
	case "stats":
		// 统计信息渲染应由converter包自行处理
		// UI包不应依赖converter包的类型
		oc.WriteLine(msg.Content)
	default:
		// 默认处理
		oc.WriteLine(msg.Content)
	}

	oc.Flush()
}

// SendMessage 发送UI更新消息 - 直接同步处理
func (rc *RenderChannel) SendMessage(msg UIMessage[any]) {
	// 直接处理消息，不再使用复杂的channel机制
	rc.renderMessage(msg)
}

// Close 关闭渲染通道 - 不再需要复杂的清理
func (rc *RenderChannel) Close() {
	// 不再需要复杂的goroutine清理
}
