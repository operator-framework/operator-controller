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
	for _, validator := range validators {
		if err := validator(operator); err != nil {
			return err
		}
	}
	return nil
}
