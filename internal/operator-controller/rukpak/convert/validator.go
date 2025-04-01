package convert

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

type BundleValidator []func(v1 *RegistryV1) []error

func (v BundleValidator) Validate(rv1 *RegistryV1) error {
	var errs []error
	for _, validator := range v {
		errs = append(errs, validator(rv1)...)
	}
	return errors.Join(errs...)
}

var RegistryV1BundleValidator = BundleValidator{
	// NOTE: if you update this list, Test_BundleValidatorHasAllValidationFns will fail until
	// you bring the same changes over to that test. This helps ensure all validation rules are executed
	// while giving us the flexibility to test each validation function individually
	CheckDeploymentSpecUniqueness,
	CheckCRDResourceUniqueness,
	CheckOwnedCRDExistence,
	CheckWebhookDeploymentReferentialIntegrity,
	CheckWebhookNameUniqueness,
	CheckConversionWebhookCRDReferences,
}

// CheckDeploymentSpecUniqueness checks that each strategy deployment spec in the csv has a unique name.
// Errors are sorted by deployment name.
func CheckDeploymentSpecUniqueness(rv1 *RegistryV1) []error {
	deploymentNameSet := sets.Set[string]{}
	duplicateDeploymentNames := sets.Set[string]{}
	for _, dep := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		if deploymentNameSet.Has(dep.Name) {
			duplicateDeploymentNames.Insert(dep.Name)
		}
		deploymentNameSet.Insert(dep.Name)
	}

	//nolint:prealloc
	var errs []error
	for _, d := range slices.Sorted(slices.Values(duplicateDeploymentNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("cluster service version contains duplicate strategy deployment spec '%s'", d))
	}
	return errs
}

// CheckOwnedCRDExistence checks bundle owned custom resource definitions declared in the csv exist in the bundle
func CheckOwnedCRDExistence(rv1 *RegistryV1) []error {
	crdsNames := sets.Set[string]{}
	for _, crd := range rv1.CRDs {
		crdsNames.Insert(crd.Name)
	}

	//nolint:prealloc
	var errs []error
	missingCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CSV.Spec.CustomResourceDefinitions.Owned {
		if !crdsNames.Has(crd.Name) {
			missingCRDNames.Insert(crd.Name)
		}
	}

	for _, crdName := range slices.Sorted(slices.Values(missingCRDNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("cluster service definition references owned custom resource definition '%s' not found in bundle", crdName))
	}
	return errs
}

// CheckCRDResourceUniqueness checks that the bundle CRD names are unique
func CheckCRDResourceUniqueness(rv1 *RegistryV1) []error {
	crdsNames := sets.Set[string]{}
	duplicateCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CRDs {
		if crdsNames.Has(crd.Name) {
			duplicateCRDNames.Insert(crd.Name)
		}
		crdsNames.Insert(crd.Name)
	}

	//nolint:prealloc
	var errs []error
	for _, crdName := range slices.Sorted(slices.Values(duplicateCRDNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("bundle contains duplicate custom resource definition '%s'", crdName))
	}
	return errs
}

// CheckWebhookDeploymentReferentialIntegrity checks that each webhook definition in the csv
// references an existing strategy deployment spec. Errors are sorted by strategy deployment spec name,
// webhook type, and webhook name.
func CheckWebhookDeploymentReferentialIntegrity(rv1 *RegistryV1) []error {
	webhooksByDeployment := map[string][]v1alpha1.WebhookDescription{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		webhooksByDeployment[wh.DeploymentName] = append(webhooksByDeployment[wh.DeploymentName], wh)
	}

	for _, depl := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		delete(webhooksByDeployment, depl.Name)
	}

	var errs []error
	// Loop through sorted keys to keep error messages ordered by deployment name
	for _, deploymentName := range slices.Sorted(maps.Keys(webhooksByDeployment)) {
		webhookDefns := webhooksByDeployment[deploymentName]
		slices.SortFunc(webhookDefns, func(a, b v1alpha1.WebhookDescription) int {
			return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.GenerateName, b.GenerateName))
		})
		for _, webhookDef := range webhookDefns {
			errs = append(errs, fmt.Errorf("webhook '%s' of type '%s' references non-existent deployment '%s'", webhookDef.GenerateName, webhookDef.Type, webhookDef.DeploymentName))
		}
	}
	return errs
}

// CheckWebhookNameUniqueness checks that each webhook definition of each type (validating, mutating, or conversion)
// has a unique name. Webhooks of different types can have the same name. Errors are sorted by webhook type
// and name.
func CheckWebhookNameUniqueness(rv1 *RegistryV1) []error {
	webhookNameSetByType := map[v1alpha1.WebhookAdmissionType]sets.Set[string]{}
	duplicateWebhooksByType := map[v1alpha1.WebhookAdmissionType]sets.Set[string]{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if _, ok := webhookNameSetByType[wh.Type]; !ok {
			webhookNameSetByType[wh.Type] = sets.Set[string]{}
		}
		if webhookNameSetByType[wh.Type].Has(wh.GenerateName) {
			if _, ok := duplicateWebhooksByType[wh.Type]; !ok {
				duplicateWebhooksByType[wh.Type] = sets.Set[string]{}
			}
			duplicateWebhooksByType[wh.Type].Insert(wh.GenerateName)
		}
		webhookNameSetByType[wh.Type].Insert(wh.GenerateName)
	}

	var errs []error
	for _, whType := range slices.Sorted(maps.Keys(duplicateWebhooksByType)) {
		for _, webhookName := range slices.Sorted(slices.Values(duplicateWebhooksByType[whType].UnsortedList())) {
			errs = append(errs, fmt.Errorf("duplicate webhook '%s' of type '%s'", webhookName, whType))
		}
	}
	return errs
}

// CheckConversionWebhookCRDReferences checks defined conversion webhooks reference bundle owned CRDs.
// Errors are sorted by webhook name and CRD name.
func CheckConversionWebhookCRDReferences(rv1 *RegistryV1) []error {
	//nolint:prealloc
	var conversionWebhooks []v1alpha1.WebhookDescription
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ConversionWebhook {
			continue
		}
		conversionWebhooks = append(conversionWebhooks, wh)
	}

	if len(conversionWebhooks) == 0 {
		return nil
	}

	ownedCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CSV.Spec.CustomResourceDefinitions.Owned {
		ownedCRDNames.Insert(crd.Name)
	}

	slices.SortFunc(conversionWebhooks, func(a, b v1alpha1.WebhookDescription) int {
		return cmp.Compare(a.GenerateName, b.GenerateName)
	})

	var errs []error
	for _, webhook := range conversionWebhooks {
		webhookCRDs := webhook.ConversionCRDs
		slices.Sort(webhookCRDs)
		for _, crd := range webhookCRDs {
			if !ownedCRDNames.Has(crd) {
				errs = append(errs, fmt.Errorf("conversion webhook '%s' references custom resource definition '%s' not owned bundle", webhook.GenerateName, crd))
			}
		}
	}
	return errs
}
