package variablesources_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestMakeBundleUniquenessVariables(t *testing.T) {
	const fakeCatalogName = "fake-catalog"
	channel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		"test-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&channel},
		},
		"test-package.v1.0.1": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.1",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.1"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&channel},
		},

		"some-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "some-package.v1.0.0",
				Package: "some-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&channel},
		},
	}

	// Input into the testable function must include more than one bundle
	// from the same package to ensure that the function
	// enforces uniqueness correctly.
	// We also need at least one bundle variable to have a dependency
	// on another package. This will help to make sure that the function
	// also creates uniqueness variables for dependencies.
	bundleVariables := []*olmvariables.BundleVariable{
		olmvariables.NewBundleVariable(
			bundleSet["test-package.v1.0.0"],
			[]*catalogmetadata.Bundle{
				bundleSet["some-package.v1.0.0"],
			},
		),
		olmvariables.NewBundleVariable(
			bundleSet["test-package.v1.0.1"],
			[]*catalogmetadata.Bundle{
				bundleSet["some-package.v1.0.0"],
			},
		),
	}

	variables := variablesources.MakeBundleUniquenessVariables(bundleVariables)

	// Each package in the input must have own uniqueness variable.
	// Each variable expected to have one constraint enforcing at most one
	// of the involved bundles to be allowed in the solution
	expectedVariables := []*olmvariables.BundleUniquenessVariable{
		{
			SimpleVariable: input.NewSimpleVariable(
				"test-package package uniqueness",
				constraint.AtMost(
					1,
					deppy.Identifier("fake-catalog-test-package-test-package.v1.0.0"),
					deppy.Identifier("fake-catalog-test-package-test-package.v1.0.1"),
				),
			),
		},
		{
			SimpleVariable: input.NewSimpleVariable(
				"some-package package uniqueness",
				constraint.AtMost(
					1,
					deppy.Identifier("fake-catalog-some-package-some-package.v1.0.0"),
				),
			),
		},
	}
	require.Empty(t, cmp.Diff(variables, expectedVariables, cmp.AllowUnexported(input.SimpleVariable{}, constraint.AtMostConstraint{})))
}
