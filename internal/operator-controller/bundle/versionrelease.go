package bundle

import (
	"errors"
	"fmt"
	"strings"

	bsemver "github.com/blang/semver/v4"

	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

// NewLegacyRegistryV1VersionRelease parses a registry+v1 bundle version string and returns a
// VersionRelease. Some registry+v1 bundles utilize the build metadata field of the semver version
// as release information (a semver spec violation maintained for backward compatibility). When the
// bundle version includes build metadata that is parsable as a release, the returned
// VersionRelease has the build metadata extracted into the Release field, and the Version field
// has its Build metadata cleared. When the bundle version includes build metadata that is NOT
// parseable as a release, the returned VersionRelease has its Version set to the full semver
// version (with build metadata) and its Release left empty.
func NewLegacyRegistryV1VersionRelease(vStr string) (*VersionRelease, error) {
	vers, err := bsemver.Parse(vStr)
	if err != nil {
		return nil, err
	}

	vr := &VersionRelease{
		Version: vers,
	}

	rel, err := NewRelease(strings.Join(vr.Version.Build, "."))
	if err == nil {
		// If the version build metadata parses successfully as a release
		// then use it as a release and drop the build metadata
		//
		// If we don't parse the build metadata as a release successfully,
		// that doesn't mean we have an invalid version. It just means
		// that we have a valid semver version with valid build metadata,
		// but no release value. In this case, we return a VersionRelease
		// with:
		//   - Version: the full version (with build metadata)
		//   - Release: <empty>
		vr.Release = rel
		vr.Version.Build = nil
	}
	return vr, nil
}

type VersionRelease struct {
	Version bsemver.Version
	Release Release
}

// Compare compares two VersionRelease values. It returns:
//
//	-1 if vr < other
//	 0 if vr == other
//	+1 if vr > other
//
// Comparison is done first by Version, then by Release if versions are equal.
func (vr *VersionRelease) Compare(other VersionRelease) int {
	if vCmp := vr.Version.Compare(other.Version); vCmp != 0 {
		return vCmp
	}
	return vr.Release.Compare(other.Release)
}

// AsLegacyRegistryV1Version converts a VersionRelease into a standard semver version.
// If the VersionRelease's Release field is set, the returned semver version's build
// metadata is set to the VersionRelease's Release. Otherwise, the build metadata is
// set to the VersionRelease's Version field's build metadata.
func (vr *VersionRelease) AsLegacyRegistryV1Version() bsemver.Version {
	v := bsemver.Version{
		Major: vr.Version.Major,
		Minor: vr.Version.Minor,
		Patch: vr.Version.Patch,
		Pre:   vr.Version.Pre,
		Build: vr.Version.Build,
	}
	if len(vr.Release) > 0 {
		v.Build = slicesutil.Map(vr.Release, func(i bsemver.PRVersion) string { return i.String() })
	}
	return v
}

type Release []bsemver.PRVersion

// Compare compares two Release values. It returns:
//
//	-1 if r < other
//	 0 if r == other
//	+1 if r > other
//
// Comparison is done segment by segment from left to right. Numeric segments are
// compared numerically, and alphanumeric segments are compared lexically in ASCII
// sort order. A shorter release is considered less than a longer release if all
// corresponding segments are equal.
func (r Release) Compare(other Release) int {
	if len(r) == 0 && len(other) > 0 {
		return -1
	}
	if len(other) == 0 && len(r) > 0 {
		return 1
	}
	a := bsemver.Version{Pre: r}
	b := bsemver.Version{Pre: other}
	return a.Compare(b)
}

// NewRelease parses a release string into a Release. The release string should be
// a dot-separated sequence of non-empty identifiers, where each identifier contains
// only ASCII alphanumerics and hyphens [0-9A-Za-z-]. Numeric identifiers (those
// containing only digits) must not have leading zeros. An empty string returns a nil
// Release. Returns an error if any segment is invalid.
func NewRelease(relStr string) (Release, error) {
	if relStr == "" {
		return nil, nil
	}

	var (
		segments = strings.Split(relStr, ".")
		r        = make(Release, 0, len(segments))
		errs     []error
	)
	for i, segment := range segments {
		prVer, err := bsemver.NewPRVersion(segment)
		if err != nil {
			errs = append(errs, fmt.Errorf("segment %d: %v", i, err))
			continue
		}
		r = append(r, prVer)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("invalid release %q: %v", relStr, err)
	}
	return r, nil
}
