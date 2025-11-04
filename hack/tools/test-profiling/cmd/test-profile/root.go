package main

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "test-profile",
	Short: "Test profiling tool for operator-controller",
	Long: `Test profiling tool for collecting, analyzing, and comparing
heap and CPU profiles during operator-controller tests.

Examples:
  # Run test with profiling
  test-profile run baseline

  # Start profiling daemon
  test-profile start my-test

  # Stop profiling daemon
  test-profile stop

  # Analyze collected profiles
  test-profile analyze baseline

  # Compare two runs
  test-profile compare baseline optimized

  # Collect single snapshot
  test-profile collect`,
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(collectCmd)
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(compareCmd)
}
