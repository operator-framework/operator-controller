package v2

import (
	"context"
	"fmt"
	"strings"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/go-openapi/jsonreference"
	"github.com/go-openapi/spec"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func TestRegistryV1ToHelmChart(t *testing.T) {
	type testCase struct {
		name             string
		cfg              genBundleConfig
		installNamespace string
		values           chartutil.Values
		assert           func(*testing.T, string, *chart.Chart, error, string, error)
	}

	type configOverrideFields struct {
		selector     *metav1.LabelSelector
		nodeSelector map[string]string
		tolerations  []corev1.Toleration
		volumes      []corev1.Volume
		affinity     *corev1.Affinity
		resources    *corev1.ResourceRequirements
		env          []corev1.EnvVar
		envFrom      []corev1.EnvFromSource
		volumeMounts []corev1.VolumeMount
		annotations  map[string]string
	}

	allNamespacesAssertions := func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
		// The deployment's pod metadata should have CSV annotations + olm.targetNamespaces=""
		expectedAnnotations := mergeMaps(csvAnnotations(), map[string]string{"olm.targetNamespaces": ""})
		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "Deployment", nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, expectedAnnotations, `{.spec.template.metadata.annotations}`)
			},
		)

		// operators watching all namespaces should have their permissions promoted to a ClusterRole and ClusterRoleBinding
		clusterRoleName := ""
		assertPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				if obj.GetObjectKind().GroupVersionKind().Kind != "ClusterRole" {
					return false, nil
				}
				var cr rbacv1.ClusterRole
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &cr); err != nil {
					return false, nil
				}
				if !equality.Semantic.DeepEqualWithNilDifferentFromEmpty(csvPermissionRules(), cr.Rules) {
					return false, nil
				}
				clusterRoleName = cr.Name
				return true, nil
			},
		)
		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				if obj.GetObjectKind().GroupVersionKind().Kind != "ClusterRoleBinding" {
					return false, nil
				}
				var rb rbacv1.RoleBinding
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &rb); err != nil {
					return false, err
				}
				return rb.RoleRef.Name == clusterRoleName, nil
			},
			func(t *testing.T, obj client.Object) {
				subjects := []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      csvPodServiceAccountName(),
						Namespace: installNamespace,
					},
				}
				assertFieldEqual(t, obj, subjects, `{.subjects}`)
			},
		)
		assertNotPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				// There should be no Roles in the manifest
				return obj.GetObjectKind().GroupVersionKind().Kind == "Role", nil
			},
		)
	}

	multiNamespaceAssertions := func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error, watchNamespaces ...string) {
		// The deployment's pod spec should have CSV annotations + olm.targetNamespaces=<watchNamespaces>
		expectedAnnotations := mergeMaps(csvAnnotations(), map[string]string{"olm.targetNamespaces": strings.Join(watchNamespaces, ",")})
		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "Deployment", nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, expectedAnnotations, `{.spec.template.metadata.annotations}`)
			},
		)

		for _, watchNamespace := range watchNamespaces {
			roleName := ""
			assertPresent(t, manifest,
				func(obj client.Object) (bool, error) {
					if obj.GetObjectKind().GroupVersionKind().Kind != "Role" || obj.GetNamespace() != watchNamespace {
						return false, nil
					}
					var r rbacv1.Role
					if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &r); err != nil {
						return false, nil
					}
					if !equality.Semantic.DeepEqualWithNilDifferentFromEmpty(csvPermissionRules(), r.Rules) {
						return false, nil
					}
					roleName = r.Name
					return true, nil
				},
			)
			assertInManifest(t, manifest,
				func(obj client.Object) (bool, error) {
					if obj.GetObjectKind().GroupVersionKind().Kind != "RoleBinding" && obj.GetNamespace() == watchNamespace {
						return false, nil
					}
					var rb rbacv1.RoleBinding
					if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &rb); err != nil {
						return false, err
					}
					return rb.RoleRef.Name == roleName, nil
				},
				func(t *testing.T, obj client.Object) {
					subjects := []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Name:      csvPodServiceAccountName(),
							Namespace: installNamespace,
						},
					}
					assertFieldEqual(t, obj, subjects, `{.subjects}`)
				},
			)
		}
	}

	singleNamespaceAssertions := func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error, watchNamespace string) {
		multiNamespaceAssertions(t, installNamespace, chrt, convertError, manifest, templateError, watchNamespace)
	}

	ownNamespaceAssertions := func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
		multiNamespaceAssertions(t, installNamespace, chrt, convertError, manifest, templateError, installNamespace)
	}

	standardAssertions := func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error, overrides configOverrideFields) {
		assert.NoError(t, convertError)
		assert.NotNil(t, chrt)
		assert.NoError(t, templateError)
		assert.NotEmpty(t, manifest)

		assert.Equal(t, "v2", chrt.Metadata.APIVersion)
		assert.Equal(t, "example-operator", chrt.Metadata.Name)
		assert.Equal(t, csvSpecVersion().String(), chrt.Metadata.Version)
		assert.Equal(t, csvSpecDescription(), chrt.Metadata.Description)
		assert.Equal(t, csvSpecKeywords(), chrt.Metadata.Keywords)
		assert.Equal(t, convertMaintainers(csvSpecMaintainers()), chrt.Metadata.Maintainers)
		assert.Equal(t, csvAnnotations(), chrt.Metadata.Annotations)
		assert.Equal(t, convertSpecLinks(csvSpecLinks()), chrt.Metadata.Sources)
		assert.Equal(t, csvSpecProvider().URL, chrt.Metadata.Home)
		assert.Equal(t, ">= "+csvSpecMinKubeVersion(), chrt.Metadata.KubeVersion)

		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "Deployment", nil
			},
			func(t *testing.T, obj client.Object) {
				if overrides.selector != nil {
					assertFieldEqual(t, obj, overrides.selector, `{.spec.selector}`)
				} else {
					assertFieldEqual(t, obj, csvDeploymentSelector(), `{.spec.selector}`)
				}

				if overrides.nodeSelector != nil {
					assertFieldEqual(t, obj, overrides.nodeSelector, `{.spec.template.spec.nodeSelector}`)
				} else {
					assertFieldEqual(t, obj, csvPodNodeSelector(), `{.spec.template.spec.nodeSelector}`)
				}

				if overrides.tolerations != nil {
					assertFieldEqual(t, obj, overrides.tolerations, `{.spec.template.spec.tolerations}`)
				} else {
					assertFieldEqual(t, obj, csvPodTolerations(), `{.spec.template.spec.tolerations}`)
				}

				if overrides.affinity != nil {
					assertFieldEqual(t, obj, overrides.affinity, `{.spec.template.spec.affinity}`)
				} else {
					assertFieldEqual(t, obj, csvPodAffinity(), `{.spec.template.spec.affinity}`)
				}

				if overrides.volumes != nil {
					assertFieldEqual(t, obj, overrides.volumes, `{.spec.template.spec.volumes}`)
				} else {
					assertFieldEqual(t, obj, csvPodVolumes(), `{.spec.template.spec.volumes}`)
				}

				if overrides.resources != nil {
					assertFieldEqual(t, obj, overrides.resources, `{.spec.template.spec.containers[0].resources}`)
				} else {
					assertFieldEqual(t, obj, csvContainerResources(), `{.spec.template.spec.containers[0].resources}`)
				}

				if overrides.env != nil {
					assertFieldEqual(t, obj, overrides.env, `{.spec.template.spec.containers[0].env}`)
				} else {
					assertFieldEqual(t, obj, csvContainerEnv(), `{.spec.template.spec.containers[0].env}`)
				}

				if overrides.envFrom != nil {
					assertFieldEqual(t, obj, overrides.envFrom, `{.spec.template.spec.containers[0].envFrom}`)
				} else {
					assertFieldEqual(t, obj, csvContainerEnvFrom(), `{.spec.template.spec.containers[0].envFrom}`)
				}

				if overrides.volumeMounts != nil {
					assertFieldEqual(t, obj, overrides.volumeMounts, `{.spec.template.spec.containers[0].volumeMounts}`)
				} else {
					assertFieldEqual(t, obj, csvContainerVolumeMounts(), `{.spec.template.spec.containers[0].volumeMounts}`)
				}

				// pod template spec should have the original values from the CSV that are not overridable
				assertFieldEqual(t, obj, csvPodLabels(), `{.spec.template.metadata.labels}`)
				assertFieldEqual(t, obj, csvContainerName(), `{.spec.template.spec.containers[0].name}`)
				assertFieldEqual(t, obj, csvContainerImage(), `{.spec.template.spec.containers[0].image}`)
			},
		)

		assertPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				if obj.GetObjectKind().GroupVersionKind().Kind != "ClusterRole" {
					return false, nil
				}
				var cr rbacv1.ClusterRole
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &cr); err != nil {
					return false, err
				}
				return equality.Semantic.DeepEqualWithNilDifferentFromEmpty(csvClusterPermissionRules(), cr.Rules), nil
			},
		)

		assertPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "ServiceAccount" &&
					obj.GetName() == csvPodServiceAccountName() &&
					(obj.GetNamespace() == installNamespace || obj.GetNamespace() == ""), nil
			},
		)

		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "ConfigMap" && obj.GetName() == "example-config", nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, configMapData(), `{.data}`)
			},
		)

		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "CustomResourceDefinition" && obj.GetName() == crdName(), nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, crdSpec().Group, `{.spec.group}`)
				assertFieldEqual(t, obj, crdSpec().Names, `{.spec.names}`)
				assertFieldEqual(t, obj, crdSpec().Scope, `{.spec.scope}`)
				assertFieldEqual(t, obj, crdSpec().Versions, `{.spec.versions}`)
			},
		)
	}

	tests := []testCase{
		{
			name: "No overrides",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			},
			installNamespace: "test-namespace",
			values:           nil,
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "Merge overrides",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			},
			installNamespace: "test-namespace",
			values: chartutil.Values{
				"selector":     &metav1.LabelSelector{MatchLabels: map[string]string{"overrideKey": "overrideValue"}},
				"nodeSelector": map[string]string{"nodeSelectorOverrideKey": "nodeSelectorOverrideValue"},
				"tolerations":  []corev1.Toleration{{Key: "tolerationOverrideKey", Value: "tolerationOverrideValue"}},
				"affinity": &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"affinityOverrideKey": "affinityOverrideValue"}}},
					}},
				},
				"volumes": append(
					csvPodVolumesShared(corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "valuesSecretVolume"}}),
					corev1.Volume{Name: "volumeOverrideName", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				),
				"resources": &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
				"env": append(
					csvContainerEnvShared("valuesValue"),
					corev1.EnvVar{Name: "envOverrideName", Value: "envOverrideValue"},
				),
				"envFrom": []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "overrideEnvFrom"},
				}}},
				"volumeMounts": append(
					csvContainerVolumeMountsShared("valuesVolumeMountPath"),
					corev1.VolumeMount{Name: "volumeMountOverrideName", MountPath: "volumeMountOverridePath"},
				),

				// TODO: what about annotations?
				//   Looks like they are supposed to propagate to _at least_ the following places:
				//   - Deployment.metadata.annotations
				//   - Deployment.spec.template.metadata.annotations
			},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{
					selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"overrideKey": "overrideValue"}},
					nodeSelector: map[string]string{"nodeSelectorOverrideKey": "nodeSelectorOverrideValue"},
					tolerations:  append(csvPodTolerations(), corev1.Toleration{Key: "tolerationOverrideKey", Value: "tolerationOverrideValue"}),
					affinity: &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"affinityOverrideKey": "affinityOverrideValue"}}},
						}},
					},
					volumes: append(
						csvPodVolumesShared(corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "valuesSecretVolume"}}),
						append([]corev1.Volume{{Name: "volumeOverrideName", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
							csvPodVolumesUniq()...)...),
					resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
					env: append(
						csvContainerEnvShared("valuesValue"),
						append([]corev1.EnvVar{{Name: "envOverrideName", Value: "envOverrideValue"}},
							csvContainerEnvUniq()...)...,
					),
					envFrom: append(csvContainerEnvFrom(), corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "overrideEnvFrom"},
					}}),
					volumeMounts: append(
						csvContainerVolumeMountsShared("valuesVolumeMountPath"),
						append([]corev1.VolumeMount{{Name: "volumeMountOverrideName", MountPath: "volumeMountOverridePath"}},
							csvContainerVolumeMountsUniq()...)...,
					),
				})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "Only overrides",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				excludeMergeables:     true,
			},
			installNamespace: "test-namespace",
			values: chartutil.Values{
				"selector":     &metav1.LabelSelector{MatchLabels: map[string]string{"overrideKey": "overrideValue"}},
				"nodeSelector": map[string]string{"nodeSelectorOverrideKey": "nodeSelectorOverrideValue"},
				"tolerations":  []corev1.Toleration{{Key: "tolerationOverrideKey", Value: "tolerationOverrideValue"}},
				"affinity": &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"affinityOverrideKey": "affinityOverrideValue"}}},
					}},
				},
				"volumes": append(
					csvPodVolumesShared(corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "valuesSecretVolume"}}),
					corev1.Volume{Name: "volumeOverrideName", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				),
				"resources": &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
				"env": append(
					csvContainerEnvShared("valuesValue"),
					corev1.EnvVar{Name: "envOverrideName", Value: "envOverrideValue"},
				),
				"envFrom": []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "overrideEnvFrom"},
				}}},
				"volumeMounts": append(
					csvContainerVolumeMountsShared("valuesVolumeMountPath"),
					corev1.VolumeMount{Name: "volumeMountOverrideName", MountPath: "volumeMountOverridePath"},
				),

				// TODO: what about annotations?
			},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{
					selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"overrideKey": "overrideValue"}},
					nodeSelector: map[string]string{"nodeSelectorOverrideKey": "nodeSelectorOverrideValue"},
					tolerations:  []corev1.Toleration{{Key: "tolerationOverrideKey", Value: "tolerationOverrideValue"}},
					affinity: &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"affinityOverrideKey": "affinityOverrideValue"}}},
						}},
					},
					volumes: append(
						csvPodVolumesShared(corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "valuesSecretVolume"}}),
						corev1.Volume{Name: "volumeOverrideName", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					),
					resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
					env: append(
						csvContainerEnvShared("valuesValue"),
						corev1.EnvVar{Name: "envOverrideName", Value: "envOverrideValue"},
					),
					envFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "overrideEnvFrom"},
					}}},
					volumeMounts: append(
						csvContainerVolumeMountsShared("valuesVolumeMountPath"),
						corev1.VolumeMount{Name: "volumeMountOverrideName", MountPath: "volumeMountOverridePath"},
					),
				})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "OwnNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			},
			installNamespace: "test-namespace",
			values:           nil,
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				ownNamespaceAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, don't specify installMode",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, specify AllNamespaces installMode",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"installMode": "AllNamespaces"},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, specify OwnNamespace installMode",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"installMode": "OwnNamespace"},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				ownNamespaceAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, invalid installMode",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"installMode": "foo"},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.ErrorContains(t, templateError, `installMode must be one of the following: "AllNamespaces", "OwnNamespace"`)
				assert.Empty(t, manifest)
			},
		},
		{
			name: "SingleNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"watchNamespace": "watch-namespace"},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				singleNamespaceAssertions(t, installNamespace, chrt, convertError, manifest, templateError, "watch-namespace")
			},
		},
		{
			name: "SingleNamespace, specify own namespace to watch",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"watchNamespace": "test-namespace"},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.ErrorContains(t, templateError, "OwnNamespace mode is not supported")
				assert.Empty(t, manifest)
			},
		},
		{
			name: "SingleNamespace, don't specify a namespace to watch",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.ErrorContains(t, templateError, "watchNamespace is required")
				assert.Empty(t, manifest)
			},
		},
		{
			name: "MultiNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"watchNamespaces": []string{"watch-namespace1", "watch-namespace2"}},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				multiNamespaceAssertions(t, installNamespace, chrt, convertError, manifest, templateError, "watch-namespace1", "watch-namespace2")
			},
		},
		{
			name: "MultiNamespace, specify own namespace to watch",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"watchNamespaces": []string{"test-namespace", "watch-namespace2"}},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.ErrorContains(t, templateError, "OwnNamespace mode is not supported")
				assert.Empty(t, manifest)
			},
		},
		{
			name: "MultiNamespace, specify too many namespaces",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			},
			installNamespace: "test-namespace",
			values:           chartutil.Values{"watchNamespaces": []string{"ws1", "ws2", "ws3", "ws4", "ws5", "ws6", "ws7", "ws8", "ws9", "ws10", "ws11"}},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.ErrorContains(t, templateError, "watchNamespaces: Array must have at most 10 items")
				assert.Empty(t, manifest)
			},
		},
		{
			name: "AllNamespaces|SingleNamespace, no watchNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|MultiNamespace, no watchNamespaces",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeMultiNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces, include all webhooks",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				includeConversionWebhook: true,
				includeValidatingWebhook: true,
				includeMutatingWebhook:   true,
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.NoError(t, templateError)
				assert.NotEmpty(t, manifest)

				cwDef := csvWebhookConversionDefinition()
				vwDef := csvWebhookValidationDefinition()
				mwDef := csvWebhookMutationDefinition()

				var svcNames []string
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Service", nil
					},
					func(t *testing.T, obj client.Object) {
						svcNames = append(svcNames, obj.GetName())
						expectedSpec := corev1.ServiceSpec{
							Ports: []corev1.ServicePort{
								{Name: fmt.Sprintf("%d", cwDef.ContainerPort), Port: cwDef.ContainerPort, TargetPort: *cwDef.TargetPort},
								{Name: fmt.Sprintf("%d", vwDef.ContainerPort), Port: vwDef.ContainerPort, TargetPort: *vwDef.TargetPort},
								{Name: fmt.Sprintf("%d", mwDef.ContainerPort), Port: mwDef.ContainerPort, TargetPort: *mwDef.TargetPort},
							},
							Selector: csvPodLabels(),
						}
						assertFieldEqual(t, obj, expectedSpec, `{.spec}`)
					},
				)
				require.Len(t, svcNames, 1)

				var issuerNames []string
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Issuer", nil
					},
					func(t *testing.T, obj client.Object) {
						issuerNames = append(issuerNames, obj.GetName())
						expectedSpec := certmanagerv1.IssuerConfig{
							SelfSigned: &certmanagerv1.SelfSignedIssuer{},
						}
						assertFieldEqual(t, obj, expectedSpec, `{.spec}`)
					},
				)
				require.Len(t, issuerNames, 1)

				var secretName string
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Certificate", nil
					},
					func(t *testing.T, obj client.Object) {
						var cert certmanagerv1.Certificate
						if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &cert); err != nil {
							t.Fatal(err)
						}
						secretName = cert.Spec.SecretName
						assertFieldEqual(t, obj, []string{fmt.Sprintf("%s.%s.svc", svcNames[0], installNamespace)}, `{.spec.dnsNames}`)
						assertFieldEqual(t, obj, []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth}, `{.spec.usages}`)
						assertFieldEqual(t, obj, certmanagermetav1.ObjectReference{Name: issuerNames[0]}, `{.spec.issuerRef}`)
					},
				)
				require.NotEmpty(t, secretName)

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "CustomResourceDefinition", nil
					},
					func(t *testing.T, obj client.Object) {
						conversion := apiextensionsv1.CustomResourceConversion{
							Strategy: apiextensionsv1.WebhookConverter,
							Webhook: &apiextensionsv1.WebhookConversion{
								ClientConfig: &apiextensionsv1.WebhookClientConfig{Service: &apiextensionsv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: cwDef.WebhookPath, Port: ptr.To(cwDef.ContainerPort),
								}},
								ConversionReviewVersions: cwDef.AdmissionReviewVersions,
							},
						}
						assertFieldEqual(t, obj, conversion, `{.spec.conversion}`)
					},
				)

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "ValidatingWebhookConfiguration", nil
					},
					func(t *testing.T, obj client.Object) {
						validation := []admissionregistrationv1.ValidatingWebhook{{
							AdmissionReviewVersions: vwDef.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: vwDef.WebhookPath, Port: ptr.To(vwDef.ContainerPort),
								},
							},
							FailurePolicy:  vwDef.FailurePolicy,
							MatchPolicy:    vwDef.MatchPolicy,
							Name:           vwDef.GenerateName,
							ObjectSelector: vwDef.ObjectSelector,
							Rules:          vwDef.Rules,
							SideEffects:    vwDef.SideEffects,
							TimeoutSeconds: vwDef.TimeoutSeconds,
						}}
						assertFieldEqual(t, obj, validation, `{.webhooks}`)
						assertFieldEqual(t, obj, map[string]string{"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/%s", installNamespace, csvDeploymentName())}, `{.metadata.annotations}`)
					},
				)
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "MutatingWebhookConfiguration", nil
					},
					func(t *testing.T, obj client.Object) {
						mutation := []admissionregistrationv1.MutatingWebhook{{
							AdmissionReviewVersions: mwDef.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: mwDef.WebhookPath, Port: ptr.To(mwDef.ContainerPort),
								},
							},
							FailurePolicy:      mwDef.FailurePolicy,
							MatchPolicy:        mwDef.MatchPolicy,
							Name:               mwDef.GenerateName,
							ObjectSelector:     mwDef.ObjectSelector,
							ReinvocationPolicy: mwDef.ReinvocationPolicy,
							Rules:              mwDef.Rules,
							SideEffects:        mwDef.SideEffects,
							TimeoutSeconds:     mwDef.TimeoutSeconds,
						}}
						assertFieldEqual(t, obj, mutation, `{.webhooks}`)
						assertFieldEqual(t, obj, map[string]string{"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/%s", installNamespace, csvDeploymentName())}, `{.metadata.annotations}`)
					},
				)

				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{
					volumes: append(csvPodVolumes(),
						corev1.Volume{Name: "apiservice-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: "apiserver.crt"},
							{Key: "tls.key", Path: "apiserver.key"},
						}}}},
						corev1.Volume{Name: "webhook-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: "tls.crt"},
							{Key: "tls.key", Path: "tls.key"},
						}}}},
					),
					volumeMounts: append(csvContainerVolumeMounts(),
						corev1.VolumeMount{Name: "apiservice-cert", MountPath: "/apiserver.local.config/certificates"},
						corev1.VolumeMount{Name: "webhook-cert", MountPath: "/tmp/k8s-webhook-server/serving-certs"},
					),
				})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)

			},
		},
		{
			name: "AllNamespaces|OwnNamespace, fail with conversion webhook",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
				includeConversionWebhook: true,
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.ErrorContains(t, convertError, "CSVs with conversion webhooks must support only AllNamespaces install mode")
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, succeed with admission webhooks",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
				includeValidatingWebhook: true,
				includeMutatingWebhook:   true,
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError)
				assert.NotNil(t, chrt)
				assert.NoError(t, templateError)
				assert.NotEmpty(t, manifest)
			},
		},
		{
			name: "Include API service",
			cfg: genBundleConfig{
				supportedInstallModes:        []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				includeAPIServiceDefinitions: true,
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.ErrorContains(t, convertError, "apiServiceDefintions are not supported")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotChart, convertError := RegistryV1ToHelmChart(context.Background(), genBundleFS(tc.cfg))
			if convertError != nil {
				tc.assert(t, tc.installNamespace, gotChart, convertError, "", nil)
				return
			}

			manifest, templateError := templateChart(gotChart, tc.installNamespace, tc.values)
			tc.assert(t, tc.installNamespace, gotChart, nil, manifest, templateError)
		})
	}
}

func Test_getWatchNamespacesSchema(t *testing.T) {
	tests := []struct {
		supportedInstallModeSets []sets.Set[v1alpha1.InstallModeType]
		shouldPanic              bool
		want                     watchNamespaceSchemaConfig
	}{
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces)},
			want: watchNamespaceSchemaConfig{
				TemplateHelperDefaultValue: `""`,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "installMode",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type: spec.StringOrArray{"string"},
						Enum: []interface{}{"AllNamespaces", "OwnNamespace"},
					},
				},
				TemplateHelperDefaultValue: `"AllNamespaces"`,
				AllowReleaseNamespace:      true,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespace",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:        spec.StringOrArray{"string"},
						Description: "A namespace that the extension should watch.",
						Pattern:     watchNamespacePattern,
						MinLength:   ptr.To(int64(1)),
						MaxLength:   ptr.To(int64(63)),
					},
				},
				TemplateHelperDefaultValue: `""`,
				AllowReleaseNamespace:      true,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace),
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespaces",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:     spec.StringOrArray{"array"},
						MinItems: ptr.To(int64(1)),
						MaxItems: ptr.To(int64(10)),
						Items: &spec.SchemaOrArray{
							Schema: &spec.Schema{
								SchemaProps: spec.SchemaProps{
									Type:        spec.StringOrArray{"string"},
									Description: "A namespace that the extension should watch.",
									Pattern:     watchNamespacePattern,
									MinLength:   ptr.To(int64(1)),
									MaxLength:   ptr.To(int64(63)),
								},
							},
						},
					},
				},
				TemplateHelperDefaultValue: `(list "")`,
				AllowReleaseNamespace:      true,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace),
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeMultiNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespaces",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:     spec.StringOrArray{"array"},
						MinItems: ptr.To(int64(1)),
						MaxItems: ptr.To(int64(10)),
						Items: &spec.SchemaOrArray{
							Schema: &spec.Schema{
								SchemaProps: spec.SchemaProps{
									Type:        spec.StringOrArray{"string"},
									Description: "A namespace that the extension should watch.",
									Pattern:     watchNamespacePattern,
									MinLength:   ptr.To(int64(1)),
									MaxLength:   ptr.To(int64(63)),
								},
							},
						},
					},
				},
				TemplateHelperDefaultValue: `(list "")`,
				AllowReleaseNamespace:      false,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespace",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:        spec.StringOrArray{"string"},
						Description: "A namespace that the extension should watch.",
						Pattern:     watchNamespacePattern,
						MinLength:   ptr.To(int64(1)),
						MaxLength:   ptr.To(int64(63)),
					},
				},
				TemplateHelperDefaultValue: `""`,
				AllowReleaseNamespace:      false,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeOwnNamespace),
			},
			want: watchNamespaceSchemaConfig{
				TemplateHelperDefaultValue: `.Release.Namespace`,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespace",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:        spec.StringOrArray{"string"},
						Description: "A namespace that the extension should watch.",
						Pattern:     watchNamespacePattern,
						MinLength:   ptr.To(int64(1)),
						MaxLength:   ptr.To(int64(63)),
					},
				},
				TemplateHelperDefaultValue: `.Release.Namespace`,
				AllowReleaseNamespace:      true,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace),
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespaces",
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:     spec.StringOrArray{"array"},
						MinItems: ptr.To(int64(1)),
						MaxItems: ptr.To(int64(10)),
						Items: &spec.SchemaOrArray{
							Schema: &spec.Schema{
								SchemaProps: spec.SchemaProps{
									Type:        spec.StringOrArray{"string"},
									Description: "A namespace that the extension should watch.",
									Pattern:     watchNamespacePattern,
									MinLength:   ptr.To(int64(1)),
									MaxLength:   ptr.To(int64(63)),
								},
							},
						},
					},
				},
				TemplateHelperDefaultValue: `(list .Release.Namespace)`,
				AllowReleaseNamespace:      true,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeSingleNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespace",
				Required:     true,
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:        spec.StringOrArray{"string"},
						Description: "A namespace that the extension should watch.",
						Pattern:     watchNamespacePattern,
						MinLength:   ptr.To(int64(1)),
						MaxLength:   ptr.To(int64(63)),
					},
				},
				AllowReleaseNamespace: false,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace),
				sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeMultiNamespace),
			},
			want: watchNamespaceSchemaConfig{
				IncludeField: true,
				FieldName:    "watchNamespaces",
				Required:     true,
				Schema: &spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type:     spec.StringOrArray{"array"},
						MinItems: ptr.To(int64(1)),
						MaxItems: ptr.To(int64(10)),
						Items: &spec.SchemaOrArray{
							Schema: &spec.Schema{
								SchemaProps: spec.SchemaProps{
									Type:        spec.StringOrArray{"string"},
									Description: "A namespace that the extension should watch.",
									Pattern:     watchNamespacePattern,
									MinLength:   ptr.To(int64(1)),
									MaxLength:   ptr.To(int64(63)),
								},
							},
						},
					},
				},
				AllowReleaseNamespace: false,
			},
		},
		{
			supportedInstallModeSets: []sets.Set[v1alpha1.InstallModeType]{{}},
			shouldPanic:              true,
		},
	}
	for _, tt := range tests {
		for _, supportedInstallModeSet := range tt.supportedInstallModeSets {
			modes := []string{}
			installModes := []v1alpha1.InstallMode{}

			for _, mode := range []v1alpha1.InstallModeType{
				v1alpha1.InstallModeTypeAllNamespaces,
				v1alpha1.InstallModeTypeOwnNamespace,
				v1alpha1.InstallModeTypeSingleNamespace,
				v1alpha1.InstallModeTypeMultiNamespace,
			} {
				if supportedInstallModeSet.Has(mode) {
					modes = append(modes, string(mode))
				}
				installModes = append(installModes, v1alpha1.InstallMode{Type: mode, Supported: supportedInstallModeSet.Has(mode)})
			}
			supportedInstallModes := getSupportedInstallModes(installModes)
			name := strings.Join(modes, "|")
			if name == "" {
				name = "none"
			}
			t.Run(name, func(t *testing.T) {
				if tt.shouldPanic {
					require.Panics(t, func() {
						getWatchNamespacesSchema(supportedInstallModes)
					})
					return
				}
				got := getWatchNamespacesSchema(supportedInstallModes)
				if diff := cmp.Diff(got, tt.want, cmpopts.IgnoreUnexported(jsonreference.Ref{})); diff != "" {
					t.Errorf("getWatchNamespacesSchema() mismatch (-got +want):\n%s", diff)
				}
			})
		}
	}
}
