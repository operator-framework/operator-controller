package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/collector"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/spf13/cobra"
)

var (
	daemonMode bool
)

var startCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start profiling in daemon mode",
	Long: `Start collecting profiles in the background. Use 'stop' to end collection.

The profiler will:
- Daemonize itself and return immediately
- Wait for the cluster to be ready
- Set up port-forwarding to components
- Collect profiles at regular intervals
- Continue until 'stop' is called

Examples:
  # Auto-generated name (timestamp)
  test-profile start

  # Custom name
  test-profile start my-test`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

func init() {
	startCmd.Flags().BoolVar(&daemonMode, "daemon", false, "Internal flag for daemon process")
	startCmd.Flags().MarkHidden("daemon")
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()

	// Set name
	if len(args) > 0 {
		cfg.Name = args[0]
	} else {
		cfg.Name = time.Now().Format("20060102-150405")
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	// If not in daemon mode, fork and exit
	if !daemonMode {
		return daemonize(args)
	}

	// We are now the daemon process
	// Setup log file
	logFile := filepath.Join(cfg.ProfileDir(), "profiler.log")
	if err := os.MkdirAll(cfg.ProfileDir(), 0755); err != nil {
		return fmt.Errorf("failed to create profile directory: %w", err)
	}

	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer log.Close()

	// Redirect stdout and stderr to log file
	os.Stdout = log
	os.Stderr = log

	// Check if already running
	if _, err := os.Stat(cfg.PIDFile()); err == nil {
		return fmt.Errorf("profiler already running (PID file exists: %s)", cfg.PIDFile())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n🛑 Received interrupt, stopping...")
		cancel()
	}()

	// Create and start collector
	c := collector.NewCollector(cfg)
	if err := c.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Error starting profiler: %v\n", err)
		return err
	}

	// Wait for context cancellation
	<-ctx.Done()

	return c.Stop()
}

// daemonize forks the process to run in the background
func daemonize(args []string) error {
	// Get the executable path
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build command with --daemon flag
	cmdArgs := []string{"start", "--daemon"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(executable, cmdArgs...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Print success message with helpful info
	fmt.Printf("✅ Profiler started in background (PID: %d)\n", cmd.Process.Pid)

	// Try to determine the profile name for better UX
	var name string
	if len(args) > 0 {
		name = args[0]
	} else {
		name = time.Now().Format("20060102-150405")
	}

	cfg := config.DefaultConfig()
	cfg.Name = name
	logFile := filepath.Join(cfg.ProfileDir(), "profiler.log")

	fmt.Printf("📁 Profile directory: %s\n", cfg.ProfileDir())
	fmt.Printf("📋 Logs: %s\n", logFile)
	fmt.Printf("🛑 Stop with: test-profile stop\n")

	return nil
}
