package convert

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
)

type BundleValidator []func(v1 *RegistryV1) []error

func (v BundleValidator) Validate(rv1 *RegistryV1) []error {
	var errs []error
	for _, validator := range v {
		errs = append(errs, validator(rv1)...)
	}
	return errs
}

func NewBundleValidator() BundleValidator {
	// NOTE: if you update this list, Test_BundleValidatorHasAllValidationFns will fail until
	// you bring the same changes over to that test. This helps ensure all validation rules are executed
	// while giving us the flexibility to test each validation function individually
	return BundleValidator{
		CheckDeploymentSpecUniqueness,
		CheckCRDResourceUniqueness,
		CheckOwnedCRDExistence,
	}
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
