package filter

import (
	"fmt"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

func SuccessorsOf(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (filter.Predicate[declcfg.Bundle], error) {
	installedBundleVersion, err := mmsemver.NewVersion(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("parsing installed bundle %q version %q: %w", installedBundle.Name, installedBundle.Version, err)
	}

	installedVersionConstraint, err := mmsemver.NewConstraint(installedBundleVersion.String())
	if err != nil {
		return nil, fmt.Errorf("parsing installed version constraint %q: %w", installedBundleVersion.String(), err)
	}

	successorsPredicate, err := legacySuccessor(installedBundle, channels...)
	if err != nil {
		return nil, fmt.Errorf("getting successorsPredicate: %w", err)
	}

	// We need either successors or current version (no upgrade)
	return filter.Or(
		successorsPredicate,
		InMastermindsSemverRange(installedVersionConstraint),
	), nil
}

func legacySuccessor(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (filter.Predicate[declcfg.Bundle], error) {
	installedBundleVersion, err := bsemver.Parse(installedBundle.Version)
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
			// There are differences between how "github.com/blang/semver/v4" and "github.com/Masterminds/semver/v3"
			// handle version ranges. OLM v0 used blang and there might still be registry+v1 bundles that rely
			// on those specific differences. Because OLM v1 supports registry+v1 bundles,
			// blang needs to be kept alongside any other semver lib for range handling.
			// see: https://github.com/operator-framework/operator-controller/pull/1565#issuecomment-2586455768
			skipRange, err := bsemver.ParseRange(candidateBundleEntry.SkipRange)
			if err == nil && skipRange(installedBundleVersion) {
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
