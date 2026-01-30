package config_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

// Test_ErrorFormatting_SchemaLibraryVersion verifies error messages from the JSON schema
// library and our custom format validators.
//
// These tests serve two purposes:
//  1. Guard against breaking changes if we upgrade github.com/santhosh-tekuri/jsonschema/v6
//     (tests for formatSchemaError parsing may need updates)
//  2. Document the actual error messages end users see (especially for namespace constraints)
func Test_ErrorFormatting_SchemaLibraryVersion(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		rawConfig             []byte
		supportedInstallModes []v1alpha1.InstallModeType
		installNamespace      string
		// We verify the error message contains these key phrases
		expectedErrSubstrings []string
	}{
		{
			name:                  "Unknown field error formatting",
			rawConfig:             []byte(`{"unknownField": "value"}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			expectedErrSubstrings: []string{
				"unknown field",
				"unknownField",
			},
		},
		{
			name:                  "Required field missing error formatting",
			rawConfig:             []byte(`{}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			expectedErrSubstrings: []string{
				"required field",
				"watchNamespace",
				"is missing",
			},
		},
		{
			name:                  "Required field null error formatting",
			rawConfig:             []byte(`{"watchNamespace": null}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			expectedErrSubstrings: []string{
				"required field",
				"watchNamespace",
				"is missing",
			},
		},
		{
			name:                  "Type mismatch error formatting",
			rawConfig:             []byte(`{"watchNamespace": 123}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			expectedErrSubstrings: []string{
				"invalid type",
				"watchNamespace",
			},
		},
		{
			name:                  "OwnNamespace constraint error formatting",
			rawConfig:             []byte(`{"watchNamespace": "wrong-namespace"}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			installNamespace:      "correct-namespace",
			expectedErrSubstrings: []string{
				"invalid format for field \"watchNamespace\"",
				"invalid value",
				"wrong-namespace",
				"correct-namespace",
				"the namespace where the operator is installed",
				"OwnNamespace install mode",
			},
		},
		{
			name:                  "SingleNamespace constraint error formatting",
			rawConfig:             []byte(`{"watchNamespace": "install-ns"}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			installNamespace:      "install-ns",
			expectedErrSubstrings: []string{
				"invalid format for field \"watchNamespace\"",
				"not valid singleNamespaceInstallMode",
				"invalid value",
				"install-ns",
				"must be different from",
				"the install namespace",
				"SingleNamespace install mode",
				"watch a different namespace",
			},
		},
		{
			name:                  "SingleNamespace constraint error bad namespace format",
			rawConfig:             []byte(`{"watchNamespace": "---AAAA-BBBB-super-long-namespace-that-that-is-waaaaaaaaayyy-longer-than-sixty-three-characters"}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			installNamespace:      "install-ns",
			expectedErrSubstrings: []string{
				"field \"watchNamespace\"",
				"must match pattern \"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$\"",
			},
		},
		{
			name:                  "Single- and OwnNamespace constraint error bad namespace format",
			rawConfig:             []byte(`{"watchNamespace": ` + strings.Repeat("A", 63) + `"}`),
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			installNamespace:      "install-ns",
			expectedErrSubstrings: []string{
				"invalid configuration",
				"multiple errors found",
				"must have maximum length of 63 (len=64)",
				"must match pattern \"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$\"",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv1 := bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("test-operator").
					WithInstallModeSupportFor(tc.supportedInstallModes...).
					Build(),
			}

			schema, err := rv1.GetConfigSchema()
			require.NoError(t, err)

			_, err = config.UnmarshalConfig(tc.rawConfig, schema, tc.installNamespace)
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
		{
			name:      "Wrong type for field",
			rawConfig: []byte(`{"watchNamespace": {"nested": "object"}}`),
			expectedErrSubstrings: []string{
				"invalid type",
				"got object, want string",
				"watchNamespace",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv1 := bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("test-operator").
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).
					Build(),
			}

			schema, err := rv1.GetConfigSchema()
			require.NoError(t, err)

			_, err = config.UnmarshalConfig(tc.rawConfig, schema, "test-namespace")
			require.Error(t, err, "Expected parse error")

			errMsg := err.Error()
			for _, substring := range tc.expectedErrSubstrings {
				require.Contains(t, errMsg, substring,
					"Error message should contain %q. Full error: %s", substring, errMsg)
			}
		})
	}
}
