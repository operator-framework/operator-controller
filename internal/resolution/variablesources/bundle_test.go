package variablesources_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestMakeBundleVariables(t *testing.T) {
	channel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
	allBundles := []*catalogmetadata.Bundle{
		// required package bundles
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-1",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
					{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-2",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
					{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// dependencies
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-4",
				Package: "some-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-5",
				Package: "some-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.5.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-6",
				Package: "some-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-7",
				Package: "some-other-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.0.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-8",
				Package: "some-other-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.5.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
					{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"bar.io","kind":"Bar","version":"v1"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "another-package", "versionRange": "< 2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// dependencies of dependencies
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name: "bundle-9", Package: "another-package", Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "another-package", "version": "1.0.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-10",
				Package: "bar-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "1.0.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-11",
				Package: "bar-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "2.0.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// test-package-2 required package - no dependencies
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-15",
				Package: "test-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.5.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-16",
				Package: "test-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.1"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-17",
				Package: "test-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "3.16.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// completely unrelated
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-12",
				Package: "unrelated-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package", "version": "2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-13",
				Package: "unrelated-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{
			CatalogName: "fake-catalog",
			Bundle: declcfg.Bundle{
				Name:    "bundle-14",
				Package: "unrelated-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "3.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
	}

	t.Run("valid dependencies", func(t *testing.T) {
		requiredPackages := []*olmvariables.RequiredPackageVariable{
			olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
				{
					CatalogName: "fake-catalog",
					Bundle: declcfg.Bundle{
						Name:    "bundle-2",
						Package: "test-package",
						Properties: []property.Property{
							{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
							{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
							{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
						},
					},
					InChannels: []*catalogmetadata.Channel{&channel},
				},
				{
					CatalogName: "fake-catalog",
					Bundle: declcfg.Bundle{
						Name:    "bundle-1",
						Package: "test-package",
						Properties: []property.Property{
							{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
							{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
						},
					},
					InChannels: []*catalogmetadata.Channel{&channel},
				},
			}),
		}
		installedPackages := []*olmvariables.InstalledPackageVariable{
			olmvariables.NewInstalledPackageVariable("test-package-2", []*catalogmetadata.Bundle{
				// test-package-2 required package - no dependencies
				{
					CatalogName: "fake-catalog",
					Bundle: declcfg.Bundle{
						Name:    "bundle-15",
						Package: "test-package-2",
						Properties: []property.Property{
							{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.5.0"}`)},
						},
					},
					InChannels: []*catalogmetadata.Channel{&channel},
				},
				{
					CatalogName: "fake-catalog",
					Bundle: declcfg.Bundle{
						Name:    "bundle-16",
						Package: "test-package-2",
						Properties: []property.Property{
							{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.1"}`)},
						},
					},
					InChannels: []*catalogmetadata.Channel{&channel},
				},
				{
					CatalogName: "fake-catalog",
					Bundle: declcfg.Bundle{
						Name:    "bundle-17",
						Package: "test-package-2",
						Properties: []property.Property{
							{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "3.16.0"}`)},
						},
					},
					InChannels: []*catalogmetadata.Channel{&channel},
				},
			}),
		}

		bundles, err := variablesources.MakeBundleVariables(allBundles, requiredPackages, installedPackages)
		require.NoError(t, err)

		// Note: When accounting for Required GVKs (currently not implemented), we would expect additional
		// dependencies to appear here due to their GVKs being required by some of the packages.
		expectedIDs := []string{
			"fake-catalog-test-package-bundle-2",
			"fake-catalog-test-package-bundle-1",
			"fake-catalog-test-package-2-bundle-15",
			"fake-catalog-test-package-2-bundle-16",
			"fake-catalog-test-package-2-bundle-17",
			"fake-catalog-some-package-bundle-5",
			"fake-catalog-some-package-bundle-4",
		}
		actualIDs := collectVariableIDs(bundles)
		assert.EqualValues(t, expectedIDs, actualIDs)

		// check dependencies for one of the bundles
		bundle2 := findVariableWithName(bundles, "bundle-2")
		// Note: As above, bundle-2 has GVK requirements satisfied by bundles 7, 8, and 9, but they
		// will not appear in this list as we are not currently taking Required GVKs into account
		dependencies := bundle2.Dependencies()
		require.Len(t, dependencies, 2)
		assert.Equal(t, "bundle-5", dependencies[0].Name)
		assert.Equal(t, "bundle-4", dependencies[1].Name)
	})

	t.Run("non existent dependencies", func(t *testing.T) {
		allBundles := []*catalogmetadata.Bundle{}
		requiredPackages := []*olmvariables.RequiredPackageVariable{
			olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
				{
					CatalogName: "fake-catalog",
					Bundle: declcfg.Bundle{
						Name:    "bundle-2",
						Package: "test-package",
						Properties: []property.Property{
							{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
							{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
							{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
						},
					},
					InChannels: []*catalogmetadata.Channel{{Channel: declcfg.Channel{Name: "stable"}}},
				},
			}),
		}
		installedPackages := []*olmvariables.InstalledPackageVariable{}

		bundles, err := variablesources.MakeBundleVariables(allBundles, requiredPackages, installedPackages)
		assert.ErrorContains(t, err, "could not determine dependencies for bundle")
		assert.Nil(t, bundles)
	})
}
