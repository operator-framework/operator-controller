package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/analyzer"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop profiling daemon and generate analysis",
	Long: `Stop the running profiling daemon and generate analysis report.

This will:
- Find and stop the running profiler process
- Clean up port-forwarding
- Generate analysis report

Example:
  test-profile stop`,
	RunE: runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()

	// Find the most recent profile directory
	profileDir, err := findRecentProfile(cfg.OutputDir)
	if err != nil {
		return err
	}

	cfg.Name = filepath.Base(profileDir)

	pidFile := cfg.PIDFile()
	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		return fmt.Errorf("no profiler running (PID file not found: %s)", pidFile)
	}

	// Read PID
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	fmt.Printf("🛑 Stopping profiler (PID: %d)...\n", pid)

	// Send SIGTERM to the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		fmt.Printf("Warning: failed to signal process: %v\n", err)
	}

	// Clean up kubectl port-forward processes
	cleanupPortForwards()

	// Remove PID file
	_ = os.Remove(pidFile)

	fmt.Println("✅ Profiler stopped")

	// Generate analysis
	fmt.Println("\n📊 Generating analysis report...")
	a := analyzer.NewAnalyzer(cfg)
	if err := a.Analyze(); err != nil {
		return fmt.Errorf("failed to analyze profiles: %w", err)
	}

	return nil
}

func findRecentProfile(outputDir string) (string, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return "", fmt.Errorf("failed to read output directory: %w", err)
	}

	var latestDir string
	var latestTime int64

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(outputDir, entry.Name())
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			latestDir = fullPath
		}
	}

	if latestDir == "" {
		return "", fmt.Errorf("no profile directory found in %s", outputDir)
	}

	return latestDir, nil
}

func cleanupPortForwards() {
	cmd := exec.Command("pkill", "-f", "kubectl port-forward.*6060")
	_ = cmd.Run()
	cmd = exec.Command("pkill", "-f", "kubectl port-forward.*6061")
	_ = cmd.Run()
}
