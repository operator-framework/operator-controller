package operatore2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

const bundlePath = "bundles/plain-v0"

type Object struct {
	Kind       string `yaml:"kind"`
	APIVersion string `yaml:"apiVersion"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

// collects the Kubernetes objects present in the bundle manifest folder
func collectKubernetesObjects(packageName string, version string) ([]Object, error) {
	objects := []Object{}

	bundleManifestPath := filepath.Join(bundlePath, packageName+".v"+version, "manifests")
	files, err := os.ReadDir(bundleManifestPath)
	if err != nil {
		return nil, fmt.Errorf("error reading directory: %v", err)
	}

	for _, file := range files {
		filePath := filepath.Join(bundleManifestPath, file.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("error reading file: %v", err)
		}

		documents := strings.Split(string(content), "---")
		for _, doc := range documents {
			obj := Object{}
			err := yaml.Unmarshal([]byte(doc), &obj)
			if err != nil {
				return nil, fmt.Errorf("error parsing YAML: %v", err)
			}

			if obj.Kind != "" && obj.APIVersion != "" && obj.Metadata.Name != "" {
				objects = append(objects, obj)
			}
		}
	}

	return objects, nil
}
