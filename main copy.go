package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pixly/pkg/core/types"
	"pixly/pkg/progress"
	"pixly/pkg/ui/interactive"

	"go.uber.org/zap"
)

const (
	AppVersion     = "v1.25.0.0" // README.MD严格要求的版本号
	AppName        = "Pixly"
	AppDescription = "CLI 重大版本更新发布版" // README.MD要求的描述
)

// 7步标准化流程状态
type StandardFlowState struct {
	Step            int
	TotalSteps      int
	TargetDir       string
	SecurityPassed  bool
	ScanComplete    bool
	QualityAnalyzed bool
	ModeSelected    types.AppMode
	ProcessingDone  bool
	ReportGenerated bool
}

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// 定义命令行参数
	nonInteractive := flag.Bool("non-interactive", false, "Enable non-interactive mode with default settings.")
	flag.Parse()

	isInteractive := !*nonInteractive

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	uiManager := interactive.NewInterface(logger, isInteractive)
	unifiedProgress := progress.NewUnifiedProgress(logger)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("收到退出信号，正在安全退出...")
		unifiedProgress.Stop()
		cancel()
		os.Exit(0)
	}()

	uiManager.ShowWelcome()

	for {
		run(ctx, logger, uiManager, unifiedProgress, isInteractive)
		if !isInteractive {
			uiManager.ShowSuccess("🎉 All tasks complete!")
			return // 在非交互模式下，执行一次后退出
		}
		uiManager.ShowSuccess("🎉 所有任务完成！程序将返回初始状态...")
		time.Sleep(2 * time.Second)
	}
}

func run(ctx context.Context, logger *zap.Logger, uiManager *interactive.Interface, unifiedProgress *progress.UnifiedProgress, isInteractive bool) {
	var targetDir string
	args := flag.Args()
	if len(args) > 0 {
		targetDir = args[0]
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			uiManager.ShowError(fmt.Sprintf("❌ Specified directory does not exist: %s", targetDir))
			os.Exit(1)
		}
		absPath, err := filepath.Abs(targetDir)
		if err != nil {
			uiManager.ShowError(fmt.Sprintf("❌ Failed to resolve path: %s", targetDir))
			os.Exit(1)
		}
		targetDir = absPath
		uiManager.ShowSuccess(fmt.Sprintf("✅ Using directory specified by command line: %s", targetDir))
	} else if !isInteractive {
		uiManager.ShowError("❌ In non-interactive mode, a target directory must be provided as an argument.")
		os.Exit(1)
	}

	state := &StandardFlowState{
		Step:       0,
		TotalSteps: 7,
		TargetDir:  targetDir,
	}

	if err := executeStandardFlow(ctx, state, logger, uiManager, unifiedProgress, isInteractive); err != nil {
		uiManager.ShowError(fmt.Sprintf("❌ Processing failed: %v", err))
	}
}
