package bundlefs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/bundlefs"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_BundleFSBuilder(t *testing.T) {
	t.Run("returns empty bundle file system by default", func(t *testing.T) {
		bundleFs := bundlefs.Builder().Build()
		assert.Empty(t, bundleFs)
	})

	t.Run("WithPackageName sets the bundle package annotation", func(t *testing.T) {
		bundleFs := bundlefs.Builder().WithPackageName("test").Build()
		require.Contains(t, bundleFs, "metadata/annotations.yaml")
		require.Equal(t, []byte(`annotations:
  operators.operatorframework.io.bundle.channel.default.v1: ""
  operators.operatorframework.io.bundle.channels.v1: ""
  operators.operatorframework.io.bundle.package.v1: test
`), bundleFs["metadata/annotations.yaml"].Data)
	})

	t.Run("WithChannels sets the bundle channels annotation", func(t *testing.T) {
		bundleFs := bundlefs.Builder().WithChannels("alpha", "beta", "stable").Build()
		require.Contains(t, bundleFs, "metadata/annotations.yaml")
		require.Equal(t, []byte(`annotations:
  operators.operatorframework.io.bundle.channel.default.v1: ""
  operators.operatorframework.io.bundle.channels.v1: alpha,beta,stable
  operators.operatorframework.io.bundle.package.v1: ""
`), bundleFs["metadata/annotations.yaml"].Data)
	})

	t.Run("WithDefaultChannel sets the bundle default channel annotation", func(t *testing.T) {
		bundleFs := bundlefs.Builder().WithDefaultChannel("stable").Build()
		require.Contains(t, bundleFs, "metadata/annotations.yaml")
		require.Equal(t, []byte(`annotations:
  operators.operatorframework.io.bundle.channel.default.v1: stable
  operators.operatorframework.io.bundle.channels.v1: ""
  operators.operatorframework.io.bundle.package.v1: ""
`), bundleFs["metadata/annotations.yaml"].Data)
	})

	t.Run("WithBundleProperty sets the bundle properties", func(t *testing.T) {
		bundleFs := bundlefs.Builder().
			WithBundleProperty("foo", "bar").
			WithBundleProperty("key", "value").
			Build()

		require.Contains(t, bundleFs, "metadata/properties.yaml")
		require.Equal(t, []byte(`properties:
- type: foo
  value: bar
- type: key
  value: value
`), bundleFs["metadata/properties.yaml"].Data)
	})

	t.Run("WithBundleResource adds a resource to the manifests directory", func(t *testing.T) {
		bundleFs := bundlefs.Builder().WithBundleResource("service.yaml", &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}).Build()
		require.Contains(t, bundleFs, "manifests/service.yaml")
		require.Equal(t, []byte(`apiVersion: v1
kind: Service
metadata:
  name: test
spec: {}
status:
  loadBalancer: {}
`), bundleFs["manifests/service.yaml"].Data)
	})

	t.Run("WithCSV adds a csv to the manifests directory", func(t *testing.T) {
		bundleFs := bundlefs.Builder().WithCSV(clusterserviceversion.Builder().WithName("some-csv").Build()).Build()
		require.Contains(t, bundleFs, "manifests/csv.yaml")
		require.Equal(t, []byte(`apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: some-csv
spec:
  apiservicedefinitions: {}
  cleanup:
    enabled: false
  customresourcedefinitions: {}
  displayName: ""
  install:
    spec:
      deployments: null
    strategy: ""
  provider: {}
  version: 0.0.0
status:
  cleanup: {}
`), bundleFs["manifests/csv.yaml"].Data)
	})
}
