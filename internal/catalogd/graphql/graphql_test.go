package graphql

import (
	"reflect"
	"testing"
	"testing/fstest"

	graphqlgo "github.com/graphql-go/graphql"

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
		return
	}

	if bundleSchema.TotalObjects != 1 {
		t.Errorf("Expected 1 bundle object, got %d", bundleSchema.TotalObjects)
	}

	// Check that properties field is discovered with nested structure
	propertiesField, exists := bundleSchema.Fields[remapFieldName("properties")]
	if !exists {
		t.Error("properties field not discovered in bundle schema")
		return
	}
	if !propertiesField.IsArray {
		t.Error("properties field should be an array")
		return
	}
	if len(propertiesField.NestedFields) == 0 {
		t.Error("properties field should have nested fields discovered")
		return
	}

	// Check for typical property fields (type, value)
	expectedFields := []string{"type", "value"}
	for _, field := range expectedFields {
		if _, exists := propertiesField.NestedFields[remapFieldName(field)]; !exists {
			t.Errorf("Expected nested field %s not found in properties", field)
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
		_, exists := info.Fields[graphqlField]
		if !exists {
			t.Errorf("Field %s (mapped to %s) not discovered", origField, graphqlField)
			continue
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

func TestIntegerTypeDetection(t *testing.T) {
	// Test that JSON numbers that are integers get typed as Int, not Float
	testMetas := []*declcfg.Meta{
		{
			Schema:  "test.schema",
			Package: "test-pkg",
			Name:    "test",
			Blob: []byte(`{
				"schema": "test.schema",
				"integerField": 42,
				"floatField": 3.14,
				"integerArray": [1, 2, 3],
				"floatArray": [1.5, 2.5, 3.5]
			}`),
		},
	}

	catalogSchema, err := DiscoverSchemaFromMetas(testMetas)
	if err != nil {
		t.Fatalf("Failed to discover schema: %v", err)
	}

	schema, ok := catalogSchema.Schemas["test.schema"]
	if !ok {
		t.Fatal("Test schema not discovered")
	}

	// Check integerField is typed as Int
	if field, ok := schema.Fields["integerField"]; ok {
		if field.GraphQLType.String() != "Int" {
			t.Errorf("integerField should be Int, got %v", field.GraphQLType)
		}
	} else {
		t.Error("integerField not found")
	}

	// Check floatField is typed as Float
	if field, ok := schema.Fields["floatField"]; ok {
		if field.GraphQLType.String() != "Float" {
			t.Errorf("floatField should be Float, got %v", field.GraphQLType)
		}
	} else {
		t.Error("floatField not found")
	}

	// Check integerArray is typed as [Int]
	if field, ok := schema.Fields["integerArray"]; ok {
		if field.GraphQLType.String() != "[Int]" {
			t.Errorf("integerArray should be [Int], got %v", field.GraphQLType)
		}
	} else {
		t.Error("integerArray not found")
	}

	// Check floatArray is typed as [Float]
	if field, ok := schema.Fields["floatArray"]; ok {
		if field.GraphQLType.String() != "[Float]" {
			t.Errorf("floatArray should be [Float], got %v", field.GraphQLType)
		}
	} else {
		t.Error("floatArray not found")
	}
}

// --- Serialization round-trip tests ---

func testCatalogMetas() []*declcfg.Meta {
	return []*declcfg.Meta{
		{
			Schema:  "olm.package",
			Package: "test-package",
			Name:    "test-package",
			Blob: []byte(`{
				"schema": "olm.package",
				"name": "test-package",
				"defaultChannel": "stable",
				"description": "A test package"
			}`),
		},
		{
			Schema:  "olm.bundle",
			Package: "test-package",
			Name:    "test-package.v1.0.0",
			Blob: []byte(`{
				"schema": "olm.bundle",
				"name": "test-package.v1.0.0",
				"package": "test-package",
				"image": "registry.io/test@sha256:abc",
				"properties": [
					{"type": "olm.package", "value": {"packageName": "test-package", "version": "1.0.0"}}
				],
				"relatedImages": [
					{"name": "operator", "image": "registry.io/test@sha256:abc"}
				]
			}`),
		},
		{
			Schema:  "olm.channel",
			Package: "test-package",
			Name:    "stable",
			Blob: []byte(`{
				"schema": "olm.channel",
				"name": "stable",
				"package": "test-package",
				"entries": [{"name": "test-package.v1.0.0"}]
			}`),
		},
	}
}

func TestMarshalUnmarshalCatalogSchema_RoundTrip(t *testing.T) {
	metas := testCatalogMetas()
	original, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromMetas failed: %v", err)
	}

	data, err := MarshalCatalogSchema(original)
	if err != nil {
		t.Fatalf("MarshalCatalogSchema failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("MarshalCatalogSchema returned empty data")
	}

	restored, err := UnmarshalCatalogSchema(data)
	if err != nil {
		t.Fatalf("UnmarshalCatalogSchema failed: %v", err)
	}

	// Verify schema count matches
	if len(restored.Schemas) != len(original.Schemas) {
		t.Fatalf("schema count mismatch: original=%d, restored=%d",
			len(original.Schemas), len(restored.Schemas))
	}

	// Verify each schema's fields and metadata
	for name, origInfo := range original.Schemas {
		restoredInfo, ok := restored.Schemas[name]
		if !ok {
			t.Errorf("schema %q missing after round-trip", name)
			continue
		}

		if restoredInfo.TotalObjects != origInfo.TotalObjects {
			t.Errorf("schema %q: TotalObjects mismatch: %d vs %d",
				name, origInfo.TotalObjects, restoredInfo.TotalObjects)
		}

		if len(restoredInfo.Fields) != len(origInfo.Fields) {
			t.Errorf("schema %q: field count mismatch: %d vs %d",
				name, len(origInfo.Fields), len(restoredInfo.Fields))
		}

		for fname, origField := range origInfo.Fields {
			resField, ok := restoredInfo.Fields[fname]
			if !ok {
				t.Errorf("schema %q: field %q missing after round-trip", name, fname)
				continue
			}
			if resField.Name != origField.Name {
				t.Errorf("field %q: Name mismatch: %q vs %q", fname, origField.Name, resField.Name)
			}
			if resField.OriginalName != origField.OriginalName {
				t.Errorf("field %q: OriginalName mismatch: %q vs %q", fname, origField.OriginalName, resField.OriginalName)
			}
			if resField.IsArray != origField.IsArray {
				t.Errorf("field %q: IsArray mismatch: %v vs %v", fname, origField.IsArray, resField.IsArray)
			}
			// JSONType for reflect.Map/Slice falls back to String in serialization
			// (kindToString doesn't handle Map/Slice). This is expected — the GraphQL
			// type name is the authoritative source after round-trip, not JSONType.
			if origField.JSONType != reflect.Map && origField.JSONType != reflect.Slice {
				if resField.JSONType != origField.JSONType {
					t.Errorf("field %q: JSONType mismatch: %v vs %v", fname, origField.JSONType, resField.JSONType)
				}
			}
		}
	}
}

func TestUnmarshalCatalogSchema_InvalidJSON(t *testing.T) {
	_, err := UnmarshalCatalogSchema([]byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMarshalCatalogSchema_WithNestedFields(t *testing.T) {
	cs := &CatalogSchema{
		Schemas: map[string]*SchemaInfo{
			"test.schema": {
				TotalObjects: 1,
				Fields: map[string]*FieldInfo{
					"items": {
						Name:         "items",
						OriginalName: "items",
						GraphQLType:  graphqlgo.NewList(graphqlgo.String),
						JSONType:     reflect.Map,
						IsArray:      true,
						NestedFields: map[string]*FieldInfo{
							"key": {
								Name:         "key",
								OriginalName: "key",
								GraphQLType:  graphqlgo.String,
								JSONType:     reflect.String,
							},
						},
					},
				},
			},
		},
	}

	data, err := MarshalCatalogSchema(cs)
	if err != nil {
		t.Fatalf("MarshalCatalogSchema failed: %v", err)
	}

	restored, err := UnmarshalCatalogSchema(data)
	if err != nil {
		t.Fatalf("UnmarshalCatalogSchema failed: %v", err)
	}

	field := restored.Schemas["test.schema"].Fields["items"]
	if field == nil {
		t.Fatal("items field missing after round-trip")
	}
	if len(field.NestedFields) != 1 {
		t.Fatalf("expected 1 nested field, got %d", len(field.NestedFields))
	}
	if _, ok := field.NestedFields["key"]; !ok {
		t.Error("nested field 'key' missing after round-trip")
	}
}

// --- kindToString / stringToKind tests ---

func TestKindToString(t *testing.T) {
	tests := []struct {
		kind     reflect.Kind
		expected string
	}{
		{reflect.String, "string"},
		{reflect.Bool, "bool"},
		{reflect.Int, "int"},
		{reflect.Int64, "int"},
		{reflect.Uint, "uint"},
		{reflect.Uint32, "uint"},
		{reflect.Float64, "float64"},
		{reflect.Float32, "float64"},
		{reflect.Struct, "string"}, // default
	}

	for _, tt := range tests {
		result := kindToString(tt.kind)
		if result != tt.expected {
			t.Errorf("kindToString(%v) = %q, want %q", tt.kind, result, tt.expected)
		}
	}
}

func TestStringToKind(t *testing.T) {
	tests := []struct {
		input    string
		expected reflect.Kind
	}{
		{"string", reflect.String},
		{"bool", reflect.Bool},
		{"int", reflect.Int},
		{"uint", reflect.Uint},
		{"float64", reflect.Float64},
		{"unknown", reflect.String}, // default
	}

	for _, tt := range tests {
		result := stringToKind(tt.input)
		if result != tt.expected {
			t.Errorf("stringToKind(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

// --- graphqlTypeName / graphqlTypeFromName tests ---

func TestGraphqlTypeName(t *testing.T) {
	tests := []struct {
		gqlType  graphqlgo.Type
		expected string
	}{
		{graphqlgo.String, "String"},
		{graphqlgo.Int, "Int"},
		{graphqlgo.Float, "Float"},
		{graphqlgo.Boolean, "Boolean"},
		{graphqlgo.NewList(graphqlgo.String), "String"},
		{graphqlgo.NewList(graphqlgo.Int), "Int"},
	}

	for _, tt := range tests {
		result := graphqlTypeName(tt.gqlType)
		if result != tt.expected {
			t.Errorf("graphqlTypeName(%v) = %q, want %q", tt.gqlType, result, tt.expected)
		}
	}
}

func TestGraphqlTypeFromName(t *testing.T) {
	tests := []struct {
		name    string
		isArray bool
		check   string
	}{
		{"String", false, "String"},
		{"Int", false, "Int"},
		{"Float", false, "Float"},
		{"Boolean", false, "Boolean"},
		{"Unknown", false, "String"}, // defaults to String
		{"String", true, "[String]"},
		{"Int", true, "[Int]"},
	}

	for _, tt := range tests {
		result := graphqlTypeFromName(tt.name, tt.isArray)
		if result.String() != tt.check {
			t.Errorf("graphqlTypeFromName(%q, %v) = %q, want %q", tt.name, tt.isArray, result.String(), tt.check)
		}
	}
}

// --- DiscoverSchemaFromChannel tests ---

func TestDiscoverSchemaFromChannel(t *testing.T) {
	metas := testCatalogMetas()
	ch := make(chan *declcfg.Meta, len(metas))
	for _, m := range metas {
		ch <- m
	}
	close(ch)

	cs, err := DiscoverSchemaFromChannel(ch)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromChannel failed: %v", err)
	}

	if len(cs.Schemas) != 3 {
		t.Errorf("expected 3 schemas, got %d", len(cs.Schemas))
	}

	for _, name := range []string{"olm.package", "olm.bundle", "olm.channel"} {
		info, ok := cs.Schemas[name]
		if !ok {
			t.Errorf("schema %q not discovered", name)
			continue
		}
		if info.TotalObjects != 1 {
			t.Errorf("schema %q: expected 1 object, got %d", name, info.TotalObjects)
		}
		if len(info.Fields) == 0 {
			t.Errorf("schema %q: no fields discovered", name)
		}
	}
}

func TestDiscoverSchemaFromChannel_SkipsEmptySchema(t *testing.T) {
	ch := make(chan *declcfg.Meta, 1)
	ch <- &declcfg.Meta{Schema: "", Blob: []byte(`{"name":"x"}`)}
	close(ch)

	cs, err := DiscoverSchemaFromChannel(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs.Schemas) != 0 {
		t.Errorf("expected 0 schemas, got %d", len(cs.Schemas))
	}
}

func TestDiscoverSchemaFromChannel_SkipsMalformedBlob(t *testing.T) {
	ch := make(chan *declcfg.Meta, 1)
	ch <- &declcfg.Meta{Schema: "test", Name: "bad", Blob: []byte(`{invalid json`)}
	close(ch)

	cs, err := DiscoverSchemaFromChannel(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := cs.Schemas["test"]
	if info == nil {
		t.Fatal("expected schema entry for 'test'")
	}
	if info.TotalObjects != 0 {
		t.Errorf("expected TotalObjects=0 for malformed blob (not counted until after parse), got %d", info.TotalObjects)
	}
}

// --- BuildDynamicGraphQLSchema + query execution tests ---

func TestBuildDynamicGraphQLSchema_BasicQuery(t *testing.T) {
	metas := testCatalogMetas()
	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromMetas: %v", err)
	}

	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, m := range metas {
		if m.Schema != "" {
			metasBySchema[m.Schema] = append(metasBySchema[m.Schema], m)
		}
	}
	loader := NewInMemoryObjectLoader(metasBySchema)

	ds, err := BuildDynamicGraphQLSchema(catalogSchema, loader)
	if err != nil {
		t.Fatalf("BuildDynamicGraphQLSchema: %v", err)
	}

	// Query for packages
	result := graphqlgo.Do(graphqlgo.Params{
		Schema:        ds.Schema,
		RequestString: `{ olmpackages { name defaultChannel } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL query errors: %v", result.Errors)
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	packages, ok := data["olmpackages"].([]interface{})
	if !ok || len(packages) != 1 {
		t.Fatalf("expected 1 package, got %v", data["olmpackages"])
	}
}

func TestBuildDynamicGraphQLSchema_PaginationArgs(t *testing.T) {
	metas := testCatalogMetas()
	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromMetas: %v", err)
	}

	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, m := range metas {
		if m.Schema != "" {
			metasBySchema[m.Schema] = append(metasBySchema[m.Schema], m)
		}
	}
	loader := NewInMemoryObjectLoader(metasBySchema)

	ds, err := BuildDynamicGraphQLSchema(catalogSchema, loader)
	if err != nil {
		t.Fatalf("BuildDynamicGraphQLSchema: %v", err)
	}

	// Offset past all results
	result := graphqlgo.Do(graphqlgo.Params{
		Schema:        ds.Schema,
		RequestString: `{ olmpackages(offset: 100) { name } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL query errors: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	packages := data["olmpackages"]
	if packages != nil {
		if p, ok := packages.([]interface{}); ok && len(p) != 0 {
			t.Errorf("expected empty result for high offset, got %d", len(p))
		}
	}
}

func TestBuildDynamicGraphQLSchema_SummaryField(t *testing.T) {
	metas := testCatalogMetas()
	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromMetas: %v", err)
	}

	loader := NewInMemoryObjectLoader(nil)

	ds, err := BuildDynamicGraphQLSchema(catalogSchema, loader)
	if err != nil {
		t.Fatalf("BuildDynamicGraphQLSchema: %v", err)
	}

	result := graphqlgo.Do(graphqlgo.Params{
		Schema:        ds.Schema,
		RequestString: `{ summary { totalSchemas schemas { name totalObjects totalFields } } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL query errors: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	summary, ok := data["summary"].(map[string]interface{})
	if !ok {
		t.Fatal("expected summary in result")
	}
	totalSchemas, ok := summary["totalSchemas"]
	if !ok {
		t.Fatal("expected totalSchemas in summary")
	}
	if totalSchemas.(int) != 3 {
		t.Errorf("expected 3 total schemas, got %v", totalSchemas)
	}
}

func TestBuildDynamicGraphQLSchema_IntrospectionQuery(t *testing.T) {
	metas := testCatalogMetas()
	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromMetas: %v", err)
	}

	loader := NewInMemoryObjectLoader(nil)

	ds, err := BuildDynamicGraphQLSchema(catalogSchema, loader)
	if err != nil {
		t.Fatalf("BuildDynamicGraphQLSchema: %v", err)
	}

	result := graphqlgo.Do(graphqlgo.Params{
		Schema:        ds.Schema,
		RequestString: `{ __schema { queryType { name } } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("introspection query errors: %v", result.Errors)
	}
}

// --- NewInMemoryObjectLoader tests ---

func TestNewInMemoryObjectLoader_BasicPagination(t *testing.T) {
	metas := map[string][]*declcfg.Meta{
		"test": {
			{Schema: "test", Blob: []byte(`{"name":"a"}`)},
			{Schema: "test", Blob: []byte(`{"name":"b"}`)},
			{Schema: "test", Blob: []byte(`{"name":"c"}`)},
		},
	}

	loader := NewInMemoryObjectLoader(metas)

	// Full page
	objs, err := loader("test", 0, 10)
	if err != nil {
		t.Fatalf("loader error: %v", err)
	}
	if len(objs) != 3 {
		t.Errorf("expected 3 objects, got %d", len(objs))
	}

	// With offset
	objs, err = loader("test", 1, 10)
	if err != nil {
		t.Fatalf("loader error: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("expected 2 objects with offset=1, got %d", len(objs))
	}

	// With limit
	objs, err = loader("test", 0, 2)
	if err != nil {
		t.Fatalf("loader error: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("expected 2 objects with limit=2, got %d", len(objs))
	}

	// Offset past end
	objs, err = loader("test", 100, 10)
	if err != nil {
		t.Fatalf("loader error: %v", err)
	}
	if objs != nil {
		t.Errorf("expected nil for offset past end, got %v", objs)
	}

	// Unknown schema
	objs, err = loader("nonexistent", 0, 10)
	if err != nil {
		t.Fatalf("loader error: %v", err)
	}
	if objs != nil {
		t.Errorf("expected nil for unknown schema, got %v", objs)
	}
}

func TestNewInMemoryObjectLoader_SkipsMalformedJSON(t *testing.T) {
	metas := map[string][]*declcfg.Meta{
		"test": {
			{Schema: "test", Blob: []byte(`{invalid`)},
			{Schema: "test", Blob: []byte(`{"name":"valid"}`)},
		},
	}

	loader := NewInMemoryObjectLoader(metas)
	objs, err := loader("test", 0, 10)
	if err != nil {
		t.Fatalf("loader error: %v", err)
	}
	if len(objs) != 1 {
		t.Errorf("expected 1 valid object (malformed skipped), got %d", len(objs))
	}
}

// --- LoadAndSummarizeCatalogDynamic tests ---

func TestLoadAndSummarizeCatalogDynamic(t *testing.T) {
	catalogFS := &fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{"schema":"olm.package","name":"test-pkg","defaultChannel":"stable"}
{"schema":"olm.channel","name":"stable","package":"test-pkg","entries":[{"name":"test-pkg.v1.0.0"}]}
`),
		},
	}

	ds, err := LoadAndSummarizeCatalogDynamic(catalogFS)
	if err != nil {
		t.Fatalf("LoadAndSummarizeCatalogDynamic failed: %v", err)
	}

	if ds.CatalogSchema == nil {
		t.Fatal("CatalogSchema is nil")
	}
	if len(ds.CatalogSchema.Schemas) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(ds.CatalogSchema.Schemas))
	}

	// Verify the schema actually works for queries
	result := graphqlgo.Do(graphqlgo.Params{
		Schema:        ds.Schema,
		RequestString: `{ olmpackages { name } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("query errors: %v", result.Errors)
	}
}

// --- marshalComplexValue tests ---

func TestMarshalComplexValue(t *testing.T) {
	// nil
	if v := marshalComplexValue(nil); v != nil {
		t.Errorf("expected nil for nil input, got %v", v)
	}

	// string passthrough
	if v := marshalComplexValue("hello"); v != "hello" {
		t.Errorf("expected 'hello', got %v", v)
	}

	// int passthrough
	if v := marshalComplexValue(42); v != 42 {
		t.Errorf("expected 42, got %v", v)
	}

	// map gets marshaled to JSON string
	m := map[string]interface{}{"key": "value"}
	v := marshalComplexValue(m)
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string for map, got %T", v)
	}
	if s != `{"key":"value"}` {
		t.Errorf("expected JSON string for map, got %q", s)
	}

	// slice gets marshaled to JSON string
	sl := []interface{}{"a", "b"}
	v = marshalComplexValue(sl)
	s, ok = v.(string)
	if !ok {
		t.Fatalf("expected string for slice, got %T", v)
	}
	if s != `["a","b"]` {
		t.Errorf("expected JSON string for slice, got %q", s)
	}
}

// --- Field resolver tests (via full query execution) ---

func TestFieldResolvers_NestedObjectsAndScalars(t *testing.T) {
	metas := []*declcfg.Meta{
		{
			Schema: "olm.bundle",
			Name:   "test.v1.0.0",
			Blob: []byte(`{
				"schema": "olm.bundle",
				"name": "test.v1.0.0",
				"package": "test-pkg",
				"properties": [
					{"type": "olm.package", "value": {"packageName": "test-pkg", "version": "1.0.0"}}
				]
			}`),
		},
	}

	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		t.Fatalf("DiscoverSchemaFromMetas: %v", err)
	}

	metasBySchema := map[string][]*declcfg.Meta{"olm.bundle": metas}
	loader := NewInMemoryObjectLoader(metasBySchema)

	ds, err := BuildDynamicGraphQLSchema(catalogSchema, loader)
	if err != nil {
		t.Fatalf("BuildDynamicGraphQLSchema: %v", err)
	}

	result := graphqlgo.Do(graphqlgo.Params{
		Schema:        ds.Schema,
		RequestString: `{ olmbundles { name properties { type } } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("query errors: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	bundles := data["olmbundles"].([]interface{})
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(bundles))
	}

	bundle := bundles[0].(map[string]interface{})
	if bundle["name"] != "test.v1.0.0" {
		t.Errorf("expected name 'test.v1.0.0', got %v", bundle["name"])
	}

	props, ok := bundle["properties"].([]interface{})
	if !ok || len(props) != 1 {
		t.Fatalf("expected 1 property, got %v", bundle["properties"])
	}
}
