package bundleutil

import (
	"encoding/json"
	"fmt"
	"strings"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

func GetVersionAndRelease(b declcfg.Bundle) (*declcfg.VersionRelease, error) {
	for _, p := range b.Properties {
		if p.Type == property.TypePackage {
			return parseVersionRelease(p.Value)
		}
	}
	return nil, fmt.Errorf("no package property found in bundle %q", b.Name)
}

// ParseLegacyVersionRelease parses a registry+v1 bundle version string and returns a
// VersionRelease. Some registry+v1 bundles utilize the build metadata field of the semver version
// as release information (a semver spec violation maintained for backward compatibility). When the
// bundle version includes build metadata that is parsable as a release, the returned
// VersionRelease has the build metadata extracted into the Release field, and the Version field
// has its Build metadata cleared. When the bundle version includes build metadata that is NOT
// parseable as a release, the returned VersionRelease has its Version set to the full semver
// version (with build metadata) and its Release left empty.
func ParseLegacyVersionRelease(vStr string) (*declcfg.VersionRelease, error) {
	vers, err := bsemver.Parse(vStr)
	if err != nil {
		return nil, err
	}

	vr := &declcfg.VersionRelease{
		Version: vers,
	}

	buildMetadata := strings.Join(vr.Version.Build, ".")

	rel, err := declcfg.NewRelease(buildMetadata)
	if err == nil && len(rel) > 0 {
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

func parseVersionRelease(pkgData json.RawMessage) (*declcfg.VersionRelease, error) {
	var pkg property.Package
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		return nil, fmt.Errorf("error unmarshalling package property: %w", err)
	}

	// Check if release field is explicitly present in JSON (even if empty).
	// property.Package has Release string, so we can't distinguish "field absent" from "field empty".
	// We unmarshal again into a helper struct with Release *string to detect presence.
	var releaseField struct {
		Release *string `json:"release"`
	}
	if err := json.Unmarshal(pkgData, &releaseField); err != nil {
		return nil, fmt.Errorf("error unmarshalling package release field: %w", err)
	}

	// When BundleReleaseSupport is enabled and bundle has explicit release field, use it.
	if features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport) && releaseField.Release != nil {
		return ParseExplicitRelease(pkg.Version, *releaseField.Release)
	}

	// Fall back to legacy registry+v1 behavior (release in build metadata)
	//
	// TODO: For now, we assume that all bundles are registry+v1 bundles.
	//   In the future, for supporting other bundle formats, we should not
	//   use the legacy registry+v1 mechanism (i.e. using build metadata in
	//   the version) to determine the bundle's release.
	return ParseLegacyVersionRelease(pkg.Version)
}

// ParseExplicitRelease parses version and release from separate fields.
// Build metadata is preserved in the version because with an explicit release field,
// build metadata serves its proper semver purpose (e.g., git commit, build number).
// In contrast, ParseLegacyVersionRelease clears build metadata because it
// interprets build metadata AS the release value for registry+v1 bundles.
func ParseExplicitRelease(version, releaseStr string) (*declcfg.VersionRelease, error) {
	vers, err := bsemver.Parse(version)
	if err != nil {
		return nil, fmt.Errorf("error parsing version %q: %w", version, err)
	}

	var rel declcfg.Release
	if releaseStr == "" {
		// Explicit empty release: use empty slice (not nil)
		rel = declcfg.Release([]bsemver.PRVersion{})
	} else {
		rel, err = declcfg.NewRelease(releaseStr)
		if err != nil {
			return nil, fmt.Errorf("error parsing release %q: %w", releaseStr, err)
		}
	}

	return &declcfg.VersionRelease{
		Version: vers,
		Release: rel,
	}, nil
}

// MetadataFor returns a BundleMetadata for the given bundle name and version/release.
func MetadataFor(bundleName string, vr declcfg.VersionRelease) ocv1.BundleMetadata {
	if features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport) {
		// New behavior: separate Version and Release fields
		bm := ocv1.BundleMetadata{
			Name:    bundleName,
			Version: vr.Version.String(),
		}
		if vr.Release != nil {
			relStr := vr.Release.String()
			bm.Release = &relStr
		}
		return bm
	}
	// Old behavior for backward compatibility: reconstitute build metadata in Version field
	// This preserves release information (e.g., "1.0.0+2") for standard CRD users where
	// the Release field is pruned by the API server.
	return ocv1.BundleMetadata{
		Name:    bundleName,
		Version: asLegacyRegistryV1Version(vr).String(),
	}
}

// asLegacyRegistryV1Version converts a VersionRelease into a standard semver version.
// If the VersionRelease's Release field is set, the returned semver version's build
// metadata is set to the VersionRelease's Release. Otherwise, the build metadata is
// set to the VersionRelease's Version field's build metadata.
func asLegacyRegistryV1Version(vr declcfg.VersionRelease) bsemver.Version {
	v := bsemver.Version{
		Major: vr.Version.Major,
		Minor: vr.Version.Minor,
		Patch: vr.Version.Patch,
		Pre:   vr.Version.Pre,
		Build: vr.Version.Build,
	}
	if len(vr.Release) > 0 {
		v.Build = slicesutil.Map(vr.Release, func(pr bsemver.PRVersion) string { return pr.String() })
	}
	return v
}
