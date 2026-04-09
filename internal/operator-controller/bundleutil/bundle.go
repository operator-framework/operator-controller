package bundleutil

import (
	"encoding/json"
	"fmt"
	"strings"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

func GetVersionAndRelease(b declcfg.Bundle) (*bundle.VersionRelease, error) {
	for _, p := range b.Properties {
		if p.Type == property.TypePackage {
			return parseVersionRelease(p.Value)
		}
	}
	return nil, fmt.Errorf("no package property found in bundle %q", b.Name)
}

func parseVersionRelease(pkgData json.RawMessage) (*bundle.VersionRelease, error) {
	var pkg property.Package
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		return nil, fmt.Errorf("error unmarshalling package property: %w", err)
	}

	// When BundleReleaseSupport is enabled and bundle has explicit release field, use it.
	// Note: Build metadata is preserved here because with an explicit release field,
	// build metadata serves its proper semver purpose (e.g., git commit, build number).
	// In contrast, NewLegacyRegistryV1VersionRelease clears build metadata because it
	// interprets build metadata AS the release value for registry+v1 bundles.
	if features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport) && pkg.Release != "" {
		vers, err := bsemver.Parse(pkg.Version)
		if err != nil {
			return nil, fmt.Errorf("error parsing version %q: %w", pkg.Version, err)
		}
		rel, err := bundle.NewRelease(pkg.Release)
		if err != nil {
			return nil, fmt.Errorf("error parsing release %q: %w", pkg.Release, err)
		}
		return &bundle.VersionRelease{
			Version: vers,
			Release: rel,
		}, nil
	}

	// Fall back to legacy registry+v1 behavior (release in build metadata)
	vr, err := bundle.NewLegacyRegistryV1VersionRelease(pkg.Version)
	if err != nil {
		return nil, err
	}
	return vr, nil
}

// MetadataFor returns a BundleMetadata for the given bundle name and version/release.
func MetadataFor(bundleName string, vr bundle.VersionRelease) ocv1.BundleMetadata {
	bm := ocv1.BundleMetadata{
		Name:    bundleName,
		Version: vr.Version.String(),
	}
	if len(vr.Release) > 0 {
		parts := slicesutil.Map(vr.Release, func(pr bsemver.PRVersion) string { return pr.String() })
		bm.Release = strings.Join(parts, ".")
	}
	return bm
}
