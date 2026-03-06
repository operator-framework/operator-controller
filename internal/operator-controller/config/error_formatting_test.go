package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

// Test_ErrorFormatting_SchemaLibraryVersion verifies error messages from the JSON schema
// library.
//
// These tests serve two purposes:
//  1. Guard against breaking changes if we upgrade github.com/santhosh-tekuri/jsonschema/v6
//     (tests for formatSchemaError parsing may need updates)
//  2. Document the actual error messages end users see
func Test_ErrorFormatting_SchemaLibraryVersion(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		rawConfig             []byte
		expectedErrSubstrings []string
	}{
		{
			name:      "Unknown field error formatting",
			rawConfig: []byte(`{"unknownField": "value"}`),
			expectedErrSubstrings: []string{
				"unknown field",
				"unknownField",
			},
		},
		{
			name:      "Type mismatch error formatting for deploymentConfig",
			rawConfig: []byte(`{"deploymentConfig": "not-an-object"}`),
			expectedErrSubstrings: []string{
				"invalid type",
				"deploymentConfig",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv1 := bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("test-operator").
					Build(),
			}

			schema, err := rv1.GetConfigSchema()
			require.NoError(t, err)

			_, err = config.UnmarshalConfig(tc.rawConfig, schema)
			require.Error(t, err, "Expected validation error")

			errMsg := err.Error()
			for _, substring := range tc.expectedErrSubstrings {
				require.Contains(t, errMsg, substring,
					"Error message should contain %q. Full error: %s", substring, errMsg)
			}
		})
	}
}

// Test_ErrorFormatting_YAMLParseErrors verifies YAML/JSON parsing errors are formatted correctly.
func Test_ErrorFormatting_YAMLParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		rawConfig             []byte
		expectedErrSubstrings []string
	}{
		{
			name:      "Malformed JSON",
			rawConfig: []byte(`{"incomplete`),
			expectedErrSubstrings: []string{
				"unexpected end of stream",
			},
		},
		{
			name:      "Non-object JSON",
			rawConfig: []byte(`true`),
			expectedErrSubstrings: []string{
				"invalid type",
				"got boolean, want object",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv1 := bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("test-operator").
					Build(),
			}

			schema, err := rv1.GetConfigSchema()
			require.NoError(t, err)

			_, err = config.UnmarshalConfig(tc.rawConfig, schema)
			require.Error(t, err, "Expected parse error")

			errMsg := err.Error()
			for _, substring := range tc.expectedErrSubstrings {
				require.Contains(t, errMsg, substring,
					"Error message should contain %q. Full error: %s", substring, errMsg)
			}
		})
	}
}
