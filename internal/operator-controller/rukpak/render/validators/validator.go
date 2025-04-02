package validators

import (
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

// RegistryV1BundleValidator validates RegistryV1 bundles
var RegistryV1BundleValidator = render.BundleValidator{
	// NOTE: if you update this list, Test_BundleValidatorHasAllValidationFns will fail until
	// you bring the same changes over to that test. This helps ensure all validation rules are executed
	// while giving us the flexibility to test each validation function individually
	CheckDeploymentSpecUniqueness,
	CheckCRDResourceUniqueness,
	CheckOwnedCRDExistence,
	CheckPackageNameNotEmpty,
}

// CheckDeploymentSpecUniqueness checks that each strategy deployment spec in the csv has a unique name.
// Errors are sorted by deployment name.
func CheckDeploymentSpecUniqueness(rv1 *render.RegistryV1) []error {
	deploymentNameSet := sets.Set[string]{}
	duplicateDeploymentNames := sets.Set[string]{}
	for _, dep := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		if deploymentNameSet.Has(dep.Name) {
			duplicateDeploymentNames.Insert(dep.Name)
		}
		deploymentNameSet.Insert(dep.Name)
	}

	errs := make([]error, 0, len(duplicateDeploymentNames))
	for _, d := range slices.Sorted(slices.Values(duplicateDeploymentNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("cluster service version contains duplicate strategy deployment spec '%s'", d))
	}
	return errs
}

// CheckOwnedCRDExistence checks bundle owned custom resource definitions declared in the csv exist in the bundle
func CheckOwnedCRDExistence(rv1 *render.RegistryV1) []error {
	crdsNames := sets.Set[string]{}
	for _, crd := range rv1.CRDs {
		crdsNames.Insert(crd.Name)
	}

	missingCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CSV.Spec.CustomResourceDefinitions.Owned {
		if !crdsNames.Has(crd.Name) {
			missingCRDNames.Insert(crd.Name)
		}
	}

	errs := make([]error, 0, len(missingCRDNames))
	for _, crdName := range slices.Sorted(slices.Values(missingCRDNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("cluster service definition references owned custom resource definition '%s' not found in bundle", crdName))
	}
	return errs
}

// CheckCRDResourceUniqueness checks that the bundle CRD names are unique
func CheckCRDResourceUniqueness(rv1 *render.RegistryV1) []error {
	crdsNames := sets.Set[string]{}
	duplicateCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CRDs {
		if crdsNames.Has(crd.Name) {
			duplicateCRDNames.Insert(crd.Name)
		}
		crdsNames.Insert(crd.Name)
	}

	errs := make([]error, 0, len(duplicateCRDNames))
	for _, crdName := range slices.Sorted(slices.Values(duplicateCRDNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("bundle contains duplicate custom resource definition '%s'", crdName))
	}
	return errs
}

// CheckPackageNameNotEmpty checks that PackageName is not empty
func CheckPackageNameNotEmpty(rv1 *render.RegistryV1) []error {
	if rv1.PackageName == "" {
		return []error{errors.New("package name is empty")}
	}
	return nil
}
