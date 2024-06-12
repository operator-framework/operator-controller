package filter

import (
	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

func WithPackageName(packageName string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		return bundle.Package == packageName
	}
}

func InMastermindsSemverRange(semverRange *mmsemver.Constraints) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		bVersion, err := bundle.Version()
		if err != nil {
			return false
		}
		// No error should occur here because the simple version was successfully parsed by blang
		// We are unaware of any tests cases that would cause one to fail but not the other
		// This will cause code coverage to drop for this line. We don't ignore the error because
		// there might be that one extreme edge case that might cause one to fail but not the other
		mVersion, err := mmsemver.NewVersion(bVersion.String())
		if err != nil {
			return false
		}
		return semverRange.Check(mVersion)
	}
}

func InBlangSemverRange(semverRange bsemver.Range) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		bundleVersion, err := bundle.Version()
		if err != nil {
			return false
		}
		return semverRange(*bundleVersion)
	}
}

func InChannel(channelName string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		for _, ch := range bundle.InChannels {
			if ch.Name == channelName {
				return true
			}
		}
		return false
	}
}

func WithBundleImage(bundleImage string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		return bundle.Image == bundleImage
	}
}

func WithBundleName(bundleName string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		return bundle.Name == bundleName
	}
}

func LegacySuccessor(installedBundle *ocv1alpha1.BundleMetadata) Predicate[catalogmetadata.Bundle] {
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
			installedBundleVersion, vErr := bsemver.Parse(installedBundle.Version)
			skipRange, srErr := bsemver.ParseRange(candidateBundleEntry.SkipRange)
			if vErr == nil && srErr == nil && skipRange(installedBundleVersion) {
				return true
			}
		}
		return false
	}

	return func(candidateBundle *catalogmetadata.Bundle) bool {
		for _, ch := range candidateBundle.InChannels {
			for _, chEntry := range ch.Entries {
				if candidateBundle.Name == chEntry.Name && isSuccessor(chEntry) {
					return true
				}
			}
		}
		return false
	}
}

func WithDeprecation(deprecated bool) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		return bundle.HasDeprecation() == deprecated
	}
}
