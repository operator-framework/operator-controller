package applier_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
)

func Test_BundleFSChartLoader_UnsupportedBundleType(t *testing.T) {
	t.Log("Check BundleFSChartLoader surfaces unsupported bundle type errors")

	t.Log("By attempting to load a chart with no helm chart loaders configured")
	chartLoader := applier.BundleFSChartLoader{
		HelmChartLoaders: nil,
	}

	_, err := chartLoader.Load(fstest.MapFS{}, "installNamespace", "watchNamespace")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported bundle type")
}

func Test_BundleFSChartLoader_Fails(t *testing.T) {
	t.Log("Check BundleFSChartLoader surfaces chart loading errors")
	chartLoader := applier.BundleFSChartLoader{
		HelmChartLoaders: map[applier.BundleType]applier.HelmChartLoader{
			applier.BundleTypeRegistryV1: fakeHelmChartLoader{func(fs.FS, string, string) (*chart.Chart, error) {
				return nil, fmt.Errorf("some error")
			}},
		},
	}
	_, err := chartLoader.Load(fstest.MapFS{}, "installNamespace", "watchNamespace")
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_BundleFSChartLoader_Succeeds(t *testing.T) {
	t.Log("Check BundleFSChartLoader succeeds")

	t.Log("By creating a BundleFSChartLoader that supports both registry+v1 and helm bundles")
	chartLoader := applier.BundleFSChartLoader{
		HelmChartLoaders: applier.HelmChartLoaderMap{
			applier.BundleTypeHelm: fakeHelmChartLoader{func(fs.FS, string, string) (*chart.Chart, error) {
				return &chart.Chart{
					Metadata: &chart.Metadata{
						Name: "helm-chart-bundle-chart",
					},
				}, nil
			}},
			applier.BundleTypeRegistryV1: fakeHelmChartLoader{func(fs.FS, string, string) (*chart.Chart, error) {
				return &chart.Chart{
					Metadata: &chart.Metadata{
						Name: "registry-v1-bundle-chart",
					},
				}, nil
			}},
		},
	}

	t.Log("Ensuring registry+v1 bundles are handled by the registry+v1 helm chart loader")
	c, err := chartLoader.Load(newRegistryV1BundleFS(), "installNamespace", "watchNamespace")
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Equal(t, "registry-v1-bundle-chart", c.Metadata.Name)

	t.Log("Ensuring helm bundles are handled by the helm bundle loader")
	c, err = chartLoader.Load(fstest.MapFS{
		"some-helm-chart-archive.tgz": &fstest.MapFile{
			Data: []byte(""),
		},
	}, "installNamespace", "watchNamespace")
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Equal(t, "helm-chart-bundle-chart", c.Metadata.Name)
}

func Test_RegistryV1BundleLoader_CallsConverter_Success(t *testing.T) {
	t.Log("Testing integration between RegistryV1BundleLoader and RegistryV1BundleToHelmChartConverter")
	chartLoader := applier.RegistryV1BundleLoader{
		RegistryV1BundleToHelmChartConverter: fakeBundleToHelmChartConverter(func(bundleSource source.BundleSource, installNamespace string, watchNamespace string) (*chart.Chart, error) {
			t.Log("By checking the correct parameter values are passed in")
			require.Equal(t, "installNamespace", installNamespace)
			require.Equal(t, "watchNamespace", watchNamespace)

			t.Log("By checking the correct bundle fs is passed in")
			b, err := bundleSource.GetBundle()
			require.NoError(t, err)
			require.Equal(t, "test", b.PackageName)
			return &chart.Chart{
				Metadata: &chart.Metadata{
					Name: "test-chart",
				},
			}, nil
		}),
	}
	c, err := chartLoader.Load(newRegistryV1BundleFS(), "installNamespace", "watchNamespace")
	t.Log("By checking the any errors in the loading process are surfaced")
	require.NoError(t, err)
	require.Equal(t, &chart.Chart{
		Metadata: &chart.Metadata{
			Name: "test-chart",
		},
	}, c)
}

func Test_RegistryV1BundleLoader_CallsConverter_Failure(t *testing.T) {
	t.Log("Testing integration between RegistryV1BundleLoader and RegistryV1BundleToHelmChartConverter")
	chartLoader := applier.RegistryV1BundleLoader{
		RegistryV1BundleToHelmChartConverter: fakeBundleToHelmChartConverter(func(bundleSource source.BundleSource, installNamespace string, watchNamespace string) (*chart.Chart, error) {
			return nil, fmt.Errorf("test error")
		}),
	}
	c, err := chartLoader.Load(fstest.MapFS{}, "installNamespace", "watchNamespace")
	t.Log("By checking the any errors in the loading process are surfaced")
	require.Error(t, err)
	require.Nil(t, c)
	require.Equal(t, "test error", err.Error())
}

func Test_HelmBundleLoader_Success(t *testing.T) {
	t.Log("Test HelmBundleLoader can load a chart from a helm bundle")
	helmArchive := newHelmChartArchive(t, map[string][]byte{
		"testchart/Chart.yaml":                []byte("apiVersion: v2\nname: test-chart\nversion: 0.1.0"),
		"testchart/templates/deployment.yaml": []byte("kind: Deployment\napiVersion: apps/v1\nmetadata:\n  name: test-chart\n  namespace: {{ .Release.Namespace }}"),
	})
	bundleFS := fstest.MapFS{
		"test-chart.v0.1.0.tgz": &fstest.MapFile{
			Mode: 0600,
			Data: helmArchive,
		},
	}

	charLoader := applier.HelmBundleLoader{}
	c, err := charLoader.Load(bundleFS, "installNamespace", "watchNamespace")
	t.Log("By checking the chart loader succeeds")
	require.NoError(t, err)
	require.NotNil(t, c)
	t.Log("By checking the chart metadata is correct")
	require.Equal(t, &chart.Metadata{
		Name:       "test-chart",
		Version:    "0.1.0",
		APIVersion: "v2",
	}, c.Metadata)

	t.Log("By checking the number of templates")
	require.Len(t, c.Templates, 1)

	t.Log("By checking the correct install namespace is being used")
	obj := map[string]interface{}{}
	require.NoError(t, yaml.Unmarshal(c.Templates[0].Data, obj))
	require.Equal(t, "installNamespace", (&unstructured.Unstructured{Object: obj}).GetNamespace())
}

func Test_HelmBundleLoader_BadHelmChartBundle_Fails(t *testing.T) {
	t.Log("Test HelmBundleLoader surfaces issues loading bad helm chart bundles")
	helmArchive := newHelmChartArchive(t, map[string][]byte{})
	bundleFS := fstest.MapFS{
		"test-chart.v0.1.0.tgz": &fstest.MapFile{
			Mode: 0000,
			Data: helmArchive,
		},
	}
	charLoader := applier.HelmBundleLoader{}

	t.Log("By attempting to load an empty helm bundle")
	c, err := charLoader.Load(bundleFS, "installNamespace", "watchNamespace")
	t.Log("and checking no chart is returned")
	require.Nil(t, c)
	t.Log("and checking an error is returned")
	require.Error(t, err)
}

type fakeBundleToHelmChartConverter func(source.BundleSource, string, string) (*chart.Chart, error)

func (f fakeBundleToHelmChartConverter) ToHelmChart(bundleSource source.BundleSource, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	return f(bundleSource, installNamespace, watchNamespace)
}

func newHelmChartArchive(t *testing.T, fsMap map[string][]byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add files to the chart archive
	for name, content := range fsMap {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(content)),
		}))
		_, _ = tw.Write(content)
	}

	require.NoError(t, tw.Close())

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	_, err := gz.Write(buf.Bytes())
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	return gzBuf.Bytes()
}

func newRegistryV1BundleFS() fstest.MapFS {
	annotationsYml := `
annotations:
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.package.v1: test
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
		"metadata/annotations.yaml": &fstest.MapFile{Data: []byte(strings.Trim(annotationsYml, "\n"))},
		"manifests/csv.yaml":        &fstest.MapFile{Data: []byte(strings.Trim(csvYml, "\n"))},
	}
}
