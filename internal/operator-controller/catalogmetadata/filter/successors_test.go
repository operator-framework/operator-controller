package filter

import (
	"slices"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
	"github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

func TestSuccessorsPredicate(t *testing.T) {
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
			installedBundle: bundleutil.MetadataFor("test-package.v2.0.0", bsemver.MustParse("2.0.0")),
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
			installedBundle: bundleutil.MetadataFor("test-package.v2.2.1", bsemver.MustParse("2.2.1")),
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
			installedBundle: bundleutil.MetadataFor("test-package.v2.3.0", bsemver.MustParse("2.3.0")),
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
			result := filter.InPlace(allBundles, successors)

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
