package predicates

import (
	bsemver "github.com/blang/semver/v4"

	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

type Predicate func(variable *olmvariables.BundleVariable) bool

func WithPackageName(packageName string) Predicate {
	return func(variable *olmvariables.BundleVariable) bool {
		bundleVariable := olmvariables.NewBundleVariable(variable, make([]*olmvariables.BundleVariable, 0), variable.Properties)
		name, err := bundleVariable.PackageName()
		if err != nil {
			return false
		}
		return name == packageName
	}
}

func InSemverRange(semverRange bsemver.Range) Predicate {
	return func(variable *olmvariables.BundleVariable) bool {
		bundleVariable := olmvariables.NewBundleVariable(variable, make([]*olmvariables.BundleVariable, 0), variable.Properties)
		bundleVersion, err := bundleVariable.Version()
		if err != nil {
			return false
		}
		return semverRange(*bundleVersion)
	}
}

func InChannel(channelName string) Predicate {
	return func(variable *olmvariables.BundleVariable) bool {
		bundleVariable := olmvariables.NewBundleVariable(variable, make([]*olmvariables.BundleVariable, 0), variable.Properties)
		bundleChannel, err := bundleVariable.ChannelName()
		if err != nil {
			return false
		}
		return channelName == bundleChannel
	}
}

func ProvidesGVK(gvk *olmvariables.GVK) Predicate {
	return func(variable *olmvariables.BundleVariable) bool {
		bundleVariable := olmvariables.NewBundleVariable(variable, make([]*olmvariables.BundleVariable, 0), variable.Properties)
		providedGVKs, err := bundleVariable.ProvidedGVKs()
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

func WithBundleImage(bundleImage string) Predicate {
	return func(variable *olmvariables.BundleVariable) bool {
		bundleVariable := olmvariables.NewBundleVariable(variable, make([]*olmvariables.BundleVariable, 0), variable.Properties)
		bundlePath, err := bundleVariable.BundlePath()
		if err != nil {
			return false
		}
		return bundlePath == bundleImage
	}
}

func Replaces(bundleID string) Predicate {
	return func(variable *olmvariables.BundleVariable) bool {
		bundleVariable := olmvariables.NewBundleVariable(variable, make([]*olmvariables.BundleVariable, 0), variable.Properties)
		replaces, err := bundleVariable.BundleChannelEntry()
		if err != nil {
			return false
		}
		return replaces.Replaces == bundleID
	}
}
