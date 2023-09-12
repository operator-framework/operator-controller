package catalogmetadata_test

import (
	"encoding/json"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

func TestGVK(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		gvk := catalogmetadata.GVK{Group: "bar.io", Kind: "Bar", Version: "v1"}

		assert.Equal(t, `group:"bar.io" version:"v1" kind:"Bar"`, gvk.String())
	})
}

func TestGVKRequired(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		gvk := catalogmetadata.GVKRequired{Group: "bar.io", Kind: "Bar", Version: "v1"}

		assert.Equal(t, `group:"bar.io" version:"v1" kind:"Bar"`, gvk.String())
	})

	t.Run("AsGVK", func(t *testing.T) {
		gvk := catalogmetadata.GVKRequired{Group: "bar.io", Kind: "Bar", Version: "v1"}

		assert.Equal(t, catalogmetadata.GVK{Group: "bar.io", Kind: "Bar", Version: "v1"}, gvk.AsGVK())
	})
}

func TestBundle(t *testing.T) {
	t.Run("Version", func(t *testing.T) {
		validVersion := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.v1",
			Properties: []property.Property{
				{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
				},
			},
		}}
		invalidVersion := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.invalid",
			Properties: []property.Property{
				{
					Type:  property.TypePackage,
					Value: json.RawMessage(`{"packageName": "package1", "version": "broken"}`),
				},
			},
		}}
		noVersion := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.noVersion",
		}}

		ver, err := validVersion.Version()
		assert.NoError(t, err)
		assert.Equal(t, &bsemver.Version{Major: 1}, ver)

		ver, err = invalidVersion.Version()
		assert.EqualError(t, err, "could not parse semver \"broken\" for bundle 'fake-bundle.invalid': No Major.Minor.Patch elements found")
		assert.Nil(t, ver)

		ver, err = noVersion.Version()
		assert.EqualError(t, err, "bundle property with type \"olm.package\" not found")
		assert.Nil(t, ver)
	})

	t.Run("ProvidedGVKs", func(t *testing.T) {
		validGVK := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.v1",
			Properties: []property.Property{
				{
					Type:  property.TypeGVK,
					Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"},{"group":"bar.io","kind":"Bar","version":"v1alpha1"}]`),
				},
			},
		}}
		invalidGVK := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.invalid",
			Properties: []property.Property{
				{
					Type:  property.TypeGVK,
					Value: json.RawMessage(`badGvkStructure`),
				},
			},
		}}
		noGVK := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.noGVK",
		}}

		gvk, err := validGVK.ProvidedGVKs()
		assert.NoError(t, err)
		assert.Equal(t, []catalogmetadata.GVK{
			{Group: "foo.io", Kind: "Foo", Version: "v1"},
			{Group: "bar.io", Kind: "Bar", Version: "v1alpha1"},
		}, gvk)

		gvk, err = invalidGVK.ProvidedGVKs()
		assert.EqualError(t, err, "error determining provided GVKs for bundle \"fake-bundle.invalid\": property \"olm.gvk\" with value \"badGvkStructure\" could not be parsed: invalid character 'b' looking for beginning of value")
		assert.Nil(t, gvk)

		gvk, err = noGVK.ProvidedGVKs()
		assert.NoError(t, err)
		assert.Nil(t, gvk)
	})

	t.Run("RequiredGVKs", func(t *testing.T) {
		validGVK := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.v1",
			Properties: []property.Property{
				{
					Type:  property.TypeGVKRequired,
					Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"},{"group":"bar.io","kind":"Bar","version":"v1alpha1"}]`),
				},
			},
		}}
		invalidGVK := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.invalid",
			Properties: []property.Property{
				{
					Type:  property.TypeGVKRequired,
					Value: json.RawMessage(`badGvkStructure`),
				},
			},
		}}
		noGVK := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
			Name: "fake-bundle.noGVK",
		}}

		gvk, err := validGVK.RequiredGVKs()
		assert.NoError(t, err)
		assert.Equal(t, []catalogmetadata.GVKRequired{
			{Group: "foo.io", Kind: "Foo", Version: "v1"},
			{Group: "bar.io", Kind: "Bar", Version: "v1alpha1"},
		}, gvk)

		gvk, err = invalidGVK.RequiredGVKs()
		assert.EqualError(t, err, "error determining required GVKs for bundle \"fake-bundle.invalid\": property \"olm.gvk.required\" with value \"badGvkStructure\" could not be parsed: invalid character 'b' looking for beginning of value")
		assert.Nil(t, gvk)

		gvk, err = noGVK.RequiredGVKs()
		assert.NoError(t, err)
		assert.Nil(t, gvk)
	})
}
