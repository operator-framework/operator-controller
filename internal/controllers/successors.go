package controllers

import (
	"fmt"

	mmsemver "github.com/Masterminds/semver/v3"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	"github.com/operator-framework/operator-controller/pkg/features"
)

func SuccessorsPredicate(packageName string, installedBundle *ocv1alpha1.BundleMetadata) (catalogfilter.Predicate[catalogmetadata.Bundle], error) {
	var successors successorsPredicateFunc = legacySemanticsSuccessorsPredicate
	if features.OperatorControllerFeatureGate.Enabled(features.ForceSemverUpgradeConstraints) {
		successors = semverSuccessorsPredicate
	}

	installedBundleVersion, err := mmsemver.NewVersion(installedBundle.Version)
	if err != nil {
		return nil, err
	}

	installedVersionConstraint, err := mmsemver.NewConstraint(installedBundleVersion.String())
	if err != nil {
		return nil, err
	}

	successorsPredicate, err := successors(packageName, installedBundle)
	if err != nil {
		return nil, err
	}

	// We need either successors or current version (no upgrade)
	return catalogfilter.Or(
		successorsPredicate,
		catalogfilter.And(
			catalogfilter.WithPackageName(packageName),
			catalogfilter.InMastermindsSemverRange(installedVersionConstraint),
		),
	), nil
}

// successorsPredicateFunc returns a predicate to find successors
// for a bundle. Predicate must not include the current version.
type successorsPredicateFunc func(packageName string, bundle *ocv1alpha1.BundleMetadata) (catalogfilter.Predicate[catalogmetadata.Bundle], error)

// legacySemanticsSuccessorsPredicate returns a predicate to find successors
// based on legacy OLMv0 semantics which rely on Replaces, Skips and skipRange.
func legacySemanticsSuccessorsPredicate(packageName string, bundle *ocv1alpha1.BundleMetadata) (catalogfilter.Predicate[catalogmetadata.Bundle], error) {
	// find the bundles that replace, skip, or skipRange the bundle provided
	return catalogfilter.And(
		catalogfilter.WithPackageName(packageName),
		catalogfilter.LegacySuccessor(bundle),
	), nil
}

// semverSuccessorsPredicate returns a predicate to find successors based on Semver.
// Successors will not include versions outside the major version of the
// installed bundle as major version is intended to indicate breaking changes.
func semverSuccessorsPredicate(packageName string, bundle *ocv1alpha1.BundleMetadata) (catalogfilter.Predicate[catalogmetadata.Bundle], error) {
	currentVersion, err := mmsemver.NewVersion(bundle.Version)
	if err != nil {
		return nil, err
	}

	// Based on current version create a caret range comparison constraint
	// to allow only minor and patch version as successors and exclude current version.
	constraintStr := fmt.Sprintf("^%s, != %s", currentVersion.String(), currentVersion.String())
	wantedVersionRangeConstraint, err := mmsemver.NewConstraint(constraintStr)
	if err != nil {
		return nil, err
	}

	return catalogfilter.And(
		catalogfilter.WithPackageName(packageName),
		catalogfilter.InMastermindsSemverRange(wantedVersionRangeConstraint),
	), nil
}
