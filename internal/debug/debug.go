//go:build debug

// Package debug provides debugging and testing utilities for the pixly application.
// This package is only compiled when the debug build tag is specified.
package debug

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"pixly/debug/testsuite"
)

// RunDebugSuite runs the comprehensive debug test suite
func RunDebugSuite(configPath string, scenarioFile string, reportPath string, verbose bool) error {
	// Initialize logger
	logger := initLogger(verbose)
	defer logger.Sync()

	// Check config file
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Fatal("Config file not found", zap.String("path", configPath))
	}

	// Check scenario file
	if _, err := os.Stat(scenarioFile); os.IsNotExist(err) {
		logger.Fatal("Scenario file not found", zap.String("path", scenarioFile))
	}

	// Create test suite
	ts, err := testsuite.NewTestSuite(configPath, logger)
	if err != nil {
		logger.Fatal("Failed to create test suite", zap.Error(err))
	}

	// Load test scenarios
	if err := ts.LoadScenariosFromFile(scenarioFile); err != nil {
		logger.Fatal("Failed to load test scenarios", zap.Error(err))
	}

	// Run all test scenarios
	if err := ts.RunAllScenarios(); err != nil {
		logger.Error("Test suite execution failed", zap.Error(err))
		return err
	}

	// Generate test report
	report := ts.GenerateReport()

	// Save test report
	if err := ts.SaveReport(report, reportPath); err != nil {
		logger.Error("Failed to save test report", zap.Error(err))
		return err
	}

	// Print summary
	fmt.Printf("\n🧪 Debug Test Suite Completed\n")
	fmt.Printf("📊 Total Scenarios: %d\n", report.TotalScenarios)
	fmt.Printf("✅ Passed Scenarios: %d\n", report.PassedScenarios)
	fmt.Printf("❌ Failed Scenarios: %d\n", report.FailedScenarios)
	fmt.Printf("📄 Report Path: %s\n", reportPath)

	if report.FailedScenarios > 0 {
		fmt.Printf("\n⚠️  Some test scenarios failed, please check the detailed report\n")
		return fmt.Errorf("some test scenarios failed")
	}

	fmt.Printf("\n🎉 All test scenarios passed!\n")
	return nil
}

func initLogger(verbose bool) *zap.Logger {
	var config zap.Config
	if verbose {
		config = zap.NewDevelopmentConfig()
	} else {
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	// Ensure logs go to stderr to avoid UI pollution
	config.OutputPaths = []string{"stderr"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build()
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	return logger
}
