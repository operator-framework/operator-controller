package registryv1_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/validators"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_BundleValidatorHasAllValidationFns(t *testing.T) {
	expectedValidationFns := []func(v1 *bundle.RegistryV1) []error{
		validators.CheckDeploymentSpecUniqueness,
		validators.CheckDeploymentNameIsDNS1123SubDomain,
		validators.CheckCRDResourceUniqueness,
		validators.CheckOwnedCRDExistence,
		validators.CheckPackageNameNotEmpty,
		validators.CheckConversionWebhookSupport,
		validators.CheckWebhookDeploymentReferentialIntegrity,
		validators.CheckWebhookNameUniqueness,
		validators.CheckWebhookNameIsDNS1123SubDomain,
		validators.CheckConversionWebhookCRDReferenceUniqueness,
		validators.CheckConversionWebhooksReferenceOwnedCRDs,
		validators.CheckWebhookRules,
	}
	actualValidationFns := registryv1.BundleValidator

	require.Len(t, actualValidationFns, len(expectedValidationFns))
	for i := range expectedValidationFns {
		require.Equal(t, reflect.ValueOf(expectedValidationFns[i]).Pointer(), reflect.ValueOf(actualValidationFns[i]).Pointer(), "bundle validator has unexpected validation function")
	}
}

func Test_ResourceGeneratorsHasAllGenerators(t *testing.T) {
	expectedGenerators := []render.ResourceGenerator{
		generators.BundleCSVServiceAccountGenerator,
		generators.BundleCSVPermissionsGenerator,
		generators.BundleCSVClusterPermissionsGenerator,
		generators.BundleCRDGenerator,
		generators.BundleAdditionalResourcesGenerator,
		generators.BundleCSVDeploymentGenerator,
		generators.BundleValidatingWebhookResourceGenerator,
		generators.BundleMutatingWebhookResourceGenerator,
		generators.BundleDeploymentServiceResourceGenerator,
		generators.CertProviderResourceGenerator,
	}
	actualGenerators := registryv1.ResourceGenerators

	require.Len(t, actualGenerators, len(expectedGenerators))
	for i := range expectedGenerators {
		require.Equal(t, reflect.ValueOf(expectedGenerators[i]).Pointer(), reflect.ValueOf(actualGenerators[i]).Pointer(), "bundle validator has unexpected validation function")
	}
}

func Test_Renderer_Success(t *testing.T) {
	someBundle := bundle.RegistryV1{
		PackageName: "my-package",
		CSV: clusterserviceversion.Builder().
			WithName("test-bundle").
			WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build(),
		Others: []unstructured.Unstructured{
			*ToUnstructuredT(t, &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
			}),
		},
	}

	objs, err := registryv1.Renderer.Render(someBundle, "install-namespace")
	t.Log("Check renderer returns objects and no errors")
	require.NoError(t, err)
	require.NotEmpty(t, objs)

	t.Log("Check renderer returns a single object")
	// bundle only contains a service - bundle csv is empty
	require.Len(t, objs, 1)

	t.Log("Check correct name and that the correct namespace was applied")
	require.Equal(t, "my-service", objs[0].GetName())
	require.Equal(t, "install-namespace", objs[0].GetNamespace())
}

func Test_Renderer_Failure_UnsupportedKind(t *testing.T) {
	someBundle := bundle.RegistryV1{
		PackageName: "my-package",
		CSV: clusterserviceversion.Builder().
			WithName("test-bundle").
			WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build(),
		Others: []unstructured.Unstructured{
			*ToUnstructuredT(t, &corev1.Event{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Event",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "testEvent",
				},
			}),
		},
	}

	objs, err := registryv1.Renderer.Render(someBundle, "install-namespace")
	t.Log("Check renderer returns objects and no errors")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported resource")
	require.Empty(t, objs)
}
