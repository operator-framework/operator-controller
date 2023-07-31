package operatore2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

type Object struct {
	Kind       string `yaml:"kind"`
	APIVersion string `yaml:"apiVersion"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

// / collectKubernetesObjects collects the Kubernetes objects present in the bundle manifest folder for a particular package and its version
func collectKubernetesObjects(bundlePath, packageName, version string) ([]Object, error) {
	objects := []Object{}

	bundleManifestPath := filepath.Join(bundlePath, packageName+".v"+version, "manifests")
	err := filepath.Walk(bundleManifestPath, func(filePath string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking path %s: %v", filePath, err)
		}

		if fileInfo.IsDir() {
			return nil
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", filePath, err)
		}

		documents := strings.Split(string(content), "---")
		for _, doc := range documents {
			obj := Object{}
			if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
				return fmt.Errorf("error parsing YAML in file %s: %v", filePath, err)
			}

			if obj.Kind != "" && obj.APIVersion != "" && obj.Metadata.Name != "" {
				objects = append(objects, obj)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return objects, nil
}
