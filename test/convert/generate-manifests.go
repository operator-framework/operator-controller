package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func main() {
	bundleRootDir := "testdata/bundles/"
	outputRootDir := "test/convert/expected-manifests/"

	if err := os.RemoveAll(outputRootDir); err != nil {
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
		},
	} {
		bundlePath := filepath.Join(bundleRootDir, tc.bundle)
		generatedManifestPath := filepath.Join(outputRootDir, tc.bundle, tc.testCaseName)
		if err := generateManifests(generatedManifestPath, bundlePath, tc.installNamespace, tc.watchNamespace); err != nil {
			fmt.Printf("Error generating manifests: %v", err)
			os.Exit(1)
		}
	}
}

func generateManifests(outputPath, bundleDir, installNamespace, watchNamespace string) error {
	// Parse bundleFS into RegistryV1
	regv1, err := convert.ParseFS(os.DirFS(bundleDir))
	if err != nil {
		fmt.Printf("error parsing bundle directory: %v\n", err)
		os.Exit(1)
	}

	// Convert RegistryV1 to plain manifests
	plain, err := convert.Convert(regv1, installNamespace, []string{watchNamespace})
	if err != nil {
		return fmt.Errorf("error converting registry+v1 bundle: %w", err)
	}

	// Write plain manifests out to testcase directory
	if err := os.MkdirAll(outputPath, os.ModePerm); err != nil {
		return fmt.Errorf("error creating bundle directory: %w", err)
	}

	if err := func() error {
		for idx, obj := range slices.SortedFunc(slices.Values(plain.Objects), orderByKindNamespaceName) {
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
