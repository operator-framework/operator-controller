package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <bundle-dir> <install-namespace>\n", os.Args[0])
		os.Exit(1)
	}
	bundleDir := os.Args[1]
	installNs := os.Args[2]

	// load optional configuration values from config.yaml
	cfg := map[string]interface{}{}
	configFile := filepath.Join(bundleDir, "config.yaml")
	if data, err := os.ReadFile(configFile); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error unmarshalling config file %q: %v\n", configFile, err)
			os.Exit(1)
		}
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
