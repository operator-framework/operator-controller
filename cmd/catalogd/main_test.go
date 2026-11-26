package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetiredCatalogsFlag(t *testing.T) {
	// The OpenShift Helm values pass --retired-catalogs to catalogd; an unknown
	// flag would prevent catalogd from starting.
	flag := catalogdCmd.Flags().Lookup("retired-catalogs")
	require.NotNil(t, flag)
	assert.Equal(t, "stringSlice", flag.Value.Type())
	assert.Equal(t, "[]", flag.DefValue)
}

func TestValidateConfigRetiredCatalogs(t *testing.T) {
	for _, tc := range []struct {
		name            string
		retiredCatalogs []string
		wantErr         string
	}{
		{
			name:            "no retired catalogs",
			retiredCatalogs: nil,
		},
		{
			name:            "valid catalog names",
			retiredCatalogs: []string{"openshift-redhat-marketplace", "example.catalog"},
		},
		{
			name:            "empty name is rejected",
			retiredCatalogs: []string{"openshift-redhat-marketplace", ""},
			wantErr:         `invalid retired-catalogs value ""`,
		},
		{
			name:            "name with surrounding whitespace is rejected",
			retiredCatalogs: []string{"openshift-redhat-marketplace", " example-catalog"},
			wantErr:         `invalid retired-catalogs value " example-catalog"`,
		},
		{
			name:            "name that is not a valid object name is rejected",
			retiredCatalogs: []string{"OpenShift-Redhat-Marketplace"},
			wantErr:         `invalid retired-catalogs value "OpenShift-Redhat-Marketplace"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfig(&config{retiredCatalogs: tc.retiredCatalogs})
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
