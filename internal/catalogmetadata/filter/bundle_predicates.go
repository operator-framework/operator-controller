package filter

import (
	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"

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

func InChannel(channelName string, channels []*catalogmetadata.Channel) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		for _, ch := range channels {
			if ch.Name == channelName {
				for _, entry := range ch.Entries {
					if entry.Name == bundle.Name {
						return true
					}
				}
			}
		}
		return false
	}
}

func WithImage(image string) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		return bundle.Image == image
	}
}

func Replaces(bundleName string, channels []*catalogmetadata.Channel) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		for _, ch := range channels {
			for _, chEntry := range ch.Entries {
				if bundle.Name == chEntry.Name && chEntry.Replaces == bundleName {
					return true
				}
			}
		}
		return false
	}
}

func WithDeprecated(deprecated bool) Predicate[catalogmetadata.Bundle] {
	return func(bundle *catalogmetadata.Bundle) bool {
		return bundle.IsDeprecated() == deprecated
	}
}
