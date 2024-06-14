package filter

import (
	"fmt"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

func WithPackageName(packageName string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		value := bundle.Package == packageName
		if !value {
			return false, []string{packageName}
		}
		return value, nil
	}
}

func InMastermindsSemverRange(semverRange *mmsemver.Constraints) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		bVersion, err := bundle.Version()
		if err != nil {
			return false, []string{err.Error()}
		}
		// No error should occur here because the simple version was successfully parsed by blang
		// We are unaware of any tests cases that would cause one to fail but not the other
		// This will cause code coverage to drop for this line. We don't ignore the error because
		// there might be that one extreme edge case that might cause one to fail but not the other
		mVersion, err := mmsemver.NewVersion(bVersion.String())
		if err != nil {
			return false, []string{err.Error()}
		}
		res := semverRange.Check(mVersion)
		if !res {
			return false, []string{fmt.Sprintf("no package %q matching version %q found", bundle.Package, semverRange.String())}
		}
		return true, nil
	}
}

func InBlangSemverRange(semverRange bsemver.Range, semverRangeString string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		bundleVersion, err := bundle.Version()
		if err != nil {
			return false, []string{err.Error()}
		}
		if !semverRange(*bundleVersion) {
			return false, []string{fmt.Sprintf("no package %q matching version %q found", bundle.Package, semverRangeString)}
		}
		return true, nil
	}
}

func InChannel(channelName string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		for _, ch := range bundle.InChannels {
			if ch.Name == channelName {
				return true, nil
			}
		}
		return false, []string{fmt.Sprintf("no package %q found in channel %q", bundle.Package, channelName)}
	}
}

func WithBundleImage(bundleImage string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		res := bundle.Image == bundleImage
		if !res {
			return false, []string{fmt.Sprintf("no matching bundle image %q found for package %s", bundleImage, bundle.Package)}
		} else {
			return true, nil
		}
	}
}

func WithBundleName(bundleName string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		res := bundle.Name == bundleName
		if !res {
			return false, []string{fmt.Sprintf("no matching bundle name %q found for package %s", bundleName, bundle.Package)}
		} else {
			return true, nil
		}
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

	return func(candidateBundle *catalogmetadata.Bundle) (bool, []string) {
		for _, ch := range candidateBundle.InChannels {
			for _, chEntry := range ch.Entries {
				if candidateBundle.Name == chEntry.Name && isSuccessor(chEntry) {
					return true, nil
				}
			}
		}
		return false, []string{fmt.Sprintf("no legacy successor found for bundle name %q", candidateBundle.Name)}
	}
}

func WithDeprecation(deprecated bool) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) (bool, []string) {
		res := bundle.HasDeprecation() == deprecated
		if !res {
			return false, []string{fmt.Sprintf("no bundle %q found with desired deprecation status %t", bundle.Name, deprecated)}
		} else {
			return true, nil
		}
	}
}
