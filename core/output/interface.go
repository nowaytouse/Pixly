package output

import (
	"fmt"
	"os"
)

// OutputWriter 输出接口 - Linus式极简设计，消除循环依赖
type OutputWriter interface {
	// WriteLine 写入一行文本
	WriteLine(text string) error
	// WriteString 写入字符串
	WriteString(text string) error
	// Flush 刷新输出缓冲区
	Flush() error
}

// GlobalOutputWriter 全局输出写入器
var GlobalOutputWriter OutputWriter

// SetGlobalOutputWriter 设置全局输出写入器
func SetGlobalOutputWriter(writer OutputWriter) {
	GlobalOutputWriter = writer
}

// WriteLine 全局写入一行
func WriteLine(text string) {
	if GlobalOutputWriter != nil {
		if err := GlobalOutputWriter.WriteLine(text); err != nil {
			// 输出错误时使用标准错误输出作为备用
			fmt.Fprintf(os.Stderr, "Output error: %v\n", err)
		}
	}
}

// WriteString 全局写入字符串
func WriteString(text string) {
	if GlobalOutputWriter != nil {
		if err := GlobalOutputWriter.WriteString(text); err != nil {
			// 输出错误时使用标准错误输出作为备用
			fmt.Fprintf(os.Stderr, "Output error: %v\n", err)
		}
	}
}

// Flush 全局刷新
func Flush() {
	if GlobalOutputWriter != nil {
		if err := GlobalOutputWriter.Flush(); err != nil {
			// 刷新错误时使用标准错误输出作为备用
			fmt.Fprintf(os.Stderr, "Flush error: %v\n", err)
		}
	}
}
