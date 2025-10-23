package bundleutil_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
)

func TestGetVersionAndRelease(t *testing.T) {
	tests := []struct {
		name        string
		pkgProperty *property.Property
		wantErr     bool
	}{
		{
			name: "valid version",
			pkgProperty: &property.Property{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"version": "1.0.0-pre+1.alpha.2"}`),
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
			wantErr: true,
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

			_, err := bundleutil.GetVersionAndRelease(bundle)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
