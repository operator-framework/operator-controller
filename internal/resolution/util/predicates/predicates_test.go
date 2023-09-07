package predicates_test

import (
	"testing"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	"github.com/operator-framework/operator-controller/internal/resolution/util/predicates"
)

type testData struct {
	entity map[string]string
	value  string
	result bool
}

func TestPredicatesWithPackageName(t *testing.T) {
	testData := []testData{
		{map[string]string{property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`},
			"mypackage",
			true},
		{map[string]string{property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`},
			"notmypackage",
			false},
		{map[string]string{},
			"mypackage",
			false},
	}

	for _, d := range testData {
		t.Run("InMastermindsSemverRange", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			if predicates.WithPackageName(d.value)(entity) != d.result {
				if d.result {
					t.Errorf("package %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("package %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}

func TestPredicatesInMastermindsSemverRange(t *testing.T) {
	testData := []testData{
		{map[string]string{property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`},
			">=1.0.0",
			true},
		{map[string]string{property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`},
			">=2.0.0",
			false},
		{map[string]string{},
			">=1.0.0",
			false},
	}

	for _, d := range testData {
		t.Run("InMastermindsSemverRange", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			c, err := mmsemver.NewConstraint(d.value)
			if err != nil {
				t.Fatalf("unable to parse constraint '%v': %v", d.value, err)
			}
			if predicates.InMastermindsSemverRange(c)(entity) != d.result {
				if d.result {
					t.Errorf("version %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("version %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}

func TestPredicatesInBlangSemverRange(t *testing.T) {
	testData := []testData{
		{map[string]string{property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`},
			">=1.0.0",
			true},
		{map[string]string{property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`},
			">=2.0.0",
			false},
		{map[string]string{},
			">=1.0.0",
			false},
	}

	for _, d := range testData {
		t.Run("InBlangSemverRange", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			r := bsemver.MustParseRange(d.value)
			if predicates.InBlangSemverRange(r)(entity) != d.result {
				if d.result {
					t.Errorf("version %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("version %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}

func TestPredicatesInChannel(t *testing.T) {
	testData := []testData{
		{map[string]string{property.TypeChannel: `{"channelName":"stable","priority":0}`},
			"stable",
			true},
		{map[string]string{property.TypeChannel: `{"channelName":"stable","priority":0}`},
			"unstable",
			false},
		{map[string]string{},
			"stable",
			false},
	}

	for _, d := range testData {
		t.Run("InChannel", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			if predicates.InChannel(d.value)(entity) != d.result {
				if d.result {
					t.Errorf("channel %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("channel %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}

func TestPredicatesWithBundleImage(t *testing.T) {
	testData := []testData{
		{map[string]string{olmentity.PropertyBundlePath: `"registry.io/repo/image@sha256:1234567890"`},
			"registry.io/repo/image@sha256:1234567890",
			true},
		{map[string]string{olmentity.PropertyBundlePath: `"registry.io/repo/image@sha256:1234567890"`},
			"registry.io/repo/image@sha256:0987654321",
			false},
		{map[string]string{},
			"registry.io/repo/image@sha256:1234567890",
			false},
	}

	for _, d := range testData {
		t.Run("WithBundleImage", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			if predicates.WithBundleImage(d.value)(entity) != d.result {
				if d.result {
					t.Errorf("bundle %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("bundle %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}

type testGVK struct {
	entity map[string]string
	value  *olmentity.GVK
	result bool
}

func TestProvidesGVK(t *testing.T) {
	testData := []testGVK{
		{map[string]string{property.TypeGVK: `[{"group":"foo.io","kind":"Foo","version":"v1"},{"group":"bar.io","kind":"Bar","version":"v1"}]`},
			&olmentity.GVK{Group: "foo.io", Version: "v1", Kind: "Foo"},
			true},
		{map[string]string{property.TypeGVK: `[{"group":"foo.io","kind":"Foo","version":"v1"},{"group":"bar.io","kind":"Bar","version":"v1"}]`},
			&olmentity.GVK{Group: "baz.io", Version: "v1alpha1", Kind: "Baz"},
			false},
		{map[string]string{},
			&olmentity.GVK{Group: "foo.io", Version: "v1", Kind: "Foo"},
			false},
	}

	for _, d := range testData {
		t.Run("WithBundleImage", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			if predicates.ProvidesGVK(d.value)(entity) != d.result {
				if d.result {
					t.Errorf("replaces %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("replaces %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}

func TestPredicatesReplaces(t *testing.T) {
	testData := []testData{
		{map[string]string{"olm.bundle.channelEntry": `{"replaces": "test.v0.2.0"}`},
			"test.v0.2.0",
			true},
		{map[string]string{"olm.bundle.channelEntry": `{"replaces": "test.v0.2.0"}`},
			"test.v0.1.0",
			false},
		{map[string]string{},
			"test.v0.2.0",
			false},
		{map[string]string{"olm.bundle.channelEntry": `{"replaces"}`},
			"test.v0.2.0",
			false},
	}

	for _, d := range testData {
		t.Run("WithBundleImage", func(t *testing.T) {
			entity := input.NewEntity("test", d.entity)
			if predicates.Replaces(d.value)(entity) != d.result {
				if d.result {
					t.Errorf("replaces %v should be in entity %v", d.value, entity)
				} else {
					t.Errorf("replaces %v should not be in entity %v", d.value, entity)
				}
			}
		})
	}
}
