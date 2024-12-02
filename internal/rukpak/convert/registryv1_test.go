package convert

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const (
	olmNamespaces    = "olm.targetNamespaces"
	olmProperties    = "olm.properties"
	installNamespace = "testInstallNamespace"
)

func getCsvAndService() (v1alpha1.ClusterServiceVersion, corev1.Service) {
	csv := v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testCSV",
		},
		Spec: v1alpha1.ClusterServiceVersionSpec{
			InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}},
		},
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testService",
		},
	}
	svc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})
	return csv, svc
}

func TestRegistryV1SuiteNamespaceNotAvailable(t *testing.T) {
	var targetNamespaces []string

	t.Log("RegistryV1 Suite Convert")
	t.Log("It should set the namespaces of the object correctly")
	t.Log("It should set the namespace to installnamespace if not available")

	t.Log("By creating a registry v1 bundle")
	csv, svc := getCsvAndService()

	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 1)

	t.Log("By verifying if ns has been set correctly")
	resObj := findObjectByName(svc.Name, plainBundle.Objects)
	require.NotNil(t, resObj)
	require.Equal(t, installNamespace, resObj.GetNamespace())
}

func TestRegistryV1SuiteNamespaceAvailable(t *testing.T) {
	var targetNamespaces []string

	t.Log("RegistryV1 Suite Convert")
	t.Log("It should set the namespaces of the object correctly")
	t.Log("It should override namespace if already available")

	t.Log("By creating a registry v1 bundle")
	csv, svc := getCsvAndService()

	svc.SetNamespace("otherNs")
	unstructuredSvc := convertToUnstructured(t, svc)
	unstructuredSvc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})

	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 1)

	t.Log("By verifying if ns has been set correctly")
	resObj := findObjectByName(svc.Name, plainBundle.Objects)
	require.NotNil(t, plainBundle)
	require.Equal(t, installNamespace, resObj.GetNamespace())
}

func TestRegistryV1SuiteNamespaceUnsupportedKind(t *testing.T) {
	var targetNamespaces []string

	t.Log("RegistryV1 Suite Convert")
	t.Log("It should set the namespaces of the object correctly")
	t.Log("It should error when object is not supported")
	t.Log("It should error when unsupported GVK is passed")

	t.Log("By creating a registry v1 bundle")
	csv, _ := getCsvAndService()

	t.Log("By creating an unsupported kind")
	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testEvent",
		},
	}

	unstructuredEvt := convertToUnstructured(t, event)
	unstructuredEvt.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Event"})

	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredEvt},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
	require.Error(t, err)
	require.ErrorContains(t, err, "bundle contains unsupported resource")
	require.Nil(t, plainBundle)
}

func TestRegistryV1SuiteNamespaceClusterScoped(t *testing.T) {
	var targetNamespaces []string

	t.Log("RegistryV1 Suite Convert")
	t.Log("It should set the namespaces of the object correctly")
	t.Log("It should not set ns cluster scoped object is passed")
	t.Log("It should not error when cluster scoped obj is passed and not set its namespace")

	t.Log("By creating a registry v1 bundle")
	csv, _ := getCsvAndService()

	t.Log("By creating an unsupported kind")
	pc := schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testPriorityClass",
		},
	}

	unstructuredpriorityclass := convertToUnstructured(t, pc)
	unstructuredpriorityclass.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PriorityClass"})

	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredpriorityclass},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 1)

	t.Log("By verifying if ns has been set correctly")
	resObj := findObjectByName(pc.Name, plainBundle.Objects)
	require.NotNil(t, resObj)
	require.Empty(t, resObj.GetNamespace())
}

func getBaseCsvAndService() (v1alpha1.ClusterServiceVersion, corev1.Service) {
	// base CSV definition that each test case will deep copy and modify
	baseCSV := v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testCSV",
			Annotations: map[string]string{
				olmProperties: fmt.Sprintf("[{\"type\": %s, \"value\": \"%s\"}]", property.TypeConstraint, "value"),
			},
		},
		Spec: v1alpha1.ClusterServiceVersionSpec{
			InstallStrategy: v1alpha1.NamedInstallStrategy{
				StrategySpec: v1alpha1.StrategyDetailsDeployment{
					DeploymentSpecs: []v1alpha1.StrategyDeploymentSpec{
						{
							Name: "testDeployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "testContainer",
												Image: "testImage",
											},
										},
									},
								},
							},
						},
					},
					Permissions: []v1alpha1.StrategyDeploymentPermissions{
						{
							ServiceAccountName: "testServiceAccount",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{"test"},
									Resources: []string{"pods"},
									Verbs:     []string{"*"},
								},
							},
						},
					},
				},
			},
		},
	}

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testService",
		},
	}
	svc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})
	return baseCSV, svc
}

func TestRegistryV1SuiteGenerateAllNamespace(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should convert into plain manifests successfully with AllNamespaces")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{""}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 5)

	t.Log("By verifying olm.targetNamespaces annotation in the deployment's pod template")
	dep := findObjectByName("testDeployment", plainBundle.Objects)
	require.NotNil(t, dep)
	require.Contains(t, dep.(*appsv1.Deployment).Spec.Template.Annotations, olmNamespaces)
	require.Equal(t, strings.Join(watchNamespaces, ","), dep.(*appsv1.Deployment).Spec.Template.Annotations[olmNamespaces])
}

func TestRegistryV1SuiteGenerateMultiNamespace(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should convert into plain manifests successfully with MultiNamespace")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{"testWatchNs1", "testWatchNs2"}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 7)

	t.Log("By verifying olm.targetNamespaces annotation in the deployment's pod template")
	dep := findObjectByName("testDeployment", plainBundle.Objects)
	require.NotNil(t, dep)
	require.Contains(t, dep.(*appsv1.Deployment).Spec.Template.Annotations, olmNamespaces)
	require.Equal(t, strings.Join(watchNamespaces, ","), dep.(*appsv1.Deployment).Spec.Template.Annotations[olmNamespaces])
}

func TestRegistryV1SuiteGenerateSingleNamespace(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should convert into plain manifests successfully with SingleNamespace")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{"testWatchNs1"}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 5)

	t.Log("By verifying olm.targetNamespaces annotation in the deployment's pod template")
	dep := findObjectByName("testDeployment", plainBundle.Objects)
	require.NotNil(t, dep)
	require.Contains(t, dep.(*appsv1.Deployment).Spec.Template.Annotations, olmNamespaces)
	require.Equal(t, strings.Join(watchNamespaces, ","), dep.(*appsv1.Deployment).Spec.Template.Annotations[olmNamespaces])
}

func TestRegistryV1SuiteGenerateOwnNamespace(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should convert into plain manifests successfully with own namespace")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{installNamespace}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.NoError(t, err)

	t.Log("By verifying if plain bundle has required objects")
	require.NotNil(t, plainBundle)
	require.Len(t, plainBundle.Objects, 5)

	t.Log("By verifying olm.targetNamespaces annotation in the deployment's pod template")
	dep := findObjectByName("testDeployment", plainBundle.Objects)
	require.NotNil(t, dep)
	require.Contains(t, dep.(*appsv1.Deployment).Spec.Template.Annotations, olmNamespaces)
	require.Equal(t, strings.Join(watchNamespaces, ","), dep.(*appsv1.Deployment).Spec.Template.Annotations[olmNamespaces])
}

func TestRegistryV1SuiteGenerateErrorMultiNamespaceEmpty(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should error when multinamespace mode is supported with an empty string in target namespaces")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{"testWatchNs1", ""}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.Nil(t, plainBundle)
}

func TestRegistryV1SuiteGenerateErrorSingleNamespaceDisabled(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should error when single namespace mode is disabled with more than one target namespaces")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{"testWatchNs1", "testWatchNs2"}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.Nil(t, plainBundle)
}

func TestRegistryV1SuiteGenerateErrorAllNamespaceDisabled(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should error when all namespace mode is disabled with target namespace containing an empty string")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{
		{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: false},
		{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
		{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
		{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
	}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{""}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.Nil(t, plainBundle)
}

func TestRegistryV1SuiteGeneratePropagateCsvAnnotations(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should propagate csv annotations to chart metadata annotation")
	baseCSV, svc := getBaseCsvAndService()
	csv := baseCSV.DeepCopy()
	csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}}

	t.Log("By creating a registry v1 bundle")
	watchNamespaces := []string{"testWatchNs1", "testWatchNs2"}
	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to helm")
	chrt, err := toChart(registryv1Bundle, installNamespace, watchNamespaces)
	require.NoError(t, err)
	require.Contains(t, chrt.Metadata.Annotations, olmProperties)
}

func TestRegistryV1SuiteReadBundleFileSystem(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should read the registry+v1 bundle filesystem correctly")
	t.Log("It should include metadata/properties.yaml and csv.metadata.annotations['olm.properties'] in chart metadata")
	fsys := os.DirFS("testdata/combine-properties-bundle")
	chrt, err := RegistryV1ToHelmChart(context.Background(), fsys, "", nil)
	require.NoError(t, err)
	require.NotNil(t, chrt)
	require.NotNil(t, chrt.Metadata)
	require.Contains(t, chrt.Metadata.Annotations, olmProperties)
	require.Equal(t, `[{"type":"from-csv-annotations-key","value":"from-csv-annotations-value"},{"type":"from-file-key","value":"from-file-value"}]`, chrt.Metadata.Annotations[olmProperties])
}

func TestRegistryV1SuiteGenerateNoWebhooks(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should enforce limitations")
	t.Log("It should not allow bundles with webhooks")
	t.Log("By creating a registry v1 bundle")
	csv := v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testCSV",
		},
		Spec: v1alpha1.ClusterServiceVersionSpec{
			InstallModes:       []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}},
			WebhookDefinitions: []v1alpha1.WebhookDescription{{ConversionCRDs: []string{"fake-webhook.package-with-webhooks.io"}}},
		},
	}
	watchNamespaces := []string{metav1.NamespaceAll}
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.ErrorContains(t, err, "webhookDefinitions are not supported")
	require.Nil(t, plainBundle)
}

func TestRegistryV1SuiteGenerateNoAPISerciceDefinitions(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should enforce limitations")
	t.Log("It should not allow bundles with API service definitions")
	t.Log("By creating a registry v1 bundle")
	csv := v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testCSV",
		},
		Spec: v1alpha1.ClusterServiceVersionSpec{
			InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}},
			APIServiceDefinitions: v1alpha1.APIServiceDefinitions{
				Owned: []v1alpha1.APIServiceDescription{{Name: "fake-owned-api-definition"}},
			},
		},
	}
	watchNamespaces := []string{metav1.NamespaceAll}
	registryv1Bundle := RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
	}

	t.Log("By converting to plain")
	plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.ErrorContains(t, err, "apiServiceDefintions are not supported")
	require.Nil(t, plainBundle)
}

func convertToUnstructured(t *testing.T, obj interface{}) unstructured.Unstructured {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	require.NoError(t, err)
	require.NotNil(t, unstructuredObj)
	return unstructured.Unstructured{Object: unstructuredObj}
}

func findObjectByName(name string, result []client.Object) client.Object {
	for _, o := range result {
		// Since this is a controlled env, comparing only the names is sufficient for now.
		// In future, compare GVKs too by ensuring its set on the unstructuredObj.
		if o.GetName() == name {
			return o
		}
	}
	return nil
}
