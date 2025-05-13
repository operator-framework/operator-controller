package source_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
)

const (
	olmProperties = "olm.properties"

	bundlePathAnnotations = "metadata/annotations.yaml"
	bundlePathProperties  = "metadata/properties.yaml"
	bundlePathCSV         = "manifests/csv.yaml"
)

func Test_FromBundle_Success(t *testing.T) {
	expectedBundle := bundle.RegistryV1{
		PackageName: "my-package",
	}
	b, err := source.FromBundle(expectedBundle).GetBundle()
	require.NoError(t, err)
	require.Equal(t, expectedBundle, b)
}

func Test_FromFS_Success(t *testing.T) {
	rv1, err := source.FromFS(newBundleFS()).GetBundle()
	require.NoError(t, err)

	t.Log("Check package name is correctly taken from metadata/annotations.yaml")
	require.Equal(t, "test", rv1.PackageName)

	t.Log("Check metadata/properties.yaml is merged into csv.annotations[olm.properties]")
	require.JSONEq(t, `[{"type":"from-csv-annotations-key","value":"from-csv-annotations-value"},{"type":"from-file-key","value":"from-file-value"}]`, rv1.CSV.Annotations[olmProperties])
}

func Test_FromFS_Fails(t *testing.T) {
	for _, tt := range []struct {
		name string
		FS   fs.FS
	}{
		{
			name: "bundle missing ClusterServiceVersion manifest",
			FS:   removePaths(newBundleFS(), bundlePathCSV),
		}, {
			name: "bundle missing metadata/annotations.yaml",
			FS:   removePaths(newBundleFS(), bundlePathAnnotations),
		}, {
			name: "bundle missing metadata/ directory",
			FS:   removePaths(newBundleFS(), "metadata/"),
		}, {
			name: "bundle missing manifests/ directory",
			FS:   removePaths(newBundleFS(), "manifests/"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := source.FromFS(tt.FS).GetBundle()
			require.Error(t, err)
		})
	}
}

func newBundleFS() fstest.MapFS {
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
		bundlePathAnnotations: &fstest.MapFile{Data: []byte(strings.Trim(annotationsYml, "\n"))},
		bundlePathProperties:  &fstest.MapFile{Data: []byte(strings.Trim(propertiesYml, "\n"))},
		bundlePathCSV:         &fstest.MapFile{Data: []byte(strings.Trim(csvYml, "\n"))},
	}
}

func removePaths(mapFs fstest.MapFS, paths ...string) fstest.MapFS {
	for k := range mapFs {
		for _, path := range paths {
			if strings.HasPrefix(k, path) {
				delete(mapFs, k)
			}
		}
	}
	return mapFs
}
