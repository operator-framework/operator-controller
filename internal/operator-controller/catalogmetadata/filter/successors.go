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
	// Construct VersionRelease from BundleMetadata.
	// If the Release field is populated, parse version and release separately.
	// Otherwise, parse release from version build metadata (registry+v1 legacy format).
	var installedVersionRelease *bundle.VersionRelease
	var err error

	if installedBundle.Release != "" {
		// Bundle has explicit release field - parse version and release from separate fields.
		// Note: We can't use NewLegacyRegistryV1VersionRelease here because the version might
		// already contain build metadata (e.g., "1.0.0+git.abc"), which serves its proper
		// semver purpose when using explicit pkg.Release. Concatenating would create invalid
		// semver like "1.0.0+git.abc+2".
		version, err := bsemver.Parse(installedBundle.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to parse installed bundle version %q: %w", installedBundle.Version, err)
		}
		release, err := bundle.NewRelease(installedBundle.Release)
		if err != nil {
			return nil, fmt.Errorf("failed to parse installed bundle release %q: %w", installedBundle.Release, err)
		}
		installedVersionRelease = &bundle.VersionRelease{
			Version: version,
			Release: release,
		}
	} else {
		// Legacy registry+v1: release embedded in version's build metadata
		installedVersionRelease, err = bundle.NewLegacyRegistryV1VersionRelease(installedBundle.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to get version and release of installed bundle: %w", err)
		}
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
