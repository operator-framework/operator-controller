package applier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestGenerateRevisionFromHelmRelease_PreservesCOS(t *testing.T) {
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	input := struct {
		release      *release.Release
		extension    *ocv1.ClusterExtension
		objectLabels map[string]string
	}{
		release:      helmRelease,
		extension:    ext,
		objectLabels: objectLabels,
	}
	// Pin the legacy COS contract, including inline content and omission of
	// COD-specific defaults such as the extension's progress deadline.
	want := ocv1ac.ClusterObjectSet("test-ext-1").
		WithLabels(map[string]string{labels.OwnerKindKey: ocv1.ClusterExtensionKind, labels.OwnerNameKey: ext.Name}).
		WithAnnotations(expectedHelmBundleAnnotations()).
		WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithGroup(ext.Name).
			WithRevision(1).
			WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive).
			WithCollisionProtection(ocv1.CollisionProtectionNone).
			WithProgressionProbes(defaultProgressionProbes...).
			WithPhases(expectedHelmPhases(t)...))

	// When the legacy generator converts the stored release.
	gen := &SimpleRevisionGenerator{}
	revision, err := gen.GenerateRevisionFromHelmRelease(t.Context(), input.release, input.extension, input.objectLabels)

	// Then it preserves the inline COS contract and its original defaults.
	require.NoError(t, err)
	assert.Equal(t, want, revision)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_Metadata(t *testing.T) {
	// Given stored Helm bundle metadata and the labels used for managed resources.
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	input := struct {
		release         *release.Release
		extension       *ocv1.ClusterExtension
		objectLabels    map[string]string
		systemNamespace string
	}{
		release:         helmRelease,
		extension:       ext,
		objectLabels:    objectLabels,
		systemNamespace: "test-system",
	}
	want := struct {
		name                string
		labels              map[string]string
		ownerReferenceCount int
		annotationCount     int
		templateLabels      map[string]string
		templateAnnotations map[string]string
	}{
		name: ext.Name,
		labels: map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.Name,
			labels.ManagedByKey: labels.ManagedByOperatorController,
		},
		ownerReferenceCount: 0,
		annotationCount:     0,
		templateLabels: map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.Name,
		},
		templateAnnotations: expectedHelmBundleAnnotations(),
	}
	gen := &SimpleRevisionGenerator{SystemNamespace: input.systemNamespace}

	// When the in-memory generator creates the COD.
	cod, _, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), input.release, input.extension, input.objectLabels)

	// Then bundle metadata belongs on the template, leaving ownership to the migrator.
	require.NoError(t, err)
	assert.Equal(t, want.name, *cod.Name)
	assert.Equal(t, want.labels, cod.Labels)
	assert.Len(t, cod.OwnerReferences, want.ownerReferenceCount)
	assert.Len(t, cod.Annotations, want.annotationCount)
	assert.Equal(t, want.templateLabels, cod.Spec.Template.Metadata.Labels)
	assert.Equal(t, want.templateAnnotations, cod.Spec.Template.Metadata.Annotations)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_RolloutSettings(t *testing.T) {
	type input struct {
		systemNamespace string
		deadline        int32
	}
	type expected struct {
		deadline   *int32
		protection ocv1.CollisionProtection
		probes     []ocv1ac.ProgressionProbeApplyConfiguration
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "default deadline",
			input: input{
				systemNamespace: "test-system",
				deadline:        0,
			},
			want: expected{
				deadline:   nil,
				protection: ocv1.CollisionProtectionNone,
				probes:     ocv1ac.ClusterObjectDeploymentTemplateSpec().WithProgressionProbes(defaultProgressionProbes...).ProgressionProbes,
			},
		},
		{
			name: "explicit deadline",
			input: input{
				systemNamespace: "test-system",
				deadline:        15,
			},
			want: expected{
				deadline:   ptr.To(int32(15)),
				protection: ocv1.CollisionProtectionNone,
				probes:     ocv1ac.ClusterObjectDeploymentTemplateSpec().WithProgressionProbes(defaultProgressionProbes...).ProgressionProbes,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given a Helm release and the CE deadline declared by this case.
			helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
			ext.Spec.ProgressDeadlineMinutes = tc.input.deadline
			gen := &SimpleRevisionGenerator{SystemNamespace: tc.input.systemNamespace}

			// When the in-memory generator builds the migration deployment.
			cod, _, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)

			// Then rollout settings match the declared expectations.
			require.NoError(t, err)
			spec := cod.Spec.Template.Spec
			assert.Equal(t, tc.want.protection, *spec.CollisionProtection)
			assert.Equal(t, tc.want.probes, spec.ProgressionProbes)
			assert.Equal(t, tc.want.deadline, cod.Spec.ProgressDeadlineMinutes)
		})
	}
}

func TestHelmReleaseGeneration_BundleReleaseMetadata(t *testing.T) {
	type input struct {
		systemNamespace string
		releaseLabels   map[string]string
	}
	type expected struct {
		present bool
		value   string
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "absent bundle release",
			input: input{
				systemNamespace: "test-system",
				releaseLabels:   nil,
			},
			want: expected{
				present: false,
				value:   "",
			},
		},
		{
			name: "empty bundle release",
			input: input{
				systemNamespace: "test-system",
				releaseLabels:   map[string]string{labels.BundleReleaseKey: ""},
			},
			want: expected{
				present: true,
				value:   "",
			},
		},
		{
			name: "bundle release",
			input: input{
				systemNamespace: "test-system",
				releaseLabels:   map[string]string{labels.BundleReleaseKey: "2"},
			},
			want: expected{
				present: true,
				value:   "2",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given an absent, explicitly empty, or populated bundle release label.
			helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
			maps.Copy(helmRelease.Labels, tc.input.releaseLabels)
			gen := &SimpleRevisionGenerator{SystemNamespace: tc.input.systemNamespace}

			// When both generators convert the stored release.
			revision, err := gen.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
			require.NoError(t, err)
			cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
			require.NoError(t, err)
			objects, err := objectsFromSecrets(secrets)
			require.NoError(t, err)

			// Then template and content annotations preserve presence separately from value.
			assertRelease := func(annotations map[string]string) {
				t.Helper()
				value, present := annotations[labels.BundleReleaseKey]
				assert.Equal(t, tc.want.present, present)
				assert.Equal(t, tc.want.value, value)
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
	// Given manifests containing runtime metadata and fields that must be sanitized.
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	input := struct {
		release         *release.Release
		extension       *ocv1.ClusterExtension
		objectLabels    map[string]string
		systemNamespace string
	}{
		release:         helmRelease,
		extension:       ext,
		objectLabels:    objectLabels,
		systemNamespace: "test-system",
	}
	want := packedCODContentExpectation{
		secretCount:         1,
		namespace:           "test-system",
		labels:              map[string]string{labels.OwnerNameKey: "test-ext"},
		immutable:           true,
		secretType:          labels.SecretTypeObjectData,
		ownerReferenceCount: 0,
		objectType:          ocv1.ObjectTypeSecretReference,
		phases:              ocv1ac.ClusterObjectDeploymentTemplateSpec().WithPhases(expectedHelmPhases(t)...).Phases,
	}
	gen := &SimpleRevisionGenerator{SystemNamespace: input.systemNamespace}

	// When the generator packs the manifests into content Secrets.
	cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), input.release, input.extension, input.objectLabels)

	// Then the packed content matches the sanitized desired manifests and phase order.
	require.NoError(t, err)
	assertPackedCODContent(t, cod, secrets, want)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_PacksContentAcrossSecrets(t *testing.T) {
	// Each ConfigMap fits on its own, but their combined data exceeds the packing limit.
	payload := strings.Repeat("x", maxSecretDataSize/2+1)
	const manifest = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q,"namespace":"target"},"data":{"payload":%q}}`
	input := struct {
		release         *release.Release
		extension       *ocv1.ClusterExtension
		objectLabels    map[string]string
		systemNamespace string
	}{
		release: &release.Release{
			Name: "test-ext",
			Manifest: strings.Join([]string{
				fmt.Sprintf(manifest, "config-a", payload),
				fmt.Sprintf(manifest, "config-b", payload),
			}, "\n"),
		},
		extension:       &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext"}},
		objectLabels:    map[string]string{"managed": "true"},
		systemNamespace: "test-system",
	}

	phase := ocv1ac.ClusterObjectSetPhase().WithName("configuration")
	for _, name := range []string{"config-a", "config-b"} {
		configMap := testConfigMap(name, "target")
		configMap.SetLabels(map[string]string{"managed": "true"})
		configMap.Object["data"] = map[string]any{"payload": payload}
		phase.WithObjects(ocv1ac.ClusterObjectSetObject().WithInlineManifest(configMap))
	}
	want := packedCODContentExpectation{
		secretCount:         2,
		namespace:           "test-system",
		labels:              map[string]string{labels.OwnerNameKey: "test-ext"},
		immutable:           true,
		secretType:          labels.SecretTypeObjectData,
		ownerReferenceCount: 0,
		objectType:          ocv1.ObjectTypeSecretReference,
		phases:              []ocv1ac.ClusterObjectSetPhaseApplyConfiguration{*phase},
	}

	gen := &SimpleRevisionGenerator{SystemNamespace: input.systemNamespace}
	cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), input.release, input.extension, input.objectLabels)

	require.NoError(t, err)
	assertPackedCODContent(t, cod, secrets, want)
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_ManifestOrderDoesNotChangeOutput(t *testing.T) {
	// Given identical manifests in opposite orders.
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	reorderedRelease, reorderedExt, reorderedLabels := helmReleaseGenerationFixture(t)
	manifests := strings.Split(strings.TrimSuffix(reorderedRelease.Manifest, "\n"), "\n")
	slices.Reverse(manifests)
	reorderedRelease.Manifest = strings.Join(manifests, "\n") + "\n"

	type generationInput struct {
		release      *release.Release
		extension    *ocv1.ClusterExtension
		objectLabels map[string]string
	}
	input := struct {
		original        generationInput
		reordered       generationInput
		systemNamespace string
	}{
		original:        generationInput{release: helmRelease, extension: ext, objectLabels: objectLabels},
		reordered:       generationInput{release: reorderedRelease, extension: reorderedExt, objectLabels: reorderedLabels},
		systemNamespace: "test-system",
	}

	// The original ordering establishes the expected artifacts for this comparison.
	gen := &SimpleRevisionGenerator{SystemNamespace: input.systemNamespace}
	wantCOD, wantSecrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), input.original.release, input.original.extension, input.original.objectLabels)
	require.NoError(t, err)

	// When the same content is generated with the reversed manifest ordering.
	reorderedGen := &SimpleRevisionGenerator{SystemNamespace: input.systemNamespace}
	reorderedCOD, reorderedSecrets, err := reorderedGen.GenerateClusterObjectDeploymentFromHelmRelease(
		t.Context(), input.reordered.release, input.reordered.extension, input.reordered.objectLabels,
	)
	require.NoError(t, err)

	// Then manifest order changes neither the COD nor its content-addressed Secrets.
	assert.Equal(t, wantCOD, reorderedCOD)
	assert.Equal(t, wantSecrets, reorderedSecrets)
}

func TestHelmReleaseGeneration_DoesNotMutateInputs(t *testing.T) {
	helmRelease, ext, objectLabels := helmReleaseGenerationFixture(t)
	helmRelease.Labels[labels.BundleReleaseKey] = ""
	type input struct {
		legacy          bool
		release         *release.Release
		extension       *ocv1.ClusterExtension
		objectLabels    map[string]string
		systemNamespace string
	}
	type expected struct {
		release      release.Release
		extension    *ocv1.ClusterExtension
		objectLabels map[string]string
	}
	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "COS",
			input: input{
				legacy:          true,
				release:         helmRelease,
				extension:       ext,
				objectLabels:    objectLabels,
				systemNamespace: "test-system",
			},
			want: expected{
				release:      *helmRelease,
				extension:    ext.DeepCopy(),
				objectLabels: maps.Clone(objectLabels),
			},
		},
		{
			name: "COD",
			input: input{
				legacy:          false,
				release:         helmRelease,
				extension:       ext,
				objectLabels:    objectLabels,
				systemNamespace: "test-system",
			},
			want: expected{
				release:      *helmRelease,
				extension:    ext.DeepCopy(),
				objectLabels: maps.Clone(objectLabels),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given independent caller-owned inputs for this generator.
			helmRelease := *tc.input.release
			helmRelease.Labels = maps.Clone(tc.input.release.Labels)
			ext := tc.input.extension.DeepCopy()
			objectLabels := maps.Clone(tc.input.objectLabels)
			gen := &SimpleRevisionGenerator{SystemNamespace: tc.input.systemNamespace}

			// When generation reads the release, extension, and labels.
			var err error
			if tc.input.legacy {
				_, err = gen.GenerateRevisionFromHelmRelease(t.Context(), &helmRelease, ext, objectLabels)
			} else {
				_, _, err = gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), &helmRelease, ext, objectLabels)
			}

			// Then all caller-owned inputs retain their declared values.
			require.NoError(t, err)
			assert.Equal(t, tc.want.release, helmRelease)
			assert.Equal(t, tc.want.extension, ext)
			assert.Equal(t, tc.want.objectLabels, objectLabels)
		})
	}
}

func TestGenerateClusterObjectDeploymentFromHelmRelease_RejectsEmptyManifest(t *testing.T) {
	type expected struct {
		deployment *ocv1ac.ClusterObjectDeploymentApplyConfiguration
		secrets    []corev1.Secret
		errMessage string
	}
	testCases := []struct {
		name    string
		release release.Release
		want    expected
	}{
		{
			name:    "empty manifest",
			release: release.Release{Name: "test-release", Manifest: ""},
			want: expected{
				deployment: nil,
				secrets:    nil,
				errMessage: `Helm release "test-release" has no objects to migrate`,
			},
		},
		{
			name:    "whitespace-only manifest",
			release: release.Release{Name: "test-release", Manifest: " \n\t\r\n"},
			want: expected{
				deployment: nil,
				secrets:    nil,
				errMessage: `Helm release "test-release" has no objects to migrate`,
			},
		},
	}
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext"}}
	gen := &SimpleRevisionGenerator{SystemNamespace: "test-system"}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), &tc.release, ext, nil)

			require.EqualError(t, err, tc.want.errMessage)
			assert.Equal(t, tc.want.deployment, cod)
			assert.Equal(t, tc.want.secrets, secrets)
		})
	}
}

func TestHelmReleaseGeneration_MalformedManifest(t *testing.T) {
	// Given a Helm manifest with a malformed second document.
	input := struct {
		release         *release.Release
		extension       *ocv1.ClusterExtension
		objectLabels    map[string]string
		systemNamespace string
	}{
		release:         &release.Release{Manifest: "{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}\n{invalid"},
		extension:       &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext"}},
		objectLabels:    nil,
		systemNamespace: "test-system",
	}
	want := struct {
		revision   *ocv1ac.ClusterObjectSetApplyConfiguration
		deployment *ocv1ac.ClusterObjectDeploymentApplyConfiguration
		secrets    []corev1.Secret
	}{
		revision:   nil,
		deployment: nil,
		secrets:    nil,
	}

	gen := &SimpleRevisionGenerator{SystemNamespace: input.systemNamespace}
	revision, legacyErr := gen.GenerateRevisionFromHelmRelease(t.Context(), input.release, input.extension, input.objectLabels)
	cod, secrets, err := gen.GenerateClusterObjectDeploymentFromHelmRelease(t.Context(), input.release, input.extension, input.objectLabels)

	// Both generators report the same error without returning partial content.
	require.Error(t, legacyErr)
	require.EqualError(t, err, legacyErr.Error())
	assert.Equal(t, want.revision, revision)
	assert.Equal(t, want.deployment, cod)
	assert.Equal(t, want.secrets, secrets)
}

// helmReleaseGenerationFixture returns fresh in-memory inputs for a Helm bundle
// containing Namespace, ConfigMap, Secret, and Deployment manifests. The ConfigMap
// deliberately includes stale runtime metadata; no resources or Secrets are stored.
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

// expectedHelmBundleAnnotations lists the bundle identity retained on the COD template.
func expectedHelmBundleAnnotations() map[string]string {
	return map[string]string{
		labels.BundleNameKey:      "my-bundle",
		labels.BundleVersionKey:   "1.2.0",
		labels.PackageNameKey:     "my-package",
		labels.BundleReferenceKey: "registry.example/bundle@sha256:abc",
	}
}

// expectedHelmPhases describes sanitized content in its expected rollout phases.
// These are expected values only; packing and generation are not performed here.
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
