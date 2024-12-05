package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chartutil"

	"github.com/operator-framework/operator-controller/internal/features"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var registryV1CertProviderName string
	cmd := &cobra.Command{
		Use:  "registryv1-to-helm <registry+v1-directory-path> [output-path]",
		Args: cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			registryv1Path := args[0]

			saveDir := "."
			if len(args) == 2 {
				saveDir = args[1]
			}

			rv1, err := convert.LoadRegistryV1(cmd.Context(), os.DirFS(registryv1Path))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to load registry+v1 bundle: %v\n", err)
				os.Exit(1)
			}

			rv1CertProvider, err := convert.CertProviderByName(registryV1CertProviderName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to load certificate provider: %v\n", err)
				os.Exit(1)
			}

			chrt, err := rv1.ToHelmChart(convert.WithCertificateProvider(rv1CertProvider))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to convert registry v1 to helm chart: %v\n", err)
				os.Exit(1)
			}

			if err := chartutil.SaveDir(chrt, saveDir); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write helm chart to directory: %v\n", err)
				os.Exit(1)
			}

			origChartDir := filepath.Join(saveDir, chrt.Metadata.Name)
			desiredChartDir := filepath.Join(saveDir, fmt.Sprintf("%s-%s", chrt.Metadata.Name, chrt.Metadata.Version))
			if err := os.Rename(origChartDir, desiredChartDir); err != nil {
				fmt.Fprintf(os.Stderr, "failed to rename helm chart directory: %v\n", err)
				os.Exit(1)
			}
			cmd.Printf("Chart saved to %s\n", desiredChartDir)
		},
	}
	features.InitializeFromCLIFlags(cmd.Flags())
	if features.OperatorControllerFeatureGate.Enabled(features.RegistryV1WebhookSupport) {
		cmd.Flags().StringVar(&registryV1CertProviderName, "registry-v1-cert-provider", "", "a certificate provider to use to generate certificates for registry+v1-defined webhooks (if unset, registry+v1 bundles that define webhooks are unsupported)")
	}

	return cmd
}
