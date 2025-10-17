package filter

import (
	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

func ExactVersionRelease(expect bundle.VersionRelease) filter.Predicate[declcfg.Bundle] {
	return func(b declcfg.Bundle) bool {
		actual, err := bundleutil.GetVersionAndRelease(b)
		if err != nil {
			return false
		}
		return expect.Compare(*actual) == 0
	}
}

func InSemverRange(versionRange bsemver.Range) filter.Predicate[declcfg.Bundle] {
	return func(b declcfg.Bundle) bool {
		vr, err := bundleutil.GetVersionAndRelease(b)
		if err != nil {
			return false
		}
		return versionRange(vr.Version)
	}
}

func InAnyChannel(channels ...declcfg.Channel) filter.Predicate[declcfg.Bundle] {
	return func(bundle declcfg.Bundle) bool {
		for _, ch := range channels {
			for _, entry := range ch.Entries {
				if entry.Name == bundle.Name {
					return true
				}
			}
		}
		return false
	}
}
