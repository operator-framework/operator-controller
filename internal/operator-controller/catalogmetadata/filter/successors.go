package filter

import (
	"fmt"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

// parseInstalledBundleVersionRelease constructs a VersionRelease from BundleMetadata.
// If the Release field is not nil, use it as the explicit release.
// If the Release field is nil, parse release from version build metadata (registry+v1 legacy format).
func parseInstalledBundleVersionRelease(installedBundle ocv1.BundleMetadata) (*declcfg.VersionRelease, error) {
	// Handle legacy registry+v1 format: release embedded in version's build metadata
	if installedBundle.Release == nil {
		return newLegacyRegistryV1VersionRelease(installedBundle.Version)
	}

	// Bundle has explicit release field (or explicitly empty) - parse version and release from separate fields.
	// Note: We can't use newLegacyRegistryV1VersionRelease here because the version might
	// already contain build metadata (e.g., "1.0.0+git.abc"), which serves its proper
	// semver purpose when using explicit pkg.Release. Concatenating would create invalid
	// semver like "1.0.0+git.abc+2".
	version, err := bsemver.Parse(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to parse installed bundle version %q: %w", installedBundle.Version, err)
	}

	var release declcfg.Release
	if *installedBundle.Release == "" {
		// Explicit empty release: use empty slice (not nil) to match catalog parsing behavior.
		// NewRelease("") returns nil, but we need empty slice for roundtrip correctness.
		release = declcfg.Release([]bsemver.PRVersion{})
	} else {
		release, err = declcfg.NewRelease(*installedBundle.Release)
		if err != nil {
			return nil, fmt.Errorf("failed to parse installed bundle release %q: %w", *installedBundle.Release, err)
		}
	}

	return &declcfg.VersionRelease{
		Version: version,
		Release: release,
	}, nil
}

// newLegacyRegistryV1VersionRelease parses a registry+v1 bundle version string and returns a
// VersionRelease. Some registry+v1 bundles utilize the build metadata field of the semver version
// as release information (a semver spec violation maintained for backward compatibility).
func newLegacyRegistryV1VersionRelease(vStr string) (*declcfg.VersionRelease, error) {
	vers, err := bsemver.Parse(vStr)
	if err != nil {
		return nil, fmt.Errorf("failed to get version and release of installed bundle: %w", err)
	}

	vr := &declcfg.VersionRelease{
		Version: vers,
	}

	buildMetadata := ""
	if len(vr.Version.Build) > 0 {
		buildMetadata = vr.Version.Build[0]
		for i := 1; i < len(vr.Version.Build); i++ {
			buildMetadata += "." + vr.Version.Build[i]
		}
	}

	rel, err := declcfg.NewRelease(buildMetadata)
	if err == nil && len(rel) > 0 {
		vr.Release = rel
		vr.Version.Build = nil
	}
	return vr, nil
}

func SuccessorsOf(installedBundle ocv1.BundleMetadata, channels ...declcfg.Channel) (filter.Predicate[declcfg.Bundle], error) {
	installedVersionRelease, err := parseInstalledBundleVersionRelease(installedBundle)
	if err != nil {
		return nil, err
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
