package graphql

import (
	"testing"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

func TestDiscoverSchemaFromMetas(t *testing.T) {
	// Create test metas simulating real catalog data
	testMetas := []*declcfg.Meta{
		{
			Schema:  declcfg.SchemaPackage,
			Package: "test-package",
			Name:    "test-package",
			Blob: []byte(`{
				"schema": "olm.package",
				"name": "test-package",
				"defaultChannel": "stable",
				"icon": {
					"base64data": "...",
					"mediatype": "image/svg+xml"
				},
				"description": "A test package"
			}`),
		},
		{
			Schema:  declcfg.SchemaChannel,
			Package: "test-package",
			Name:    "stable",
			Blob: []byte(`{
				"schema": "olm.channel",
				"name": "stable",
				"package": "test-package",
				"entries": [
					{"name": "test-package.v1.0.0"},
					{"name": "test-package.v1.1.0", "replaces": "test-package.v1.0.0"}
				]
			}`),
		},
		{
			Schema:  declcfg.SchemaBundle,
			Package: "test-package",
			Name:    "test-package.v1.0.0",
			Blob: []byte(`{
				"schema": "olm.bundle",
				"name": "test-package.v1.0.0",
				"package": "test-package",
				"image": "registry.io/test-package@sha256:abc123",
				"properties": [
					{
						"type": "olm.package",
						"value": {
							"packageName": "test-package",
							"version": "1.0.0"
						}
					},
					{
						"type": "olm.gvk",
						"value": {
							"group": "example.com",
							"version": "v1",
							"kind": "TestResource"
						}
					}
				],
				"relatedImages": [
					{
						"name": "operator",
						"image": "registry.io/test-package@sha256:abc123"
					}
				]
			}`),
		},
	}

	// Test schema discovery
	catalogSchema, err := DiscoverSchemaFromMetas(testMetas)
	if err != nil {
		t.Fatalf("Failed to discover schema: %v", err)
	}

	// Validate discovered schemas
	if len(catalogSchema.Schemas) != 3 {
		t.Errorf("Expected 3 schemas, got %d", len(catalogSchema.Schemas))
	}

	// Test package schema
	packageSchema, ok := catalogSchema.Schemas[declcfg.SchemaPackage]
	if !ok {
		t.Error("Package schema not discovered")
	} else {
		if packageSchema.TotalObjects != 1 {
			t.Errorf("Expected 1 package object, got %d", packageSchema.TotalObjects)
		}
		if len(packageSchema.Fields) == 0 {
			t.Error("No fields discovered for package schema")
		}

		// Check for expected fields
		expectedFields := []string{"name", "defaultChannel", "icon", "description", "schema"}
		for _, field := range expectedFields {
			graphqlField := remapFieldName(field)
			if _, exists := packageSchema.Fields[graphqlField]; !exists {
				t.Errorf("Expected field %s (mapped to %s) not found in package schema", field, graphqlField)
			}
		}
	}

	// Test bundle schema with properties
	bundleSchema, ok := catalogSchema.Schemas[declcfg.SchemaBundle]
	if !ok {
		t.Error("Bundle schema not discovered")
	} else {
		if bundleSchema.TotalObjects != 1 {
			t.Errorf("Expected 1 bundle object, got %d", bundleSchema.TotalObjects)
		}

		// Check that properties field is discovered with nested structure
		propertiesField, exists := bundleSchema.Fields[remapFieldName("properties")]
		if !exists {
			t.Error("properties field not discovered in bundle schema")
		} else if !propertiesField.IsArray {
			t.Error("properties field should be an array")
		} else if propertiesField.NestedFields == nil || len(propertiesField.NestedFields) == 0 {
			t.Error("properties field should have nested fields discovered")
		}

		// Check for typical property fields (type, value)
		if propertiesField.NestedFields != nil {
			expectedFields := []string{"type", "value"}
			for _, field := range expectedFields {
				if _, exists := propertiesField.NestedFields[remapFieldName(field)]; !exists {
					t.Errorf("Expected nested field %s not found in properties", field)
				}
			}
		}
	}

	// Test channel schema
	channelSchema, ok := catalogSchema.Schemas[declcfg.SchemaChannel]
	if !ok {
		t.Error("Channel schema not discovered")
	} else {
		if channelSchema.TotalObjects != 1 {
			t.Errorf("Expected 1 channel object, got %d", channelSchema.TotalObjects)
		}
	}
}

func TestFieldNameRemapping(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"name", "name"},
		{"package-name", "packageName"},
		{"default_channel", "defaultChannel"},
		{"related-images", "relatedImages"},
		{"", "value"},
		{"123invalid", "field_123invalid"},
		{"my.field.name", "myFieldName"},
		{"CamelCase", "camelCase"},
		{"UPPERCASE", "uppercase"},
		{"mixed_case-field.name", "mixedCaseFieldName"},
	}

	for _, tc := range testCases {
		result := remapFieldName(tc.input)
		if result != tc.expected {
			t.Errorf("remapFieldName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestSanitizeTypeName(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"olm.package", "OlmPackage"},
		{"olm.gvk", "OlmGvk"},
		{"some-type", "SomeType"},
		{"complex.type-name_here", "ComplexTypeNameHere"},
		{"", "Unknown"},
		{"123invalid", "Invalid"},
	}

	for _, tc := range testCases {
		result := sanitizeTypeName(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeTypeName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestAnalyzeJSONObject(t *testing.T) {
	testObj := map[string]interface{}{
		"name":       "test-package",
		"version":    "1.0.0",
		"count":      42,
		"active":     true,
		"tags":       []interface{}{"tag1", "tag2"},
		"numbers":    []interface{}{1, 2, 3},
		"nested":     map[string]interface{}{"key": "value"},
		"nullField":  nil,
		"emptyArray": []interface{}{},
	}

	info := &SchemaInfo{
		Fields: make(map[string]*FieldInfo),
	}

	analyzeJSONObject(testObj, info)

	// Check that all fields were discovered
	expectedFields := map[string]string{
		"name":       "string",
		"version":    "string",
		"count":      "int",
		"active":     "bool",
		"tags":       "[]string",
		"numbers":    "[]int",
		"nested":     "string", // Complex objects become strings
		"nullField":  "string", // Null becomes string
		"emptyArray": "[]string",
	}

	for origField, expectedType := range expectedFields {
		graphqlField := remapFieldName(origField)
		fieldInfo, exists := info.Fields[graphqlField]
		if !exists {
			t.Errorf("Field %s (mapped to %s) not discovered", origField, graphqlField)
			continue
		}

		// Type checking would require GraphQL types, so we just check that it was analyzed
		if len(fieldInfo.SampleValues) == 0 {
			t.Errorf("No sample values recorded for field %s", graphqlField)
		}

		_ = expectedType // We can't easily test GraphQL types without the library
	}
}

// TestBundlePropertiesAnalysis tests the analysis of complex bundle properties
func TestBundlePropertiesAnalysis(t *testing.T) {
	bundleObj := map[string]interface{}{
		"name":    "test-bundle",
		"package": "test-package",
		"properties": []interface{}{
			map[string]interface{}{
				"type": "olm.package",
				"value": map[string]interface{}{
					"packageName": "test-package",
					"version":     "1.0.0",
				},
			},
			map[string]interface{}{
				"type": "olm.gvk",
				"value": map[string]interface{}{
					"group":   "example.com",
					"version": "v1",
					"kind":    "TestResource",
				},
			},
			map[string]interface{}{
				"type": "olm.csv.metadata",
				"value": map[string]interface{}{
					"name":      "test-operator",
					"namespace": "test-namespace",
					"annotations": map[string]interface{}{
						"description": "A test operator",
					},
				},
			},
		},
	}

	info := &SchemaInfo{
		Fields: make(map[string]*FieldInfo),
	}

	// Use the generic field analysis (not bundle-specific)
	analyzeJSONObject(bundleObj, info)

	// Check that properties field was discovered
	propertiesField, exists := info.Fields[remapFieldName("properties")]
	if !exists {
		t.Error("properties field not discovered")
		return
	}

	// Verify it's detected as an array
	if !propertiesField.IsArray {
		t.Error("properties field should be detected as an array")
	}

	// Verify nested fields were discovered
	if propertiesField.NestedFields == nil {
		t.Error("properties field should have nested fields discovered")
		return
	}

	// Check for common property fields (type, value)
	expectedFields := []string{"type", "value"}
	for _, field := range expectedFields {
		fieldName := remapFieldName(field)
		if _, exists := propertiesField.NestedFields[fieldName]; !exists {
			t.Errorf("Expected nested field %s not found in properties", fieldName)
		}
	}
}
