package compare

import (
	"strings"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

// NewVersionRange returns a function that tests whether a semver version is in the
// provided versionRange. The versionRange provided to this function can be any valid semver
// version string or any range constraint.
//
// When the provided version range is a valid semver version then the returned function will match
// any version that matches the semver version, ignoring the build metadata of matched versions.
//
// This function is intended to be used to parse the ClusterExtension.spec.source.catalog.version
// field. See the API documentation for more details on the supported syntax.
func NewVersionRange(versionRange string) (bsemver.Range, error) {
	return newMastermindsRange(versionRange)
}

func newMastermindsRange(versionRange string) (bsemver.Range, error) {
	constraint, err := mmsemver.NewConstraint(versionRange)
	if err != nil {
		return nil, err
	}
	return func(in bsemver.Version) bool {
		pre := slicesutil.Map(in.Pre, func(pr bsemver.PRVersion) string { return pr.String() })
		mmVer := mmsemver.New(in.Major, in.Minor, in.Patch, strings.Join(pre, "."), strings.Join(in.Build, "."))
		return constraint.Check(mmVer)
	}, nil
}

// ByVersionAndRelease is a comparison function that compares bundles by
// version and release. Bundles with lower versions/releases are
// considered less than bundles with higher versions/releases.
func ByVersionAndRelease(b1, b2 declcfg.Bundle) int {
	vr1, err1 := bundleutil.GetVersionAndRelease(b1)
	vr2, err2 := bundleutil.GetVersionAndRelease(b2)

	// We don't really expect errors, because we expect well-formed/validated
	// FBC as input. However, just in case we'll check the errors and sort
	// invalid bundles as "lower" than valid bundles.
	if err1 != nil || err2 != nil {
		return compareErrors(err2, err1)
	}
	return vr2.Compare(*vr1)
}

func ByDeprecationFunc(deprecation declcfg.Deprecation) func(a, b declcfg.Bundle) int {
	deprecatedBundles := sets.New[string]()
	for _, entry := range deprecation.Entries {
		if entry.Reference.Schema == declcfg.SchemaBundle {
			deprecatedBundles.Insert(entry.Reference.Name)
		}
	}
	return func(a, b declcfg.Bundle) int {
		aDeprecated := deprecatedBundles.Has(a.Name)
		bDeprecated := deprecatedBundles.Has(b.Name)
		if aDeprecated && !bDeprecated {
			return 1
		}
		if !aDeprecated && bDeprecated {
			return -1
		}
		return 0
	}
}

// compareErrors returns 0 if both errors are either nil or not nil,
// -1 if err1 is not nil and err2 is nil, and
// +1 if err1 is nil and err2 is not nil
// The semantic is that errors are "less than" non-errors.
func compareErrors(err1 error, err2 error) int {
	if err1 != nil && err2 == nil {
		return -1
	}
	if err1 == nil && err2 != nil {
		return 1
	}
	return 0
}
