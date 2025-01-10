package bundleutil

import (
	"encoding/json"
	"fmt"

	mmsemver "github.com/Masterminds/semver/v3"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func GetVersion(b declcfg.Bundle) (*mmsemver.Version, error) {
	for _, p := range b.Properties {
		if p.Type == property.TypePackage {
			var pkg property.Package
			if err := json.Unmarshal(p.Value, &pkg); err != nil {
				return nil, fmt.Errorf("error unmarshalling package property: %w", err)
			}
			vers, err := mmsemver.NewVersion(pkg.Version)
			if err != nil {
				return nil, err
			}
			return vers, nil
		}
	}
	return nil, fmt.Errorf("no package property found in bundle %q", b.Name)
}

// MetadataFor returns a BundleMetadata for the given bundle name and version.
func MetadataFor(bundleName string, bundleVersion *mmsemver.Version) ocv1.BundleMetadata {
	return ocv1.BundleMetadata{
		Name:    bundleName,
		Version: bundleVersion.String(),
	}
}
