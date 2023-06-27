package predicates

import (
	"github.com/blang/semver/v4"
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

func InSemverRange(semverRange semver.Range) input.Predicate {
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
		replaces, err := bundleEntity.Replaces()
		if err != nil {
			return false
		}
		return replaces == bundleID
	}
}
