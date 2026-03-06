package config_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_UnmarshalConfig(t *testing.T) {
	for _, tc := range []struct {
		name               string
		rawConfig          []byte
		expectedErrMessage string
	}{
		{
			name:      "accepts nil config",
			rawConfig: nil,
		},
		{
			name:      "accepts empty config",
			rawConfig: []byte(`{}`),
		},
		{
			name:      "accepts json config with deploymentConfig",
			rawConfig: []byte(`{"deploymentConfig": {"env": [{"name": "FOO", "value": "bar"}]}}`),
		},
		{
			name:               "rejects invalid json",
			rawConfig:          []byte(`{"hello`),
			expectedErrMessage: `invalid configuration: found unexpected end of stream`,
		},
		{
			name:               "rejects valid json that isn't of object type",
			rawConfig:          []byte(`true`),
			expectedErrMessage: `got boolean, want object`,
		},
		{
			name:               "rejects additional fields",
			rawConfig:          []byte(`somekey: somevalue`),
			expectedErrMessage: `unknown field "somekey"`,
		},
		{
			name: "rejects selector field in deploymentConfig (v0 field not supported in v1)",
			rawConfig: []byte(`{
				"deploymentConfig": {
					"selector": {
						"matchLabels": {
							"app": "test"
						}
					}
				}
			}`),
			expectedErrMessage: `unknown field "selector"`,
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

			cfg, err := config.UnmarshalConfig(tc.rawConfig, schema)
			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			}
		})
	}
}

// Test_UnmarshalConfig_EmptySchema tests when a ClusterExtension doesn't provide a configuration schema.
func Test_UnmarshalConfig_EmptySchema(t *testing.T) {
	for _, tc := range []struct {
		name               string
		rawConfig          []byte
		expectedErrMessage string
	}{
		{
			name:      "no config provided",
			rawConfig: nil,
		},
		{
			name:      "empty config provided",
			rawConfig: []byte(`{}`),
		},
		{
			name:      "config with unknown fields provided",
			rawConfig: []byte(`{"someField": "someValue"}`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noSchemaBundle := &mockNoSchemaBundle{}
			schema, err := noSchemaBundle.GetConfigSchema()
			require.NoError(t, err)

			config, err := config.UnmarshalConfig(tc.rawConfig, schema)

			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, config)
			}
		})
	}
}

// Test_UnmarshalConfig_HelmLike proves validation works the same for ANY package format type.
//
//   - registry+v1 -> generates schema from bundle
//   - Helm -> reads values.schema.json from chart
//   - registry+v2 -> (future) provides schema via its own mechanism
//
// Same validation process regardless of package format type.
func Test_UnmarshalConfig_HelmLike(t *testing.T) {
	for _, tc := range []struct {
		name               string
		rawConfig          []byte
		helmSchema         string // what values.schema.json would contain
		expectedErrMessage string
	}{
		{
			name: "Helm chart with typical config values",
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock Helm package (real Helm would read values.schema.json)
			helmBundle := &mockHelmBundle{schema: tc.helmSchema}
			schema, err := helmBundle.GetConfigSchema()
			require.NoError(t, err)

			// Same validation function works for Helm, registry+v1, registry+v2, etc.
			config, err := config.UnmarshalConfig(tc.rawConfig, schema)

			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, config)
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

// mockNoSchemaBundle represents a bundle that doesn't provide a configuration schema.
type mockNoSchemaBundle struct{}

func (e *mockNoSchemaBundle) GetConfigSchema() (map[string]any, error) {
	// Return nil to indicate "no schema" (skip validation)
	return nil, nil
}

// Test_GetDeploymentConfig tests the GetDeploymentConfig accessor method.
func Test_GetDeploymentConfig(t *testing.T) {
	// Create a bundle that returns nil schema (no validation)
	bundle := &mockNoSchemaBundle{}

	tests := []struct {
		name                        string
		rawConfig                   []byte
		expectedDeploymentConfig    map[string]any
		expectedDeploymentConfigNil bool
	}{
		{
			name:                        "empty config returns nil",
			rawConfig:                   []byte(`{}`),
			expectedDeploymentConfigNil: true,
		},
		{
			name:                        "config without deploymentConfig field returns nil",
			rawConfig:                   []byte(`{"someField": "test-ns"}`),
			expectedDeploymentConfigNil: true,
		},
		{
			name:                        "config with null deploymentConfig returns nil",
			rawConfig:                   []byte(`{"deploymentConfig": null}`),
			expectedDeploymentConfigNil: true,
		},
		{
			name: "config with valid deploymentConfig returns the object",
			rawConfig: []byte(`{
				"deploymentConfig": {
					"nodeSelector": {
						"kubernetes.io/os": "linux"
					},
					"resources": {
						"requests": {
							"memory": "128Mi"
						}
					}
				}
			}`),
			expectedDeploymentConfig: map[string]any{
				"nodeSelector": map[string]any{
					"kubernetes.io/os": "linux",
				},
				"resources": map[string]any{
					"requests": map[string]any{
						"memory": "128Mi",
					},
				},
			},
			expectedDeploymentConfigNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := bundle.GetConfigSchema()
			require.NoError(t, err)

			cfg, err := config.UnmarshalConfig(tt.rawConfig, schema)
			require.NoError(t, err)

			result := cfg.GetDeploymentConfig()
			if tt.expectedDeploymentConfigNil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.expectedDeploymentConfig, result)
			}
		})
	}

	// Test nil config separately
	t.Run("nil config returns nil", func(t *testing.T) {
		var cfg *config.Config
		result := cfg.GetDeploymentConfig()
		require.Nil(t, result)
	})

	// Test that returned map is a defensive copy (mutations don't affect original)
	t.Run("returned map is defensive copy - mutations don't affect original", func(t *testing.T) {
		rawConfig := []byte(`{
			"deploymentConfig": {
				"nodeSelector": {
					"kubernetes.io/os": "linux"
				}
			}
		}`)

		schema, err := bundle.GetConfigSchema()
		require.NoError(t, err)

		cfg, err := config.UnmarshalConfig(rawConfig, schema)
		require.NoError(t, err)

		// Get the deploymentConfig
		result1 := cfg.GetDeploymentConfig()
		require.NotNil(t, result1)

		// Mutate the returned map
		result1["nodeSelector"] = map[string]any{
			"mutated": "value",
		}
		result1["newField"] = "added"

		// Get deploymentConfig again - should be unaffected by mutations
		result2 := cfg.GetDeploymentConfig()
		require.NotNil(t, result2)

		// Original values should be intact
		require.Equal(t, map[string]any{
			"nodeSelector": map[string]any{
				"kubernetes.io/os": "linux",
			},
		}, result2)

		// New field should not exist
		_, exists := result2["newField"]
		require.False(t, exists)

		// result1 should have the mutations
		require.Equal(t, "added", result1["newField"])
	})
}
