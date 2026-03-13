package graphql

import (
	"testing"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

func TestFieldNameRemapping_EdgeCases(t *testing.T) {
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
		{"spec.template.spec.containers", "specTemplateSpecContainers"},
		{"metadata.annotations.description", "metadataAnnotationsDescription"},
		{"operators.operatorframework.io/bundle.channels.v1", "operatorsOperatorframeworkIoBundleChannelsV1"},
		{"---", "field_"},
		{"123", "field_123"},
		{"field@#$%", "field"},
	}

	for _, tc := range testCases {
		result := remapFieldName(tc.input)
		if result != tc.expected {
			t.Errorf("remapFieldName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestSanitizeTypeName_EdgeCases(t *testing.T) {
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
		{"operators.operatorframework.io/bundle.channels.v1", "OperatorsOperatorframeworkIoBundleChannelsV1"},
		{"@#$%", "Unknown"},
		{"_____", "Unknown"},
		{"ABC", "Abc"},
		{"lowercase", "Lowercase"},
	}

	for _, tc := range testCases {
		result := sanitizeTypeName(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeTypeName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestAnalyzeJSONObject_FieldTypes(t *testing.T) {
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
		"floatValue": 3.14,
		"mixedArray": []interface{}{"string", 123, true},
	}

	info := &SchemaInfo{
		Fields: make(map[string]*FieldInfo),
	}

	analyzeJSONObject(testObj, info)

	// Check that all fields were discovered
	expectedFieldCount := len(testObj)
	if len(info.Fields) != expectedFieldCount {
		t.Errorf("Expected %d fields discovered, got %d", expectedFieldCount, len(info.Fields))
	}

	// Check specific field types
	testField := func(origName string, shouldBeArray bool) {
		graphqlField := remapFieldName(origName)
		fieldInfo, exists := info.Fields[graphqlField]
		if !exists {
			t.Errorf("Field %s (mapped to %s) not discovered", origName, graphqlField)
			return
		}

		if fieldInfo.IsArray != shouldBeArray {
			t.Errorf("Field %s array status: expected %v, got %v", graphqlField, shouldBeArray, fieldInfo.IsArray)
		}

		if len(fieldInfo.SampleValues) == 0 {
			t.Errorf("No sample values recorded for field %s", graphqlField)
		}
	}

	testField("name", false)
	testField("count", false)
	testField("active", false)
	testField("tags", true)
	testField("numbers", true)
	testField("emptyArray", true)
}

func TestBundlePropertiesAnalysis_ComprehensiveTypes(t *testing.T) {
	// Test that properties field is discovered with nested structure
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

func TestSchemaDiscovery_RealWorldExample(t *testing.T) {
	// Test with more realistic catalog data
	packageMeta := &declcfg.Meta{
		Schema:  declcfg.SchemaPackage,
		Package: "nginx-ingress-operator",
		Name:    "nginx-ingress-operator",
		Blob: []byte(`{
			"defaultChannel": "alpha",
			"icon": {
				"base64data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
				"mediatype": "image/png"
			},
			"name": "nginx-ingress-operator",
			"schema": "olm.package"
		}`),
	}

	channelMeta := &declcfg.Meta{
		Schema:  declcfg.SchemaChannel,
		Package: "nginx-ingress-operator",
		Name:    "alpha",
		Blob: []byte(`{
			"entries": [
				{"name": "nginx-ingress-operator.v0.0.1"},
				{"name": "nginx-ingress-operator.v0.0.2", "replaces": "nginx-ingress-operator.v0.0.1"}
			],
			"name": "alpha",
			"package": "nginx-ingress-operator",
			"schema": "olm.channel"
		}`),
	}

	bundleMeta := &declcfg.Meta{
		Schema:  declcfg.SchemaBundle,
		Package: "nginx-ingress-operator",
		Name:    "nginx-ingress-operator.v0.0.2",
		Blob: []byte(`{
			"image": "quay.io/operatorhubio/nginx-ingress-operator@sha256:abc123",
			"name": "nginx-ingress-operator.v0.0.2",
			"package": "nginx-ingress-operator",
			"properties": [
				{
					"type": "olm.package",
					"value": {
						"packageName": "nginx-ingress-operator",
						"version": "0.0.2"
					}
				},
				{
					"type": "olm.gvk",
					"value": {
						"group": "k8s.nginx.org",
						"kind": "NginxIngress",
						"version": "v1"
					}
				},
				{
					"type": "olm.bundle.mediatype",
					"value": "registry+v1"
				}
			],
			"relatedImages": [
				{
					"image": "quay.io/operatorhubio/nginx-ingress-operator@sha256:abc123",
					"name": "operator"
				}
			],
			"schema": "olm.bundle"
		}`),
	}

	testMetas := []*declcfg.Meta{packageMeta, channelMeta, bundleMeta}

	catalogSchema, err := DiscoverSchemaFromMetas(testMetas)
	if err != nil {
		t.Fatalf("Failed to discover schema: %v", err)
	}

	// Validate the results
	if len(catalogSchema.Schemas) != 3 {
		t.Errorf("Expected 3 schemas, got %d", len(catalogSchema.Schemas))
	}

	// Check bundle property discovery
	bundleSchema := catalogSchema.Schemas[declcfg.SchemaBundle]
	if bundleSchema == nil {
		t.Fatal("Bundle schema not found")
	}

	// With the schema-agnostic approach, we verify the properties field has nested structure
	propertiesField, exists := bundleSchema.Fields[remapFieldName("properties")]
	if !exists {
		t.Error("properties field not found in bundle schema")
		return
	}

	if !propertiesField.IsArray {
		t.Error("properties field should be an array")
	}
	if len(propertiesField.NestedFields) == 0 {
		t.Error("properties field should have nested fields")
		return
	}

	// Verify common property fields
	for _, field := range []string{"type", "value"} {
		if _, exists := propertiesField.NestedFields[remapFieldName(field)]; !exists {
			t.Errorf("Expected field %s not found in properties", field)
		}
	}

	// Validate that complex fields are properly mapped
	packageSchema := catalogSchema.Schemas[declcfg.SchemaPackage]
	if packageSchema == nil {
		t.Fatal("Package schema not found")
	}

	// Check that icon field exists (it's a complex object)
	if _, exists := packageSchema.Fields["icon"]; !exists {
		t.Error("Icon field not discovered in package schema")
	}

	// Validate total object counts
	if packageSchema.TotalObjects != 1 {
		t.Errorf("Expected 1 package, got %d", packageSchema.TotalObjects)
	}
	if bundleSchema.TotalObjects != 1 {
		t.Errorf("Expected 1 bundle, got %d", bundleSchema.TotalObjects)
	}
}
