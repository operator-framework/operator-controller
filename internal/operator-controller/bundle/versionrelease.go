package bundle

import (
	"errors"
	"fmt"
	"strings"

	bsemver "github.com/blang/semver/v4"

	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

func NewLegacyRegistryV1VersionRelease(vStr string) (*VersionRelease, error) {
	vers, err := bsemver.Parse(vStr)
	if err != nil {
		return nil, err
	}

	rel, err := NewRelease(strings.Join(vers.Build, "."))
	if err != nil {
		return nil, err
	}
	vers.Build = nil

	return &VersionRelease{
		Version: vers,
		Release: rel,
	}, nil
}

type VersionRelease struct {
	Version bsemver.Version
	Release Release
}

func (vr *VersionRelease) Compare(other VersionRelease) int {
	if vCmp := vr.Version.Compare(other.Version); vCmp != 0 {
		return vCmp
	}
	return vr.Release.Compare(other.Release)
}

func (vr *VersionRelease) AsLegacyRegistryV1Version() bsemver.Version {
	return bsemver.Version{
		Major: vr.Version.Major,
		Minor: vr.Version.Minor,
		Patch: vr.Version.Patch,
		Pre:   vr.Version.Pre,
		Build: slicesutil.Map(vr.Release, func(i bsemver.PRVersion) string { return i.String() }),
	}
}

type Release []bsemver.PRVersion

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
