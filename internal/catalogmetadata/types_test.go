package catalogmetadata_test

import (
	"encoding/json"
	"fmt"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

func TestBundleVersion(t *testing.T) {
	for _, tt := range []struct {
		name        string
		bundle      *catalogmetadata.Bundle
		wantVersion *bsemver.Version
		wantErr     string
	}{
		{
			name: "valid version",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.v1",
				Properties: []property.Property{
					{
						Type:  property.TypePackage,
						Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
					},
				},
			}},
			wantVersion: &bsemver.Version{Major: 1},
		},
		{
			name: "invalid version",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.invalidVersion",
				Properties: []property.Property{
					{
						Type:  property.TypePackage,
						Value: json.RawMessage(`{"packageName": "package1", "version": "broken"}`),
					},
				},
			}},
			wantVersion: nil,
			wantErr:     `could not parse semver "broken" for bundle 'fake-bundle.invalidVersion': No Major.Minor.Patch elements found`,
		},
		{
			name: "not found",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.noVersion",
			}},
			wantVersion: nil,
			wantErr:     `bundle property with type "olm.package" not found`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			version, err := tt.bundle.Version()
			assert.Equal(t, tt.wantVersion, version)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBundleRequiredPackages(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		bundle               *catalogmetadata.Bundle
		wantRequiredPackages []catalogmetadata.PackageRequired
		wantErr              string
	}{
		{
			name: "valid package requirements",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.v1",
				Properties: []property.Property{
					{
						Type:  property.TypePackageRequired,
						Value: json.RawMessage(`{"packageName": "packageA", "versionRange": ">1.0.0"}`),
					},
					{
						Type:  property.TypePackageRequired,
						Value: json.RawMessage(`{"packageName": "packageB", "versionRange": ">0.5.0 <0.8.6"}`),
					},
				},
			}},
			wantRequiredPackages: []catalogmetadata.PackageRequired{
				{PackageRequired: property.PackageRequired{PackageName: "packageA", VersionRange: ">1.0.0"}},
				{PackageRequired: property.PackageRequired{PackageName: "packageB", VersionRange: ">0.5.0 <0.8.6"}},
			},
		},
		{
			name: "bad package requirement",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.badPackageRequirement",
				Properties: []property.Property{
					{
						Type:  property.TypePackageRequired,
						Value: json.RawMessage(`badRequiredPackageStructure`),
					},
				},
			}},
			wantRequiredPackages: nil,
			wantErr:              `error determining bundle required packages for bundle "fake-bundle.badPackageRequirement": property "olm.package.required" with value "badRequiredPackageStructure" could not be parsed: invalid character 'b' looking for beginning of value`,
		},
		{
			name: "invalid version range",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.badVersionRange",
				Properties: []property.Property{
					{
						Type:  property.TypePackageRequired,
						Value: json.RawMessage(`{"packageName": "packageA", "versionRange": "invalid"}`),
					},
				},
			}},
			wantRequiredPackages: nil,
			wantErr:              `error parsing bundle required package semver range for bundle "fake-bundle.badVersionRange" (required package "packageA"): Could not get version from string: "invalid"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			packages, err := tt.bundle.RequiredPackages()
			assert.Len(t, packages, len(tt.wantRequiredPackages))
			for i := range packages {
				// SemverRange is a function which is not comparable
				// so we just make sure that it is set.
				assert.NotNil(t, packages[i].SemverRange)

				// And then we set it to nil for ease of further comparisons
				packages[i].SemverRange = nil
			}

			assert.Equal(t, tt.wantRequiredPackages, packages)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBundleMediaType(t *testing.T) {
	for _, tt := range []struct {
		name          string
		bundle        *catalogmetadata.Bundle
		wantMediaType string
		wantErr       string
	}{
		{
			name: "valid mediatype property",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.v1",
				Properties: []property.Property{
					{
						Type:  catalogmetadata.PropertyBundleMediaType,
						Value: json.RawMessage(fmt.Sprintf(`"%s"`, catalogmetadata.MediaTypePlain)),
					},
				},
			}},
			wantMediaType: catalogmetadata.MediaTypePlain,
		},
		{
			name: "no media type provided",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name:       "fake-bundle.noMediaType",
				Properties: []property.Property{},
			}},
			wantMediaType: "",
		},
		{
			name: "malformed media type",
			bundle: &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Name: "fake-bundle.badMediaType",
				Properties: []property.Property{
					{
						Type:  catalogmetadata.PropertyBundleMediaType,
						Value: json.RawMessage("badtype"),
					},
				},
			}},
			wantMediaType: "",
			wantErr:       `error determining bundle mediatype for bundle "fake-bundle.badMediaType": property "olm.bundle.mediatype" with value "badtype" could not be parsed: invalid character 'b' looking for beginning of value`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mediaType, err := tt.bundle.MediaType()
			assert.Equal(t, tt.wantMediaType, mediaType)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
