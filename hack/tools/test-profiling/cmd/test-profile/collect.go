package main

import (
	"context"
	"time"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/collector"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/spf13/cobra"
)

var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Collect a single profile snapshot",
	Long: `Collect a single snapshot of heap and CPU profiles from all components.

This is useful for quick spot checks without running the full daemon.

Example:
  test-profile collect`,
	RunE: runCollect,
}

func runCollect(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	cfg.Name = time.Now().Format("snapshot-20060102-150405")

	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx := context.Background()

	c := collector.NewCollector(cfg)
	return c.CollectOnce(ctx)
}
