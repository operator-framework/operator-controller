package validators

import (
	"fmt"

	mmsemver "github.com/Masterminds/semver/v3"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type extensionCRValidatorFunc func(e *ocv1alpha1.Extension) error

// validatePackageSemver validates that the clusterExtension's version is a valid SemVer.
// this validation should already be happening at the CRD level. But, it depends
// on a regex that could possibly fail to validate a valid SemVer. This is added as an
// extra measure to ensure a valid spec before the CR is processed for resolution
func validatePackageSemver(e *ocv1alpha1.Extension) error {
	pkg := e.GetPackageSpec()
	if pkg == nil {
		return nil
	}
	if pkg.Version == "" {
		return nil
	}
	if _, err := mmsemver.NewConstraint(pkg.Version); err != nil {
		return fmt.Errorf("invalid package version spec: %w", err)
	}
	return nil
}

// validatePackageOrDirect validates that one or the other exists
// For now, this just makes sure there's a Package, as no other Source type has been defined
func validatePackage(e *ocv1alpha1.Extension) error {
	pkg := e.GetPackageSpec()
	if pkg == nil {
		return fmt.Errorf("package not found")
	}
	return nil
}

// ValidateSpec validates the (cluster)Extension spec, e.g. ensuring that .spec.source.package.version, if provided, is a valid SemVer
func ValidateExtensionSpec(e *ocv1alpha1.Extension) error {
	validators := []extensionCRValidatorFunc{
		validatePackageSemver,
		validatePackage,
	}

	// Currently we only have a two validators, but more will likely be added in the future
	// we need to make a decision on whether we want to run all validators or stop at the first error. If the the former,
	// we should consider how to present this to the user in a way that is easy to understand and fix.
	// this issue is tracked here: https://github.com/operator-framework/operator-controller/issues/167
	for _, validator := range validators {
		if err := validator(e); err != nil {
			return err
		}
	}
	return nil
}
