package filter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/filter"
)

func TestInSemverRange(t *testing.T) {
	b1 := declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
			},
		},
	}
	b2 := declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`),
			},
		},
	}
	b3 := declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "broken"}`),
			},
		},
	}

	vRange, err := compare.NewVersionRange(">=1.0.0")
	require.NoError(t, err)

	f := filter.InSemverRange(vRange)

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestInAnyChannel(t *testing.T) {
	alpha := declcfg.Channel{Name: "alpha", Entries: []declcfg.ChannelEntry{{Name: "b1"}, {Name: "b2"}}}
	stable := declcfg.Channel{Name: "stable", Entries: []declcfg.ChannelEntry{{Name: "b1"}}}

	b1 := declcfg.Bundle{Name: "b1"}
	b2 := declcfg.Bundle{Name: "b2"}
	b3 := declcfg.Bundle{Name: "b3"}

	fAlpha := filter.InAnyChannel(alpha)
	assert.True(t, fAlpha(b1))
	assert.True(t, fAlpha(b2))
	assert.False(t, fAlpha(b3))

	fStable := filter.InAnyChannel(stable)
	assert.True(t, fStable(b1))
	assert.False(t, fStable(b2))
	assert.False(t, fStable(b3))
}

func TestSameVersionHigherRelease(t *testing.T) {
	const testPackageName = "test-package"

	// Expected bundle version 2.0.0+1
	expect, err := bundle.NewLegacyRegistryV1VersionRelease("2.0.0+1")
	require.NoError(t, err)

	tests := []struct {
		name          string
		bundleVersion string
		shouldMatch   bool
	}{
		{
			name:          "same version, higher release",
			bundleVersion: "2.0.0+2",
			shouldMatch:   true,
		},
		{
			name:          "same version, same release",
			bundleVersion: "2.0.0+1",
			shouldMatch:   false,
		},
		{
			name:          "same version, lower release",
			bundleVersion: "2.0.0+0",
			shouldMatch:   false,
		},
		{
			name:          "same version, no release",
			bundleVersion: "2.0.0",
			shouldMatch:   false,
		},
		{
			name:          "different version, higher release",
			bundleVersion: "2.1.0+2",
			shouldMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testBundle := declcfg.Bundle{
				Name:    "test-package.v" + tt.bundleVersion,
				Package: testPackageName,
				Properties: []property.Property{
					property.MustBuildPackage(testPackageName, tt.bundleVersion),
				},
			}

			f := filter.SameVersionHigherRelease(*expect)
			assert.Equal(t, tt.shouldMatch, f(testBundle), "version %s should match=%v", tt.bundleVersion, tt.shouldMatch)
		})
	}

	// Test when expected version has no release (e.g: "2.0.0")
	t.Run("expected version without release", func(t *testing.T) {
		expectNoRelease, err := bundle.NewLegacyRegistryV1VersionRelease("2.0.0")
		require.NoError(t, err)

		// Bundle with release should be considered higher
		bundleWithRelease := declcfg.Bundle{
			Name:    "test-package.v2.0.0+1",
			Package: testPackageName,
			Properties: []property.Property{
				property.MustBuildPackage(testPackageName, "2.0.0+1"),
			},
		}

		f := filter.SameVersionHigherRelease(*expectNoRelease)
		assert.True(t, f(bundleWithRelease), "2.0.0+1 should be higher than 2.0.0")
	})

	// Test error case: invalid bundle (no package property)
	t.Run("invalid bundle - no package property", func(t *testing.T) {
		testBundle := declcfg.Bundle{
			Name:       "test-package.invalid",
			Package:    testPackageName,
			Properties: []property.Property{},
		}

		f := filter.SameVersionHigherRelease(*expect)
		assert.False(t, f(testBundle))
	})
}
