package variablesources_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
	"github.com/operator-framework/operator-controller/pkg/features"
	testutil "github.com/operator-framework/operator-controller/test/util"
)

func TestInstalledPackageVariableSource(t *testing.T) {
	channel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "stable",
		Entries: []declcfg.ChannelEntry{
			{
				Name: "test-package.v1.0.0",
			},
			{
				Name:     "test-package.v2.0.0",
				Replaces: "test-package.v1.0.0",
			},
			{
				Name:     "test-package.v2.1.0",
				Replaces: "test-package.v2.0.0",
			},
			{
				Name:     "test-package.v2.2.0",
				Replaces: "test-package.v2.1.0",
			},
			{
				Name:     "test-package.v3.0.0",
				Replaces: "test-package.v2.2.0",
			},
			{
				Name:     "test-package.v4.0.0",
				Replaces: "test-package.v3.0.0",
			},
			{
				Name:     "test-package.v5.0.0",
				Replaces: "test-package.v4.0.0",
			},
		},
	}}
	bundleList := []*catalogmetadata.Bundle{
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v1.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v1.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v3.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v3.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v2.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.1.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v2.1.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.1.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.2.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v2.2.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.2.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v4.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v4.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "4.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v5.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v5.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "5-0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&channel},
		},
	}

	fakeCatalogClient := testutil.NewFakeCatalogClient(bundleList)

	t.Run("with ForceSemverUpgradeConstraints feature gate disabled", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()

		pkgName := "test-package"
		bundleName := "test-package.v2.0.0"
		bundleVersion := "2.0.0"
		ipvs := variablesources.NewInstalledPackageVariableSource(&fakeCatalogClient, pkgName, bundleName, bundleVersion)

		variables, err := ipvs.GetVariables(context.TODO())
		require.NoError(t, err)
		require.Len(t, variables, 1)
		packageVariable, ok := variables[0].(*olmvariables.InstalledPackageVariable)
		assert.True(t, ok)
		assert.Equal(t, deppy.IdentifierFromString("installed package test-package"), packageVariable.Identifier())

		// ensure bundles are in version order (high to low)
		bundles := packageVariable.Bundles()
		require.Len(t, bundles, 2)
		assert.Equal(t, "test-package.v2.1.0", packageVariable.Bundles()[0].Name)
		assert.Equal(t, "test-package.v2.0.0", packageVariable.Bundles()[1].Name)
	})
}
