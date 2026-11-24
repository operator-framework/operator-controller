package applier

import (
	"bytes"
	"encoding/json"
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
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
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
	input := struct {
		extension   *ocv1.ClusterExtension
		objects     []ocv1ac.ClusterObjectSetObjectApplyConfiguration
		annotations map[string]string
		protection  ocv1.CollisionProtection
	}{
		extension: &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "ext"},
			Spec:       ocv1.ClusterExtensionSpec{ProgressDeadlineMinutes: 15},
		},
		objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
			*ocv1ac.ClusterObjectSetObject().
				WithType(ocv1.ObjectTypeInlineManifest).
				WithInlineManifest(unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "test-cm"},
				}}),
		},
		annotations: map[string]string{"test-key": "test-value"},
		protection:  ocv1.CollisionProtectionPrevent,
	}
	want := struct {
		name           string
		annotations    map[string]string
		labels         map[string]string
		templateLabels map[string]string
		phaseCount     int
		protection     ocv1.CollisionProtection
		deadline       int32
	}{
		name:        "ext",
		annotations: map[string]string{"test-key": "test-value"},
		labels: map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: "ext",
			labels.ManagedByKey: labels.ManagedByOperatorController,
		},
		templateLabels: map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: "ext",
		},
		phaseCount: 1,
		protection: ocv1.CollisionProtectionPrevent,
		deadline:   15,
	}

	// When rendered content is placed in a deployment template.
	result := buildClusterObjectDeployment(input.objects, input.extension, input.annotations, input.protection)

	// Then deployment metadata and rollout policy match the requested contract.
	require.NotNil(t, result)
	assert.Equal(t, want.name, *result.Name)
	assert.Equal(t, want.labels, result.Labels)
	require.NotNil(t, result.Spec)
	require.NotNil(t, result.Spec.Template)
	require.NotNil(t, result.Spec.Template.Metadata)
	assert.Equal(t, want.annotations, result.Spec.Template.Metadata.Annotations)
	assert.Equal(t, want.templateLabels, result.Spec.Template.Metadata.Labels)
	require.NotNil(t, result.Spec.Template.Spec)
	templateSpec := result.Spec.Template.Spec
	assert.Len(t, templateSpec.Phases, want.phaseCount)
	assert.Equal(t, want.protection, *templateSpec.CollisionProtection)
	assert.Equal(t, want.deadline, *result.Spec.ProgressDeadlineMinutes)
	assert.NotEmpty(t, templateSpec.ProgressionProbes)
}

func TestGenerateClusterObjectDeployment_PacksAndRefs(t *testing.T) {
	gen, testBundle, ext, bundleAnnotations := bundleDeploymentFixture()
	input := struct {
		bundle       fs.FS
		extension    *ocv1.ClusterExtension
		objectLabels map[string]string
		annotations  map[string]string
	}{
		bundle:       testBundle,
		extension:    ext,
		objectLabels: map[string]string{"test-label": "test-value"},
		annotations:  bundleAnnotations,
	}
	want := struct {
		name         string
		annotations  map[string]string
		labels       map[string]string
		protection   ocv1.CollisionProtection
		secretPrefix string
		secretLabels map[string]string
		err          error
	}{
		name: "test-ext",
		annotations: map[string]string{
			labels.BundleNameKey:      "test-bundle",
			labels.PackageNameKey:     "test-package",
			labels.BundleVersionKey:   "1.0.0",
			labels.BundleReferenceKey: "test-catalog/test-package@sha256:abc123",
		},
		labels: map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: "test-ext",
			labels.ManagedByKey: labels.ManagedByOperatorController,
		},
		protection:   ocv1.CollisionProtectionPrevent,
		secretPrefix: "test-ext-",
		secretLabels: map[string]string{labels.OwnerNameKey: "test-ext"},
		err:          nil,
	}

	// When the normal bundle path generates a deployment and content Secrets.
	cod, secrets, err := gen.GenerateClusterObjectDeployment(t.Context(), input.bundle, input.extension, input.objectLabels, input.annotations)

	// Then bundle identity is retained and phases reference the packed Secrets.
	require.ErrorIs(t, err, want.err)
	require.NotNil(t, cod)
	require.NotEmpty(t, secrets)
	require.NotNil(t, cod.Spec.Template.Metadata)
	for key, value := range want.annotations {
		assert.Equal(t, value, cod.Spec.Template.Metadata.Annotations[key])
	}
	assert.Equal(t, want.name, *cod.Name)
	assert.Equal(t, want.labels, cod.Labels)
	require.NotNil(t, cod.Spec.Template.Spec.CollisionProtection)
	assert.Equal(t, want.protection, *cod.Spec.Template.Spec.CollisionProtection)
	require.NotNil(t, cod.Spec.Template.Spec.Phases)
	for _, phase := range cod.Spec.Template.Spec.Phases {
		for _, obj := range phase.Objects {
			assert.Nil(t, obj.InlineManifest, "phase %s should have no inline objects", *phase.Name)
			assert.NotNil(t, obj.SecretRef, "phase %s should have refs", *phase.Name)
		}
	}
	for _, secret := range secrets {
		assert.Contains(t, secret.Name, want.secretPrefix, "Secret name should be prefixed with extension name")
		for key, value := range want.secretLabels {
			assert.Equal(t, value, secret.Labels[key])
		}
	}
}

func TestGenerateClusterObjectDeployment_PacksExpectedContent(t *testing.T) {
	gen, testBundle, ext, revisionAnnotations := bundleDeploymentFixture()

	input := struct {
		bundle       fs.FS
		extension    *ocv1.ClusterExtension
		objectLabels map[string]string
		annotations  map[string]string
	}{
		bundle:       testBundle,
		extension:    ext,
		objectLabels: map[string]string{"test-label": "test-value"},
		annotations:  revisionAnnotations,
	}

	// Pin the normal path's packed bytes as well as its reference shape, so
	// sharing packing with Helm generation cannot silently change its content.
	expectedObjects := make([]*ocv1ac.ClusterObjectSetObjectApplyConfiguration, 0, 2)
	for _, resource := range []struct{ kind, name string }{{"ConfigMap", "test-cm"}, {"Secret", "test-secret"}} {
		expectedObjects = append(expectedObjects, ocv1ac.ClusterObjectSetObject().
			WithType(ocv1.ObjectTypeInlineManifest).
			WithInlineManifest(unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       resource.kind,
				"metadata": map[string]interface{}{
					"name":   resource.name,
					"labels": map[string]interface{}{"test-label": "test-value"},
					"annotations": map[string]interface{}{
						labels.BundleVersionKey: "1.0.0",
						labels.PackageNameKey:   "test-package",
					},
				},
			}}))
	}
	expectedPhases := []ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
		*ocv1ac.ClusterObjectSetPhase().WithName("configuration").WithObjects(expectedObjects...),
	}
	want := struct {
		content packedCODContentExpectation
		err     error
	}{
		content: packedCODContentExpectation{
			secretCount:         1,
			namespace:           "test-system",
			labels:              map[string]string{labels.OwnerNameKey: "test-ext"},
			immutable:           true,
			secretType:          labels.SecretTypeObjectData,
			ownerReferenceCount: 0,
			objectType:          ocv1.ObjectTypeSecretReference,
			phases:              expectedPhases,
		},
		err: nil,
	}

	// When the normal bundle path packs the rendered manifests.
	cod, secrets, err := gen.GenerateClusterObjectDeployment(t.Context(), input.bundle, input.extension, input.objectLabels, input.annotations)

	// Then references resolve to the independently expected serialized content.
	require.ErrorIs(t, err, want.err)
	assertPackedCODContent(t, cod, secrets, want.content)
}

func bundleDeploymentFixture() (*SimpleRevisionGenerator, fs.FS, *ocv1.ClusterExtension, map[string]string) {
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
	revisionAnnotations := map[string]string{
		labels.BundleNameKey:      "test-bundle",
		labels.PackageNameKey:     "test-package",
		labels.BundleVersionKey:   "1.0.0",
		labels.BundleReferenceKey: "test-catalog/test-package@sha256:abc123",
	}
	return gen, testBundle, ext, revisionAnnotations
}

type packedCODContentExpectation struct {
	secretCount         int
	namespace           string
	labels              map[string]string
	immutable           bool
	secretType          corev1.SecretType
	ownerReferenceCount int
	objectType          ocv1.ObjectType
	phases              []ocv1ac.ClusterObjectSetPhaseApplyConfiguration
}

// assertPackedCODContent resolves each reference by namespace, name and key and
// checks the exact serialized content against independently expected inline objects.
func assertPackedCODContent(
	t *testing.T,
	cod *ocv1ac.ClusterObjectDeploymentApplyConfiguration,
	secrets []corev1.Secret,
	expected packedCODContentExpectation,
) {
	t.Helper()
	require.Len(t, secrets, expected.secretCount)
	type packedSecret struct {
		data           map[string][]byte
		referencedKeys map[string]struct{}
	}
	packedSecrets := make(map[client.ObjectKey]packedSecret, len(secrets))
	for _, secret := range secrets {
		assert.Equal(t, expected.namespace, secret.Namespace)
		assert.Equal(t, expected.labels, secret.Labels)
		require.NotNil(t, secret.Immutable)
		assert.Equal(t, expected.immutable, *secret.Immutable)
		assert.Equal(t, expected.secretType, secret.Type)
		assert.Len(t, secret.OwnerReferences, expected.ownerReferenceCount)
		name := client.ObjectKeyFromObject(&secret)
		require.NotContains(t, packedSecrets, name, "duplicate packed Secret %s", name)
		require.NotEmpty(t, secret.Data, "packed Secret %s has no data", name)
		packedSecrets[name] = packedSecret{data: secret.Data, referencedKeys: make(map[string]struct{}, len(secret.Data))}
	}

	require.Len(t, cod.Spec.Template.Spec.Phases, len(expected.phases))
	for phaseIdx, phase := range cod.Spec.Template.Spec.Phases {
		assert.Equal(t, expected.phases[phaseIdx].Name, phase.Name)
		require.Len(t, phase.Objects, len(expected.phases[phaseIdx].Objects))
		for objIdx, obj := range phase.Objects {
			require.NotNil(t, obj.Type)
			assert.Equal(t, expected.objectType, *obj.Type)
			assert.Nil(t, obj.InlineManifest)
			ref := obj.SecretRef
			require.NotNil(t, ref)
			require.NotNil(t, ref.Name)
			require.NotNil(t, ref.Namespace)
			require.NotNil(t, ref.Key)
			name := client.ObjectKey{Namespace: *ref.Namespace, Name: *ref.Name}
			secret, ok := packedSecrets[name]
			require.True(t, ok, "referenced Secret %s was not packed", name)
			data, ok := secret.data[*ref.Key]
			require.True(t, ok, "packed Secret %s does not contain key %q", name, *ref.Key)
			raw, err := json.Marshal(expected.phases[phaseIdx].Objects[objIdx].InlineManifest.Object)
			require.NoError(t, err)
			assert.Equal(t, contentHash(raw), *ref.Key)
			assert.True(t, bytes.Equal(raw, data), "packed Secret %s has unexpected content at key %q", name, *ref.Key)
			secret.referencedKeys[*ref.Key] = struct{}{}
		}
	}
	for name, secret := range packedSecrets {
		assert.Len(t, secret.referencedKeys, len(secret.data), "packed Secret %s has unreferenced data", name)
	}
}
