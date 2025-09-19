package registryv1

import (
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/validators"
)

// Renderer renders registry+v1 bundles into plain kubernetes manifests
var Renderer = render.BundleRenderer{
	BundleValidator:    BundleValidator,
	ResourceGenerators: ResourceGenerators,
}

// BundleValidator validates RegistryV1 bundles
var BundleValidator = render.BundleValidator{
	// NOTE: if you update this list, Test_BundleValidatorHasAllValidationFns will fail until
	// you bring the same changes over to that test. This helps ensure all validation rules are executed
	// while giving us the flexibility to test each validation function individually
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

// ResourceGenerators a slice of ResourceGenerators required to generate plain resource manifests for
// registry+v1 bundles
var ResourceGenerators = []render.ResourceGenerator{
	// NOTE: if you update this list, Test_ResourceGeneratorsHasAllGenerators will fail until
	// you bring the same changes over to that test. This helps ensure all validation rules are executed
	// while giving us the flexibility to test each generator individually
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
