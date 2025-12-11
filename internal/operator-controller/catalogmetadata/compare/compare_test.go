package compare_test

import (
	"encoding/json"
	"slices"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
)

func TestNewVersionRange(t *testing.T) {
	type testCase struct {
		name         string
		versionRange string
		assertFunc   func(t *testing.T, vr bsemver.Range, err error)
	}
	for _, tc := range []testCase{
		{
			name:         "version without build metadata matches range with build metadata",
			versionRange: "1.0.0+1",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.NoError(t, err)
				assert.True(t, vr(bsemver.MustParse("1.0.0")))
			},
		},
		{
			name:         "version with different build metadata matches range with build metadata",
			versionRange: "1.0.0+1",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.NoError(t, err)
				assert.True(t, vr(bsemver.MustParse("1.0.0+2")))
			},
		},
		{
			name:         "version with same build metadata matches range with build metadata",
			versionRange: "1.0.0+1",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.NoError(t, err)
				assert.True(t, vr(bsemver.MustParse("1.0.0+1")))
			},
		},
		{
			name:         "exact version match without build metadata",
			versionRange: "1.0.0",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.NoError(t, err)
				assert.True(t, vr(bsemver.MustParse("1.0.0")))
			},
		},
		{
			name:         "version with build metadata matches range without build metadata",
			versionRange: "1.0.0",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.NoError(t, err)
				assert.True(t, vr(bsemver.MustParse("1.0.0+1")))
			},
		},
		{
			name:         "invalid range returns error",
			versionRange: "not-a-valid-version",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.Error(t, err)
			},
		},
		{
			name:         "version does not match range",
			versionRange: ">=2.0.0",
			assertFunc: func(t *testing.T, vr bsemver.Range, err error) {
				require.NoError(t, err)
				assert.False(t, vr(bsemver.MustParse("1.0.0")))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			versionRange, err := compare.NewVersionRange(tc.versionRange)
			tc.assertFunc(t, versionRange, err)
		})
	}
}

func TestByVersionAndRelease(t *testing.T) {
	b1 := declcfg.Bundle{
		Name: "package1.v1.0.0",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
			},
		},
	}
	b2 := declcfg.Bundle{
		Name: "package1.v0.0.1",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`),
			},
		},
	}
	b3_1 := declcfg.Bundle{
		Name: "package1.v1.0.0-alpha+1",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0-alpha+1"}`),
			},
		},
	}
	b3_2 := declcfg.Bundle{
		Name: "package1.v1.0.0-alpha+2",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0-alpha+2"}`),
			},
		},
	}
	b4noVersion := declcfg.Bundle{
		Name: "package1.no-version",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1"}`),
			},
		},
	}
	b5empty := declcfg.Bundle{
		Name: "package1.empty",
	}

	t.Run("all bundles valid", func(t *testing.T) {
		toSort := []declcfg.Bundle{b3_1, b2, b3_2, b1}
		slices.SortStableFunc(toSort, compare.ByVersionAndRelease)
		assert.Equal(t, []declcfg.Bundle{b1, b3_2, b3_1, b2}, toSort)
	})

	t.Run("some bundles are missing version", func(t *testing.T) {
		toSort := []declcfg.Bundle{b3_1, b4noVersion, b2, b3_2, b5empty, b1}
		slices.SortStableFunc(toSort, compare.ByVersionAndRelease)
		assert.Equal(t, []declcfg.Bundle{b1, b3_2, b3_1, b2, b4noVersion, b5empty}, toSort)
	})
}

func TestByDeprecationFunc(t *testing.T) {
	deprecation := declcfg.Deprecation{
		Entries: []declcfg.DeprecationEntry{
			{Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: "a"}},
			{Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: "b"}},
		},
	}
	byDeprecation := compare.ByDeprecationFunc(deprecation)
	a := declcfg.Bundle{Name: "a"}
	b := declcfg.Bundle{Name: "b"}
	c := declcfg.Bundle{Name: "c"}
	d := declcfg.Bundle{Name: "d"}

	assert.Equal(t, 0, byDeprecation(a, b))
	assert.Equal(t, 0, byDeprecation(b, a))
	assert.Equal(t, 1, byDeprecation(a, c))
	assert.Equal(t, -1, byDeprecation(c, a))
	assert.Equal(t, 0, byDeprecation(c, d))
	assert.Equal(t, 0, byDeprecation(d, c))
}
