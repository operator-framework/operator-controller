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
