package source_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

const (
	olmProperties = "olm.properties"
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
	rv1, err := source.FromFS(NewBundleFS()).GetBundle()
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
			FS:   removePaths(NewBundleFS(), BundlePathCSV),
		}, {
			name: "bundle missing metadata/annotations.yaml",
			FS:   removePaths(NewBundleFS(), BundlePathAnnotations),
		}, {
			name: "bundle missing metadata/ directory",
			FS:   removePaths(NewBundleFS(), "metadata/"),
		}, {
			name: "bundle missing manifests/ directory",
			FS:   removePaths(NewBundleFS(), "manifests/"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := source.FromFS(tt.FS).GetBundle()
			require.Error(t, err)
		})
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
