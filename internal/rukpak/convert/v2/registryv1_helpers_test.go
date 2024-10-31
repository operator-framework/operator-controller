package v2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	k8sresource "k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/util/jsonpath"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func assertInManifest(t *testing.T, manifest string, match func(client.Object) (bool, error), assert func(*testing.T, client.Object)) {
	t.Helper()

	foundMatch := false
	res := k8sresource.NewLocalBuilder().Unstructured().Stream(strings.NewReader(manifest), "manifest").Do()
	_ = res.Visit(func(info *k8sresource.Info, err error) error {
		require.NoError(t, err)

		obj := info.Object.(client.Object)
		ok, err := match(obj)
		require.NoError(t, err)
		if ok {
			foundMatch = true
			assert(t, obj)
		}
		return nil
	})
	if !foundMatch {
		t.Errorf("no object matched the given criteria")
	}
}

func assertPresent(t *testing.T, manifest string, match func(client.Object) (bool, error)) {
	t.Helper()

	foundMatch := false
	res := k8sresource.NewLocalBuilder().Unstructured().Stream(strings.NewReader(manifest), "manifest").Do()
	_ = res.Visit(func(info *k8sresource.Info, err error) error {
		require.NoError(t, err)
		obj := info.Object.(client.Object)
		ok, err := match(obj)
		require.NoError(t, err)
		if ok {
			foundMatch = true
		}
		return nil
	})
	if !foundMatch {
		t.Errorf("no object matched the given criteria")
	}
}

func assertNotPresent(t *testing.T, manifest string, match func(client.Object) (bool, error)) {
	t.Helper()

	res := k8sresource.NewLocalBuilder().Unstructured().Stream(strings.NewReader(manifest), "manifest").Do()
	_ = res.Visit(func(info *k8sresource.Info, err error) error {
		require.NoError(t, err)
		obj := info.Object.(client.Object)
		ok, err := match(obj)
		require.NoError(t, err)
		if ok {
			t.Errorf("manifest contains unexpected object: %#v, %#v", obj.GetObjectKind().GroupVersionKind(), types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()})
		}
		return nil
	})
}

func assertFieldEqual(t *testing.T, obj client.Object, value interface{}, path string) bool {
	t.Helper()
	uObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	require.NoError(t, err)

	jp := jsonpath.New("assert").AllowMissingKeys(false)
	jp.EnableJSONOutput(true)
	require.NoError(t, jp.Parse(path))

	var resultBuf bytes.Buffer
	require.NoError(t, jp.Execute(&resultBuf, uObject))

	var result []json.RawMessage
	require.NoError(t, json.Unmarshal(resultBuf.Bytes(), &result))

	expected, err := json.Marshal(value)
	require.NoError(t, err)

	return assert.JSONEq(t, string(expected), string(result[0]))
}

type genBundleConfig struct {
	supportedInstallModes        []v1alpha1.InstallModeType
	includeAPIServiceDefinitions bool
	includeConversionWebhook     bool
	includeValidatingWebhook     bool
	includeMutatingWebhook       bool
	excludeMergeables            bool
}

func genBundleFS(cfg genBundleConfig) fs.FS {
	mustMarshal := func(v interface{}) []byte {
		b, err := yaml.Marshal(v)
		if err != nil {
			panic(err)
		}
		return b
	}
	newFSFile := func(data []byte) *fstest.MapFile {
		return &fstest.MapFile{
			Data: data,
			Mode: 0644,
		}
	}

	configMap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "example-config",
		},
		Data: configMapData(),
	}
	crd := apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: crdName(),
		},
		Spec: crdSpec(),
	}
	csv := v1alpha1.ClusterServiceVersion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operators.coreos.com/v1alpha1",
			Kind:       "ClusterServiceVersion",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        csvName(),
			Annotations: csvAnnotations(),
			Labels:      csvLabels(),
		},
		Spec: v1alpha1.ClusterServiceVersionSpec{
			Annotations:               csvSpecAnnotations(),
			APIServiceDefinitions:     csvSpecAPIServiceDefinitions(cfg.includeAPIServiceDefinitions),
			CustomResourceDefinitions: csvSpecCRDDefinitions(crd),
			Description:               csvSpecDescription(),
			InstallModes:              csvSpecInstallModes(cfg.supportedInstallModes),
			Keywords:                  csvSpecKeywords(),
			Links:                     csvSpecLinks(),
			Maintainers:               csvSpecMaintainers(),
			MinKubeVersion:            csvSpecMinKubeVersion(),
			Provider:                  csvSpecProvider(),
			Version:                   csvSpecVersion(),
			WebhookDefinitions:        csvSpecWebhookDefinitions(cfg.includeConversionWebhook, cfg.includeValidatingWebhook, cfg.includeMutatingWebhook),

			InstallStrategy: v1alpha1.NamedInstallStrategy{
				StrategyName: v1alpha1.InstallStrategyNameDeployment,
				StrategySpec: v1alpha1.StrategyDetailsDeployment{
					ClusterPermissions: []v1alpha1.StrategyDeploymentPermissions{
						{
							ServiceAccountName: csvPodServiceAccountName(),
							Rules:              csvClusterPermissionRules(),
						},
					},
					Permissions: []v1alpha1.StrategyDeploymentPermissions{
						{
							ServiceAccountName: csvPodServiceAccountName(),
							Rules:              csvPermissionRules(),
						},
					},
					DeploymentSpecs: []v1alpha1.StrategyDeploymentSpec{
						{
							Name:  csvDeploymentName(),
							Label: csvDeploymentLabels(),
							Spec: appsv1.DeploymentSpec{
								Replicas: csvDeploymentReplicas(),
								Selector: csvDeploymentSelector(),
								Template: corev1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Labels: csvPodLabels(),
									},
									Spec: corev1.PodSpec{
										Affinity:           csvPodAffinity(),
										NodeSelector:       csvPodNodeSelector(),
										ServiceAccountName: csvPodServiceAccountName(),
										Containers: []corev1.Container{{
											Image:     csvContainerImage(),
											Name:      csvContainerName(),
											Resources: csvContainerResources(),
										}},
									},
								},
							},
						},
					},
				},
			},

			// TODO: Do Labels and Selector need to be accounted for in the conversion logic?
			Labels:   nil,
			Selector: nil,
		},
	}
	if !cfg.excludeMergeables {
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Tolerations = csvPodTolerations()
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Volumes = csvPodVolumes()
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env = csvContainerEnv()
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].EnvFrom = csvContainerEnvFrom()
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].VolumeMounts = csvContainerVolumeMounts()
	}
	bundleAnnotations := map[string]map[string]string{
		"annotations": {
			"operators.operatorframework.io.bundle.manifests.v1": "manifests/",
			"operators.operatorframework.io.bundle.mediatype.v1": "registry+v1",
			"operators.operatorframework.io.bundle.package.v1":   "example-operator",
		},
	}
	return fstest.MapFS{
		"manifests/configmap.yaml":  newFSFile(mustMarshal(configMap)),
		"manifests/csv.yaml":        newFSFile(mustMarshal(csv)),
		"manifests/crd.yaml":        newFSFile(mustMarshal(crd)),
		"metadata/annotations.yaml": newFSFile(mustMarshal(bundleAnnotations)),
	}
}

func configMapData() map[string]string {
	return map[string]string{"config": "example-config"}
}

func crdName() string {
	spec := crdSpec()
	return fmt.Sprintf("%s.%s", spec.Names.Plural, spec.Group)
}

func crdSpec() apiextensionsv1.CustomResourceDefinitionSpec {
	return apiextensionsv1.CustomResourceDefinitionSpec{
		Group: "example.com",
		Names: apiextensionsv1.CustomResourceDefinitionNames{
			Plural:   "examples",
			Singular: "example",
			Kind:     "Example",
			ListKind: "ExampleList",
		},
		Scope: apiextensionsv1.NamespaceScoped,
		Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
			{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Schema:  &apiextensionsv1.CustomResourceValidation{},
			},
		},
	}
}

func csvLabels() map[string]string {
	return map[string]string{"csvLabel": "csvLabelValue"}
}
func csvName() string {
	return fmt.Sprintf("example-operator.v%s", csvSpecVersion().String())
}
func csvAnnotations() map[string]string {
	return map[string]string{"csvAnnotation": "csvAnnotationValue"}
}
func csvSpecAnnotations() map[string]string {
	return map[string]string{"csvSpecAnnotation": "csvSpecAnnotationValue"}
}

func csvSpecAPIServiceDefinitions(included bool) v1alpha1.APIServiceDefinitions {
	if !included {
		return v1alpha1.APIServiceDefinitions{}
	}
	return v1alpha1.APIServiceDefinitions{
		Owned: []v1alpha1.APIServiceDescription{
			{
				Name:    "v1alpha1.example.com",
				Group:   "example.com",
				Version: "v1alpha1",
				Kind:    "Example",
			},
		},
	}
}

func csvSpecCRDDefinitions(crd apiextensionsv1.CustomResourceDefinition) v1alpha1.CustomResourceDefinitions {
	descs := make([]v1alpha1.CRDDescription, 0, len(crd.Spec.Versions))
	for _, v := range crd.Spec.Versions {
		descs = append(descs, v1alpha1.CRDDescription{
			Name:    crd.Name,
			Version: v.Name,
			Kind:    crd.Spec.Names.Kind,
		})
	}
	return v1alpha1.CustomResourceDefinitions{Owned: descs}
}

func csvSpecDescription() string {
	return "csvSpecDescription"
}

func csvSpecInstallModes(supportedInstallModes []v1alpha1.InstallModeType) []v1alpha1.InstallMode {
	allInstallModes := []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeOwnNamespace,
		v1alpha1.InstallModeTypeSingleNamespace,
		v1alpha1.InstallModeTypeMultiNamespace,
	}
	supported := sets.New[v1alpha1.InstallModeType](supportedInstallModes...)
	modes := make([]v1alpha1.InstallMode, 0, len(allInstallModes))
	for _, mode := range allInstallModes {
		modes = append(modes, v1alpha1.InstallMode{Type: mode, Supported: supported.Has(mode)})
	}
	return modes
}

func csvSpecKeywords() []string {
	return []string{"csvSpecKeyword1", "csvSpecKeyword2"}
}

func csvSpecLinks() []v1alpha1.AppLink {
	return []v1alpha1.AppLink{
		{
			Name: "Operator source",
			URL:  "https://example.com/operator",
		},
		{
			Name: "Operand source",
			URL:  "https://example.com/operand",
		},
	}
}

func csvSpecMaintainers() []v1alpha1.Maintainer {
	return []v1alpha1.Maintainer{
		{
			Name:  "maintainer1",
			Email: "maintainer1@example.com",
		},
		{
			Name:  "maintainer2",
			Email: "maintainer2@example.com",
		},
	}
}

func csvSpecMinKubeVersion() string {
	return "1.31.0"
}

func csvSpecProvider() v1alpha1.AppLink {
	return v1alpha1.AppLink{
		URL: "https://example.com",
	}
}

func csvSpecVersion() version.OperatorVersion {
	return version.OperatorVersion{Version: semver.MustParse("0.1.0")}
}

func csvSpecWebhookDefinitions(includeConversion, includeValidating, includeMutating bool) []v1alpha1.WebhookDescription {
	var webhooks []v1alpha1.WebhookDescription
	if includeConversion {
		webhooks = append(webhooks, csvWebhookConversionDefinition())
	}
	if includeValidating {
		webhooks = append(webhooks, csvWebhookValidationDefinition())
	}
	if includeMutating {
		webhooks = append(webhooks, csvWebhookMutationDefinition())
	}
	return webhooks
}

func csvWebhookConversionDefinition() v1alpha1.WebhookDescription {
	return v1alpha1.WebhookDescription{
		Type:                    v1alpha1.ConversionWebhook,
		AdmissionReviewVersions: []string{"v1-convert", "v1beta1-convert"},
		ContainerPort:           10001,
		ConversionCRDs:          []string{crdName()},
		DeploymentName:          csvDeploymentName(),
		TargetPort:              ptr.To(intstr.FromInt32(10002)),
		WebhookPath:             ptr.To("/convert-example"),
	}
}

func csvWebhookValidationDefinition() v1alpha1.WebhookDescription {
	return v1alpha1.WebhookDescription{
		Type:                    v1alpha1.ValidatingAdmissionWebhook,
		AdmissionReviewVersions: []string{"v1-validate", "v1beta1-validate"},
		ContainerPort:           20001,
		DeploymentName:          csvDeploymentName(),
		FailurePolicy:           ptr.To(admissionregistrationv1.Ignore),
		MatchPolicy:             ptr.To(admissionregistrationv1.Equivalent),
		ObjectSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"csvWebhookValidationKey": "csvWebhookValidationValue"}},
		Rules: []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Delete},
			Rule: admissionregistrationv1.Rule{
				APIGroups:   []string{"mutate.example.com"},
				APIVersions: []string{"v1-mutate"},
				Resources:   []string{"tests-mutate"},
			},
		}},
		SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassSome),
		TargetPort:     ptr.To(intstr.FromInt32(20002)),
		TimeoutSeconds: ptr.To(int32(20)),
		WebhookPath:    ptr.To("/validate-example"),
	}
}

func csvWebhookMutationDefinition() v1alpha1.WebhookDescription {
	return v1alpha1.WebhookDescription{
		Type:                    v1alpha1.MutatingAdmissionWebhook,
		AdmissionReviewVersions: []string{"v1-mutate", "v1beta1-mutate"},
		ContainerPort:           30001,
		DeploymentName:          csvDeploymentName(),
		FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
		MatchPolicy:             ptr.To(admissionregistrationv1.Exact),
		ObjectSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"csvWebhookMutationKey": "csvWebhookMutationValue"}},
		ReinvocationPolicy:      ptr.To(admissionregistrationv1.IfNeededReinvocationPolicy),
		Rules: []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule: admissionregistrationv1.Rule{
				APIGroups:   []string{"validate.example.com"},
				APIVersions: []string{"v1-validate"},
				Resources:   []string{"tests-validate"},
			},
		}},
		SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassUnknown),
		TargetPort:     ptr.To(intstr.FromInt32(30002)),
		TimeoutSeconds: ptr.To(int32(30)),
		WebhookPath:    ptr.To("/validate-example"),
	}
}

func csvDeploymentLabels() map[string]string {
	return map[string]string{"csvDeploymentLabel": "csvDeploymentLabelValue"}
}

func csvDeploymentName() string {
	return "csvDeploymentName"
}

func csvDeploymentReplicas() *int32 {
	return ptr.To(int32(1))
}

func csvDeploymentSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: csvPodLabels()}
}

func csvPermissionRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{{
		APIGroups: []string{"example.com"},
		Resources: []string{"tests"},
		Verbs:     []string{"*"},
	}}
}

func csvClusterPermissionRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{{
		APIGroups: []string{"example.com"},
		Resources: []string{"clustertests"},
		Verbs:     []string{"*"},
	}}
}

func csvPodAffinity() *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key:      "csvPodAffinityKey",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"csvPodAffinityValue"},
					}},
				}},
			},
		},
	}
}

func csvPodLabels() map[string]string {
	return map[string]string{"csvPodLabel": "csvPodLabelValue"}
}

func csvPodNodeSelector() map[string]string {
	return map[string]string{"csvPodNodeSelector": "csvPodNodeSelectorValue"}
}

func csvPodServiceAccountName() string {
	return "csvPodServiceAccountName"
}

func csvPodTolerations() []corev1.Toleration {
	return []corev1.Toleration{{Key: "csvPodTolerationKey", Operator: corev1.TolerationOpExists}}
}

func csvPodVolumes() []corev1.Volume {
	return append(csvPodVolumesUniq(), csvPodVolumesShared(corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "csvContainerConfigMapVolumeSource"},
		},
	})...)
}

func csvPodVolumesUniq() []corev1.Volume {
	return []corev1.Volume{{Name: "csvVolume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
}

func csvPodVolumesShared(vs corev1.VolumeSource) []corev1.Volume {
	return []corev1.Volume{{Name: "sharedVolume", VolumeSource: vs}}
}

func csvContainerEnv() []corev1.EnvVar {
	return append(csvContainerEnvUniq(), csvContainerEnvShared("csvContainerSharedEnvValue")...)
}

func csvContainerEnvUniq() []corev1.EnvVar {
	return []corev1.EnvVar{{Name: "csvContainerEnvName", Value: "csvContainerEnvValue"}}
}

func csvContainerEnvShared(value string) []corev1.EnvVar {
	return []corev1.EnvVar{{Name: "sharedContainerEnvName", Value: value}}
}

func csvContainerEnvFrom() []corev1.EnvFromSource {
	return []corev1.EnvFromSource{{
		ConfigMapRef: &corev1.ConfigMapEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "csvContainerEnvFromName"},
		},
	}}
}

func csvContainerImage() string {
	return "csvContainerImage:latest"
}

func csvContainerName() string {
	return "csvContainerName"
}

func csvContainerResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
	}
}

func csvContainerVolumeMounts() []corev1.VolumeMount {
	return append(csvContainerVolumeMountsUniq(), csvContainerVolumeMountsShared("csvContainerSharedVolumeMountPath")...)
}

func csvContainerVolumeMountsUniq() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "csvContainerVolumeMountName", MountPath: "csvContainerVolumeMountPath"},
	}
}

func csvContainerVolumeMountsShared(mountPath string) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "sharedContainerVolumeMountName", MountPath: mountPath},
	}
}

func templateChart(chrt *chart.Chart, namespace string, values chartutil.Values) (string, error) {
	i := action.NewInstall(&action.Configuration{})
	i.Namespace = namespace
	i.DryRun = true
	i.DryRunOption = "true"
	i.ReleaseName = "release-name"
	i.ClientOnly = true
	i.IncludeCRDs = true
	i.KubeVersion = &chartutil.KubeVersion{
		Version: "v1.31.0",
		Major:   "1",
		Minor:   "31",
	}

	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		return "", err
	}
	valuesMap := chartutil.Values{}
	if err := yaml.Unmarshal(valuesYAML, &valuesMap); err != nil {
		return "", err
	}

	rel, err := i.Run(chrt, valuesMap)
	if err != nil {
		return "", err
	}
	return rel.Manifest, nil
}
