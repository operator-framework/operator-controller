package bundle_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
)

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
