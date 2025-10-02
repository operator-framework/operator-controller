package source_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/bundlefs"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
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
	bundleFS := bundlefs.Builder().
		WithPackageName("test").
		WithBundleProperty("from-file-key", "from-file-value").
		WithBundleResource("csv.yaml", ptr.To(clusterserviceversion.Builder().
			WithName("test.v1.0.0").
			WithAnnotations(map[string]string{
				"olm.properties": `[{"type":"from-csv-annotations-key", "value":"from-csv-annotations-value"}]`,
			}).
			WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build())).
		Build()

	rv1, err := source.FromFS(bundleFS).GetBundle()
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
			FS: bundlefs.Builder().
				WithPackageName("test").
				WithBundleProperty("foo", "bar").
				WithBundleResource("service.yaml", &corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
				}).Build(),
		}, {
			name: "bundle missing metadata/annotations.yaml",
			FS: bundlefs.Builder().
				WithBundleProperty("foo", "bar").
				WithBundleResource("csv.yaml", ptr.To(clusterserviceversion.Builder().Build())).Build(),
		}, {
			name: "metadata/annotations.yaml missing package name annotation",
			FS: bundlefs.Builder().
				WithBundleProperty("foo", "bar").
				WithBundleResource("csv.yaml", ptr.To(clusterserviceversion.Builder().Build())).Build(),
		}, {
			name: "bundle missing manifests directory",
			FS: bundlefs.Builder().
				WithPackageName("test").
				WithBundleProperty("foo", "bar").Build(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := source.FromFS(tt.FS).GetBundle()
			require.Error(t, err)
		})
	}
}
