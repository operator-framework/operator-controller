package filter

import (
	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

// ExactVersionRelease returns a predicate that matches bundles with an exact
// version and release match. Both the semver version and the release must match
// exactly for the predicate to return true.
func ExactVersionRelease(expect bundle.VersionRelease) filter.Predicate[declcfg.Bundle] {
	return func(b declcfg.Bundle) bool {
		actual, err := bundleutil.GetVersionAndRelease(b)
		if err != nil {
			return false
		}
		return expect.Compare(*actual) == 0
	}
}

// InSemverRange returns a predicate that matches bundles whose version falls within
// the provided semver range. The range is applied only to the semver version portion,
// ignoring the release metadata.
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

// SameVersionHigherRelease returns a predicate that matches bundles with the same
// semantic version as the provided version-release, but with a higher release value.
// This is used to identify re-released bundles (e.g., 2.0.0+2 when 2.0.0+1 is installed).
func SameVersionHigherRelease(expect bundle.VersionRelease) filter.Predicate[declcfg.Bundle] {
	return func(b declcfg.Bundle) bool {
		actual, err := bundleutil.GetVersionAndRelease(b)
		if err != nil {
			return false
		}

		if expect.Version.Compare(actual.Version) != 0 {
			return false
		}

		return expect.Release.Compare(actual.Release) < 0
	}
}
