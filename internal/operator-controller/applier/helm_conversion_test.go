package applier

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestGenerateRevisionFromHelmRelease_PreservesCOS(t *testing.T) {
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	gen := &SimpleRevisionGenerator{}

	revision, err := gen.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)

	// Pin the legacy COS contract, including inline content and omission of
	// COD-specific defaults such as the extension's progress deadline.
	expected := ocv1ac.ClusterObjectSet("test-ext-1").
		WithLabels(map[string]string{labels.OwnerKindKey: ocv1.ClusterExtensionKind, labels.OwnerNameKey: ext.Name}).
		WithAnnotations(expectedHelmBundleAnnotations()).
		WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithGroup(ext.Name).
			WithRevision(1).
			WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive).
			WithCollisionProtection(ocv1.CollisionProtectionNone).
			WithProgressionProbes(defaultProgressionProbes...).
			WithPhases(expectedHelmPhases(t)...))
	assert.Equal(t, expected, revision)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_Metadata(t *testing.T) {
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

	cod, _, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)
	assert.Equal(t, ext.Name, *cod.Name)
	assert.Equal(t, map[string]string{
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: ext.Name,
		labels.ManagedByKey: labels.ManagedByOperatorController,
	}, cod.Labels)
	assert.Empty(t, cod.OwnerReferences)
	assert.Empty(t, cod.Annotations)
	assert.Equal(t, map[string]string{
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: ext.Name,
	}, cod.Spec.Template.Metadata.Labels)
	assert.Equal(t, expectedHelmBundleAnnotations(), cod.Spec.Template.Metadata.Annotations)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_RolloutSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deadline int32
	}{
		{name: "default deadline"},
		{name: "explicit deadline", deadline: 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
			ext.Spec.ProgressDeadlineMinutes = tc.deadline
			gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

			cod, _, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
			require.NoError(t, err)
			spec := cod.Spec.Template.Spec
			assert.Equal(t, ocv1.CollisionProtectionNone, *spec.CollisionProtection)
			expectedProbes := ocv1ac.ClusterObjectDeploymentTemplateSpec().WithProgressionProbes(defaultProgressionProbes...)
			assert.Equal(t, expectedProbes.ProgressionProbes, spec.ProgressionProbes)
			if tc.deadline == 0 {
				assert.Nil(t, cod.Spec.ProgressDeadlineMinutes)
			} else {
				require.NotNil(t, cod.Spec.ProgressDeadlineMinutes)
				assert.Equal(t, tc.deadline, *cod.Spec.ProgressDeadlineMinutes)
			}
		})
	}
}

func TestHelmReleaseGeneration_BundleReleaseMetadata(t *testing.T) {
	for _, tc := range []struct {
		name          string
		releaseLabels map[string]string
		present       bool
		value         string
	}{
		{name: "absent bundle release"},
		{name: "empty bundle release", releaseLabels: map[string]string{labels.BundleReleaseKey: ""}, present: true},
		{name: "bundle release", releaseLabels: map[string]string{labels.BundleReleaseKey: "2"}, present: true, value: "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
			maps.Copy(helmRelease.Labels, tc.releaseLabels)
			gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

			revision, err := gen.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
			require.NoError(t, err)
			cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
			require.NoError(t, err)
			objects, err := objectsFromSecrets(secrets)
			require.NoError(t, err)

			assertRelease := func(annotations map[string]string) {
				t.Helper()
				value, present := annotations[labels.BundleReleaseKey]
				assert.Equal(t, tc.present, present)
				assert.Equal(t, tc.value, value)
			}
			assertRelease(revision.Annotations)
			assertRelease(cod.Spec.Template.Metadata.Annotations)
			for _, phase := range revision.Spec.Phases {
				for _, obj := range phase.Objects {
					assertRelease(obj.InlineManifest.GetAnnotations())
				}
			}
			for _, obj := range objects {
				assertRelease(obj.GetAnnotations())
			}
		})
	}
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_PacksSanitizedContent(t *testing.T) {
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

	cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)
	expected := ocv1ac.ClusterObjectDeploymentTemplateSpec().WithPhases(expectedHelmPhases(t)...)
	assertPackedCODContent(t, cod, secrets, expected.Phases)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_ManifestOrderDoesNotChangeOutput(t *testing.T) {
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	reorderedRelease, reorderedExt, reorderedLabels := helmReleaseGenerationFixture(t)
	manifests := strings.Split(strings.TrimSuffix(reorderedRelease.Manifest, "\n"), "\n")
	slices.Reverse(manifests)
	reorderedRelease.Manifest = strings.Join(manifests, "\n") + "\n"

	gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}
	reorderedGen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

	cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)
	reorderedCOD, reorderedSecrets, err := reorderedGen.GenerateClusterObjectDeploymentFromHelmRelease(
		t.Context(), reorderedRelease, reorderedExt, reorderedLabels,
	)
	require.NoError(t, err)
	assert.Equal(t, cod, reorderedCOD)
	assert.Equal(t, secrets, reorderedSecrets)
}

func TestHelmReleaseGeneration_DoesNotMutateInputs(t *testing.T) {
	for _, generator := range []string{"COS", "COD"} {
		t.Run(generator, func(t *testing.T) {
			helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
			helmRelease.Labels[labels.BundleReleaseKey] = ""
			originalRelease := *helmRelease
			originalRelease.Labels = maps.Clone(helmRelease.Labels)
			originalExtension := ext.DeepCopy()
			originalObjectLabels := maps.Clone(objectLabels)
			gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

			if generator == "COS" {
				_, err := gen.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
				require.NoError(t, err)
			} else {
				_, _, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
				require.NoError(t, err)
			}

			assert.Equal(t, originalRelease, *helmRelease)
			assert.Equal(t, originalExtension, ext)
			assert.Equal(t, originalObjectLabels, objectLabels)
		})
	}
}

func TestHelmReleaseGenerationMalformedManifest(t *testing.T) {
	gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext"}}
	helmRelease := &release.Release{Manifest: "{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}\n{invalid"}

	revision, legacyErr := gen.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, nil)
	require.Error(t, legacyErr)
	assert.Nil(t, revision)
	cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, nil)
	require.EqualError(t, err, legacyErr.Error())
	assert.Nil(t, cod)
	assert.Nil(t, secrets)
}

func helmReleaseGenerationFixture(t *testing.T) (*release.Release, *ocv1.ClusterExtension, map[string]string) {
	t.Helper()
	// Store one JSON object per line, in a different order from the expected
	// phases. Keep runtime metadata on the ConfigMap to exercise sanitization.
	manifests := []string{
		`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"operator","namespace":"target"},"spec":{"replicas":3}}`,
		`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"credentials","namespace":"target"},"data":{"token":"dG9rZW4="}}`,
		`{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"name": "config",
				"namespace": "target",
				"labels": {"keep-label": "keep", "managed": "old"},
				"annotations": {
					"keep-annotation": "keep",
					"kubectl.kubernetes.io/last-applied-configuration": "large",
					"olm.operatorframework.io/bundle-version": "old",
					"olm.operatorframework.io/package-name": "old"
				},
				"uid": "old-uid",
				"resourceVersion": "42",
				"generation": 5,
				"creationTimestamp": "2026-01-01T00:00:00Z",
				"deletionTimestamp": "2026-01-02T00:00:00Z",
				"deletionGracePeriodSeconds": 30,
				"ownerReferences": [{"kind": "OldOwner", "name": "old"}],
				"finalizers": ["old"],
				"managedFields": [{"manager": "old"}]
			},
			"data": {"setting": "value"},
			"status": {"ready": true}
		}`,
		`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"target"}}`,
	}
	for i, manifest := range manifests {
		var compact bytes.Buffer
		require.NoError(t, json.Compact(&compact, []byte(manifest)))
		manifests[i] = compact.String()
	}
	helmRelease := &release.Release{
		Name:     "helm-release",
		Manifest: strings.Join(manifests, "\n") + "\n",
		Labels: map[string]string{
			labels.BundleNameKey:      "my-bundle",
			labels.BundleVersionKey:   "1.2.0",
			labels.PackageNameKey:     "my-package",
			labels.BundleReferenceKey: "registry.example/bundle@sha256:abc",
			"unrelated":               "not-bundle-metadata",
		},
	}
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext"},
		Spec:       ocv1.ClusterExtensionSpec{ProgressDeadlineMinutes: 15},
	}
	return helmRelease, ext, map[string]string{"managed": "new"}
}

func expectedHelmBundleAnnotations() map[string]string {
	return map[string]string{
		labels.BundleNameKey:      "my-bundle",
		labels.BundleVersionKey:   "1.2.0",
		labels.PackageNameKey:     "my-package",
		labels.BundleReferenceKey: "registry.example/bundle@sha256:abc",
	}
}

func expectedHelmPhases(t *testing.T) []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration {
	t.Helper()
	inline := func(raw string) *ocv1ac.ClusterObjectSetObjectApplyConfiguration {
		t.Helper()
		obj := unstructured.Unstructured{}
		require.NoError(t, obj.UnmarshalJSON([]byte(raw)))
		obj.SetLabels(map[string]string{"managed": "new"})
		obj.SetAnnotations(map[string]string{
			labels.BundleVersionKey: "1.2.0",
			labels.PackageNameKey:   "my-package",
		})
		if obj.GetKind() == "ConfigMap" {
			obj.SetLabels(map[string]string{"managed": "new", "keep-label": "keep"})
			obj.SetAnnotations(map[string]string{
				labels.BundleVersionKey: "1.2.0",
				labels.PackageNameKey:   "my-package",
				"keep-annotation":       "keep",
			})
		}
		return ocv1ac.ClusterObjectSetObject().WithType(ocv1.ObjectTypeInlineManifest).WithInlineManifest(obj)
	}
	return []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
		ocv1ac.ClusterObjectSetPhase().WithName("namespaces").WithObjects(
			inline(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"target"}}`)),
		ocv1ac.ClusterObjectSetPhase().WithName("configuration").WithObjects(
			inline(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"config","namespace":"target"},"data":{"setting":"value"}}`),
			inline(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"credentials","namespace":"target"},"data":{"token":"dG9rZW4="}}`)),
		ocv1ac.ClusterObjectSetPhase().WithName("deploy").WithObjects(
			inline(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"operator","namespace":"target"},"spec":{"replicas":3}}`)),
	}
}
