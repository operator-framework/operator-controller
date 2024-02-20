package sort_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
)

func TestByVersion(t *testing.T) {
	b1 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Name: "package1.v1.0.0",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
			},
		},
	}}
	b2 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Name: "package1.v0.0.1",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`),
			},
		},
	}}
	b3 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Name: "package1.v1.0.0-alpha+001",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0-alpha+001"}`),
			},
		},
	}}
	b4noVersion := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Name: "package1.no-version",
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1"}`),
			},
		},
	}}
	b5empty := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Name: "package1.empty",
	}}

	t.Run("all bundles valid", func(t *testing.T) {
		toSort := []*catalogmetadata.Bundle{b3, b2, b1}
		sort.SliceStable(toSort, func(i, j int) bool {
			return catalogsort.ByVersion(toSort[i], toSort[j])
		})

		assert.Len(t, toSort, 3)
		assert.Equal(t, b1, toSort[0])
		assert.Equal(t, b3, toSort[1])
		assert.Equal(t, b2, toSort[2])
	})

	t.Run("some bundles are missing version", func(t *testing.T) {
		toSort := []*catalogmetadata.Bundle{b3, b4noVersion, b2, b5empty, b1}
		sort.SliceStable(toSort, func(i, j int) bool {
			return catalogsort.ByVersion(toSort[i], toSort[j])
		})

		assert.Len(t, toSort, 5)
		assert.Equal(t, b1, toSort[0])
		assert.Equal(t, b3, toSort[1])
		assert.Equal(t, b2, toSort[2])
		assert.Equal(t, b4noVersion, toSort[3])
		assert.Equal(t, b5empty, toSort[4])
	})
}

func TestByDeprecated(t *testing.T) {
	b1 := &catalogmetadata.Bundle{
		Catalog: "foo",
		Bundle: declcfg.Bundle{
			Name: "bar",
		},
	}

	b2 := &catalogmetadata.Bundle{
		Catalog: "foo",
		Bundle: declcfg.Bundle{
			Name: "baz",
		},
		Deprecation: &declcfg.DeprecationEntry{
			Reference: declcfg.PackageScopedReference{
				Schema: "olm.bundle",
				Name:   "baz",
			},
		},
	}

	toSort := []*catalogmetadata.Bundle{b2, b1}
	sort.SliceStable(toSort, func(i, j int) bool {
		return catalogsort.ByDeprecated(toSort[i], toSort[j])
	})

	require.Len(t, toSort, 2)
	assert.Equal(t, b1, toSort[0])
	assert.Equal(t, b2, toSort[1])

	b2.Deprecation = nil

	toSort = []*catalogmetadata.Bundle{b2, b1}
	sort.SliceStable(toSort, func(i, j int) bool {
		return catalogsort.ByDeprecated(toSort[i], toSort[j])
	})
	// No bundles are deprecated so ordering should remain the same
	require.Len(t, toSort, 2)
	assert.Equal(t, b2, toSort[0])
	assert.Equal(t, b1, toSort[1])
}
