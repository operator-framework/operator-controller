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

func TestMakeBundleUniquenessVariables(t *testing.T) {
	channel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		// required package bundles
		"bundle-1": {Bundle: declcfg.Bundle{
			Name:    "bundle-1",
			Package: "test-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
				{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bit.io","kind":"Bit","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-2": {Bundle: declcfg.Bundle{
			Name:    "bundle-2",
			Package: "test-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
				{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
				{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`{"group":"bit.io","kind":"Bit","version":"v1"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// dependencies
		"bundle-3": {Bundle: declcfg.Bundle{
			Name:    "bundle-3",
			Package: "some-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-4": {Bundle: declcfg.Bundle{
			Name:    "bundle-4",
			Package: "some-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.5.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-5": {Bundle: declcfg.Bundle{
			Name:    "bundle-5",
			Package: "some-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-6": {Bundle: declcfg.Bundle{
			Name:    "bundle-6",
			Package: "some-other-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-7": {Bundle: declcfg.Bundle{
			Name:    "bundle-7",
			Package: "some-other-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.5.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
				{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"bar.io","kind":"Bar","version":"v1"}`)},
				{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "another-package", "versionRange": "< 2.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// dependencies of dependencies
		"bundle-8": {Bundle: declcfg.Bundle{
			Name:    "bundle-8",
			Package: "another-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "another-package", "version": "1.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-9": {Bundle: declcfg.Bundle{
			Name:    "bundle-9",
			Package: "bar-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "1.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-10": {Bundle: declcfg.Bundle{
			Name:    "bundle-10",
			Package: "bar-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// test-package-2 required package - no dependencies
		"bundle-14": {Bundle: declcfg.Bundle{
			Name:    "bundle-14",
			Package: "test-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.5.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-15": {Bundle: declcfg.Bundle{
			Name:    "bundle-15",
			Package: "test-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.1"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-16": {Bundle: declcfg.Bundle{
			Name:    "bundle-16",
			Package: "test-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "3.16.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},

		// completely unrelated
		"bundle-11": {Bundle: declcfg.Bundle{
			Name:    "bundle-11",
			Package: "unrelated-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package", "version": "2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-12": {Bundle: declcfg.Bundle{
			Name:    "bundle-12",
			Package: "unrelated-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		"bundle-13": {Bundle: declcfg.Bundle{
			Name:    "bundle-13",
			Package: "unrelated-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "3.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
	}

	t.Run("convert bundle variables into create global uniqueness constraint variables", func(t *testing.T) {
		bundleVariables := []*olmvariables.BundleVariable{
			olmvariables.NewBundleVariable(
				bundleSet["bundle-2"],
				[]*catalogmetadata.Bundle{
					bundleSet["bundle-3"],
					bundleSet["bundle-4"],
					bundleSet["bundle-5"],
					bundleSet["bundle-6"],
					bundleSet["bundle-7"],
				},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-1"],
				[]*catalogmetadata.Bundle{
					bundleSet["bundle-6"],
					bundleSet["bundle-7"],
					bundleSet["bundle-8"],
				},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-3"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-4"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-5"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-6"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-7"],
				[]*catalogmetadata.Bundle{
					bundleSet["bundle-8"],
					bundleSet["bundle-9"],
					bundleSet["bundle-10"],
				},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-8"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-9"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-10"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-14"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-15"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-16"],
				[]*catalogmetadata.Bundle{},
			),
		}

		variables, err := variablesources.MakeBundleUniquenessVariables(bundleVariables)
		require.NoError(t, err)

		expectedIDs := []string{
			"test-package package uniqueness",
			"some-package package uniqueness",
			"some-other-package package uniqueness",
			"another-package package uniqueness",
			"bar-package package uniqueness",
			"test-package-2 package uniqueness",
		}
		actualIDs := collectVariableIDs(variables)
		assert.EqualValues(t, expectedIDs, actualIDs)
	})
}
