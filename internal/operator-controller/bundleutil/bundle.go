package bundleutil

import (
	"encoding/json"
	"fmt"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
)

func GetVersionAndRelease(b declcfg.Bundle) (*bundle.VersionRelease, error) {
	for _, p := range b.Properties {
		if p.Type == property.TypePackage {
			var pkg property.Package
			if err := json.Unmarshal(p.Value, &pkg); err != nil {
				return nil, fmt.Errorf("error unmarshalling package property: %w", err)
			}

			// TODO: For now, we assume that all bundles are registry+v1 bundles.
			//   In the future, when we support other bundle formats, we should stop
			//   using the legacy mechanism (i.e. using build metadata in the version)
			//   to determine the bundle's release.
			vr, err := bundle.NewLegacyRegistryV1VersionRelease(pkg.Version)
			if err != nil {
				return nil, err
			}
			return vr, nil
		}
	}
	return nil, fmt.Errorf("no package property found in bundle %q", b.Name)
}

// MetadataFor returns a BundleMetadata for the given bundle name and version.
func MetadataFor(bundleName string, bundleVersion bsemver.Version) ocv1.BundleMetadata {
	return ocv1.BundleMetadata{
		Name:    bundleName,
		Version: bundleVersion.String(),
	}
}
