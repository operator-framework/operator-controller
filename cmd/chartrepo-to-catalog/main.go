package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chartrepo-to-catalog",
		Args:  cobra.ExactArgs(1),
		Short: "opm render, but from a Helm chart repo",
		Run: func(cmd *cobra.Command, args []string) {
			repoURL := args[0]
			repoEntry := repo.Entry{
				URL: repoURL,
			}

			providers := getter.Providers{
				getter.Provider{
					Schemes: []string{"http", "https"},
					New:     getter.NewHTTPGetter,
				},
				getter.Provider{
					Schemes: []string{"oci"},
					New:     getter.NewOCIGetter,
				},
			}

			repoClient, err := repo.NewChartRepository(&repoEntry, providers)
			if err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}

			tmpDir, err := os.MkdirTemp("", "chartrepo-to-catalog")
			if err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}
			defer os.RemoveAll(tmpDir)

			repoClient.CachePath = tmpDir

			indexFilePath, err := repoClient.DownloadIndexFile()
			if err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}
			idx, err := repo.LoadIndexFile(indexFilePath)
			if err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}

			fbc := declcfg.DeclarativeConfig{}
			for name, entries := range idx.Entries {
				if len(entries) == 0 {
					continue
				}

				pkg := declcfg.Package{
					Schema:         "olm.package",
					Name:           name,
					Description:    entries[0].Description,
					DefaultChannel: "default",
				}
				ch := declcfg.Channel{
					Schema:  "olm.channel",
					Package: name,
					Name:    "default",
					Entries: mapSlice(entries, func(i int, entry *repo.ChartVersion) declcfg.ChannelEntry {
						replaces := ""
						if i < len(entries)-1 {
							replaces = bundleNameFromEntry(name, entries[i+1])
						}
						return declcfg.ChannelEntry{
							Name:     bundleNameFromEntry(name, entry),
							Replaces: replaces,
						}
					}),
				}
				bundles := mapSlice(entries, func(_ int, entry *repo.ChartVersion) declcfg.Bundle {
					return declcfg.Bundle{
						Schema:  "olm.bundle",
						Package: name,
						Name:    bundleNameFromEntry(name, entry),
						Image:   fmt.Sprintf("%s/%s", repoURL, entry.URLs[0]),
						Properties: []property.Property{
							property.MustBuildPackage(name, strings.TrimPrefix(entry.Version, "v")),
						},
					}
				})

				fbc.Packages = append(fbc.Packages, pkg)
				fbc.Channels = append(fbc.Channels, ch)
				fbc.Bundles = append(fbc.Bundles, bundles...)
			}
			if err := declcfg.WriteYAML(fbc, cmd.OutOrStdout()); err != nil {
				cmd.PrintErr(err)
				os.Exit(1)
			}
		},
	}
	return cmd
}

func bundleNameFromEntry(name string, entry *repo.ChartVersion) string {
	return fmt.Sprintf("%s.v%s", name, strings.TrimPrefix(entry.Version, "v"))
}

func mapSlice[T any, U any](s []T, f func(int, T) U) []U {
	out := make([]U, len(s))
	for i, v := range s {
		out[i] = f(i, v)
	}
	return out
}
