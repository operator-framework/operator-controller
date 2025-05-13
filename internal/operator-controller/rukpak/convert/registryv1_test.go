package convert_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

const (
	olmNamespaces    = "olm.targetNamespaces"
	olmProperties    = "olm.properties"
	installNamespace = "testInstallNamespace"

	bundlePathAnnotations = "metadata/annotations.yaml"
	bundlePathCSV         = "manifests/csv.yaml"
)

func getCsvAndService() (v1alpha1.ClusterServiceVersion, corev1.Service) {
	csv := MakeCSV(WithName("testCSV"), WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces))
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testService",
		},
	}
	svc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})
	return csv, svc
}

func TestPlainConverterUsedRegV1Validator(t *testing.T) {
	require.Equal(t, registryv1.BundleValidator, convert.PlainConverter.BundleValidator)
}

func TestRegistryV1SuiteNamespaceNotAvailable(t *testing.T) {
	var targetNamespaces []string

	t.Log("RegistryV1 Suite Convert")
	t.Log("It should set the namespaces of the object correctly")
	t.Log("It should set the namespace to installnamespace if not available")

	t.Log("By creating a registry v1 bundle")
	csv, svc := getCsvAndService()

	unstructuredSvc := *ToUnstructuredT(t, &svc)
	registryv1Bundle := render.RegistryV1{
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
	unstructuredSvc := *ToUnstructuredT(t, &svc)
	unstructuredSvc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})

	registryv1Bundle := render.RegistryV1{
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
	event := &corev1.Event{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Event",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testEvent",
		},
	}

	unstructuredEvt := *ToUnstructuredT(t, event)
	unstructuredEvt.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Event"})

	registryv1Bundle := render.RegistryV1{
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
	pc := &schedulingv1.PriorityClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: schedulingv1.SchemeGroupVersion.String(),
			Kind:       "PriorityClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testPriorityClass",
		},
	}

	unstructuredpriorityclass := *ToUnstructuredT(t, pc)
	unstructuredpriorityclass.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PriorityClass"})

	registryv1Bundle := render.RegistryV1{
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
	registryv1Bundle := render.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.ErrorContains(t, err, "webhookDefinitions are not supported")
	require.Nil(t, plainBundle)
}

func TestRegistryV1SuiteGenerateWebhooks_WebhookSupportFGEnabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.WebhookProviderCertManager, true)
	t.Log("RegistryV1 Suite Convert")
	t.Log("It should generate objects successfully based on target namespaces")

	t.Log("It should enforce limitations")
	t.Log("It should allow bundles with webhooks")
	t.Log("By creating a registry v1 bundle")
	registryv1Bundle := render.RegistryV1{
		PackageName: "testPkg",
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fake-webhook.package-with-webhooks",
				},
			},
		},
		CSV: MakeCSV(
			WithName("testCSV"),
			WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces),
			WithOwnedCRDs(
				v1alpha1.CRDDescription{
					Name: "fake-webhook.package-with-webhooks",
				},
			),
			WithStrategyDeploymentSpecs(
				v1alpha1.StrategyDeploymentSpec{
					Name: "some-deployment",
				},
			),
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:           v1alpha1.ConversionWebhook,
					ConversionCRDs: []string{"fake-webhook.package-with-webhooks"},
					DeploymentName: "some-deployment",
					GenerateName:   "my-conversion-webhook",
				},
			),
		),
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, []string{metav1.NamespaceAll})
	require.NoError(t, err)
	require.NotNil(t, plainBundle)
}

func TestRegistryV1SuiteGenerateNoAPIServiceDefinitions(t *testing.T) {
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
	registryv1Bundle := render.RegistryV1{
		PackageName: "testPkg",
		CSV:         csv,
	}

	t.Log("By converting to plain")
	plainBundle, err := convert.PlainConverter.Convert(registryv1Bundle, installNamespace, watchNamespaces)
	require.Error(t, err)
	require.ErrorContains(t, err, "apiServiceDefintions are not supported")
	require.Nil(t, plainBundle)
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
