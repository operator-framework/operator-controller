package registryv1_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/validators"
)

func Test_BundleValidatorHasAllValidationFns(t *testing.T) {
	expectedValidationFns := []func(v1 *render.RegistryV1) []error{
		validators.CheckDeploymentSpecUniqueness,
		validators.CheckDeploymentNameIsDNS1123SubDomain,
		validators.CheckCRDResourceUniqueness,
		validators.CheckOwnedCRDExistence,
		validators.CheckPackageNameNotEmpty,
		validators.CheckWebhookDeploymentReferentialIntegrity,
		validators.CheckWebhookNameUniqueness,
		validators.CheckWebhookNameIsDNS1123SubDomain,
		validators.CheckConversionWebhookCRDReferenceUniqueness,
		validators.CheckConversionWebhooksReferenceOwnedCRDs,
	}
	actualValidationFns := registryv1.BundleValidator

	require.Equal(t, len(expectedValidationFns), len(actualValidationFns))
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
		generators.BundleWebhookServiceResourceGenerator,
		generators.CertProviderResourceGenerator,
	}
	actualGenerators := registryv1.ResourceGenerators

	require.Equal(t, len(expectedGenerators), len(actualGenerators))
	for i := range expectedGenerators {
		require.Equal(t, reflect.ValueOf(expectedGenerators[i]).Pointer(), reflect.ValueOf(actualGenerators[i]).Pointer(), "bundle validator has unexpected validation function")
	}
}
