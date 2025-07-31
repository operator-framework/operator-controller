package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
)

func main() {
	helmFlag := flag.Bool("helm", false, "render as Helm chart instead of registry+v1 bundle")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [-helm] <bundle-dir> <install-namespace>\n", os.Args[0])
		os.Exit(1)
	}
	bundleDir := flag.Arg(0)
	installNs := flag.Arg(1)

	// load optional configuration values from config.yaml
	cfg := map[string]interface{}{}
	configFile := filepath.Join(bundleDir, "config.yaml")
	if data, err := os.ReadFile(configFile); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error unmarshalling config file %q: %v\n", configFile, err)
			os.Exit(1)
		}
	}

	if *helmFlag {
		// ensure a Helm chart is present
		// look for Chart.yaml in the specified dir, or try replacing "bundles" -> "charts" if not found
		chartPath := filepath.Join(bundleDir, "Chart.yaml")
		if _, err := os.Stat(chartPath); err != nil {
			// fallback: swap a "bundles" segment to "charts" in the path
			parts := strings.Split(bundleDir, string(os.PathSeparator))
			for i, p := range parts {
				if p == "bundles" {
					parts[i] = "charts"
					altDir := filepath.Join(parts...)
					if _, err2 := os.Stat(filepath.Join(altDir, "Chart.yaml")); err2 == nil {
						bundleDir = altDir
						chartPath = filepath.Join(bundleDir, "Chart.yaml")
						break
					}
				}
			}
			if _, err := os.Stat(chartPath); err != nil {
				fmt.Fprintf(os.Stderr, "error: helm chart not found in %q: %v\n", bundleDir, err)
				os.Exit(1)
			}
		}
		// render with the Helm engine
		chrt, err := loader.Load(bundleDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading Helm chart: %v\n", err)
			os.Exit(1)
		}
		// load values.yaml and apply any config overrides
		values := map[string]interface{}{}
		valuesFile := filepath.Join(bundleDir, "values.yaml")
		if data, err := os.ReadFile(valuesFile); err == nil {
			if err := yaml.Unmarshal(data, &values); err != nil {
				fmt.Fprintf(os.Stderr, "error unmarshalling values file %q: %v\n", valuesFile, err)
				os.Exit(1)
			}
		}
		for k, v := range cfg {
			values[k] = v
		}
		// render the chart templates using the provided values under the 'Values' key
		renderContext := chartutil.Values{"Values": values}
		rendered, err := engine.Engine{}.Render(chrt, renderContext)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error rendering Helm chart: %v\n", err)
			os.Exit(1)
		}
		for name, content := range rendered {
			fmt.Printf("---\n# Source: %s\n%s\n", name, content)
		}
		return
	}

	// parse registry+v1 bundle
	regv1, err := source.FromFS(os.DirFS(bundleDir)).GetBundle()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing bundle directory: %v\n", err)
		os.Exit(1)
	}

	// render bundle with configuration
	opts := []render.Option{render.WithConfig(cfg)}
	objs, err := registryv1.Renderer.Render(regv1, installNs, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error rendering bundle: %v\n", err)
		os.Exit(1)
	}

	for _, obj := range objs {
		data, err := yaml.Marshal(obj)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error marshaling object: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("---\n%s", string(data))
	}
}
