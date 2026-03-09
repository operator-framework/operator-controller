package filter

import (
	mmsemver "github.com/Masterminds/semver/v3"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

func InMastermindsSemverRange(semverRange *mmsemver.Constraints) filter.Predicate[declcfg.Bundle] {
	return func(b declcfg.Bundle) bool {
		bVersion, err := bundleutil.GetVersion(b)
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
