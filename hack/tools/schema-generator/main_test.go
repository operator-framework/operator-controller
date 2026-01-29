package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getPackageDir returns the directory path of the specified Go package.
// It uses 'go list' which automatically handles both vendor mode and module cache.
func getPackageDir(t *testing.T, pkgPath string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", pkgPath)
	out, err := cmd.Output()
	require.NoError(t, err, "failed to find package %s", pkgPath)
	return strings.TrimSpace(string(out))
}

// Mock OpenAPI spec for testing
func getMockOpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		Components: struct {
			Schemas map[string]interface{} `json:"schemas"`
		}{
			Schemas: map[string]interface{}{
				"io.k8s.api.core.v1.Toleration": map[string]interface{}{
					"type":        "object",
					"description": "The pod this Toleration is attached to tolerates any taint that matches the triple <key,value,effect> using the matching operator <operator>.",
					"properties": map[string]interface{}{
						"key":               map[string]string{"type": "string"},
						"operator":          map[string]string{"type": "string"},
						"value":             map[string]string{"type": "string"},
						"effect":            map[string]string{"type": "string"},
						"tolerationSeconds": map[string]interface{}{"type": "integer", "format": "int64"},
					},
				},
				"io.k8s.api.core.v1.ResourceRequirements": map[string]interface{}{
					"type":        "object",
					"description": "ResourceRequirements describes the compute resource requirements.",
					"properties": map[string]interface{}{
						"limits":   map[string]interface{}{"type": "object"},
						"requests": map[string]interface{}{"type": "object"},
					},
				},
				"io.k8s.api.core.v1.EnvVar": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"name": map[string]string{"type": "string"}},
				},
				"io.k8s.api.core.v1.EnvFromSource": map[string]interface{}{
					"type": "object",
				},
				"io.k8s.api.core.v1.Volume": map[string]interface{}{
					"type": "object",
				},
				"io.k8s.api.core.v1.VolumeMount": map[string]interface{}{
					"type": "object",
				},
				"io.k8s.api.core.v1.Affinity": map[string]interface{}{
					"type": "object",
				},
			},
		},
	}
}

func TestParseSubscriptionConfig(t *testing.T) {
	// Get the package directory containing subscription_types.go
	pkgDir := getPackageDir(t, "github.com/operator-framework/api/pkg/operators/v1alpha1")
	subscriptionTypesFile := filepath.Join(pkgDir, "subscription_types.go")

	fields, err := parseSubscriptionConfig(subscriptionTypesFile)
	require.NoError(t, err, "should successfully parse SubscriptionConfig")
	require.NotEmpty(t, fields, "should find fields in SubscriptionConfig")

	// Create a map for easier checking
	fieldMap := make(map[string]FieldInfo)
	for _, field := range fields {
		fieldMap[field.JSONName] = field
	}

	t.Run("includes expected fields", func(t *testing.T) {
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

		for _, fieldName := range expectedFields {
			assert.Contains(t, fieldMap, fieldName, "should include %s field", fieldName)
		}
	})

	t.Run("excludes selector field", func(t *testing.T) {
		assert.NotContains(t, fieldMap, "selector", "should exclude selector field per RFC requirement")
	})

	t.Run("parses field types correctly", func(t *testing.T) {
		// Check tolerations is a slice
		tolerations, ok := fieldMap["tolerations"]
		require.True(t, ok, "tolerations should be present")
		assert.True(t, tolerations.IsSlice, "tolerations should be a slice")
		assert.Equal(t, "corev1", tolerations.TypePkg, "tolerations should be from corev1 package")
		assert.Equal(t, "Toleration", tolerations.TypeName, "tolerations type should be Toleration")

		// Check nodeSelector is a map
		nodeSelector, ok := fieldMap["nodeSelector"]
		require.True(t, ok, "nodeSelector should be present")
		assert.True(t, nodeSelector.IsMap, "nodeSelector should be a map")

		// Check resources is an object (pointer)
		resources, ok := fieldMap["resources"]
		require.True(t, ok, "resources should be present")
		assert.Equal(t, "corev1", resources.TypePkg)
		assert.Equal(t, "ResourceRequirements", resources.TypeName)
	})
}

func TestGenerateBundleConfigSchema(t *testing.T) {
	mockOpenAPI := getMockOpenAPISpec()

	// Create mock fields similar to what parseSubscriptionConfig would return
	fields := []FieldInfo{
		{JSONName: "nodeSelector", IsMap: true},
		{JSONName: "tolerations", TypePkg: "corev1", TypeName: "Toleration", IsSlice: true},
		{JSONName: "resources", TypePkg: "corev1", TypeName: "ResourceRequirements"},
		{JSONName: "annotations", IsMap: true},
	}

	schema := generateBundleConfigSchema(mockOpenAPI, fields)

	t.Run("schema has correct metadata", func(t *testing.T) {
		assert.Equal(t, "http://json-schema.org/draft-07/schema#", schema.Schema)
		assert.Equal(t, schemaID, schema.ID)
		assert.Equal(t, schemaTitle, schema.Title)
		assert.NotEmpty(t, schema.Description)
		assert.Equal(t, "object", schema.Type)
		assert.False(t, schema.AdditionalProperties)
	})

	t.Run("includes watchNamespace property", func(t *testing.T) {
		require.Contains(t, schema.Properties, "watchNamespace")

		watchNS := schema.Properties["watchNamespace"]
		require.NotNil(t, watchNS)

		assert.NotEmpty(t, watchNS.Description)
		assert.Len(t, watchNS.AnyOf, 2, "watchNamespace should have anyOf with null and string")
	})

	t.Run("includes deploymentConfig property", func(t *testing.T) {
		require.Contains(t, schema.Properties, "deploymentConfig")

		deployConfig := schema.Properties["deploymentConfig"]
		require.NotNil(t, deployConfig)

		assert.Equal(t, "object", deployConfig.Type)
		assert.NotEmpty(t, deployConfig.Description)
		assert.Equal(t, false, deployConfig.AdditionalProperties)

		// Check that our mock fields are present
		assert.Contains(t, deployConfig.Properties, "nodeSelector")
		assert.Contains(t, deployConfig.Properties, "tolerations")
		assert.Contains(t, deployConfig.Properties, "resources")
		assert.Contains(t, deployConfig.Properties, "annotations")
	})
}

func TestMapFieldToOpenAPISchema(t *testing.T) {
	mockOpenAPI := getMockOpenAPISpec()
	collector := &schemaCollector{
		openAPISpec:      mockOpenAPI,
		collectedSchemas: make(map[string]bool),
	}

	t.Run("maps map fields correctly", func(t *testing.T) {
		field := FieldInfo{
			JSONName: "nodeSelector",
			IsMap:    true,
		}

		schema := mapFieldToOpenAPISchema(field, mockOpenAPI, collector)
		require.NotNil(t, schema)

		assert.Equal(t, "object", schema.Type)
		assert.NotNil(t, schema.AdditionalProperties)
	})

	t.Run("maps slice fields correctly", func(t *testing.T) {
		field := FieldInfo{
			JSONName: "tolerations",
			TypePkg:  "corev1",
			TypeName: "Toleration",
			IsSlice:  true,
		}

		schema := mapFieldToOpenAPISchema(field, mockOpenAPI, collector)
		require.NotNil(t, schema)

		assert.Equal(t, "array", schema.Type)
		assert.NotNil(t, schema.Items)

		// Items should be a *SchemaField with $ref
		items, ok := schema.Items.(*SchemaField)
		require.True(t, ok)
		assert.Equal(t, "#/components/schemas/io.k8s.api.core.v1.Toleration", items.Ref)
	})

	t.Run("maps object fields correctly", func(t *testing.T) {
		field := FieldInfo{
			JSONName: "resources",
			TypePkg:  "corev1",
			TypeName: "ResourceRequirements",
		}

		schema := mapFieldToOpenAPISchema(field, mockOpenAPI, collector)
		require.NotNil(t, schema)

		// Should be a $ref to the schema in components/schemas
		assert.Equal(t, "#/components/schemas/io.k8s.api.core.v1.ResourceRequirements", schema.Ref)
	})
}

func TestGetOpenAPITypeName(t *testing.T) {
	testCases := []struct {
		name     string
		field    FieldInfo
		expected string
	}{
		{
			name:     "corev1 package",
			field:    FieldInfo{TypePkg: "corev1", TypeName: "Toleration"},
			expected: "io.k8s.api.core.v1.Toleration",
		},
		{
			name:     "v1 package",
			field:    FieldInfo{TypePkg: "v1", TypeName: "ResourceRequirements"},
			expected: "io.k8s.api.core.v1.ResourceRequirements",
		},
		{
			name:     "unknown package",
			field:    FieldInfo{TypePkg: "unknown", TypeName: "SomeType"},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getOpenAPITypeName(tc.field)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSchemaIsValidJSON verifies that the generated schema is valid JSON
func TestSchemaIsValidJSON(t *testing.T) {
	mockOpenAPI := getMockOpenAPISpec()
	fields := []FieldInfo{
		{JSONName: "tolerations", TypePkg: "corev1", TypeName: "Toleration", IsSlice: true},
	}

	schema := generateBundleConfigSchema(mockOpenAPI, fields)

	// Marshal to JSON
	data, err := json.MarshalIndent(schema, "", "  ")
	require.NoError(t, err, "should marshal schema to JSON")

	// Unmarshal back to verify it's valid
	var unmarshaled map[string]interface{}
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err, "generated JSON should be valid and unmarshalable")

	// Verify key top-level fields exist
	assert.Contains(t, unmarshaled, "$schema")
	assert.Contains(t, unmarshaled, "$id")
	assert.Contains(t, unmarshaled, "type")
	assert.Contains(t, unmarshaled, "properties")
}

// TestGeneratedSchemaMatchesActualOutput validates that the checked-in schema file
// has the expected structure and required fields.
func TestGeneratedSchemaMatchesActualOutput(t *testing.T) {
	// Read the checked-in schema file
	schemaPath := "../../../internal/operator-controller/rukpak/bundle/registryv1bundleconfig.json"
	data, err := os.ReadFile(schemaPath)
	require.NoError(t, err, "should be able to read the generated schema file")

	// Unmarshal it
	var schemaFromFile map[string]interface{}
	err = json.Unmarshal(data, &schemaFromFile)
	require.NoError(t, err, "checked-in schema should be valid JSON")

	// Verify it has the expected structure
	assert.Equal(t, "http://json-schema.org/draft-07/schema#", schemaFromFile["$schema"])
	assert.Equal(t, schemaID, schemaFromFile["$id"])
	assert.Contains(t, schemaFromFile, "properties")

	props, ok := schemaFromFile["properties"].(map[string]interface{})
	require.True(t, ok)

	assert.Contains(t, props, "watchNamespace")
	assert.Contains(t, props, "deploymentConfig")

	// Verify deploymentConfig has expected fields
	deployConfig, ok := props["deploymentConfig"].(map[string]interface{})
	require.True(t, ok)

	dcProps, ok := deployConfig["properties"].(map[string]interface{})
	require.True(t, ok)

	expectedFields := []string{
		"nodeSelector", "tolerations", "resources", "env", "envFrom",
		"volumes", "volumeMounts", "affinity", "annotations",
	}

	for _, field := range expectedFields {
		assert.Contains(t, dcProps, field, "deploymentConfig should include %s", field)
	}

	// Verify selector is NOT present
	assert.NotContains(t, dcProps, "selector", "selector field should be excluded per RFC requirement")
}
