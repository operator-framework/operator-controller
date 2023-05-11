package validators

import (
	"fmt"

	"github.com/blang/semver/v4"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type operatorCRValidatorFunc func(operator *operatorsv1alpha1.Operator) error

// validateSemver validates that the operator's version is a valid SemVer.
// this validation should already be happening at the CRD level. But, it depends
// on a regex that could possibly fail to validate a valid SemVer. This is added as an
// extra measure to ensure a valid spec before the CR is processed for resolution
func validateSemver(operator *operatorsv1alpha1.Operator) error {
	if operator.Spec.Version == "" {
		return nil
	}
	if _, err := semver.Parse(operator.Spec.Version); err != nil {
		return fmt.Errorf("invalid .spec.version: %w", err)
	}
	return nil
}

// ValidateOperatorSpec validates the operator spec, e.g. ensuring that .spec.version, if provided, is a valid SemVer
func ValidateOperatorSpec(operator *operatorsv1alpha1.Operator) error {
	validators := []operatorCRValidatorFunc{
		validateSemver,
	}

	// TODO: currently we only have a single validator, but more will likely be added in the future
	//  we need to make a decision on whether we want to run all validators or stop at the first error. If the the former,
	//  we should consider how to present this to the user in a way that is easy to understand and fix.
	//  this issue is tracked here: https://github.com/operator-framework/operator-controller/issues/167
	for _, validator := range validators {
		if err := validator(operator); err != nil {
			return err
		}
	}
	return nil
}
