package filter

import (
	"fmt"

	mmsemver "github.com/Masterminds/semver/v3"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/features"
)

func SuccessorsOf(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (Predicate[declcfg.Bundle], error) {
	var successors successorsPredicateFunc = legacySuccessor
	if features.OperatorControllerFeatureGate.Enabled(features.ForceSemverUpgradeConstraints) {
		successors = semverSuccessor
	}

	installedBundleVersion, err := mmsemver.NewVersion(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("parsing installed bundle %q version %q: %w", installedBundle.Name, installedBundle.Version, err)
	}

	installedVersionConstraint, err := mmsemver.NewConstraint(installedBundleVersion.String())
	if err != nil {
		return nil, fmt.Errorf("parsing installed version constraint %q: %w", installedBundleVersion.String(), err)
	}

	successorsPredicate, err := successors(installedBundle, channels...)
	if err != nil {
		return nil, fmt.Errorf("getting successorsPredicate: %w", err)
	}

	// We need either successors or current version (no upgrade)
	return Or(
		successorsPredicate,
		InMastermindsSemverRange(installedVersionConstraint),
	), nil
}

// successorsPredicateFunc returns a predicate to find successors
// for a bundle. Predicate must not include the current version.
type successorsPredicateFunc func(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (Predicate[declcfg.Bundle], error)

func legacySuccessor(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (Predicate[declcfg.Bundle], error) {
	installedBundleVersion, err := mmsemver.NewVersion(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("error parsing installed bundle version: %w", err)
	}

	isSuccessor := func(candidateBundleEntry declcfg.ChannelEntry) bool {
		if candidateBundleEntry.Replaces == installedBundle.Name {
			return true
		}
		for _, skip := range candidateBundleEntry.Skips {
			if skip == installedBundle.Name {
				return true
			}
		}
		if candidateBundleEntry.SkipRange != "" {
			skipRange, err := mmsemver.NewConstraint(candidateBundleEntry.SkipRange)
			if err == nil && skipRange.Check(installedBundleVersion) {
				return true
			}
		}
		return false
	}

	return func(candidateBundle declcfg.Bundle) bool {
		for _, ch := range channels {
			for _, chEntry := range ch.Entries {
				if candidateBundle.Name == chEntry.Name && isSuccessor(chEntry) {
					return true
				}
			}
		}
		return false
	}, nil
}

// semverSuccessor returns a predicate to find successors based on Semver.
// Successors will not include versions outside the major version of the
// installed bundle as major version is intended to indicate breaking changes.
//
// NOTE: semverSuccessor does not consider channels since there is no information
// in a channel entry that is necessary to determine if a bundle is a successor.
// A semver range check is the only necessary element. If filtering by channel
// membership is necessary, an additional filter for that purpose should be applied.
func semverSuccessor(installedBundle ocv1.BundleMetadata, _ ...declcfg.Channel) (Predicate[declcfg.Bundle], error) {
	currentVersion, err := mmsemver.NewVersion(installedBundle.Version)
	if err != nil {
		return nil, err
	}

	// Based on current version create a caret range comparison constraint
	// to allow only minor and patch version as successors and exclude current version.
	constraintStr := fmt.Sprintf("^%[1]s, != %[1]s", currentVersion.String())
	wantedVersionRangeConstraint, err := mmsemver.NewConstraint(constraintStr)
	if err != nil {
		return nil, err
	}

	return InMastermindsSemverRange(wantedVersionRangeConstraint), nil
}
