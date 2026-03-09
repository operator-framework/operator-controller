package compare_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
)

func TestByVersion(t *testing.T) {
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
	b3 := declcfg.Bundle{
		Name: "package1.v1.0.0-alpha+001",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0-alpha+001"}`),
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
		toSort := []declcfg.Bundle{b3, b2, b1}
		slices.SortStableFunc(toSort, compare.ByVersion)
		assert.Equal(t, []declcfg.Bundle{b1, b3, b2}, toSort)
	})

	t.Run("some bundles are missing version", func(t *testing.T) {
		toSort := []declcfg.Bundle{b3, b4noVersion, b2, b5empty, b1}
		slices.SortStableFunc(toSort, compare.ByVersion)
		assert.Equal(t, []declcfg.Bundle{b1, b3, b2, b4noVersion, b5empty}, toSort)
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
