package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/analyzer"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/collector"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/spf13/cobra"
)

var (
	testTarget string
)

var runCmd = &cobra.Command{
	Use:   "run <name> [test-target]",
	Short: "Run e2e tests with profiling",
	Long: `Run e2e tests while collecting profiles in the background.

This command:
1. Starts profile collection
2. Runs the specified test target
3. Stops collection when tests complete
4. Generates analysis report

Examples:
  # Use default test target
  test-profile run baseline

  # Specify custom test target
  test-profile run baseline test-e2e

  # Use environment variable
  TEST_PROFILE_TEST_TARGET=test-e2e test-profile run baseline`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runProfile,
}

func init() {
	runCmd.Flags().StringVar(&testTarget, "test-target", "", "Make target to run (default from TEST_PROFILE_TEST_TARGET or test-experimental-e2e)")
}

func runProfile(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	cfg.Name = args[0]

	// Override test target if specified
	if len(args) > 1 {
		cfg.TestTarget = args[1]
	} else if testTarget != "" {
		cfg.TestTarget = testTarget
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n🛑 Received interrupt, cleaning up...")
		cancel()
	}()

	// Start collector
	fmt.Printf("🚀 Starting profile collection for: %s\n", cfg.Name)
	c := collector.NewCollector(cfg)
	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("failed to start collector: %w", err)
	}

	// Ensure cleanup
	defer func() {
		fmt.Println("\n🛑 Stopping profiler...")
		_ = c.Stop()
	}()

	// Run tests
	fmt.Printf("\n🧪 Running tests: make %s\n\n", cfg.TestTarget)
	testErr := runTests(ctx, cfg)

	// Stop collector
	fmt.Println("\n🛑 Stopping profiler...")
	if err := c.Stop(); err != nil {
		return err
	}

	// Generate analysis
	fmt.Println("\n📊 Generating analysis report...")
	a := analyzer.NewAnalyzer(cfg)
	if err := a.Analyze(); err != nil {
		return fmt.Errorf("failed to analyze profiles: %w", err)
	}

	if testErr != nil {
		return fmt.Errorf("test execution failed: %w", testErr)
	}

	fmt.Printf("\n✅ Profiling complete! Report: %s/analysis.md\n", cfg.ProfileDir())
	return nil
}

func runTests(ctx context.Context, cfg *config.Config) error {
	cmd := exec.CommandContext(ctx, "make", cfg.TestTarget)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}
