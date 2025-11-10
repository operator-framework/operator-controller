package main

import (
	"fmt"
	"path/filepath"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/comparator"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/spf13/cobra"
)

var compareCmd = &cobra.Command{
	Use:   "compare <baseline> <optimized>",
	Short: "Compare two profile runs",
	Long: `Generate a comparison report between two profile runs.

This helps identify improvements or regressions between different
versions or configurations.

Example:
  test-profile compare baseline optimized`,
	Args: cobra.ExactArgs(2),
	RunE: runCompare,
}

func runCompare(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()

	baselineName := args[0]
	optimizedName := args[1]

	baselineDir := filepath.Join(cfg.OutputDir, baselineName)
	optimizedDir := filepath.Join(cfg.OutputDir, optimizedName)
	outputDir := filepath.Join(cfg.OutputDir, "comparisons")

	fmt.Printf("📊 Comparing %s vs %s\n", baselineName, optimizedName)

	c := comparator.NewComparator(baselineDir, optimizedDir, outputDir)
	return c.Compare(baselineName, optimizedName)
}
