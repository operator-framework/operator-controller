package applier

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// fakeManifestProvider is a test double that returns a fixed list of objects
type fakeManifestProvider struct {
	objects []client.Object
	err     error
}

func (f *fakeManifestProvider) Get(_ fs.FS, _ *ocv1.ClusterExtension) ([]client.Object, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.objects, nil
}

func TestBuildClusterObjectDeployment(t *testing.T) {
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ext",
		},
		Spec: ocv1.ClusterExtensionSpec{
			ProgressDeadlineMinutes: 15,
		},
	}

	// Create a valid unstructured object with proper GVK
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test-cm",
			},
		},
	}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))

	objects := []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
		*ocv1ac.ClusterObjectSetObject().WithObject(*obj),
	}

	annotations := map[string]string{
		"test-key": "test-value",
	}

	collisionProtection := ocv1.CollisionProtectionPrevent

	result := buildClusterObjectDeployment(objects, ext, annotations, collisionProtection)

	require.NotNil(t, result)
	assert.Equal(t, "ext", *result.Name)
	assert.Equal(t, "test-value", result.Annotations["test-key"])
	assert.Equal(t, ocv1.ClusterExtensionKind, result.Labels[labels.OwnerKindKey])
	assert.Equal(t, "ext", result.Labels[labels.OwnerNameKey])

	require.NotNil(t, result.Spec)
	require.NotNil(t, result.Spec.Template)
	require.NotNil(t, result.Spec.Template.Spec)

	templateSpec := result.Spec.Template.Spec
	assert.Len(t, templateSpec.Phases, 1)
	assert.Equal(t, ocv1.CollisionProtectionPrevent, *templateSpec.CollisionProtection)
	assert.Equal(t, int32(15), *templateSpec.ProgressDeadlineMinutes)
	assert.NotEmpty(t, templateSpec.ProgressionProbes)
}

func TestGenerateClusterObjectDeployment_PacksAndRefs(t *testing.T) {
	// Create a test bundle with proper metadata
	testBundle := bundlefs.Builder().
		WithPackageName("test-package").
		WithCSV(bundlecsv.Builder().
			WithName("test-csv").
			WithAnnotations(map[string]string{
				labels.BundleNameKey:    "test-bundle",
				labels.PackageNameKey:   "test-package",
				labels.BundleVersionKey: "1.0.0",
			}).
			Build()).
		Build()

	fakeProvider := &fakeManifestProvider{
		objects: []client.Object{
			&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cm",
				},
			},
			&corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Secret",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			},
		},
	}

	gen := &SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: fakeProvider,
		SystemNamespace:  "test-system",
	}

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
		},
	}

	bundleAnnotations := map[string]string{
		labels.BundleNameKey:      "test-bundle",
		labels.PackageNameKey:     "test-package",
		labels.BundleVersionKey:   "1.0.0",
		labels.BundleReferenceKey: "test-catalog/test-package@sha256:abc123",
	}

	cod, secrets, err := gen.GenerateClusterObjectDeployment(
		context.Background(),
		testBundle,
		ext,
		map[string]string{"test-label": "test-value"},
		bundleAnnotations,
	)

	require.NoError(t, err)
	require.NotNil(t, cod)
	require.NotEmpty(t, secrets)

	t.Log("by checking bundle annotations are on COD metadata")
	assert.Equal(t, "test-bundle", cod.Annotations[labels.BundleNameKey])
	assert.Equal(t, "test-package", cod.Annotations[labels.PackageNameKey])
	assert.Equal(t, "1.0.0", cod.Annotations[labels.BundleVersionKey])
	assert.Equal(t, "test-catalog/test-package@sha256:abc123", cod.Annotations[labels.BundleReferenceKey])

	t.Log("by checking COD name matches extension")
	assert.Equal(t, "test-ext", *cod.Name)

	t.Log("by checking COD has owner labels")
	assert.Equal(t, ocv1.ClusterExtensionKind, cod.Labels[labels.OwnerKindKey])
	assert.Equal(t, "test-ext", cod.Labels[labels.OwnerNameKey])

	t.Log("by checking template spec has collision protection set to Prevent")
	require.NotNil(t, cod.Spec.Template.Spec.CollisionProtection)
	assert.Equal(t, ocv1.CollisionProtectionPrevent, *cod.Spec.Template.Spec.CollisionProtection)

	t.Log("by checking all phases contain only refs, no inline objects")
	require.NotNil(t, cod.Spec.Template.Spec.Phases)
	for _, phase := range cod.Spec.Template.Spec.Phases {
		for _, obj := range phase.Objects {
			assert.Nil(t, obj.Object, "phase %s should have no inline objects", *phase.Name)
			assert.NotNil(t, obj.Ref, "phase %s should have refs", *phase.Name)
		}
	}

	t.Log("by checking Secrets are named with extension prefix")
	for _, secret := range secrets {
		assert.Contains(t, secret.Name, "test-ext-", "Secret name should be prefixed with extension name")
		assert.Equal(t, "test-ext", secret.Labels[labels.OwnerNameKey])
	}
}
