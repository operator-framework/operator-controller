// generate-manifests.go
//
// Renders registry+v1 bundles into YAML manifests for regression testing.
// Used by tests to make sure output from the BundleRenderer stays consistent.
//
// By default, writes to ./testdata/tmp/generate/.
// To update expected output, run:
//
//	go run generate-manifests.go -output-dir=./testdata/expected-manifests/
//
// Only re-generate if you intentionally change rendering behavior.
// Note that if the test fails is likely a regression in the renderer.
package main

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
)

// This is a helper for a regression test to make sure the renderer output doesn't change.
//
// It renders known bundles into YAML files and writes them to a target output dir.
// By default, it writes to a temp path used in tests:
//
//	./testdata/tmp/rendered/
//
// If you want to update the expected output, run it with:
//
//	go run generate-manifests.go -output-dir=./testdata/expected-manifests/
//
// Note: Expected output should never change unless the renderer changes which is unlikely.
// If the convert_test.go test fails, it likely means a regression was introduced in the renderer.
func main() {
	bundleRootDir := "testdata/bundles/"
	defaultOutputDir := "./testdata/tmp/rendered/"
	outputRootDir := flag.String("output-dir", defaultOutputDir, "path to write rendered manifests to")
	flag.Parse()

	if err := os.RemoveAll(*outputRootDir); err != nil {
		fmt.Printf("error removing output directory: %v\n", err)
		os.Exit(1)
	}

	for _, tc := range []struct {
		name             string
		installNamespace string
		watchNamespace   string
		bundle           string
		testCaseName     string
	}{
		{
			name:             "AllNamespaces",
			installNamespace: "argocd-system",
			bundle:           "argocd-operator.v0.6.0",
			testCaseName:     "all-namespaces",
		}, {
			name:             "SingleNamespaces",
			installNamespace: "argocd-system",
			watchNamespace:   "argocd-watch",
			bundle:           "argocd-operator.v0.6.0",
			testCaseName:     "single-namespace",
		}, {
			name:             "OwnNamespaces",
			installNamespace: "argocd-system",
			watchNamespace:   "argocd-system",
			bundle:           "argocd-operator.v0.6.0",
			testCaseName:     "own-namespace",
		}, {
			name:             "Webhooks",
			installNamespace: "webhook-system",
			bundle:           "webhook-operator.v0.0.5",
			testCaseName:     "all-webhook-types",
		},
	} {
		bundlePath := filepath.Join(bundleRootDir, tc.bundle)
		generatedManifestPath := filepath.Join(*outputRootDir, tc.bundle, tc.testCaseName)
		if err := generateManifests(generatedManifestPath, bundlePath, tc.installNamespace, tc.watchNamespace); err != nil {
			fmt.Printf("Error generating manifests: %v", err)
			os.Exit(1)
		}
	}
}

func generateManifests(outputPath, bundleDir, installNamespace, watchNamespace string) error {
	// Parse bundleFS into RegistryV1
	regv1, err := source.FromFS(os.DirFS(bundleDir)).GetBundle()
	if err != nil {
		fmt.Printf("error parsing bundle directory: %v\n", err)
		os.Exit(1)
	}

	// Convert RegistryV1 to plain manifests
	objs, err := registryv1.Renderer.Render(regv1, installNamespace, render.WithTargetNamespaces(watchNamespace))
	if err != nil {
		return fmt.Errorf("error converting registry+v1 bundle: %w", err)
	}

	// Write plain manifests out to testcase directory
	if err := os.MkdirAll(outputPath, os.ModePerm); err != nil {
		return fmt.Errorf("error creating bundle directory: %w", err)
	}

	if err := func() error {
		for idx, obj := range slices.SortedFunc(slices.Values(objs), orderByKindNamespaceName) {
			kind := obj.GetObjectKind().GroupVersionKind().Kind
			fileName := fmt.Sprintf("%02d_%s_%s.yaml", idx, strings.ToLower(kind), obj.GetName())
			data, err := yaml.Marshal(obj)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(outputPath, fileName), data, 0600); err != nil {
				return err
			}
		}
		return nil
	}(); err != nil {
		// Clean up output directory in case of error
		_ = os.RemoveAll(outputPath)
		return fmt.Errorf("error writing object files: %w", err)
	}

	return nil
}

func orderByKindNamespaceName(a client.Object, b client.Object) int {
	return cmp.Or(
		cmp.Compare(a.GetObjectKind().GroupVersionKind().Kind, b.GetObjectKind().GroupVersionKind().Kind),
		cmp.Compare(a.GetNamespace(), b.GetNamespace()),
		cmp.Compare(a.GetName(), b.GetName()),
	)
}
