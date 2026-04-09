package bundleutil

import (
	"encoding/json"
	"fmt"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
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
		return parseExplicitRelease(pkg.Version, *releaseField.Release)
	}

	// Fall back to legacy registry+v1 behavior (release in build metadata)
	//
	// TODO: For now, we assume that all bundles are registry+v1 bundles.
	//   In the future, for supporting other bundle formats, we should not
	//   use the legacy registry+v1 mechanism (i.e. using build metadata in
	//   the version) to determine the bundle's release.
	vr, err := bundle.NewLegacyRegistryV1VersionRelease(pkg.Version)
	if err != nil {
		return nil, err
	}
	return vr, nil
}

// parseExplicitRelease parses version and release from separate fields.
// Build metadata is preserved in the version because with an explicit release field,
// build metadata serves its proper semver purpose (e.g., git commit, build number).
// In contrast, NewLegacyRegistryV1VersionRelease clears build metadata because it
// interprets build metadata AS the release value for registry+v1 bundles.
func parseExplicitRelease(version, releaseStr string) (*bundle.VersionRelease, error) {
	vers, err := bsemver.Parse(version)
	if err != nil {
		return nil, fmt.Errorf("error parsing version %q: %w", version, err)
	}

	var rel bundle.Release
	if releaseStr == "" {
		// Explicit empty release: use empty slice (not nil)
		rel = bundle.Release([]bsemver.PRVersion{})
	} else {
		rel, err = bundle.NewRelease(releaseStr)
		if err != nil {
			return nil, fmt.Errorf("error parsing release %q: %w", releaseStr, err)
		}
	}

	return &bundle.VersionRelease{
		Version: vers,
		Release: rel,
	}, nil
}

// MetadataFor returns a BundleMetadata for the given bundle name and version/release.
func MetadataFor(bundleName string, vr bundle.VersionRelease) ocv1.BundleMetadata {
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
		Version: vr.AsLegacyRegistryV1Version().String(),
	}
}
