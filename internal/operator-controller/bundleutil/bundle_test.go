package bundleutil_test

import (
	"encoding/json"
	"fmt"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

func TestGetVersionAndRelease(t *testing.T) {
	tests := []struct {
		name               string
		pkgProperty        *property.Property
		wantVersionRelease *bundle.VersionRelease
		wantErr            bool
	}{
		{
			name: "valid version",
			pkgProperty: &property.Property{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"version": "1.0.0-pre+1.alpha.2"}`),
			},
			wantVersionRelease: &bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0-pre"),
				Release: bundle.Release([]bsemver.PRVersion{
					{VersionNum: 1, IsNum: true},
					{VersionStr: "alpha"},
					{VersionNum: 2, IsNum: true},
				}),
			},
			wantErr: false,
		},
		{
			name: "invalid version",
			pkgProperty: &property.Property{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"version": "abcd"}`),
			},
			wantErr: true,
		},
		{
			name: "invalid release - build metadata with leading zeros",
			pkgProperty: &property.Property{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"version": "1.0.0+001"}`),
			},
			wantVersionRelease: &bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0+001"),
			},
			wantErr: false,
		},
		{
			name: "invalid json",
			pkgProperty: &property.Property{
				Type:  property.TypePackage,
				Value: json.RawMessage(`abcd`),
			},
			wantErr: true,
		},
		{
			name:        "no version property",
			pkgProperty: nil,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			properties := make([]property.Property, 0)
			if tc.pkgProperty != nil {
				properties = append(properties, *tc.pkgProperty)
			}

			bundle := declcfg.Bundle{
				Name:       "test-bundle",
				Properties: properties,
			}

			actual, err := bundleutil.GetVersionAndRelease(bundle)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantVersionRelease, actual)
			}
		})
	}
}

// TestGetVersionAndRelease_WithBundleReleaseSupport tests the feature-gated parsing behavior.
// Explicitly sets the gate to test both enabled and disabled paths.
func TestGetVersionAndRelease_WithBundleReleaseSupport(t *testing.T) {
	t.Run("gate enabled - parses explicit release field", func(t *testing.T) {
		// Enable the feature gate for this test
		prevEnabled := features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport)
		require.NoError(t, features.OperatorControllerFeatureGate.Set("BundleReleaseSupport=true"))
		t.Cleanup(func() {
			require.NoError(t, features.OperatorControllerFeatureGate.Set(fmt.Sprintf("BundleReleaseSupport=%t", prevEnabled)))
		})

		tests := []struct {
			name               string
			pkgProperty        *property.Property
			wantVersionRelease *bundle.VersionRelease
			wantErr            bool
		}{
			{
				name: "explicit release field - takes precedence over build metadata",
				pkgProperty: &property.Property{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"version": "1.0.0+ignored", "release": "2"}`),
				},
				wantVersionRelease: &bundle.VersionRelease{
					Version: bsemver.MustParse("1.0.0+ignored"), // Build metadata preserved - serves its proper semver purpose
					Release: bundle.Release([]bsemver.PRVersion{
						{VersionNum: 2, IsNum: true},
					}),
				},
				wantErr: false,
			},
			{
				name: "explicit release field - complex release",
				pkgProperty: &property.Property{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"version": "2.1.0", "release": "1.alpha.3"}`),
				},
				wantVersionRelease: &bundle.VersionRelease{
					Version: bsemver.MustParse("2.1.0"),
					Release: bundle.Release([]bsemver.PRVersion{
						{VersionNum: 1, IsNum: true},
						{VersionStr: "alpha"},
						{VersionNum: 3, IsNum: true},
					}),
				},
				wantErr: false,
			},
			{
				name: "explicit release field - invalid release",
				pkgProperty: &property.Property{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"version": "1.0.0", "release": "001"}`),
				},
				wantErr: true,
			},
			{
				name: "explicit empty release - preserves build metadata in version",
				pkgProperty: &property.Property{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"version": "1.0.0+2", "release": ""}`),
				},
				wantVersionRelease: &bundle.VersionRelease{
					Version: bsemver.MustParse("1.0.0+2"),          // Build metadata preserved (not extracted as release)
					Release: bundle.Release([]bsemver.PRVersion{}), // Explicit empty release
				},
				wantErr: false,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				bundle := declcfg.Bundle{
					Name:       "test-bundle",
					Properties: []property.Property{*tc.pkgProperty},
				}

				actual, err := bundleutil.GetVersionAndRelease(bundle)
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.wantVersionRelease, actual)
				}
			})
		}
	})

	t.Run("gate disabled - ignores explicit release field, uses build metadata", func(t *testing.T) {
		// Disable the feature gate for this test
		prevEnabled := features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport)
		require.NoError(t, features.OperatorControllerFeatureGate.Set("BundleReleaseSupport=false"))
		t.Cleanup(func() {
			require.NoError(t, features.OperatorControllerFeatureGate.Set(fmt.Sprintf("BundleReleaseSupport=%t", prevEnabled)))
		})

		// When gate disabled, explicit release field is ignored and parsing falls back to legacy behavior
		bundleWithExplicitRelease := declcfg.Bundle{
			Name: "test-bundle",
			Properties: []property.Property{
				{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"version": "1.0.0+2", "release": "999"}`),
				},
			},
		}

		actual, err := bundleutil.GetVersionAndRelease(bundleWithExplicitRelease)
		require.NoError(t, err)

		// Should parse build metadata (+2), not explicit release field (999)
		expected := &bundle.VersionRelease{
			Version: bsemver.MustParse("1.0.0"),
			Release: bundle.Release([]bsemver.PRVersion{
				{VersionNum: 2, IsNum: true},
			}),
		}
		require.Equal(t, expected, actual)
	})
}

func TestMetadataFor(t *testing.T) {
	t.Run("with feature gate enabled", func(t *testing.T) {
		prevEnabled := features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport)
		require.NoError(t, features.OperatorControllerFeatureGate.Set("BundleReleaseSupport=true"))
		t.Cleanup(func() {
			require.NoError(t, features.OperatorControllerFeatureGate.Set(fmt.Sprintf("BundleReleaseSupport=%t", prevEnabled)))
		})

		t.Run("with release", func(t *testing.T) {
			vr := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
				Release: bundle.Release([]bsemver.PRVersion{{VersionNum: 2, IsNum: true}}),
			}
			result := bundleutil.MetadataFor("test-bundle", vr)
			require.Equal(t, "test-bundle", result.Name)
			require.Equal(t, "1.0.0", result.Version)
			require.NotNil(t, result.Release)
			require.Equal(t, "2", *result.Release)
		})

		t.Run("without release", func(t *testing.T) {
			vr := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
				Release: nil,
			}
			result := bundleutil.MetadataFor("test-bundle", vr)
			require.Equal(t, "test-bundle", result.Name)
			require.Equal(t, "1.0.0", result.Version)
			require.Nil(t, result.Release)
		})

		t.Run("with explicit empty release", func(t *testing.T) {
			vr := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
				Release: bundle.Release([]bsemver.PRVersion{}),
			}
			result := bundleutil.MetadataFor("test-bundle", vr)
			require.Equal(t, "test-bundle", result.Name)
			require.Equal(t, "1.0.0", result.Version)
			require.NotNil(t, result.Release)
			require.Empty(t, *result.Release)
		})
	})

	t.Run("with feature gate disabled (legacy mode)", func(t *testing.T) {
		prevEnabled := features.OperatorControllerFeatureGate.Enabled(features.BundleReleaseSupport)
		require.NoError(t, features.OperatorControllerFeatureGate.Set("BundleReleaseSupport=false"))
		t.Cleanup(func() {
			require.NoError(t, features.OperatorControllerFeatureGate.Set(fmt.Sprintf("BundleReleaseSupport=%t", prevEnabled)))
		})

		t.Run("reconstitutes build metadata in version", func(t *testing.T) {
			vr := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
				Release: bundle.Release([]bsemver.PRVersion{{VersionNum: 2, IsNum: true}}),
			}
			result := bundleutil.MetadataFor("test-bundle", vr)
			require.Equal(t, "test-bundle", result.Name)
			require.Equal(t, "1.0.0+2", result.Version) // Build metadata reconstituted
			require.Nil(t, result.Release)              // Release field not used in legacy mode
		})

		t.Run("preserves original build metadata when no release", func(t *testing.T) {
			vr := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
				Release: nil,
			}
			result := bundleutil.MetadataFor("test-bundle", vr)
			require.Equal(t, "test-bundle", result.Name)
			require.Equal(t, "1.0.0", result.Version)
			require.Nil(t, result.Release)
		})
	})
}
