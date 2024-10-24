package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chartutil"

	v2 "github.com/operator-framework/operator-controller/internal/rukpak/convert/v2"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "registryv1-to-helm <registry+v1-directory-path> [output-path]",
		Args: cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			registryv1Path := args[0]

			saveDir := "."
			if len(args) == 2 {
				saveDir = args[1]
			}

			chrt, err := v2.RegistryV1ToHelmChart(cmd.Context(), os.DirFS(registryv1Path))
			if err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}

			if err := chartutil.SaveDir(chrt, saveDir); err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}

			origChartDir := filepath.Join(saveDir, chrt.Metadata.Name)
			desiredChartDir := filepath.Join(saveDir, fmt.Sprintf("%s-%s", chrt.Metadata.Name, chrt.Metadata.Version))
			if err := os.Rename(origChartDir, desiredChartDir); err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}
			cmd.Printf("Chart saved to %s\n", desiredChartDir)
		},
	}
	return cmd
}
