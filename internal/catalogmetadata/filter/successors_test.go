package filter

import (
	"slices"
	"testing"

	mmsemver "github.com/Masterminds/semver/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/bundleutil"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/compare"
	"github.com/operator-framework/operator-controller/internal/features"
)

func TestSuccessorsPredicateWithForceSemverUpgradeConstraintsEnabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)

	const testPackageName = "test-package"
	channelSet := map[string]declcfg.Channel{
		testPackageName: {
			Package: testPackageName,
			Name:    "stable",
		},
	}

	bundleSet := map[string]declcfg.Bundle{
		// Major version zero is for initial development and
		// has different update behaviour than versions >= 1.0.0:
		// - In versions 0.0.y updates are not allowed when using semver constraints
		// - In versions 0.x.y only patch updates are allowed (>= 0.x.y and < 0.x+1.0)
		// This means that we need in test data bundles that cover these three version ranges.
		"test-package.v0.0.1": {
			Name:    "test-package.v0.0.1",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v0.0.1",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "0.0.1"),
			},
		},
		"test-package.v0.0.2": {
			Name:    "test-package.v0.0.2",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v0.0.2",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "0.0.2"),
			},
		},
		"test-package.v0.1.0": {
			Name:    "test-package.v0.1.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v0.1.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "0.1.0"),
			},
		},
		"test-package.v0.1.1": {
			Name:    "test-package.v0.1.1",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v0.1.1",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "0.1.1"),
			},
		},
		"test-package.v0.1.2": {
			Name:    "test-package.v0.1.2",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v0.1.2",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "0.1.2"),
			},
		},
		"test-package.v0.2.0": {
			Name:    "test-package.v0.2.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v0.2.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "0.2.0"),
			},
		},
		"test-package.v2.0.0": {
			Name:    "test-package.v2.0.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.0.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.0.0"),
			},
		},
		"test-package.v2.1.0": {
			Name:    "test-package.v2.1.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.1.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.1.0"),
			},
		},
		"test-package.v2.2.0": {
			Name:    "test-package.v2.2.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.2.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.2.0"),
			},
		},
		// We need a bundle with a different major version to ensure
		// that we do not allow upgrades from one major version to another
		"test-package.v3.0.0": {
			Name:    "test-package.v3.0.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v3.0.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "3.0.0"),
			},
		},
	}

	for _, b := range bundleSet {
		ch := channelSet[b.Package]
		ch.Entries = append(ch.Entries, declcfg.ChannelEntry{Name: b.Name})
		channelSet[b.Package] = ch
	}

	for _, tt := range []struct {
		name            string
		installedBundle ocv1.BundleMetadata
		expectedResult  []declcfg.Bundle
	}{
		{
			name:            "with non-zero major version",
			installedBundle: bundleutil.MetadataFor("test-package.v2.0.0", mmsemver.MustParse("2.0.0")),
			expectedResult: []declcfg.Bundle{
				// Updates are allowed within the major version
				bundleSet["test-package.v2.2.0"],
				bundleSet["test-package.v2.1.0"],
				bundleSet["test-package.v2.0.0"],
			},
		},
		{
			name:            "with zero major and zero minor version",
			installedBundle: bundleutil.MetadataFor("test-package.v0.0.1", mmsemver.MustParse("0.0.1")),
			expectedResult: []declcfg.Bundle{
				// No updates are allowed in major version zero when minor version is also zero
				bundleSet["test-package.v0.0.1"],
			},
		},
		{
			name:            "with zero major and non-zero minor version",
			installedBundle: bundleutil.MetadataFor("test-package.v0.1.0", mmsemver.MustParse("0.1.0")),
			expectedResult: []declcfg.Bundle{
				// Patch version updates are allowed within the minor version
				bundleSet["test-package.v0.1.2"],
				bundleSet["test-package.v0.1.1"],
				bundleSet["test-package.v0.1.0"],
			},
		},
		{
			name: "installed bundle not found",
			installedBundle: ocv1.BundleMetadata{
				Name:    "test-package.v9.0.0",
				Version: "9.0.0",
			},
			expectedResult: []declcfg.Bundle{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			successors, err := SuccessorsOf(tt.installedBundle, channelSet[testPackageName])
			require.NoError(t, err)

			allBundles := make([]declcfg.Bundle, 0, len(bundleSet))
			for _, bundle := range bundleSet {
				allBundles = append(allBundles, bundle)
			}
			result := Filter(allBundles, successors)

			// sort before comparison for stable order
			slices.SortFunc(result, compare.ByVersion)

			gocmpopts := []cmp.Option{
				cmpopts.IgnoreUnexported(declcfg.Bundle{}),
			}
			require.Empty(t, cmp.Diff(result, tt.expectedResult, gocmpopts...))
		})
	}
}

func TestSuccessorsPredicateWithForceSemverUpgradeConstraintsDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)

	const testPackageName = "test-package"
	channelSet := map[string]declcfg.Channel{
		testPackageName: {
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
				{
					Name: "test-package.v2.2.1",
				},
				{
					Name:     "test-package.v2.3.0",
					Replaces: "test-package.v2.2.0",
					Skips: []string{
						"test-package.v2.2.1",
					},
				},
				{
					Name:      "test-package.v2.4.0",
					SkipRange: ">=2.3.0 <2.4.0",
				},
			},
		},
	}

	bundleSet := map[string]declcfg.Bundle{
		"test-package.v2.0.0": {
			Name:    "test-package.v2.0.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.0.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.0.0"),
			},
		},
		"test-package.v2.1.0": {
			Name:    "test-package.v2.1.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.1.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.1.0"),
			},
		},
		"test-package.v2.2.0": {
			Name:    "test-package.v2.2.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.2.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.2.0"),
			},
		},
		"test-package.v2.2.1": {
			Name:    "test-package.v2.2.1",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.2.1",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.2.1"),
			},
		},
		"test-package.v2.3.0": {
			Name:    "test-package.v2.3.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.3.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.3.0"),
			},
		},
		"test-package.v2.4.0": {
			Name:    "test-package.v2.4.0",
			Package: testPackageName,
			Image:   "registry.io/repo/test-package@v2.4.0",
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.4.0"),
			},
		},
	}

	for _, tt := range []struct {
		name            string
		installedBundle ocv1.BundleMetadata
		expectedResult  []declcfg.Bundle
	}{
		{
			name:            "respect replaces directive from catalog",
			installedBundle: bundleutil.MetadataFor("test-package.v2.0.0", mmsemver.MustParse("2.0.0")),
			expectedResult: []declcfg.Bundle{
				// Must only have two bundle:
				// - the one which replaces the current version
				// - the current version (to allow to stay on the current version)
				bundleSet["test-package.v2.1.0"],
				bundleSet["test-package.v2.0.0"],
			},
		},
		{
			name:            "respect skips directive from catalog",
			installedBundle: bundleutil.MetadataFor("test-package.v2.2.1", mmsemver.MustParse("2.2.1")),
			expectedResult: []declcfg.Bundle{
				// Must only have two bundle:
				// - the one which skips the current version
				// - the current version (to allow to stay on the current version)
				bundleSet["test-package.v2.3.0"],
				bundleSet["test-package.v2.2.1"],
			},
		},
		{
			name:            "respect skipRange directive from catalog",
			installedBundle: bundleutil.MetadataFor("test-package.v2.3.0", mmsemver.MustParse("2.3.0")),
			expectedResult: []declcfg.Bundle{
				// Must only have two bundle:
				// - the one which is skipRanges the current version
				// - the current version (to allow to stay on the current version)
				bundleSet["test-package.v2.4.0"],
				bundleSet["test-package.v2.3.0"],
			},
		},
		{
			name: "installed bundle not found",
			installedBundle: ocv1.BundleMetadata{
				Name:    "test-package.v9.0.0",
				Version: "9.0.0",
			},
			expectedResult: []declcfg.Bundle{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			successors, err := SuccessorsOf(tt.installedBundle, channelSet[testPackageName])
			require.NoError(t, err)

			allBundles := make([]declcfg.Bundle, 0, len(bundleSet))
			for _, bundle := range bundleSet {
				allBundles = append(allBundles, bundle)
			}
			result := Filter(allBundles, successors)

			// sort before comparison for stable order
			slices.SortFunc(result, compare.ByVersion)

			gocmpopts := []cmp.Option{
				cmpopts.IgnoreUnexported(declcfg.Bundle{}),
			}
			require.Empty(t, cmp.Diff(result, tt.expectedResult, gocmpopts...))
		})
	}
}

func TestLegacySuccessor(t *testing.T) {
	fakeChannel := declcfg.Channel{
		Entries: []declcfg.ChannelEntry{
			{
				Name:     "package1.v0.0.2",
				Replaces: "package1.v0.0.1",
			},
			{
				Name:     "package1.v0.0.3",
				Replaces: "package1.v0.0.2",
			},
			{
				Name:  "package1.v0.0.4",
				Skips: []string{"package1.v0.0.1"},
			},
			{
				Name:      "package1.v0.0.5",
				SkipRange: "<=0.0.1",
			},
		},
	}
	installedBundle := ocv1.BundleMetadata{
		Name:    "package1.v0.0.1",
		Version: "0.0.1",
	}

	b2 := declcfg.Bundle{Name: "package1.v0.0.2"}
	b3 := declcfg.Bundle{Name: "package1.v0.0.3"}
	b4 := declcfg.Bundle{Name: "package1.v0.0.4"}
	b5 := declcfg.Bundle{Name: "package1.v0.0.5"}
	emptyBundle := declcfg.Bundle{}

	f, err := legacySuccessor(installedBundle, fakeChannel)
	require.NoError(t, err)

	assert.True(t, f(b2))
	assert.False(t, f(b3))
	assert.True(t, f(b4))
	assert.True(t, f(b5))
	assert.False(t, f(emptyBundle))
}
