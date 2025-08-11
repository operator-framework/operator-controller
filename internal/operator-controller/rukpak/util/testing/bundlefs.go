package testing

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing/fstest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	BundlePathAnnotations = "metadata/annotations.yaml"
	BundlePathProperties  = "metadata/properties.yaml"
	BundlePathManifests   = "manifests"
	BundlePathCSV         = BundlePathManifests + "/csv.yaml"
)

func NewBundleFS() fstest.MapFS {
	annotationsYml := `
annotations:
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.package.v1: test
`

	propertiesYml := `
properties:
  - type: "from-file-key"
    value: "from-file-value"
`

	csvYml := `
apiVersion: operators.operatorframework.io/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: test.v1.0.0
  annotations:
    olm.properties: '[{"type":"from-csv-annotations-key", "value":"from-csv-annotations-value"}]'
spec:
  installModes:
    - type: AllNamespaces
      supported: true
`

	return fstest.MapFS{
		BundlePathAnnotations: &fstest.MapFile{Data: []byte(strings.Trim(annotationsYml, "\n"))},
		BundlePathProperties:  &fstest.MapFile{Data: []byte(strings.Trim(propertiesYml, "\n"))},
		BundlePathCSV:         &fstest.MapFile{Data: []byte(strings.Trim(csvYml, "\n"))},
	}
}

func AddManifest(bundleFS fstest.MapFS, obj client.Object) error {
	gvk := obj.GetObjectKind().GroupVersionKind()
	manifestName := fmt.Sprintf("%s%s_%s_%s%s.yaml", gvk.Group, gvk.Version, gvk.Kind, obj.GetNamespace(), obj.GetName())
	bytes, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	bundleFS[filepath.Join(BundlePathManifests, manifestName)] = &fstest.MapFile{
		Data: bytes,
	}
	return nil
}
