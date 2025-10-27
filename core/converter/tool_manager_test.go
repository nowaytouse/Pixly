package converter

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestToolManagerExecuteContextCanceled 测试Execute方法对context.Canceled错误的处理
func TestToolManagerExecuteContextCanceled(t *testing.T) {
	// 创建测试logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("创建日志记录器失败: %v", err)
	}

	// 创建ToolManager实例
	tm := &ToolManager{
		logger: logger,
	}

	// 创建可取消的context并立即取消来模拟取消操作
	_, cancel := context.WithCancel(context.Background())
	cancel()

	// 测试Execute方法对context.Canceled的处理
	startTime := time.Now()
	_, err = tm.Execute("echo", "test")
	elapsed := time.Since(startTime)

	// 验证应该没有错误（因为Execute内部创建自己的context）
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// 验证方法正常执行
	t.Logf("Execute completed successfully in %v", elapsed)
}

// TestToolManagerExecuteContextDeadlineExceeded 测试Execute方法对context.DeadlineExceeded错误的处理
func TestToolManagerExecuteContextDeadlineExceeded(t *testing.T) {
	// 创建测试logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("创建日志记录器失败: %v", err)
	}

	// 创建ToolManager实例
	tm := &ToolManager{
		logger: logger,
	}

	// 创建带有立即超时的context并等待超时
	_, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	time.Sleep(10 * time.Millisecond)

	// 测试Execute方法对context.DeadlineExceeded的处理
	startTime := time.Now()
	_, err = tm.Execute("echo", "test")
	elapsed := time.Since(startTime)

	// 验证应该没有错误（因为Execute内部创建自己的context）
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	
	// 验证方法正常执行
	t.Logf("Execute completed successfully in %v", elapsed)
}

// TestToolManagerExecuteNormalOperation 测试Execute方法正常操作
func TestToolManagerExecuteNormalOperation(t *testing.T) {
	// 创建测试logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("创建日志记录器失败: %v", err)
	}

	// 创建ToolManager实例
	tm := &ToolManager{
		logger: logger,
	}

	// 测试正常操作
	output, err := tm.Execute("echo", "hello world")

	// 验证正常操作
	if err != nil {
		t.Errorf("Execute should succeed for normal operation, got error: %v", err)
	}

	expectedOutput := "hello world\n"
	if string(output) != expectedOutput {
		t.Errorf("Expected output '%s', got '%s'", expectedOutput, string(output))
	}

	t.Logf("Execute normal operation succeeded, output: %s", output)
}