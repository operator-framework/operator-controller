package resolve

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

func TestNoDependencyValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		bundle  declcfg.Bundle
		wantErr string
	}{
		{
			name: "package with no dependencies",
			bundle: declcfg.Bundle{
				Name:    "fake-catalog/no-dependencies-package/alpha/1.0.0",
				Package: "no-dependencies-package",
				Image:   "quay.io/fake-catalog/no-dependencies-package@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"no-dependencies-package","version":"1.0.0"}`)},
				},
			},
		},
		{
			name: "package with olm.package.required property",
			bundle: declcfg.Bundle{
				Name:    "fake-catalog/package-required-test/alpha/1.0.0",
				Package: "package-required-test",
				Image:   "quay.io/fake-catalog/package-required-test@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"package-required-test","version":"1.0.0"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage("content-is-not-relevant")},
				},
			},
			wantErr: `bundle "fake-catalog/package-required-test/alpha/1.0.0" has a dependency declared via property "olm.package.required" which is currently not supported`,
		},
		{
			name: "package with olm.gvk.required property",
			bundle: declcfg.Bundle{
				Name:    "fake-catalog/gvk-required-test/alpha/1.0.0",
				Package: "gvk-required-test",
				Image:   "quay.io/fake-catalog/gvk-required-test@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"gvk-required-test","version":"1.0.0"}`)},
					{Type: property.TypeGVKRequired, Value: json.RawMessage(`content-is-not-relevant`)},
				},
			},
			wantErr: `bundle "fake-catalog/gvk-required-test/alpha/1.0.0" has a dependency declared via property "olm.gvk.required" which is currently not supported`,
		},
		{
			name: "package with olm.constraint property",
			bundle: declcfg.Bundle{
				Name:    "fake-catalog/constraint-test/alpha/1.0.0",
				Package: "constraint-test",
				Image:   "quay.io/fake-catalog/constraint-test@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"constraint-test","version":"1.0.0"}`)},
					{Type: property.TypeConstraint, Value: json.RawMessage(`content-is-not-relevant`)},
				},
			},
			wantErr: `bundle "fake-catalog/constraint-test/alpha/1.0.0" has a dependency declared via property "olm.constraint" which is currently not supported`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := NoDependencyValidation(&tt.bundle)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}
