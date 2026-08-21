package registryv1

import (
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/validators"
)

// Renderer renders registry+v1 bundles into plain kubernetes manifests
var Renderer = render.BundleRenderer{
	BundleValidator: BundleValidator,
	Mutators:        Mutators,
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
	validators.CheckObjectSupport,
}

// Mutators is the ordered pipeline of render.Mutators applied to the shared render Context
// to produce plain resource manifests for registry+v1 bundles. Order is significant: the
// NamespaceMutator runs first so every later mutator can read the resolved install namespace.
var Mutators = []render.Mutator{
	// NOTE: if you update this list, Test_MutatorsHasAllMutators will fail until
	// you bring the same changes over to that test. This helps ensure all mutators are executed
	// while giving us the flexibility to test each mutator individually
	render.NamespaceMutator,
	generators.BundleCSVServiceAccountGenerator,
	generators.BundleCSVPermissionsGenerator,
	generators.BundleCSVClusterPermissionsGenerator,
	generators.BundleCRDGenerator,
	generators.BundleAdditionalResourcesGenerator,
	generators.BundleCSVDeploymentGenerator,
	generators.BundleValidatingWebhookResourceGenerator,
	generators.BundleMutatingWebhookResourceGenerator,
	// CertMutator runs last: it decorates the objects the earlier mutators produced with all
	// certificate-dependent wiring (webhook client configs, CRD conversion configs, CA bundles,
	// cert volumes) and appends the webhook-serving Service and any provider objects.
	generators.CertMutator,
}
