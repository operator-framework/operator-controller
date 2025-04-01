package convert_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

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

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

const (
	olmNamespaces    = "olm.targetNamespaces"
	olmProperties    = "olm.properties"
	installNamespace = "testInstallNamespace"

	bundlePathAnnotations = "metadata/annotations.yaml"
	bundlePathCSV         = "manifests/csv.yaml"
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

func TestConverterValidatesBundle(t *testing.T) {
	converter := convert.Converter{
		BundleValidator: []func(rv1 *convert.RegistryV1) []error{
			func(rv1 *convert.RegistryV1) []error {
				return []error{errors.New("test error")}
			},
		},
	}

	_, err := converter.Convert(convert.RegistryV1{}, "installNamespace", []string{"watchNamespace"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "test error")
}

func TestPlainConverterUsedRegV1Validator(t *testing.T) {
	require.Equal(t, convert.RegistryV1BundleValidator, convert.PlainConverter.BundleValidator)
}

func TestRegistryV1SuiteNamespaceNotAvailable(t *testing.T) {
	var targetNamespaces []string

	t.Log("RegistryV1 Suite Convert")
	t.Log("It should set the namespaces of the object correctly")
	t.Log("It should set the namespace to installnamespace if not available")

	t.Log("By creating a registry v1 bundle")
	csv, svc := getCsvAndService()

	unstructuredSvc := convertToUnstructured(t, svc)
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, targetNamespaces)
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

	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, targetNamespaces)
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

	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredEvt},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, targetNamespaces)
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

	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
		Others:      []unstructured.Unstructured{unstructuredpriorityclass},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, targetNamespaces)
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
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
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
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
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
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
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
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         *csv,
		Others:      []unstructured.Unstructured{unstructuredSvc},
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
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

func TestConvertInstallModeValidation(t *testing.T) {
	for _, tc := range []struct {
		description      string
		installModes     []v1alpha1.InstallMode
		installNamespace string
		watchNamespaces  []string
	}{
		{
			description:      "fails on AllNamespaces install mode when CSV does not support it",
			installNamespace: "install-namespace",
			watchNamespaces:  []string{corev1.NamespaceAll},
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: false},
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
				{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
			},
		}, {
			description:      "fails on SingleNamespace install mode when CSV does not support it",
			installNamespace: "install-namespace",
			watchNamespaces:  []string{"watch-namespace"},
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true},
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false},
				{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
			},
		}, {
			description:      "fails on OwnNamespace install mode when CSV does not support it and watch namespace is not install namespace",
			installNamespace: "install-namespace",
			watchNamespaces:  []string{"watch-namespace"},
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true},
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false},
				{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
			},
		}, {
			description:      "fails on MultiNamespace install mode when CSV does not support it",
			installNamespace: "install-namespace",
			watchNamespaces:  []string{"watch-namespace-one", "watch-namespace-two", "watch-namespace-three"},
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true},
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
				{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: false},
			},
		}, {
			description:      "fails on MultiNamespace install mode when CSV supports it but watchNamespaces is empty",
			installNamespace: "install-namespace",
			watchNamespaces:  []string{},
			installModes: []v1alpha1.InstallMode{
				// because install mode is inferred by the watchNamespaces parameter
				// force MultiNamespace install by disabling other modes
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: false},
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: false},
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false},
				{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
			},
		}, {
			description:      "fails on MultiNamespace install mode when CSV supports it but watchNamespaces is nil",
			installNamespace: "install-namespace",
			watchNamespaces:  nil,
			installModes: []v1alpha1.InstallMode{
				// because install mode is inferred by the watchNamespaces parameter
				// force MultiNamespace install by disabling other modes
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: false},
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: false},
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false},
				{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
			},
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Log("RegistryV1 Suite Convert")
			t.Log("It should generate objects successfully based on target namespaces")

			t.Log("It should error when all namespace mode is disabled with target namespace containing an empty string")
			baseCSV, svc := getBaseCsvAndService()
			csv := baseCSV.DeepCopy()
			csv.Spec.InstallModes = tc.installModes

			t.Log("By creating a registry v1 bundle")
			unstructuredSvc := convertToUnstructured(t, svc)
			registryv1Bundle := convert.RegistryV1{
				PackageName: "testPkg",
				CSV:         *csv,
				Others:      []unstructured.Unstructured{unstructuredSvc},
			}

			t.Log("By converting to plain")
			plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, tc.installNamespace, tc.watchNamespaces)
			require.Error(t, err)
			require.Nil(t, plainBundle)
		})
	}
}

func TestRegistryV1SuiteReadBundleFileSystem(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should read the registry+v1 bundle filesystem correctly")
	t.Log("It should include metadata/properties.yaml and csv.metadata.annotations['olm.properties'] in chart metadata")
	fsys := os.DirFS("testdata/combine-properties-bundle")

	chrt, err := convert.RegistryV1ToHelmChart(fsys, "", "")
	require.NoError(t, err)
	require.NotNil(t, chrt)
	require.NotNil(t, chrt.Metadata)
	require.Contains(t, chrt.Metadata.Annotations, olmProperties)
	require.JSONEq(t, `[{"type":"from-csv-annotations-key","value":"from-csv-annotations-value"},{"type":"from-file-key","value":"from-file-value"}]`, chrt.Metadata.Annotations[olmProperties])
}

func TestParseFSFails(t *testing.T) {
	for _, tt := range []struct {
		name string
		FS   fs.FS
	}{
		{
			name: "bundle missing ClusterServiceVersion manifest",
			FS:   removePaths(newBundleFS(), bundlePathCSV),
		}, {
			name: "bundle missing metadata/annotations.yaml",
			FS:   removePaths(newBundleFS(), bundlePathAnnotations),
		}, {
			name: "bundle missing metadata/ directory",
			FS:   removePaths(newBundleFS(), "metadata/"),
		}, {
			name: "bundle missing manifests/ directory",
			FS:   removePaths(newBundleFS(), "manifests/"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convert.ParseFS(tt.FS)
			require.Error(t, err)
		})
	}
}

func TestRegistryV1SuiteReadBundleFileSystemFailsOnNoCSV(t *testing.T) {
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should read the registry+v1 bundle filesystem correctly")
	t.Log("It should include metadata/properties.yaml and csv.metadata.annotations['olm.properties'] in chart metadata")
	fsys := os.DirFS("testdata/combine-properties-bundle")

	chrt, err := convert.RegistryV1ToHelmChart(fsys, "", "")

	require.NoError(t, err)
	require.NotNil(t, chrt)
	require.NotNil(t, chrt.Metadata)
	require.Contains(t, chrt.Metadata.Annotations, olmProperties)
	require.JSONEq(t, `[{"type":"from-csv-annotations-key","value":"from-csv-annotations-value"},{"type":"from-file-key","value":"from-file-value"}]`, chrt.Metadata.Annotations[olmProperties])
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
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.Converter{}.Convert(registryv1Bundle, installNamespace, watchNamespaces)
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
	registryv1Bundle := convert.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
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
		// In the future, compare GVKs too by ensuring its set on the unstructuredObj.
		if o.GetName() == name {
			return o
		}
	}
	return nil
}

func newBundleFS() fstest.MapFS {
	annotationsYml := `
annotations:
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.package.v1: test
`

	csvYml := `
apiVersion: operators.operatorframework.io/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: test.v1.0.0
  annotations:
    olm.properties: '[{"type":"from-csv-annotations-key", "value":"from-csv-annotations-value"}]'
spec:
  installModes:
    - type: AllNamespaces
      supported: true
`

	return fstest.MapFS{
		bundlePathAnnotations: &fstest.MapFile{Data: []byte(strings.Trim(annotationsYml, "\n"))},
		bundlePathCSV:         &fstest.MapFile{Data: []byte(strings.Trim(csvYml, "\n"))},
	}
}

func removePaths(mapFs fstest.MapFS, paths ...string) fstest.MapFS {
	for k := range mapFs {
		for _, path := range paths {
			if strings.HasPrefix(k, path) {
				delete(mapFs, k)
			}
		}
	}
	return mapFs
}
