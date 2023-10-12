package variablesources_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
	"github.com/operator-framework/operator-controller/pkg/features"
)

func TestMakeInstalledPackageVariables(t *testing.T) {
	someOtherPackageChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name:    "stable",
		Package: "some-other-package",
		Entries: []declcfg.ChannelEntry{
			{
				Name: "some-other-package.v2.3.0",
			},
		},
	}}
	testPackageChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name:    "stable",
		Package: "test-package",
		Entries: []declcfg.ChannelEntry{
			{
				Name: "test-package.v0.0.1",
			},
			{
				Name:     "test-package.v0.0.2",
				Replaces: "test-package.v0.0.1",
			},
			{
				Name:     "test-package.v0.1.0",
				Replaces: "test-package.v0.0.2",
			},
			{
				Name:     "test-package.v0.1.1",
				Replaces: "test-package.v0.1.0",
			},
			{
				Name:     "test-package.v0.2.0",
				Replaces: "test-package.v0.1.1",
			},
			{
				Name:     "test-package.v1.0.0",
				Replaces: "test-package.v0.2.0",
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
	allBundles := []*catalogmetadata.Bundle{
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v0.0.1",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v0.0.1",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.0.1"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v0.0.2",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v0.0.2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.0.2"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v0.1.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v0.1.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.1.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v0.1.1",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v0.1.1",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.1.1"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v0.2.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v0.2.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.2.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v1.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v1.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v3.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v3.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v2.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.1.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v2.1.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.1.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.2.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v2.2.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.2.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v4.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v4.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "4.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v5.0.0",
			Package: "test-package",
			Image:   "registry.io/repo/test-package@v5.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "5.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "some-other-package.v2.3.0",
			Package: "some-other-package",
			Image:   "registry.io/repo/some-other-package@v2.3.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "2.3.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&someOtherPackageChannel},
		},
	}

	fakeBundleDeployments := func(bundleImages ...string) []rukpakv1alpha1.BundleDeployment {
		bundleDeployments := []rukpakv1alpha1.BundleDeployment{}
		for idx, bundleImage := range bundleImages {
			bd := rukpakv1alpha1.BundleDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("bd-%d", idx),
				},
				Spec: rukpakv1alpha1.BundleDeploymentSpec{
					Template: &rukpakv1alpha1.BundleTemplate{
						Spec: rukpakv1alpha1.BundleSpec{
							Source: rukpakv1alpha1.BundleSource{
								Image: &rukpakv1alpha1.ImageSource{
									Ref: bundleImage,
								},
							},
						},
					},
				},
			}
			bundleDeployments = append(bundleDeployments, bd)
		}

		return bundleDeployments
	}

	t.Run("with ForceSemverUpgradeConstraints feature gate enabled", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)()

		t.Run("with non-zero major version", func(t *testing.T) {
			const bundleImage = "registry.io/repo/test-package@v2.0.0"
			installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, fakeBundleDeployments(bundleImage))
			require.NoError(t, err)

			require.Len(t, installedPackages, 1)
			packageVariable := installedPackages[0]
			assert.Equal(t, deppy.IdentifierFromString("installed package test-package"), packageVariable.Identifier())

			// ensure bundles are in version order (high to low)
			bundles := packageVariable.Bundles()
			require.Len(t, bundles, 3)
			assert.Equal(t, "test-package.v2.2.0", packageVariable.Bundles()[0].Name)
			assert.Equal(t, "test-package.v2.1.0", packageVariable.Bundles()[1].Name)
			assert.Equal(t, "test-package.v2.0.0", packageVariable.Bundles()[2].Name)
		})

		t.Run("with zero major version", func(t *testing.T) {
			t.Run("with zero minor version", func(t *testing.T) {
				const bundleImage = "registry.io/repo/test-package@v0.0.1"
				installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, fakeBundleDeployments(bundleImage))
				require.NoError(t, err)

				require.Len(t, installedPackages, 1)
				packageVariable := installedPackages[0]
				assert.Equal(t, deppy.IdentifierFromString("installed package test-package"), packageVariable.Identifier())

				// No upgrades are allowed in major version zero when minor version is also zero
				bundles := packageVariable.Bundles()
				require.Len(t, bundles, 1)
				assert.Equal(t, "test-package.v0.0.1", packageVariable.Bundles()[0].Name)
			})

			t.Run("with non-zero minor version", func(t *testing.T) {
				const bundleImage = "registry.io/repo/test-package@v0.1.0"
				installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, fakeBundleDeployments(bundleImage))
				require.NoError(t, err)

				require.Len(t, installedPackages, 1)
				packageVariable := installedPackages[0]
				assert.Equal(t, deppy.IdentifierFromString("installed package test-package"), packageVariable.Identifier())

				// Patch version upgrades are allowed, but not minor upgrades
				bundles := packageVariable.Bundles()
				require.Len(t, bundles, 2)
				assert.Equal(t, "test-package.v0.1.1", packageVariable.Bundles()[0].Name)
				assert.Equal(t, "test-package.v0.1.0", packageVariable.Bundles()[1].Name)
			})
		})
	})

	t.Run("with ForceSemverUpgradeConstraints feature gate disabled", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()

		const bundleImage = "registry.io/repo/test-package@v2.0.0"
		installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, fakeBundleDeployments(bundleImage))
		require.NoError(t, err)

		require.Len(t, installedPackages, 1)
		packageVariable := installedPackages[0]
		assert.Equal(t, deppy.IdentifierFromString("installed package test-package"), packageVariable.Identifier())

		// ensure bundles are in version order (high to low)
		bundles := packageVariable.Bundles()
		require.Len(t, bundles, 2)
		assert.Equal(t, "test-package.v2.1.0", packageVariable.Bundles()[0].Name)
		assert.Equal(t, "test-package.v2.0.0", packageVariable.Bundles()[1].Name)
	})

	t.Run("installed bundle not found", func(t *testing.T) {
		const bundleImage = "registry.io/repo/test-package@v9.0.0"
		installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, fakeBundleDeployments(bundleImage))
		assert.Nil(t, installedPackages)
		assert.ErrorContains(t, err, `bundleImage "registry.io/repo/test-package@v9.0.0" not found`)
	})
}
