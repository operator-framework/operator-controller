package graphql

import (
	"testing"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

func TestDiscoverSchemaFromMetas_CoreLogic(t *testing.T) {
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

		// Check property types discovery
		if len(bundleSchema.PropertyTypes) == 0 {
			t.Error("No property types discovered for bundle schema")
		}

		// Check for specific property types
		if olmPackage, exists := bundleSchema.PropertyTypes["olm.package"]; !exists {
			t.Error("olm.package property type not discovered")
		} else {
			expectedPropertyFields := []string{"packageName", "version"}
			for _, field := range expectedPropertyFields {
				graphqlField := remapFieldName(field)
				if _, exists := olmPackage[graphqlField]; !exists {
					t.Errorf("Expected property field %s not found in olm.package", graphqlField)
				}
			}
		}

		if olmGvk, exists := bundleSchema.PropertyTypes["olm.gvk"]; !exists {
			t.Error("olm.gvk property type not discovered")
		} else {
			expectedGvkFields := []string{"group", "version", "kind"}
			for _, field := range expectedGvkFields {
				graphqlField := remapFieldName(field)
				if _, exists := olmGvk[graphqlField]; !exists {
					t.Errorf("Expected GVK field %s not found in olm.gvk", graphqlField)
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
		{"field@#$%", "fieldField"},
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
			map[string]interface{}{
				"type": "olm.bundle.object",
				"value": map[string]interface{}{
					"ref": "objects/test.yaml",
					"data": map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name": "config",
						},
					},
				},
			},
		},
	}

	info := &SchemaInfo{
		PropertyTypes: make(map[string]map[string]*FieldInfo),
	}

	analyzeBundleProperties(bundleObj, info)

	// Check that property types were discovered
	expectedPropertyTypes := []string{"olm.package", "olm.gvk", "olm.csv.metadata", "olm.bundle.object"}
	for _, propType := range expectedPropertyTypes {
		if _, exists := info.PropertyTypes[propType]; !exists {
			t.Errorf("Property type %s not discovered", propType)
		}
	}

	// Check olm.package fields
	if olmPackage, exists := info.PropertyTypes["olm.package"]; exists {
		expectedFields := []string{"packageName", "version"}
		for _, field := range expectedFields {
			if _, exists := olmPackage[field]; !exists {
				t.Errorf("Field %s not found in olm.package property type", field)
			}
		}
	}

	// Check olm.gvk fields
	if olmGvk, exists := info.PropertyTypes["olm.gvk"]; exists {
		expectedFields := []string{"group", "version", "kind"}
		for _, field := range expectedFields {
			if _, exists := olmGvk[field]; !exists {
				t.Errorf("Field %s not found in olm.gvk property type", field)
			}
		}
	}

	// Check that nested objects are handled (annotations in csv.metadata)
	if csvMetadata, exists := info.PropertyTypes["olm.csv.metadata"]; exists {
		expectedFields := []string{"name", "namespace", "annotations"}
		for _, field := range expectedFields {
			if _, exists := csvMetadata[field]; !exists {
				t.Errorf("Field %s not found in olm.csv.metadata property type", field)
			}
		}
	}

	// Check bundle object type
	if bundleObject, exists := info.PropertyTypes["olm.bundle.object"]; exists {
		expectedFields := []string{"ref", "data"}
		for _, field := range expectedFields {
			if _, exists := bundleObject[field]; !exists {
				t.Errorf("Field %s not found in olm.bundle.object property type", field)
			}
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

	expectedPropertyTypes := map[string][]string{
		"olm.package":          {"packageName", "version"},
		"olm.gvk":              {"group", "kind", "version"},
		"olm.bundle.mediatype": {}, // This is a string value, no nested fields
	}

	for propType, expectedFields := range expectedPropertyTypes {
		if propFields, exists := bundleSchema.PropertyTypes[propType]; exists {
			for _, expectedField := range expectedFields {
				if _, fieldExists := propFields[expectedField]; !fieldExists {
					t.Errorf("Expected field %s not found in property type %s", expectedField, propType)
				}
			}
		} else if len(expectedFields) > 0 {
			// Only error if we expected fields (mediatype is a string, so no fields expected)
			t.Errorf("Property type %s not discovered", propType)
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
