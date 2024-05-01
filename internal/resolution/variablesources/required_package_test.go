package variablesources_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/packageerrors"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestMakeRequiredPackageVariables(t *testing.T) {
	stableChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "stable",
	}}
	betaChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "beta",
	}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		// Bundles which belong to test-package we will be using
		// to assert wether the testable function filters out the data
		// correctly.
		"test-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
		"test-package.v3.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v3.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel, &betaChannel},
		},
		"test-package.v2.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
		"test-package.v4.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v4.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "4.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
			Deprecations: []declcfg.DeprecationEntry{
				{
					Reference: declcfg.PackageScopedReference{
						Schema: declcfg.SchemaBundle,
						Name:   "test-package.v4.0.0",
					},
					Message: "test-package.v4.0.0 has been deprecated",
				},
			},
		},
		"test-package.v4.1.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v4.1.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "4.1.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
			Deprecations: []declcfg.DeprecationEntry{
				{
					Reference: declcfg.PackageScopedReference{
						Schema: declcfg.SchemaBundle,
						Name:   "test-package.v4.1.0",
					},
					Message: "test-package.v4.1.0 has been deprecated",
				},
			},
		},
		"test-package.v5.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v5.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "5.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
			Deprecations: []declcfg.DeprecationEntry{
				{
					Reference: declcfg.PackageScopedReference{
						Schema: declcfg.SchemaBundle,
						Name:   "test-package.v5.0.0",
					},
					Message: "test-package.v5.0.0 has been deprecated",
				},
			},
		},

		// We need at least one bundle from different package
		// to make sure that we are filtering it out.
		"test-package-2.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package-2.v1.0.0",
				Package: "test-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&stableChannel},
		},
	}
	allBundles := make([]*catalogmetadata.Bundle, 0, len(bundleSet))
	for _, bundle := range bundleSet {
		allBundles = append(allBundles, bundle)
	}

	fakeClusterExtension := func(packageName, channelName, versionRange string) ocv1alpha1.ClusterExtension {
		return ocv1alpha1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("op-%s-%s-%s", packageName, channelName, versionRange),
			},
			Spec: ocv1alpha1.ClusterExtensionSpec{
				PackageName: packageName,
				Version:     versionRange,
				Channel:     channelName,
			},
		}
	}

	for _, tt := range []struct {
		name              string
		clusterExtensions []ocv1alpha1.ClusterExtension
		expectedResult    []*olmvariables.RequiredPackageVariable
		expectedError     string
	}{
		{
			name: "package name only",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("test-package", "", ""),
			},
			expectedResult: []*olmvariables.RequiredPackageVariable{
				olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
					bundleSet["test-package.v3.0.0"],
					bundleSet["test-package.v2.0.0"],
					bundleSet["test-package.v1.0.0"],
					bundleSet["test-package.v5.0.0"],
					bundleSet["test-package.v4.1.0"],
					bundleSet["test-package.v4.0.0"],
				}),
			},
		},
		{
			name: "package name and channel",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("test-package", "beta", ""),
			},
			expectedResult: []*olmvariables.RequiredPackageVariable{
				olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
					bundleSet["test-package.v3.0.0"],
				}),
			},
		},
		{
			name: "package name and version range",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("test-package", "", ">=1.0.0 !=2.0.0 <3.0.0"),
			},
			expectedResult: []*olmvariables.RequiredPackageVariable{
				olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
					bundleSet["test-package.v1.0.0"],
				}),
			},
		},
		{
			name: "package name and invalid version range",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("test-package", "", "not a valid semver"),
			},
			expectedError: `invalid version range "not a valid semver"`,
		},
		{
			name: "not found: package name only",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("non-existent-test-package", "", ""),
			},
			expectedError: packageerrors.GenerateError("non-existent-test-package").Error(),
		},
		{
			name: "not found: package name and channel",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("non-existent-test-package", "stable", ""),
			},
			expectedError: packageerrors.GenerateChannelError("non-existent-test-package", "stable").Error(),
		},
		{
			name: "not found: package name and version range",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("non-existent-test-package", "", "1.0.0"),
			},
			expectedError: packageerrors.GenerateVersionError("non-existent-test-package", "1.0.0").Error(),
		},
		{
			name: "not found: package name with channel and version range",
			clusterExtensions: []ocv1alpha1.ClusterExtension{
				fakeClusterExtension("non-existent-test-package", "stable", "1.0.0"),
			},
			expectedError: packageerrors.GenerateVersionChannelError("non-existent-test-package", "1.0.0", "stable").Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vars, err := variablesources.MakeRequiredPackageVariables(allBundles, tt.clusterExtensions)
			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.expectedError)
			}

			gocmpopts := []cmp.Option{
				cmpopts.IgnoreUnexported(catalogmetadata.Bundle{}),
				cmp.AllowUnexported(
					olmvariables.RequiredPackageVariable{},
					input.SimpleVariable{},
					constraint.DependencyConstraint{},
				),
			}
			require.Empty(t, cmp.Diff(vars, tt.expectedResult, gocmpopts...))
		})
	}
}
