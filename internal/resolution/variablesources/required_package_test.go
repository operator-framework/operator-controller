package variablesources_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestMakeRequiredPackageVariables(t *testing.T) {
	stableChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "stable",
	}}
	betaChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "beta",
	}}
	allBundles := []*catalogmetadata.Bundle{
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v1.0.0",
			Package: "test-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v3.0.0",
			Package: "test-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&stableChannel, &betaChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package.v2.0.0",
			Package: "test-package",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
		// add some bundles from a different package
		{Bundle: declcfg.Bundle{
			Name:    "test-package-2.v1.0.0",
			Package: "test-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
		{Bundle: declcfg.Bundle{
			Name:    "test-package-2.v2.0.0",
			Package: "test-package-2",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.0"}`)},
			}},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
	}

	fakeOperator := func(packageName, channelName, versionRange string) operatorsv1alpha1.Operator {
		return operatorsv1alpha1.Operator{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("op-%s-%s-%s", packageName, channelName, versionRange),
			},
			Spec: operatorsv1alpha1.OperatorSpec{
				PackageName: packageName,
				Version:     versionRange,
				Channel:     channelName,
			},
		}
	}

	t.Run("package name only", func(t *testing.T) {
		vars, err := variablesources.MakeRequiredPackageVariables(allBundles, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "", ""),
		})
		require.NoError(t, err)
		require.Len(t, vars, 1)

		reqPackageVar := vars[0]
		assert.Equal(t, deppy.IdentifierFromString("required package test-package"), reqPackageVar.Identifier())

		bundles := reqPackageVar.Bundles()
		require.Len(t, bundles, 3)
		// ensure bundles are in version order (high to low)
		assert.Equal(t, "test-package.v3.0.0", bundles[0].Name)
		assert.Equal(t, "test-package.v2.0.0", bundles[1].Name)
		assert.Equal(t, "test-package.v1.0.0", bundles[2].Name)
	})

	t.Run("package name and channel", func(t *testing.T) {
		vars, err := variablesources.MakeRequiredPackageVariables(allBundles, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "beta", ""),
		})
		require.NoError(t, err)
		require.Len(t, vars, 1)

		reqPackageVar := vars[0]
		assert.Equal(t, deppy.IdentifierFromString("required package test-package"), reqPackageVar.Identifier())

		bundles := reqPackageVar.Bundles()
		require.Len(t, bundles, 1)
		// ensure bundles are in version order (high to low)
		assert.Equal(t, "test-package.v3.0.0", bundles[0].Name)
	})

	t.Run("package name and version range", func(t *testing.T) {
		vars, err := variablesources.MakeRequiredPackageVariables(allBundles, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "", ">=1.0.0 !=2.0.0 <3.0.0"),
		})
		require.NoError(t, err)
		require.Len(t, vars, 1)

		reqPackageVar := vars[0]
		assert.Equal(t, deppy.IdentifierFromString("required package test-package"), reqPackageVar.Identifier())

		bundles := reqPackageVar.Bundles()
		require.Len(t, bundles, 1)
		// test-package.v1.0.0 is the only package that matches the provided filter
		assert.Equal(t, "test-package.v1.0.0", bundles[0].Name)
	})

	t.Run("package name and invalid version range", func(t *testing.T) {
		vars, err := variablesources.MakeRequiredPackageVariables(allBundles, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "", "not a valid semver"),
		})
		assert.Nil(t, vars)
		assert.Error(t, err)
	})

	t.Run("package not found", func(t *testing.T) {
		vars, err := variablesources.MakeRequiredPackageVariables([]*catalogmetadata.Bundle{}, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "", ""),
		})
		assert.Nil(t, vars)
		assert.ErrorContains(t, err, "no package 'test-package' found")

		vars, err = variablesources.MakeRequiredPackageVariables([]*catalogmetadata.Bundle{}, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "stable", ""),
		})
		assert.Nil(t, vars)
		assert.ErrorContains(t, err, "no package 'test-package' found in channel 'stable'")

		vars, err = variablesources.MakeRequiredPackageVariables([]*catalogmetadata.Bundle{}, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "", "1.0.0"),
		})
		assert.Nil(t, vars)
		assert.ErrorContains(t, err, "no package 'test-package' matching version '1.0.0' found")

		vars, err = variablesources.MakeRequiredPackageVariables([]*catalogmetadata.Bundle{}, []operatorsv1alpha1.Operator{
			fakeOperator("test-package", "stable", "1.0.0"),
		})
		assert.Nil(t, vars)
		assert.ErrorContains(t, err, "no package 'test-package' matching version '1.0.0' found in channel 'stable'")
	})
}
