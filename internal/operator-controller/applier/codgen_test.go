package applier_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"testing"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
)

var testScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(ocv1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(corev1.AddToScheme(s))
	return s
}()

type fakeManifestProvider struct {
	objs []client.Object
	err  error
}

func (f *fakeManifestProvider) Get(_ fs.FS, _ *ocv1.ClusterExtension) ([]client.Object, error) {
	return f.objs, f.err
}

func TestRegistryV1CODGenerator_PhaseOrdering(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{
			objs: []client.Object{
				deployment("deploy-a", "ns1"),
				configMap("cm-a", "ns1"),
				serviceAccount("sa-a", "ns1"),
				crd("things.example.com"),
			},
		},
		Scheme: testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, cod)

	phases := cod.Spec.Template.Spec.Phases
	require.Len(t, phases, 4)
	assert.Equal(t, "identity", *phases[0].Name)
	assert.Equal(t, "configuration", *phases[1].Name)
	assert.Equal(t, "crds", *phases[2].Name)
	assert.Equal(t, "deploy", *phases[3].Name)
}

func TestRegistryV1CODGenerator_PhaseChunking(t *testing.T) {
	objs := make([]client.Object, 0, 120)
	for i := range 120 {
		objs = append(objs, configMap(fmt.Sprintf("cm-%03d", i), "ns1"))
	}

	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: objs},
		Scheme:           testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	require.NoError(t, err)

	phases := cod.Spec.Template.Spec.Phases
	require.Len(t, phases, 3)
	assert.Equal(t, "configuration-1", *phases[0].Name)
	assert.Len(t, phases[0].Objects, 50)
	assert.Equal(t, "configuration-2", *phases[1].Name)
	assert.Len(t, phases[1].Objects, 50)
	assert.Equal(t, "configuration-3", *phases[2].Name)
	assert.Len(t, phases[2].Objects, 20)
}

func TestRegistryV1CODGenerator_DeterministicSorting(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{
			objs: []client.Object{
				configMap("zebra", "ns1"),
				configMap("alpha", "ns1"),
				configMap("mid", "ns2"),
			},
		},
		Scheme: testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	require.NoError(t, err)

	phases := cod.Spec.Template.Spec.Phases
	require.Len(t, phases, 1)
	require.Len(t, phases[0].Objects, 3)

	names := extractObjectNames(t, phases[0].Objects)
	// ns1/alpha, ns1/zebra, ns2/mid - sorted by namespace then name
	assert.Equal(t, []string{"alpha", "zebra", "mid"}, names)
}

func TestRegistryV1CODGenerator_Assertions(t *testing.T) {
	tests := []struct {
		name           string
		obj            client.Object
		wantAssertions int
		checkAssertion func(t *testing.T, assertions []orbac.AssertionApplyConfiguration)
	}{
		{
			name:           "CRD gets Established=True assertion",
			obj:            crd("things.example.com"),
			wantAssertions: 1,
			checkAssertion: func(t *testing.T, assertions []orbac.AssertionApplyConfiguration) {
				require.NotNil(t, assertions[0].ConditionEqual)
				assert.Equal(t, "Established", *assertions[0].ConditionEqual.Type)
				assert.Equal(t, "True", *assertions[0].ConditionEqual.Status)
			},
		},
		{
			name:           "Deployment gets two assertions",
			obj:            deployment("app", "ns1"),
			wantAssertions: 2,
			checkAssertion: func(t *testing.T, assertions []orbac.AssertionApplyConfiguration) {
				require.NotNil(t, assertions[0].FieldsEqual)
				assert.Equal(t, "status.updatedReplicas", *assertions[0].FieldsEqual.FieldA)
				assert.Equal(t, "status.replicas", *assertions[0].FieldsEqual.FieldB)
				require.NotNil(t, assertions[1].ConditionEqual)
				assert.Equal(t, "Available", *assertions[1].ConditionEqual.Type)
				assert.Equal(t, "True", *assertions[1].ConditionEqual.Status)
			},
		},
		{
			name:           "Namespace gets phase=Active assertion",
			obj:            namespace("test-ns"),
			wantAssertions: 1,
			checkAssertion: func(t *testing.T, assertions []orbac.AssertionApplyConfiguration) {
				require.NotNil(t, assertions[0].FieldValue)
				assert.Equal(t, "status.phase", *assertions[0].FieldValue.FieldPath)
				assert.Equal(t, "Active", *assertions[0].FieldValue.Value)
			},
		},
		{
			name:           "ConfigMap gets no assertions",
			obj:            configMap("cm", "ns1"),
			wantAssertions: 0,
		},
		{
			name:           "ServiceAccount gets no assertions",
			obj:            serviceAccount("sa", "ns1"),
			wantAssertions: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := &applier.RegistryV1CODGenerator{
				ManifestProvider: &fakeManifestProvider{objs: []client.Object{tt.obj}},
				Scheme:           testScheme,
			}

			cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
			require.NoError(t, err)

			phases := cod.Spec.Template.Spec.Phases
			require.Len(t, phases, 1)
			require.Len(t, phases[0].Objects, 1)
			assert.Len(t, phases[0].Objects[0].Assertions, tt.wantAssertions)
			if tt.checkAssertion != nil && tt.wantAssertions > 0 {
				tt.checkAssertion(t, phases[0].Objects[0].Assertions)
			}
		})
	}
}

func TestRegistryV1CODGenerator_RevisionAnnotations(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{configMap("cm", "ns1")}},
		Scheme:           testScheme,
	}

	revisionAnnotations := map[string]string{
		labels.BundleNameKey:      "my-bundle",
		labels.PackageNameKey:     "my-package",
		labels.BundleVersionKey:   "1.0.0",
		labels.BundleReferenceKey: "quay.io/example/bundle:v1.0.0",
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, revisionAnnotations)
	require.NoError(t, err)

	templateAnnotations := cod.Spec.Template.Metadata.Annotations
	assert.Equal(t, "my-bundle", templateAnnotations[labels.BundleNameKey])
	assert.Equal(t, "my-package", templateAnnotations[labels.PackageNameKey])
	assert.Equal(t, "1.0.0", templateAnnotations[labels.BundleVersionKey])
	assert.Equal(t, "quay.io/example/bundle:v1.0.0", templateAnnotations[labels.BundleReferenceKey])
}

func TestRegistryV1CODGenerator_BundleAnnotations(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{configMap("cm", "ns1")}},
		Scheme:           testScheme,
	}

	t.Run("olm.properties propagated from bundle", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithBundleProperty("olm.bundle.property", "some-value").
			WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
			Build()

		cod, err := gen.GenerateCOD(context.Background(), bundleFS, testExtension("test-ext"), nil, nil)
		require.NoError(t, err)

		annotations := cod.Spec.Template.Metadata.Annotations
		require.Contains(t, annotations, "olm.properties")
		assert.JSONEq(t, `[{"type":"olm.bundle.property","value":"some-value"}]`, annotations["olm.properties"])
	})

	t.Run("olm.properties not set when bundle has no properties", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
			Build()

		cod, err := gen.GenerateCOD(context.Background(), bundleFS, testExtension("test-ext"), nil, nil)
		require.NoError(t, err)

		annotations := cod.Spec.Template.Metadata.Annotations
		assert.NotContains(t, annotations, "olm.properties")
	})

	t.Run("caller revisionAnnotations win over bundle olm.properties", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithBundleProperty("olm.bundle.property", "some-value").
			WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
			Build()

		revisionAnnotations := map[string]string{
			"olm.properties": "caller-wins",
		}
		cod, err := gen.GenerateCOD(context.Background(), bundleFS, testExtension("test-ext"), nil, revisionAnnotations)
		require.NoError(t, err)

		annotations := cod.Spec.Template.Metadata.Annotations
		assert.Equal(t, "caller-wins", annotations["olm.properties"])
	})
}

func TestRegistryV1CODGenerator_CODName(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{configMap("cm", "ns1")}},
		Scheme:           testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("my-extension"), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "my-extension", *cod.GetName())
}

func TestRegistryV1CODGenerator_OwnerReference(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{configMap("cm", "ns1")}},
		Scheme:           testScheme,
	}

	ext := testExtension("my-extension")
	ext.UID = "test-uid"
	cod, err := gen.GenerateCOD(context.Background(), nil, ext, nil, nil)
	require.NoError(t, err)

	require.Len(t, cod.OwnerReferences, 1)
	ref := cod.OwnerReferences[0]
	assert.Equal(t, ocv1.ClusterExtensionKind, *ref.Kind)
	assert.Equal(t, "my-extension", *ref.Name)
	assert.Equal(t, ext.UID, *ref.UID)
	require.NotNil(t, ref.Controller)
	assert.True(t, *ref.Controller)
	require.NotNil(t, ref.BlockOwnerDeletion)
	assert.True(t, *ref.BlockOwnerDeletion)
}

func TestRegistryV1CODGenerator_ProgressDeadlineMinutes(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{configMap("cm", "ns1")}},
		Scheme:           testScheme,
	}

	t.Run("set when non-zero", func(t *testing.T) {
		ext := testExtension("test-ext")
		ext.Spec.ProgressDeadlineMinutes = 10
		cod, err := gen.GenerateCOD(context.Background(), nil, ext, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, cod.Spec.ProgressDeadlineMinutes)
		assert.Equal(t, int32(10), *cod.Spec.ProgressDeadlineMinutes)
	})

	t.Run("not set when zero", func(t *testing.T) {
		ext := testExtension("test-ext")
		ext.Spec.ProgressDeadlineMinutes = 0
		cod, err := gen.GenerateCOD(context.Background(), nil, ext, nil, nil)
		require.NoError(t, err)
		assert.Nil(t, cod.Spec.ProgressDeadlineMinutes)
	})
}

func TestRegistryV1CODGenerator_CollisionProtection(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{configMap("cm", "ns1")}},
		Scheme:           testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, cod.Spec.Template.Spec.CollisionProtection)
	assert.Equal(t, orbv1alpha1.CollisionProtectionPrevent, *cod.Spec.Template.Spec.CollisionProtection)
}

func TestRegistryV1CODGenerator_ObjectsSerialized(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{
			objs: []client.Object{configMap("test-cm", "ns1")},
		},
		Scheme: testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	require.NoError(t, err)

	phases := cod.Spec.Template.Spec.Phases
	require.Len(t, phases, 1)
	require.Len(t, phases[0].Objects, 1)

	raw := phases[0].Objects[0].Object
	require.NotNil(t, raw)

	var obj unstructured.Unstructured
	require.NoError(t, json.Unmarshal(raw.Raw, &obj.Object))
	assert.Equal(t, "ConfigMap", obj.GetKind())
	assert.Equal(t, "test-cm", obj.GetName())
	assert.Equal(t, "ns1", obj.GetNamespace())
}

func TestRegistryV1CODGenerator_ObjectLabels(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{
			objs: []client.Object{configMap("cm", "ns1")},
		},
		Scheme: testScheme,
	}

	t.Run("set on COD metadata, template metadata, and individual objects", func(t *testing.T) {
		objLabels := map[string]string{
			labels.OwnerKindKey: "ClusterExtension",
			labels.OwnerNameKey: "test-ext",
		}
		cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), objLabels, nil)
		require.NoError(t, err)

		assert.Equal(t, "ClusterExtension", cod.Labels[labels.OwnerKindKey])
		assert.Equal(t, "test-ext", cod.Labels[labels.OwnerNameKey])

		templateLabels := cod.Spec.Template.Metadata.Labels
		assert.Equal(t, "ClusterExtension", templateLabels[labels.OwnerKindKey])
		assert.Equal(t, "test-ext", templateLabels[labels.OwnerNameKey])

		phases := cod.Spec.Template.Spec.Phases
		require.Len(t, phases, 1)
		require.Len(t, phases[0].Objects, 1)
		var obj unstructured.Unstructured
		require.NoError(t, json.Unmarshal(phases[0].Objects[0].Object.Raw, &obj.Object))
		assert.Equal(t, "ClusterExtension", obj.GetLabels()[labels.OwnerKindKey])
		assert.Equal(t, "test-ext", obj.GetLabels()[labels.OwnerNameKey])
	})

	t.Run("objectLabels override conflicting object labels", func(t *testing.T) {
		cm := configMap("cm", "ns1")
		cm.SetLabels(map[string]string{
			labels.OwnerKindKey: "should-be-overwritten",
			"app":               "preserved",
		})
		genWithLabeled := &applier.RegistryV1CODGenerator{
			ManifestProvider: &fakeManifestProvider{objs: []client.Object{cm}},
			Scheme:           testScheme,
		}
		objLabels := map[string]string{
			labels.OwnerKindKey: "ClusterExtension",
			labels.OwnerNameKey: "test-ext",
		}
		cod, err := genWithLabeled.GenerateCOD(context.Background(), nil, testExtension("test-ext"), objLabels, nil)
		require.NoError(t, err)

		var obj unstructured.Unstructured
		require.NoError(t, json.Unmarshal(cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Raw, &obj.Object))
		assert.Equal(t, "ClusterExtension", obj.GetLabels()[labels.OwnerKindKey])
		assert.Equal(t, "test-ext", obj.GetLabels()[labels.OwnerNameKey])
		assert.Equal(t, "preserved", obj.GetLabels()["app"])
	})

	t.Run("not set when nil", func(t *testing.T) {
		cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
		require.NoError(t, err)

		if cod.ObjectMetaApplyConfiguration != nil {
			assert.Empty(t, cod.Labels)
		}
		assert.Nil(t, cod.Spec.Template.Metadata.Labels)
	})
}

func TestRegistryV1CODGenerator_ManifestProviderError(t *testing.T) {
	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{
			err: assert.AnError,
		},
		Scheme: testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	assert.Nil(t, cod)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestRegistryV1CODGenerator_SanitizedMetadata(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cm",
			Namespace:       "ns1",
			ResourceVersion: "12345",
			UID:             "abc-123",
			Labels:          map[string]string{"app": "test"},
			Annotations:     map[string]string{"note": "value"},
		},
	}

	gen := &applier.RegistryV1CODGenerator{
		ManifestProvider: &fakeManifestProvider{objs: []client.Object{cm}},
		Scheme:           testScheme,
	}

	cod, err := gen.GenerateCOD(context.Background(), nil, testExtension("test-ext"), nil, nil)
	require.NoError(t, err)

	phases := cod.Spec.Template.Spec.Phases
	require.Len(t, phases, 1)
	require.Len(t, phases[0].Objects, 1)

	var obj unstructured.Unstructured
	require.NoError(t, json.Unmarshal(phases[0].Objects[0].Object.Raw, &obj.Object))

	// name, namespace, labels, annotations should be preserved
	assert.Equal(t, "test-cm", obj.GetName())
	assert.Equal(t, "ns1", obj.GetNamespace())
	assert.Equal(t, map[string]string{"app": "test"}, obj.GetLabels())

	// resourceVersion, uid should be stripped by sanitizedUnstructured
	assert.Empty(t, obj.GetResourceVersion())
	assert.Empty(t, string(obj.GetUID()))
}

// helpers

func testExtension(name string) *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func configMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func serviceAccount(name, namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func deployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func namespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func crd(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func extractObjectNames(t *testing.T, objs []orbac.PhaseObjectApplyConfiguration) []string {
	t.Helper()
	names := make([]string, 0, len(objs))
	for _, obj := range objs {
		var u unstructured.Unstructured
		require.NoError(t, json.Unmarshal(obj.Object.Raw, &u.Object))
		names = append(names, u.GetName())
	}
	return names
}
