package filter

import (
	"fmt"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

func SuccessorsOf(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (filter.Predicate[declcfg.Bundle], error) {
	// TODO: We do not have an explicit field in our BundleMetadata for a bundle's release value.
	//    Legacy registry+v1 bundles embed the release value inside their versions as build metadata
	//    (in violation of the semver spec). If/when we add explicit release metadata to bundles and/or
	//    we support a new bundle format, we need to revisit the assumption that all bundles are
	//    registry+v1 and embed release in build metadata.
	installedVersionRelease, err := bundle.NewLegacyRegistryV1VersionRelease(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get version and release of installed bundle: %v", err)
	}

	successorsPredicate, err := legacySuccessor(installedBundle, channels...)
	if err != nil {
		return nil, fmt.Errorf("getting successorsPredicate: %w", err)
	}

	// We need either successors or current version (no upgrade)
	return filter.Or(
		successorsPredicate,
		ExactVersionRelease(*installedVersionRelease),
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
