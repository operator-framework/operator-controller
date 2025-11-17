package config_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_UnmarshalConfig(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		rawConfig              []byte
		supportedInstallModes  []v1alpha1.InstallModeType
		installNamespace       string
		expectedErrMessage     string
		expectedWatchNamespace *string // Expected value from GetWatchNamespace()
	}{
		{
			name:                   "accepts nil config when AllNamespaces is supported",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			rawConfig:              nil,
			expectedWatchNamespace: nil,
		},
		{
			name:                  "rejects nil config when OwnNamespace-only requires watchNamespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             nil,
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects nil config when SingleNamespace-only requires watchNamespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             nil,
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                   "accepts json config",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "install-ns", // SingleNamespace requires watchNamespace != installNamespace
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                   "accepts yaml config",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`watchNamespace: some-namespace`),
			installNamespace:       "install-ns", // SingleNamespace requires watchNamespace != installNamespace
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                  "rejects invalid json",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"hello`),
			expectedErrMessage:    `invalid configuration: found unexpected end of stream`,
		},
		{
			name:                  "rejects valid json that isn't of object type",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`true`),
			expectedErrMessage:    `got boolean, want object`,
		},
		{
			name:                  "rejects additional fields",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			rawConfig:             []byte(`somekey: somevalue`),
			expectedErrMessage:    `unknown field "somekey"`,
		},
		{
			name:                  "rejects valid json but invalid registry+v1",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": {"hello": "there"}}`),
			expectedErrMessage:    `got object, want string`,
		},
		{
			name:                  "rejects with unknown field when install modes {AllNamespaces}",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedErrMessage:    `unknown field "watchNamespace"`,
		},
		{
			name:                  "rejects with unknown field when install modes {MultiNamespace}",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedErrMessage:    `unknown field "watchNamespace"`,
		},
		{
			name:                  "reject with unknown field when install modes {AllNamespaces, MultiNamespace}",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedErrMessage:    `unknown field "watchNamespace"`,
		},
		{
			name:                  "reject with required field when install modes {OwnNamespace} and watchNamespace is null",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "reject with required field when install modes {OwnNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "reject with required field when install modes {MultiNamespace, OwnNamespace} and watchNamespace is null",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "reject with required field when install modes {MultiNamespace, OwnNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                   "accepts when install modes {SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "install-ns",
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                   "accepts when install modes {AllNamespaces, SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "install-ns",
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                   "accepts when install modes {MultiNamespace, SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "install-ns",
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                   "accepts when install modes {OwnNamespace, SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "not-namespace",
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                  "rejects when install modes {SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedErrMessage:    "invalid configuration:",
		},
		{
			name:                  "rejects when install modes {AllNamespaces, SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedErrMessage:    "invalid configuration:",
		},
		{
			name:                  "rejects when install modes {MultiNamespace, SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedErrMessage:    "invalid configuration:",
		},
		{
			name:                   "accepts when install modes {AllNamespaces, OwnNamespace} and watchNamespace == install namespace",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "some-namespace",
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                   "accepts when install modes {OwnNamespace, SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:              []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:       "some-namespace",
			expectedWatchNamespace: ptr.To("some-namespace"),
		},
		{
			name:                  "rejects when install modes {AllNamespaces, OwnNamespace} and watchNamespace != install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "invalid configuration:",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, OwnNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, OwnNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, MultiNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, OwnNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    `required field "watchNamespace" is missing`,
		},
		{
			name:                   "accepts null watchNamespace when install modes {AllNamespaces, OwnNamespace} and watchNamespace is nil",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:              []byte(`{"watchNamespace": null}`),
			installNamespace:       "not-some-namespace",
			expectedWatchNamespace: nil,
		},
		{
			name:                   "accepts null watchNamespace when install modes {AllNamespaces, OwnNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:              []byte(`{"watchNamespace": null}`),
			installNamespace:       "not-some-namespace",
			expectedWatchNamespace: nil,
		},
		{
			name:                   "accepts no watchNamespace when install modes {AllNamespaces, OwnNamespace} and watchNamespace is nil",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:              []byte(`{}`),
			installNamespace:       "not-some-namespace",
			expectedWatchNamespace: nil,
		},
		{
			name:                   "accepts no watchNamespace when install modes {AllNamespaces, OwnNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:              []byte(`{}`),
			installNamespace:       "not-some-namespace",
			expectedWatchNamespace: nil,
		},
		{
			name:                   "skips validation when installNamespace is empty for OwnNamespace only",
			supportedInstallModes:  []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:              []byte(`{"watchNamespace": "valid-ns"}`),
			installNamespace:       "",
			expectedWatchNamespace: ptr.To("valid-ns"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rv1 bundle.RegistryV1
			if tc.supportedInstallModes != nil {
				rv1 = bundle.RegistryV1{
					CSV: clusterserviceversion.Builder().
						WithName("test-operator").
						WithInstallModeSupportFor(tc.supportedInstallModes...).
						Build(),
				}
			}

			schema, err := rv1.GetConfigSchema()
			require.NoError(t, err)

			cfg, err := config.UnmarshalConfig(tc.rawConfig, schema, tc.installNamespace)
			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg)
				if tc.expectedWatchNamespace == nil {
					require.Nil(t, cfg.GetWatchNamespace())
				} else {
					require.Equal(t, *tc.expectedWatchNamespace, *cfg.GetWatchNamespace())
				}
			}
		})
	}
}

// Test_UnmarshalConfig_EmptySchema tests when a ClusterExtension doesn't provide a configuration schema.
func Test_UnmarshalConfig_EmptySchema(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		rawConfig              []byte
		expectedErrMessage     string
		expectedWatchNamespace *string
	}{
		{
			name:                   "no config provided",
			rawConfig:              nil,
			expectedWatchNamespace: nil,
		},
		{
			name:                   "empty config provided",
			rawConfig:              []byte(`{}`),
			expectedWatchNamespace: nil,
		},
		{
			name:                   "config with watchNamespace provided",
			rawConfig:              []byte(`{"watchNamespace": "some-ns"}`),
			expectedWatchNamespace: ptr.To("some-ns"),
		},
		{
			name:                   "config with unknown fields provided",
			rawConfig:              []byte(`{"someField": "someValue"}`),
			expectedWatchNamespace: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emptySchemaBundle := &mockEmptySchemaBundle{}
			schema, err := emptySchemaBundle.GetConfigSchema()
			require.NoError(t, err)

			config, err := config.UnmarshalConfig(tc.rawConfig, schema, "my-namespace")

			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, config)
				if tc.expectedWatchNamespace == nil {
					require.Nil(t, config.GetWatchNamespace())
				} else {
					require.Equal(t, *tc.expectedWatchNamespace, *config.GetWatchNamespace())
				}
			}
		})
	}
}

// Test_UnmarshalConfig_HelmLike proves validation works the same for ANY package format type.
//
//   - registry+v1 -> generates schema from install modes
//   - Helm -> reads values.schema.json from chart
//   - registry+v2 -> (future) provides schema via its own mechanism
//
// Same validation process regardless of package format type.
func Test_UnmarshalConfig_HelmLike(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		rawConfig              []byte
		helmSchema             string // what values.schema.json would contain
		expectedErrMessage     string
		expectedWatchNamespace *string
	}{
		{
			name: "Helm chart with typical config values (no watchNamespace)",
			rawConfig: []byte(`{
				"replicaCount": 3,
				"image": {"tag": "v1.2.3"},
				"service": {"port": 8080}
			}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": {
					"replicaCount": {"type": "integer", "minimum": 1},
					"image": {
						"type": "object",
						"properties": {
							"tag": {"type": "string"}
						}
					},
					"service": {
						"type": "object",
						"properties": {
							"port": {"type": "integer"}
						}
					}
				}
			}`,
			expectedWatchNamespace: nil,
		},
		{
			name: "Helm chart that ALSO uses watchNamespace (mixed config)",
			rawConfig: []byte(`{
				"watchNamespace": "my-app-namespace",
				"replicaCount": 2,
				"debug": true
			}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": {
					"watchNamespace": {"type": "string"},
					"replicaCount": {"type": "integer"},
					"debug": {"type": "boolean"}
				}
			}`,
			// watchNamespace gets extracted, other fields validated by schema
			expectedWatchNamespace: ptr.To("my-app-namespace"),
		},
		{
			name:      "Schema validation catches constraint violations (replicaCount below minimum)",
			rawConfig: []byte(`{"replicaCount": 0}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": {
					"replicaCount": {"type": "integer", "minimum": 1}
				}
			}`,
			expectedErrMessage: "invalid configuration:",
		},
		{
			name:      "Schema validation catches type mismatches",
			rawConfig: []byte(`{"replicaCount": "three"}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": {
					"replicaCount": {"type": "integer"}
				}
			}`,
			expectedErrMessage: "invalid configuration:",
		},
		{
			name:      "Empty config is valid when no required fields",
			rawConfig: nil,
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": {
					"replicaCount": {"type": "integer", "default": 1}
				}
			}`,
			expectedWatchNamespace: nil,
		},
		{
			name:      "Required fields are enforced by schema",
			rawConfig: []byte(`{"optional": "value"}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"required": ["requiredField"],
				"properties": {
					"requiredField": {"type": "string"},
					"optional": {"type": "string"}
				}
			}`,
			expectedErrMessage: `required field "requiredField" is missing`,
		},
		{
			name: "Helm with watchNamespace accepts any string value (K8s validates at runtime)",
			rawConfig: []byte(`{
				"watchNamespace": "any-value-here",
				"replicaCount": 2
			}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": {
					"watchNamespace": {"type": "string"},
					"replicaCount": {"type": "integer"}
				}
			}`,
			expectedWatchNamespace: ptr.To("any-value-here"),
		},
		{
			name: "Helm with watchNamespace using ownNamespaceInstallMode format (OwnNamespace-like)",
			rawConfig: []byte(`{
				"watchNamespace": "wrong-namespace"
			}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"required": ["watchNamespace"],
				"properties": {
					"watchNamespace": {"type": "string", "format": "ownNamespaceInstallMode"}
				}
			}`,
			expectedErrMessage: "invalid configuration:",
		},
		{
			name: "Helm with watchNamespace using singleNamespaceInstallMode format (SingleNamespace-like)",
			rawConfig: []byte(`{
				"watchNamespace": "my-namespace"
			}`),
			helmSchema: `{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"required": ["watchNamespace"],
				"properties": {
					"watchNamespace": {"type": "string", "format": "singleNamespaceInstallMode"}
				}
			}`,
			expectedErrMessage: "invalid configuration:",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock Helm package (real Helm would read values.schema.json)
			helmBundle := &mockHelmBundle{schema: tc.helmSchema}
			schema, err := helmBundle.GetConfigSchema()
			require.NoError(t, err)

			// Same validation function works for Helm, registry+v1, registry+v2, etc.
			config, err := config.UnmarshalConfig(tc.rawConfig, schema, "my-namespace")

			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, config)
				if tc.expectedWatchNamespace == nil {
					require.Nil(t, config.GetWatchNamespace())
				} else {
					require.Equal(t, *tc.expectedWatchNamespace, *config.GetWatchNamespace())
				}
			}
		})
	}
}

// mockHelmBundle shows how Helm would plug into the validation system.
//
// Real implementation would:
//  1. Read values.schema.json from the Helm chart package
//  2. Parse it into a map[string]any
//  3. Return it (just like registry+v1 returns its generated schema)
//  4. Let the shared validation logic handle the rest
type mockHelmBundle struct {
	schema string
}

// GetConfigSchema returns the schema (in real Helm, read from values.schema.json).
func (h *mockHelmBundle) GetConfigSchema() (map[string]any, error) {
	if h.schema == "" {
		return nil, nil
	}
	var schemaMap map[string]any
	if err := json.Unmarshal([]byte(h.schema), &schemaMap); err != nil {
		return nil, err
	}
	return schemaMap, nil
}

// mockEmptySchemaBundle represents a ClusterExtension that doesn't provide a configuration schema.
type mockEmptySchemaBundle struct{}

func (e *mockEmptySchemaBundle) GetConfigSchema() (map[string]any, error) {
	return nil, nil
}
