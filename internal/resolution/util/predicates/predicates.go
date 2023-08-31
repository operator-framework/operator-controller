package predicates

import (
	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
)

func WithPackageName(packageName string) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		name, err := bundleEntity.PackageName()
		if err != nil {
			return false
		}
		return name == packageName
	}
}

func InMastermindsSemverRange(semverRange *mmsemver.Constraints) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		bVersion, err := bundleEntity.Version()
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

func InBlangSemverRange(semverRange bsemver.Range) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		bundleVersion, err := bundleEntity.Version()
		if err != nil {
			return false
		}
		return semverRange(*bundleVersion)
	}
}

func InChannel(channelName string) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		bundleChannel, err := bundleEntity.ChannelName()
		if err != nil {
			return false
		}
		return channelName == bundleChannel
	}
}

func ProvidesGVK(gvk *olmentity.GVK) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		providedGVKs, err := bundleEntity.ProvidedGVKs()
		if err != nil {
			return false
		}
		for i := 0; i < len(providedGVKs); i++ {
			providedGVK := &providedGVKs[i]
			if providedGVK.String() == gvk.String() {
				return true
			}
		}
		return false
	}
}

func WithBundleImage(bundleImage string) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		bundlePath, err := bundleEntity.BundlePath()
		if err != nil {
			return false
		}
		return bundlePath == bundleImage
	}
}

func Replaces(bundleID string) input.Predicate {
	return func(entity *input.Entity) bool {
		bundleEntity := olmentity.NewBundleEntity(entity)
		replaces, err := bundleEntity.BundleChannelEntry()
		if err != nil {
			return false
		}
		return replaces.Replaces == bundleID
	}
}
