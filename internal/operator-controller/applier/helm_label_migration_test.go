package applier_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

func TestHelmApply_RemovesLiveLegacyLabelsWithoutCatalog(t *testing.T) {
	t.Parallel()
	cl, store, getter := newHelmLabelMigrationEnv(t)
	legacy := map[string]string{
		"olm.operatorframework.io/owner-kind": "ClusterExtension",
		"olm.operatorframework.io/owner-name": "my-extension",
		"example.com/custom":                  "keep",
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "managed", Labels: legacy}}
	widget := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "managed", "namespace": "managed"},
	}}
	widget.SetLabels(legacy)
	require.NoError(t, cl.Create(t.Context(), cm))
	require.NoError(t, cl.Create(t.Context(), widget))
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "my-extension"},
		Spec:       ocv1.ClusterExtensionSpec{Namespace: "managed"},
	}
	stored := &release.Release{
		Name: ext.Name, Namespace: "managed", Version: 1,
		Info: &release.Info{Status: release.StatusDeployed},
		Manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: managed
  namespace: managed
  labels:
    olm.operatorframework.io/owner-kind: ClusterExtension
    olm.operatorframework.io/owner-name: my-extension
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: managed
  namespace: managed
  labels:
    olm.operatorframework.io/owner-kind: ClusterExtension
    olm.operatorframework.io/owner-name: my-extension
`,
	}
	require.NoError(t, store.Create(stored))
	h := &applier.Helm{ActionClientGetter: getter, TrackingCache: &mockTrackingCache{}}
	for range 2 {
		complete, _, err := h.Apply(t.Context(), nil, ext, nil, nil)
		require.NoError(t, err)
		require.True(t, complete)
		for _, obj := range []client.Object{cm, widget} {
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
			assert.Equal(t, map[string]string{
				labels.ClusterExtensionNameKey: ext.Name,
				"example.com/custom":           "keep",
			}, obj.GetLabels())
		}
	}
	unchanged, err := store.Get(stored.Name, 1)
	require.NoError(t, err)
	assert.Equal(t, stored.Manifest, unchanged.Manifest)
}

func TestHelmApply_RemovesLiveLegacyLabelsWithCatalog(t *testing.T) {
	t.Parallel()
	cl, store, getter := newHelmLabelMigrationEnv(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "my-extension"},
		Spec:       ocv1.ClusterExtensionSpec{Namespace: "managed"},
	}
	chrt := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "managed", Version: "0.1.0"},
		Templates: []*chart.File{{
			Name: "templates/resources.yaml",
			Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: managed
  namespace: managed
  labels:
    example.com/custom: keep
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: managed
  namespace: managed
  labels:
    example.com/custom: keep
`),
		}},
	}
	h := &applier.Helm{
		ActionClientGetter: getter,
		HelmChartProvider: &FakeHelmChartProvider{
			fn: func(fs.FS, *ocv1.ClusterExtension) (*chart.Chart, error) { return chrt, nil },
		},
		HelmReleaseToObjectsConverter: applier.HelmReleaseToObjectsConverter{},
		TrackingCache:                 &mockTrackingCache{},
	}
	content := fstest.MapFS{}
	complete, _, err := h.Apply(t.Context(), content, ext, nil, nil)
	require.NoError(t, err)
	require.True(t, complete)
	stored, err := store.Last(ext.Name)
	require.NoError(t, err)
	storedVersion, storedManifest := stored.Version, stored.Manifest
	require.NotContains(t, storedManifest, labels.OwnerKindKey)
	require.NotContains(t, storedManifest, labels.OwnerNameKey)

	objects := []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "managed"}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.com/v1", "kind": "Widget",
			"metadata": map[string]any{"name": "managed", "namespace": "managed"},
		}},
	}
	// Introduce drift only in live resources, leaving the normalized release unchanged.
	for _, obj := range objects {
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
		obj.SetLabels(map[string]string{
			labels.ClusterExtensionNameKey: ext.Name,
			labels.OwnerKindKey:            ocv1.ClusterExtensionKind,
			labels.OwnerNameKey:            ext.Name,
			"example.com/custom":           "keep",
		})
		require.NoError(t, cl.Update(t.Context(), obj))
	}

	for range 2 {
		complete, _, err := h.Apply(t.Context(), content, ext, nil, nil)
		require.NoError(t, err)
		require.True(t, complete)
		for _, obj := range objects {
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
			assert.Equal(t, map[string]string{
				labels.ClusterExtensionNameKey: ext.Name,
				"example.com/custom":           "keep",
			}, obj.GetLabels())
		}
	}
	unchanged, err := store.Last(ext.Name)
	require.NoError(t, err)
	assert.Equal(t, storedVersion, unchanged.Version, "maintenance must not create an upgrade")
	assert.Equal(t, storedManifest, unchanged.Manifest, "stored release history must remain untouched")
}

func newHelmLabelMigrationEnv(t *testing.T) (client.Client, *storage.Storage, helmclient.ActionClientGetter) {
	t.Helper()
	env := testutil.NewEnv()
	env.CRDDirectoryPaths = nil
	env.CRDs = []*apiextensionsv1.CustomResourceDefinition{{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "widgets", Kind: "Widget"},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				},
			}},
		},
	}}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testutil.StopWithRetry(env, 30*time.Second, time.Second)) })
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	require.NoError(t, cl.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "managed"}}))
	store := storage.Init(driver.NewMemory())
	acg, err := helmclient.NewActionConfigGetter(cfg, cl.RESTMapper(),
		helmclient.ClientNamespaceMapper(func(client.Object) (string, error) { return "managed", nil }),
		helmclient.StorageDriverMapper(func(context.Context, client.Object, *rest.Config) (driver.Driver, error) {
			return store.Driver, nil
		}))
	require.NoError(t, err)
	// This environment provides the managed resource APIs; omit the owner renderer,
	// which requires discovery of the ClusterExtension API.
	getter, err := helmclient.NewActionClientGetter(acg,
		helmclient.AppendInstallOptions(helmclient.WithInstallPostRenderer(nil)),
		helmclient.AppendUpgradeOptions(helmclient.WithUpgradePostRenderer(nil)))
	require.NoError(t, err)
	return cl, store, getter
}
