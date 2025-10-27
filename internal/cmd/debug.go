//go:build debug

package cmd

import (
	"pixly/internal/debug"

	"github.com/spf13/cobra"
)

// debugCmd represents the debug command
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "🔧 Debug and testing utilities",
	Long: `Debug and testing utilities for development and troubleshooting.

This command provides various debugging tools and test suites to help
developers troubleshoot issues and verify functionality.`,
	RunE: runDebug,
}

var (
	debugConfigPath   string
	debugScenarioFile string
	debugReportPath   string
	debugVerbose      bool
)

func init() {
	// Add debug command to root command only in debug builds
	rootCmd.AddCommand(debugCmd)

	// Define flags for debug command
	debugCmd.Flags().StringVar(&debugConfigPath, "config", "config/config.yaml", "Configuration file path")
	debugCmd.Flags().StringVar(&debugScenarioFile, "scenarios", "debug/comprehensive_test_scenarios.json", "Test scenario file path")
	debugCmd.Flags().StringVar(&debugReportPath, "report", "debug_report.json", "Test report output path")
	debugCmd.Flags().BoolVarP(&debugVerbose, "verbose", "v", false, "Verbose output")
}

func runDebug(cmd *cobra.Command, args []string) error {
	// Call the RunDebugSuite function from the internal/debug package
	return debug.RunDebugSuite(debugConfigPath, debugScenarioFile, debugReportPath, debugVerbose)
}
