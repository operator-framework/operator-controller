package main

import (
	"fmt"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/analyzer"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze <name>",
	Short: "Analyze collected profiles",
	Long: `Generate an analysis report from previously collected profiles.

The report includes:
- Memory growth analysis
- Top memory allocators
- CPU profiling results
- OpenAPI and JSON deserialization analysis

Example:
  test-profile analyze baseline`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	cfg.Name = args[0]

	if err := cfg.Validate(); err != nil {
		return err
	}

	fmt.Printf("📊 Analyzing profiles in: %s\n", cfg.ProfileDir())

	a := analyzer.NewAnalyzer(cfg)
	return a.Analyze()
}
