package bundle_test

import (
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
)

func TestNewLegacyRegistryV1VersionRelease(t *testing.T) {
	type testCase struct {
		name   string
		input  string
		expect func(*testing.T, *bundle.VersionRelease, error)
	}
	for _, tc := range []testCase{
		{
			name:  "empty input",
			input: "",
			expect: func(t *testing.T, _ *bundle.VersionRelease, err error) {
				assert.Error(t, err)
			},
		},
		{
			name:  "v-prefix is invalid",
			input: "v1.0.0",
			expect: func(t *testing.T, _ *bundle.VersionRelease, err error) {
				assert.Error(t, err)
			},
		},
		{
			name:  "valid semver, valid build metadata, but not a valid release",
			input: "1.2.3-alpha+4.005",
			expect: func(t *testing.T, vr *bundle.VersionRelease, err error) {
				require.NoError(t, err)
				assert.Equal(t, uint64(1), vr.Version.Major)
				assert.Equal(t, uint64(2), vr.Version.Minor)
				assert.Equal(t, uint64(3), vr.Version.Patch)
				assert.Equal(t, []bsemver.PRVersion{{VersionStr: "alpha"}}, vr.Version.Pre)
				assert.Equal(t, []string{"4", "005"}, vr.Version.Build)
				assert.Empty(t, vr.Release)
			},
		},
		{
			name:  "valid semver, valid build metadata, valid release",
			input: "1.2.3-alpha+4.5",
			expect: func(t *testing.T, vr *bundle.VersionRelease, err error) {
				require.NoError(t, err)
				assert.Equal(t, uint64(1), vr.Version.Major)
				assert.Equal(t, uint64(2), vr.Version.Minor)
				assert.Equal(t, uint64(3), vr.Version.Patch)
				assert.Equal(t, []bsemver.PRVersion{{VersionStr: "alpha"}}, vr.Version.Pre)
				assert.Empty(t, vr.Version.Build)
				assert.Equal(t, bundle.Release([]bsemver.PRVersion{
					{VersionNum: 4, IsNum: true},
					{VersionNum: 5, IsNum: true},
				}), vr.Release)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := bundle.NewLegacyRegistryV1VersionRelease(tc.input)
			tc.expect(t, actual, err)
		})
	}
}

func TestVersionRelease_AsLegacyRegistryV1Version(t *testing.T) {
	type testCase struct {
		name   string
		input  bundle.VersionRelease
		expect bsemver.Version
	}
	for _, tc := range []testCase{
		{
			name: "Release is set, so release is set as build metadata",
			input: bundle.VersionRelease{
				Version: bsemver.Version{
					Major: 1,
					Minor: 2,
					Patch: 3,
				},
				Release: bundle.Release([]bsemver.PRVersion{{VersionStr: "release"}}),
			},
			expect: bsemver.Version{
				Major: 1,
				Minor: 2,
				Patch: 3,
				Build: []string{"release"},
			},
		},
		{
			name: "Release is unset, so version build metadata is set as build metadata",
			input: bundle.VersionRelease{
				Version: bsemver.Version{
					Major: 1,
					Minor: 2,
					Patch: 3,
					Build: []string{"buildmetadata"},
				},
			},
			expect: bsemver.Version{
				Major: 1,
				Minor: 2,
				Patch: 3,
				Build: []string{"buildmetadata"},
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, tc.input.AsLegacyRegistryV1Version())
		})
	}
}

func TestLegacyRegistryV1VersionRelease_Compare(t *testing.T) {
	type testCase struct {
		name   string
		v1     string
		v2     string
		expect int
	}
	for _, tc := range []testCase{
		{
			name:   "lower major version",
			v1:     "1.0.0-0+0",
			v2:     "2.0.0-0+0",
			expect: -1,
		},
		{
			name:   "lower minor version",
			v1:     "0.1.0-0+0",
			v2:     "0.2.0-0+0",
			expect: -1,
		},
		{
			name:   "lower patch version",
			v1:     "0.0.1-0+0",
			v2:     "0.0.2-0+0",
			expect: -1,
		},
		{
			name:   "lower prerelease version",
			v1:     "0.0.0-1+0",
			v2:     "0.0.0-2+0",
			expect: -1,
		},
		{
			name:   "lower build metadata",
			v1:     "0.0.0-0+1",
			v2:     "0.0.0-0+2",
			expect: -1,
		},
		{
			name:   "same major version",
			v1:     "1.0.0-0+0",
			v2:     "1.0.0-0+0",
			expect: 0,
		},
		{
			name:   "same minor version",
			v1:     "0.1.0-0+0",
			v2:     "0.1.0-0+0",
			expect: 0,
		},
		{
			name:   "same patch version",
			v1:     "0.0.1-0+0",
			v2:     "0.0.1-0+0",
			expect: 0,
		},
		{
			name:   "same prerelease version",
			v1:     "0.0.0-1+0",
			v2:     "0.0.0-1+0",
			expect: 0,
		},
		{
			name:   "same build metadata",
			v1:     "0.0.0-0+1",
			v2:     "0.0.0-0+1",
			expect: 0,
		},
		{
			name:   "higher major version",
			v1:     "2.0.0-0+0",
			v2:     "1.0.0-0+0",
			expect: 1,
		},
		{
			name:   "higher minor version",
			v1:     "0.2.0-0+0",
			v2:     "0.1.0-0+0",
			expect: 1,
		},
		{
			name:   "higher patch version",
			v1:     "0.0.2-0+0",
			v2:     "0.0.1-0+0",
			expect: 1,
		},
		{
			name:   "higher prerelease version",
			v1:     "0.0.0-2+0",
			v2:     "0.0.0-1+0",
			expect: 1,
		},
		{
			name:   "higher build metadata",
			v1:     "0.0.0-0+2",
			v2:     "0.0.0-0+1",
			expect: 1,
		},
		{
			name:   "non-release build metadata is less than valid release build metadata",
			v1:     "0.0.0-0+1.001",
			v2:     "0.0.0-0+0",
			expect: -1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vr1, err1 := bundle.NewLegacyRegistryV1VersionRelease(tc.v1)
			vr2, err2 := bundle.NewLegacyRegistryV1VersionRelease(tc.v2)
			require.NoError(t, err1)
			require.NoError(t, err2)

			actual := vr1.Compare(*vr2)
			assert.Equal(t, tc.expect, actual)
		})
	}
}

func TestNewRelease(t *testing.T) {
	type testCase struct {
		name      string
		input     string
		expectErr bool
	}
	for _, tc := range []testCase{
		{
			name:      "empty string",
			input:     "",
			expectErr: false,
		},
		{
			name:      "single numeric segment",
			input:     "9",
			expectErr: false,
		},
		{
			name:      "single alphanumeric segment",
			input:     "alpha",
			expectErr: false,
		},
		{
			name:      "multiple segments",
			input:     "9.10.3",
			expectErr: false,
		},
		{
			name:      "mixed numeric and alphanumeric",
			input:     "9.alpha.10",
			expectErr: false,
		},
		{
			name:      "segment with hyphens in middle",
			input:     "alpha-beta",
			expectErr: false,
		},
		{
			name:      "segment starting with hyphen",
			input:     "-alpha",
			expectErr: false,
		},
		{
			name:      "segment ending with hyphen",
			input:     "alpha-",
			expectErr: false,
		},
		{
			name:      "segment with only hyphens",
			input:     "--",
			expectErr: false,
		},
		{
			name:      "numeric segment with leading zero (single)",
			input:     "0",
			expectErr: false,
		},
		{
			name:      "numeric segment with leading zeros",
			input:     "01",
			expectErr: true,
		},
		{
			name:      "alphanumeric segment with leading zeros",
			input:     "01alpha",
			expectErr: false,
		},
		{
			name:      "alphanumeric segment with number prefix",
			input:     "pre9",
			expectErr: false,
		},
		{
			name:      "alphanumeric segment with number suffix",
			input:     "9pre",
			expectErr: false,
		},
		{
			name:      "multiple segments with one having leading zeros",
			input:     "9.010.3",
			expectErr: true,
		},
		{
			name:      "empty segment at start",
			input:     ".alpha",
			expectErr: true,
		},
		{
			name:      "empty segment at end",
			input:     "alpha.",
			expectErr: true,
		},
		{
			name:      "empty segment in middle",
			input:     "alpha..beta",
			expectErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := bundle.NewRelease(tc.input)
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tc.input == "" {
					assert.Nil(t, result)
				} else {
					assert.NotNil(t, result)
				}
			}
		})
	}
}

func TestRelease_Compare(t *testing.T) {
	type testCase struct {
		name   string
		r1     string
		r2     string
		expect int
	}
	for _, tc := range []testCase{
		{
			name:   "both empty",
			r1:     "",
			r2:     "",
			expect: 0,
		},
		{
			name:   "first empty, second not",
			r1:     "",
			r2:     "9",
			expect: -1,
		},
		{
			name:   "first not empty, second empty",
			r1:     "9",
			r2:     "",
			expect: 1,
		},
		{
			name:   "equal numeric segments",
			r1:     "9",
			r2:     "9",
			expect: 0,
		},
		{
			name:   "lower numeric segment",
			r1:     "9",
			r2:     "10",
			expect: -1,
		},
		{
			name:   "higher numeric segment",
			r1:     "10",
			r2:     "9",
			expect: 1,
		},
		{
			name:   "equal alphanumeric segments",
			r1:     "alpha",
			r2:     "alpha",
			expect: 0,
		},
		{
			name:   "lower alphanumeric segment",
			r1:     "alpha",
			r2:     "beta",
			expect: -1,
		},
		{
			name:   "higher alphanumeric segment",
			r1:     "beta",
			r2:     "alpha",
			expect: 1,
		},
		{
			name:   "numeric vs alphanumeric (numeric is less)",
			r1:     "9",
			r2:     "alpha",
			expect: -1,
		},
		{
			name:   "alphanumeric vs numeric (alphanumeric is greater)",
			r1:     "alpha",
			r2:     "9",
			expect: 1,
		},
		{
			name:   "shorter release (all segments equal)",
			r1:     "9.10",
			r2:     "9.10.3",
			expect: -1,
		},
		{
			name:   "longer release (all segments equal)",
			r1:     "9.10.3",
			r2:     "9.10",
			expect: 1,
		},
		{
			name:   "complex equal releases",
			r1:     "9.alpha.10.beta",
			r2:     "9.alpha.10.beta",
			expect: 0,
		},
		{
			name:   "segment with hyphens",
			r1:     "alpha-beta",
			r2:     "alpha-gamma",
			expect: -1,
		},
		{
			name:   "alphanumeric with numbers prefix (lexicographic)",
			r1:     "pre9",
			r2:     "pre10",
			expect: 1, // "pre9" > "pre10" lexicographically
		},
		{
			name:   "alphanumeric with numbers suffix (lexicographic)",
			r1:     "9pre",
			r2:     "10pre",
			expect: 1, // "9pre" > "10pre" lexicographically
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel1, err := bundle.NewRelease(tc.r1)
			require.NoError(t, err)
			rel2, err := bundle.NewRelease(tc.r2)
			require.NoError(t, err)

			actual := rel1.Compare(rel2)
			assert.Equal(t, tc.expect, actual)
		})
	}
}
