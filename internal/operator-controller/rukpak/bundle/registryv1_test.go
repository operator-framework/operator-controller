package bundle

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBundleConfigSchemaMap(t *testing.T) {
	schema, err := getBundleConfigSchemaMap()
	require.NoError(t, err, "should successfully get bundle config schema")
	require.NotNil(t, schema, "schema should not be nil")

	t.Run("schema has correct metadata", func(t *testing.T) {
		assert.Equal(t, "http://json-schema.org/draft-07/schema#", schema["$schema"])
		assert.Contains(t, schema["$id"], "registry-v1-bundle-config")
		assert.Equal(t, "Registry+v1 Bundle Configuration", schema["title"])
		assert.NotEmpty(t, schema["description"])
		assert.Equal(t, "object", schema["type"])
		assert.Equal(t, false, schema["additionalProperties"])
	})

	t.Run("schema includes watchNamespace and deploymentConfig properties", func(t *testing.T) {
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok, "schema should have properties")

		assert.Contains(t, properties, "watchNamespace")
		assert.Contains(t, properties, "deploymentConfig")
	})

	t.Run("watchNamespace has anyOf with null and string", func(t *testing.T) {
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok)

		watchNamespace, ok := properties["watchNamespace"].(map[string]any)
		require.True(t, ok, "watchNamespace should be present")

		anyOf, ok := watchNamespace["anyOf"].([]any)
		require.True(t, ok, "watchNamespace should have anyOf")
		assert.Len(t, anyOf, 2, "watchNamespace anyOf should have 2 options")
	})

	t.Run("deploymentConfig has expected structure", func(t *testing.T) {
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok)

		deploymentConfig, ok := properties["deploymentConfig"].(map[string]any)
		require.True(t, ok, "deploymentConfig should be present")

		assert.Equal(t, "object", deploymentConfig["type"])
		assert.Equal(t, false, deploymentConfig["additionalProperties"])
		assert.NotEmpty(t, deploymentConfig["description"])

		dcProps, ok := deploymentConfig["properties"].(map[string]any)
		require.True(t, ok, "deploymentConfig should have properties")

		// Verify expected fields from SubscriptionConfig
		expectedFields := []string{
			"nodeSelector",
			"tolerations",
			"resources",
			"env",
			"envFrom",
			"volumes",
			"volumeMounts",
			"affinity",
			"annotations",
		}

		for _, field := range expectedFields {
			assert.Contains(t, dcProps, field, "deploymentConfig should include %s field", field)
		}

		// Verify selector is NOT included
		assert.NotContains(t, dcProps, "selector", "selector field should be excluded per RFC")
	})

	t.Run("schema includes components/schemas for OpenAPI types", func(t *testing.T) {
		components, ok := schema["components"].(map[string]any)
		require.True(t, ok, "schema should have components section for $ref resolution")

		schemas, ok := components["schemas"].(map[string]any)
		require.True(t, ok, "components should have schemas")
		assert.NotEmpty(t, schemas, "components/schemas should not be empty")

		// Verify some expected Kubernetes types are included
		expectedTypes := []string{
			"io.k8s.api.core.v1.Toleration",
			"io.k8s.api.core.v1.ResourceRequirements",
			"io.k8s.api.core.v1.EnvVar",
		}

		for _, typeName := range expectedTypes {
			assert.Contains(t, schemas, typeName, "components/schemas should include %s", typeName)
		}
	})
}

// TestSchemaCompilation verifies that the generated schema can be compiled
// by a JSON schema validator without errors. This catches broken $ref targets
// and other structural issues.
func TestSchemaCompilation(t *testing.T) {
	// Get the schema as a map (same as how config package uses it)
	schemaMap, err := getBundleConfigSchemaMap()
	require.NoError(t, err, "should successfully get bundle config schema")

	// Compile the schema using the same library used by config package
	compiler := jsonschema.NewCompiler()

	// Add the schema resource (using map[string]any, same as config package)
	err = compiler.AddResource("schema.json", schemaMap)
	require.NoError(t, err, "should add schema resource to compiler")

	compiledSchema, err := compiler.Compile("schema.json")
	require.NoError(t, err, "schema should compile without errors - this verifies all $ref targets are resolvable")
	require.NotNil(t, compiledSchema, "compiled schema should not be nil")
}
