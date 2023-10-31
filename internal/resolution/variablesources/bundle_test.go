package variablesources_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestMakeBundleVariables_ValidDepedencies(t *testing.T) {
	const fakeCatalogName = "fake-catalog"
	fakeChannel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		// Test package which we will be using as input into
		// the testable function
		"test-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "first-level-dependency", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},

		// First level dependency of test-package. Will be explicitly
		// provided into the testable function as part of variable.
		// This package must have at least one dependency with a version
		// range so we can test that result has correct ordering:
		// the testable function must give priority to newer versions.
		"first-level-dependency.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "first-level-dependency.v1.0.0",
				Package: "first-level-dependency",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "first-level-dependency", "version": "1.0.0"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "second-level-dependency", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},

		// Second level dependency that matches requirements of the first level dependency.
		"second-level-dependency.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "second-level-dependency.v1.0.0",
				Package: "second-level-dependency",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "second-level-dependency", "version": "1.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},

		// Second level dependency that matches requirements of the first level dependency.
		"second-level-dependency.v1.0.1": {
			Bundle: declcfg.Bundle{
				Name:    "second-level-dependency.v1.0.1",
				Package: "second-level-dependency",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "second-level-dependency", "version": "1.0.1"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},

		// Second level dependency that does not match requirements of the first level dependency.
		"second-level-dependency.v2.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "second-level-dependency.v2.0.0",
				Package: "second-level-dependency",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "second-level-dependency", "version": "2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},

		// Package that is in a our fake catalog, but is not involved
		// in this dependency chain. We need this to make sure that
		// the testable function filters out unrelated bundles.
		"uninvolved-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "uninvolved-package.v1.0.0",
				Package: "uninvolved-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "uninvolved-package", "version": "1.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},
	}

	allBundles := make([]*catalogmetadata.Bundle, 0, len(bundleSet))
	for _, bundle := range bundleSet {
		allBundles = append(allBundles, bundle)
	}
	requiredPackages := []*olmvariables.RequiredPackageVariable{
		olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
			bundleSet["first-level-dependency.v1.0.0"],
		}),
	}
	installedPackages := []*olmvariables.InstalledPackageVariable{
		olmvariables.NewInstalledPackageVariable("test-package", []*catalogmetadata.Bundle{
			bundleSet["first-level-dependency.v1.0.0"],
		}),
	}

	bundles, err := variablesources.MakeBundleVariables(allBundles, requiredPackages, installedPackages)
	require.NoError(t, err)

	// Each dependency must have a variable.
	// Dependencies from the same package must be sorted by version
	// with higher versions first.
	expectedVariables := []*olmvariables.BundleVariable{
		olmvariables.NewBundleVariable(
			bundleSet["first-level-dependency.v1.0.0"],
			[]*catalogmetadata.Bundle{
				bundleSet["second-level-dependency.v1.0.1"],
				bundleSet["second-level-dependency.v1.0.0"],
			},
		),
		olmvariables.NewBundleVariable(
			bundleSet["second-level-dependency.v1.0.1"],
			nil,
		),
		olmvariables.NewBundleVariable(
			bundleSet["second-level-dependency.v1.0.0"],
			nil,
		),
	}
	gocmpopts := []cmp.Option{
		cmpopts.IgnoreUnexported(catalogmetadata.Bundle{}),
		cmp.AllowUnexported(
			olmvariables.BundleVariable{},
			input.SimpleVariable{},
			constraint.DependencyConstraint{},
		),
	}
	require.Empty(t, cmp.Diff(bundles, expectedVariables, gocmpopts...))
}

func TestMakeBundleVariables_NonExistentDepedencies(t *testing.T) {
	const fakeCatalogName = "fake-catalog"
	fakeChannel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		"test-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "first-level-dependency", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&fakeChannel},
		},
	}

	allBundles := make([]*catalogmetadata.Bundle, 0, len(bundleSet))
	for _, bundle := range bundleSet {
		allBundles = append(allBundles, bundle)
	}
	requiredPackages := []*olmvariables.RequiredPackageVariable{
		olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
			bundleSet["test-package.v1.0.0"],
		}),
	}
	installedPackages := []*olmvariables.InstalledPackageVariable{}

	bundles, err := variablesources.MakeBundleVariables(allBundles, requiredPackages, installedPackages)
	assert.ErrorContains(t, err, `could not determine dependencies for bundle "test-package.v1.0.0" from package "test-package" in catalog "fake-catalog"`)
	assert.Nil(t, bundles)
}
