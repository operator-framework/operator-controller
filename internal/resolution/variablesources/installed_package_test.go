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
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
	"github.com/operator-framework/operator-controller/pkg/features"
)

func TestMakeInstalledPackageVariablesWithForceSemverUpgradeConstraintsEnabled(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)()

	const testPackageName = "test-package"
	someOtherPackageChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name:    "stable",
		Package: "some-other-package",
	}}
	testPackageChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name:    "stable",
		Package: testPackageName,
	}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		// Major version zero is for initial development and
		// has different update behaviour than versions >= 1.0.0:
		// - In versions 0.0.y updates are not allowed when using semver constraints
		// - In versions 0.x.y only patch updates are allowed (>= 0.x.y and < 0.x+1.0)
		// This means that we need in test data bundles that cover these three version ranges.
		"test-package.v0.0.1": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v0.0.1",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v0.0.1",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.0.1"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v0.0.2": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v0.0.2",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v0.0.2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.0.2"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v0.1.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v0.1.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v0.1.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.1.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v0.1.1": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v0.1.1",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v0.1.1",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.1.1"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v0.1.2": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v0.1.2",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v0.1.2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.1.2"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v0.2.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v0.2.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v0.2.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "0.2.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v2.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.0.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v2.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v2.1.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.1.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v2.1.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.1.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v2.2.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.2.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v2.2.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.2.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		// We need a bundle with a different major version to ensure
		// that we do not allow upgrades from one major version to another
		"test-package.v3.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v3.0.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v3.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		// We need a bundle from different package to ensure that
		// we filter out bundles certain bundle image
		"some-other-package.v2.3.0": {
			Bundle: declcfg.Bundle{
				Name:    "some-other-package.v2.3.0",
				Package: "some-other-package",
				Image:   "registry.io/repo/some-other-package@v2.3.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "2.3.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&someOtherPackageChannel},
		},
	}
	allBundles := make([]*catalogmetadata.Bundle, 0, len(bundleSet))
	for _, bundle := range bundleSet {
		allBundles = append(allBundles, bundle)
	}

	for _, tt := range []struct {
		name                    string
		upgradeConstraintPolicy ocv1alpha1.UpgradeConstraintPolicy
		installedBundle         *catalogmetadata.Bundle
		expectedResult          []*olmvariables.InstalledPackageVariable
		expectedError           string
	}{
		{
			name:                    "with non-zero major version",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			installedBundle:         bundleSet["test-package.v2.0.0"],
			expectedResult: []*olmvariables.InstalledPackageVariable{
				olmvariables.NewInstalledPackageVariable(testPackageName, []*catalogmetadata.Bundle{
					// Updates are allowed within the major version.
					// Ensure bundles are in version order (high to low)
					// with current version at the end
					bundleSet["test-package.v2.2.0"],
					bundleSet["test-package.v2.1.0"],
					bundleSet["test-package.v2.0.0"],
				}),
			},
		},
		{
			name:                    "with zero major and zero minor version",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			installedBundle:         bundleSet["test-package.v0.0.1"],
			expectedResult: []*olmvariables.InstalledPackageVariable{
				olmvariables.NewInstalledPackageVariable(testPackageName, []*catalogmetadata.Bundle{
					// No updates are allowed in major version zero when minor version is also zero
					bundleSet["test-package.v0.0.1"],
				}),
			},
		},
		{
			name:                    "with zero major and non-zero minor version",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			installedBundle:         bundleSet["test-package.v0.1.0"],
			expectedResult: []*olmvariables.InstalledPackageVariable{
				olmvariables.NewInstalledPackageVariable(testPackageName, []*catalogmetadata.Bundle{
					// Patch version updates are allowed within the minor version
					// Ensure bundles are in version order (high to low)
					// with current version at the end.
					bundleSet["test-package.v0.1.2"],
					bundleSet["test-package.v0.1.1"],
					bundleSet["test-package.v0.1.0"],
				}),
			},
		},
		{
			name:                    "UpgradeConstraintPolicy is set to Ignore",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyIgnore,
			installedBundle:         bundleSet["test-package.v2.0.0"],
			expectedResult:          []*olmvariables.InstalledPackageVariable{},
		},
		{
			name:                    "no BundleDeployment for an ClusterExtension",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			expectedResult:          []*olmvariables.InstalledPackageVariable{},
		},
		{
			name:                    "installed bundle not found",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			installedBundle: &catalogmetadata.Bundle{
				Bundle: declcfg.Bundle{
					Name:    "test-package.v9.0.0",
					Package: testPackageName,
					Image:   "registry.io/repo/test-package@v9.0.0",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "9.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&testPackageChannel},
			},
			expectedError: `bundle with image "registry.io/repo/test-package@v9.0.0" for package "test-package" not found in available catalogs but is currently installed via BundleDeployment "test-package-bd"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeOwnerClusterExtension := fakeClusterExtension("test-extension-semver", testPackageName, tt.upgradeConstraintPolicy)
			bundleDeployments := []rukpakv1alpha2.BundleDeployment{}
			if tt.installedBundle != nil {
				bundleDeployments = append(bundleDeployments, fakeBundleDeployment("test-package-bd", tt.installedBundle.Image, &fakeOwnerClusterExtension))
			}

			installedPackages, err := variablesources.MakeInstalledPackageVariables(
				allBundles,
				[]ocv1alpha1.ClusterExtension{fakeOwnerClusterExtension},
				bundleDeployments,
			)
			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.expectedError)
			}

			gocmpopts := []cmp.Option{
				cmpopts.IgnoreUnexported(catalogmetadata.Bundle{}),
				cmp.AllowUnexported(
					olmvariables.InstalledPackageVariable{},
					input.SimpleVariable{},
					constraint.DependencyConstraint{},
				),
			}
			require.Empty(t, cmp.Diff(installedPackages, tt.expectedResult, gocmpopts...))
		})
	}
}

func TestMakeInstalledPackageVariablesWithForceSemverUpgradeConstraintsDisabled(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()

	const testPackageName = "test-package"
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
		Package: testPackageName,
		Entries: []declcfg.ChannelEntry{
			{
				Name: "test-package.v2.0.0",
			},
			{
				Name:     "test-package.v2.1.0",
				Replaces: "test-package.v2.0.0",
			},
			{
				Name:     "test-package.v2.2.0",
				Replaces: "test-package.v2.1.0",
			},
		},
	}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		"test-package.v2.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.0.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v2.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v2.1.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.1.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v2.1.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.1.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		"test-package.v2.2.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v2.2.0",
				Package: testPackageName,
				Image:   "registry.io/repo/test-package@v2.2.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.2.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&testPackageChannel},
		},
		// We need a bundle from different package to ensure that
		// we filter out bundles certain bundle image
		"some-other-package.v2.3.0": {
			Bundle: declcfg.Bundle{
				Name:    "some-other-package.v2.3.0",
				Package: "some-other-package",
				Image:   "registry.io/repo/some-other-package@v2.3.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "2.3.0"}`)},
				},
			},
			InChannels: []*catalogmetadata.Channel{&someOtherPackageChannel},
		},
	}
	allBundles := make([]*catalogmetadata.Bundle, 0, len(bundleSet))
	for _, bundle := range bundleSet {
		allBundles = append(allBundles, bundle)
	}

	for _, tt := range []struct {
		name                    string
		upgradeConstraintPolicy ocv1alpha1.UpgradeConstraintPolicy
		installedBundle         *catalogmetadata.Bundle
		expectedResult          []*olmvariables.InstalledPackageVariable
		expectedError           string
	}{
		{
			name:                    "respect replaces directive from catalog",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			installedBundle:         bundleSet["test-package.v2.0.0"],
			expectedResult: []*olmvariables.InstalledPackageVariable{
				olmvariables.NewInstalledPackageVariable(testPackageName, []*catalogmetadata.Bundle{
					// Must only have two bundle:
					// - the one which replaces the current version
					// - the current version (to allow to stay on the current version)
					bundleSet["test-package.v2.1.0"],
					bundleSet["test-package.v2.0.0"],
				}),
			},
		},
		{
			name:                    "UpgradeConstraintPolicy is set to Ignore",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyIgnore,
			installedBundle:         bundleSet["test-package.v2.0.0"],
			expectedResult:          []*olmvariables.InstalledPackageVariable{},
		},
		{
			name:                    "no BundleDeployment for an ClusterExtension",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			expectedResult:          []*olmvariables.InstalledPackageVariable{},
		},
		{
			name:                    "installed bundle not found",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyEnforce,
			installedBundle: &catalogmetadata.Bundle{
				Bundle: declcfg.Bundle{
					Name:    "test-package.v9.0.0",
					Package: testPackageName,
					Image:   "registry.io/repo/test-package@v9.0.0",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "9.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&testPackageChannel},
			},
			expectedError: `bundle with image "registry.io/repo/test-package@v9.0.0" for package "test-package" not found in available catalogs but is currently installed via BundleDeployment "test-package-bd"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeOwnerClusterExtension := fakeClusterExtension("test-extension-legacy", testPackageName, tt.upgradeConstraintPolicy)
			bundleDeployments := []rukpakv1alpha2.BundleDeployment{}
			if tt.installedBundle != nil {
				bundleDeployments = append(bundleDeployments, fakeBundleDeployment("test-package-bd", tt.installedBundle.Image, &fakeOwnerClusterExtension))
			}

			installedPackages, err := variablesources.MakeInstalledPackageVariables(
				allBundles,
				[]ocv1alpha1.ClusterExtension{fakeOwnerClusterExtension},
				bundleDeployments,
			)
			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.expectedError)
			}

			gocmpopts := []cmp.Option{
				cmpopts.IgnoreUnexported(catalogmetadata.Bundle{}),
				cmp.AllowUnexported(
					olmvariables.InstalledPackageVariable{},
					input.SimpleVariable{},
					constraint.DependencyConstraint{},
				),
			}
			require.Empty(t, cmp.Diff(installedPackages, tt.expectedResult, gocmpopts...))
		})
	}
}
