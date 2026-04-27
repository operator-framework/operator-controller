package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"regexp"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// Pre-compiled regex patterns to avoid repeated compilation in hot paths
var (
	invalidCharsRE           = regexp.MustCompile(`[^a-zA-Z0-9_]`)
	consecutiveUnderscoresRE = regexp.MustCompile(`_+`)
	startsWithLetterRE       = regexp.MustCompile(`^[a-zA-Z]`)
	alphanumericOnlyRE       = regexp.MustCompile(`[^a-zA-Z0-9]`)
	leadingDigitsRE          = regexp.MustCompile(`^[0-9]+`)
)

// FieldInfo represents discovered field information
type FieldInfo struct {
	Name         string
	GraphQLType  graphql.Type
	JSONType     reflect.Kind
	IsArray      bool
	SampleValues []interface{}
	NestedFields map[string]*FieldInfo // For array-of-objects, stores object structure
}

// SchemaInfo holds discovered schema information
type SchemaInfo struct {
	Fields       map[string]*FieldInfo
	TotalObjects int
	SampleObject map[string]interface{}
}

// CatalogSchema holds the complete discovered schema
type CatalogSchema struct {
	Schemas map[string]*SchemaInfo // schema name -> info
}

// DynamicSchema holds the generated GraphQL schema and metadata
type DynamicSchema struct {
	Schema        graphql.Schema
	CatalogSchema *CatalogSchema
	ParsedObjects map[string][]map[string]interface{} // Pre-parsed JSON objects cached during schema build
	// Performance optimization: ParsedObjects avoids json.Unmarshal on every GraphQL query.
	// Objects are parsed once during schema build and cached for all subsequent queries.
	// Memory cost: ~same as storing raw blobs (parsed maps ≈ JSON size in memory).
	// For 1000 bundles @ 5KB each: ~5MB, same as raw metadata storage.
	// Performance gain: Eliminates N × json.Unmarshal operations per query (where N = returned objects).
}

// remapFieldName converts field names to valid GraphQL camelCase identifiers
func remapFieldName(name string) string {
	// Handle empty names
	if name == "" {
		return "value"
	}

	// Replace invalid characters with underscores
	clean := invalidCharsRE.ReplaceAllString(name, "_")

	// Collapse multiple consecutive underscores
	clean = consecutiveUnderscoresRE.ReplaceAllString(clean, "_")

	// Trim leading underscores only (keep trailing to detect them)
	clean = strings.TrimLeft(clean, "_")

	// Split on underscores and camelCase
	parts := strings.Split(clean, "_")
	result := ""
	isFirst := true
	for _, part := range parts {
		// Skip empty parts (from consecutive or trailing underscores)
		if part == "" {
			continue
		}

		if isFirst {
			// For the first part, check if it's all uppercase
			if strings.ToUpper(part) == part {
				// If all uppercase, convert entirely to lowercase
				result = strings.ToLower(part)
			} else {
				// Otherwise, make only the first character lowercase
				result = strings.ToLower(string(part[0])) + part[1:]
			}
			isFirst = false
		} else {
			// For subsequent parts, capitalize first letter, lowercase rest
			result += strings.ToUpper(string(part[0])) + strings.ToLower(part[1:])
		}
	}

	// Ensure it starts with a letter
	if result == "" || !startsWithLetterRE.MatchString(result) {
		result = "field_" + result
	}

	return result
}

// jsonTypeToGraphQL maps JSON types to GraphQL types
func jsonTypeToGraphQL(jsonType reflect.Kind, isArray bool) graphql.Type {
	var baseType graphql.Type

	switch jsonType {
	case reflect.String:
		baseType = graphql.String
	case reflect.Bool:
		baseType = graphql.Boolean
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		baseType = graphql.Int
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		baseType = graphql.Int
	case reflect.Float32, reflect.Float64:
		baseType = graphql.Float
	default:
		// For complex types, use String as fallback (JSON serialized)
		baseType = graphql.String
	}

	if isArray {
		return graphql.NewList(baseType)
	}
	return baseType
}

// determineFieldType determines the JSON type and array status of a value
func determineFieldType(value interface{}) (reflect.Kind, bool) {
	if value == nil {
		return reflect.String, false
	}

	valueType := reflect.TypeOf(value)
	if valueType.Kind() != reflect.Slice {
		return valueType.Kind(), false
	}

	// Handle slice types
	slice := reflect.ValueOf(value)
	if slice.Len() == 0 {
		return reflect.String, true
	}

	firstElem := slice.Index(0).Interface()
	if firstElem == nil {
		return reflect.String, true
	}

	return reflect.TypeOf(firstElem).Kind(), true
}

// analyzeFieldValue analyzes a field value and returns type info, sample value, and nested fields
func analyzeFieldValue(value interface{}) (reflect.Kind, bool, interface{}, map[string]*FieldInfo) {
	if value == nil {
		return reflect.String, false, value, nil
	}

	valueType := reflect.TypeOf(value)
	if valueType.Kind() != reflect.Slice {
		return valueType.Kind(), false, value, nil
	}

	// Handle slice types
	slice := reflect.ValueOf(value)
	if slice.Len() == 0 {
		return reflect.String, true, value, nil
	}

	firstElem := slice.Index(0).Interface()
	if firstElem == nil {
		return reflect.String, true, value, nil
	}

	jsonType := reflect.TypeOf(firstElem).Kind()

	// If array element is an object, analyze its structure
	var nestedFields map[string]*FieldInfo
	if jsonType == reflect.Map {
		if elemObj, ok := firstElem.(map[string]interface{}); ok {
			nestedFields = analyzeNestedObject(elemObj)
		}
	}

	return jsonType, true, firstElem, nestedFields
}

// analyzeNestedObject analyzes a nested object and returns its field structure
func analyzeNestedObject(obj map[string]interface{}) map[string]*FieldInfo {
	fields := make(map[string]*FieldInfo)

	for key, value := range obj {
		fieldName := remapFieldName(key)
		jsonType, isArray := determineFieldType(value)

		fields[fieldName] = &FieldInfo{
			Name:         fieldName,
			GraphQLType:  jsonTypeToGraphQL(jsonType, isArray),
			JSONType:     jsonType,
			IsArray:      isArray,
			SampleValues: []interface{}{value},
		}
	}

	return fields
}

// mergeNestedFields merges discovered nested fields into existing ones
func mergeNestedFields(existing, new map[string]*FieldInfo) {
	for fieldName, newInfo := range new {
		if existingInfo, ok := existing[fieldName]; ok {
			// Merge sample values
			for _, sample := range newInfo.SampleValues {
				existingInfo.SampleValues = appendUnique(existingInfo.SampleValues, sample)
			}
		} else {
			existing[fieldName] = newInfo
		}
	}
}

// analyzeJSONObject analyzes a JSON object and extracts field information
func analyzeJSONObject(obj map[string]interface{}, info *SchemaInfo) {
	if info.Fields == nil {
		info.Fields = make(map[string]*FieldInfo)
	}

	for key, value := range obj {
		fieldName := remapFieldName(key)

		// Determine type, array status, sample value, and nested structure
		jsonType, isArray, sampleValue, nestedFields := analyzeFieldValue(value)

		// Update or create field info
		existing, ok := info.Fields[fieldName]
		if !ok {
			info.Fields[fieldName] = &FieldInfo{
				Name:         fieldName,
				GraphQLType:  jsonTypeToGraphQL(jsonType, isArray),
				JSONType:     jsonType,
				IsArray:      isArray,
				SampleValues: []interface{}{sampleValue},
				NestedFields: nestedFields,
			}
			continue
		}

		// Update existing field
		existing.SampleValues = appendUnique(existing.SampleValues, sampleValue)

		// Merge nested fields if discovered
		if nestedFields == nil {
			continue
		}
		if existing.NestedFields == nil {
			existing.NestedFields = nestedFields
		} else {
			mergeNestedFields(existing.NestedFields, nestedFields)
		}
	}
}

// appendUnique adds a value to slice if not already present, using JSON string as key for uniqueness
func appendUnique(slice []interface{}, value interface{}) []interface{} {
	seen := make(map[string]struct{}, len(slice))

	for _, existing := range slice {
		key, err := json.Marshal(existing)
		if err != nil {
			continue // skip values that can't be marshaled
		}
		seen[string(key)] = struct{}{}
	}

	valueKey, err := json.Marshal(value)
	if err != nil {
		return slice // skip value if it can't be marshaled
	}

	if _, exists := seen[string(valueKey)]; exists {
		return slice
	}

	return append(slice, value)
}

// DiscoverSchemaFromMetas analyzes Meta objects to discover schema structure
func DiscoverSchemaFromMetas(metas []*declcfg.Meta) (*CatalogSchema, error) {
	catalogSchema := &CatalogSchema{
		Schemas: make(map[string]*SchemaInfo),
	}

	// Process each meta object
	for _, meta := range metas {
		if meta.Schema == "" {
			continue
		}

		// Ensure schema info exists
		if catalogSchema.Schemas[meta.Schema] == nil {
			catalogSchema.Schemas[meta.Schema] = &SchemaInfo{
				Fields:       make(map[string]*FieldInfo),
				TotalObjects: 0,
			}
		}

		info := catalogSchema.Schemas[meta.Schema]
		info.TotalObjects++

		// Parse the JSON blob
		var obj map[string]interface{}
		if err := json.Unmarshal(meta.Blob, &obj); err != nil {
			continue // Skip malformed objects
		}

		// Store a sample object for reference
		if info.SampleObject == nil {
			info.SampleObject = obj
		}

		// Analyze general fields (including nested structures)
		analyzeJSONObject(obj, info)
	}

	return catalogSchema, nil
}

// marshalComplexValue marshals maps and slices as JSON strings
func marshalComplexValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	// Use reflection to detect maps and slices
	v := reflect.ValueOf(value)
	kind := v.Kind()

	if kind == reflect.Map || kind == reflect.Slice {
		// Marshal as JSON
		if jsonBytes, err := json.Marshal(value); err == nil {
			return string(jsonBytes)
		}
		// If marshal fails, return formatted string
		return fmt.Sprintf("%v", value)
	}

	// For simple types, return as-is
	return value
}

// createNestedObjectType creates a GraphQL object type for nested array elements
func createNestedObjectType(typeName string, nestedFields map[string]*FieldInfo) *graphql.Object {
	fields := graphql.Fields{}

	for fieldName, fieldInfo := range nestedFields {
		fieldName := fieldName // Capture loop variable
		fieldInfo := fieldInfo // Capture loop variable

		fields[fieldName] = &graphql.Field{
			Type: fieldInfo.GraphQLType,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				if source, ok := p.Source.(map[string]interface{}); ok {
					// Try direct field name first
					if value, ok := source[fieldName]; ok {
						return marshalComplexValue(value), nil
					}
					// Then try finding by remapped name
					for origKey, value := range source {
						if remapFieldName(origKey) == fieldName {
							return marshalComplexValue(value), nil
						}
					}
				}
				return nil, nil
			},
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   typeName,
		Fields: fields,
	})
}

// createFieldResolver creates a resolver function for a field name
func createFieldResolver(fieldName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		source, ok := p.Source.(map[string]interface{})
		if !ok {
			return nil, nil
		}

		for origKey, value := range source {
			if remapFieldName(origKey) == fieldName {
				return value, nil
			}
		}
		return nil, nil
	}
}

// buildGraphQLObjectType creates a GraphQL object type from discovered field info
func buildGraphQLObjectType(schemaName string, info *SchemaInfo) *graphql.Object {
	fields := graphql.Fields{}

	// Add discovered fields
	for fieldName, fieldInfo := range info.Fields {
		fieldName := fieldName // Capture loop variable
		fieldInfo := fieldInfo // Capture loop variable

		var fieldType graphql.Output
		// Check if this field has nested structure (array of objects)
		if len(fieldInfo.NestedFields) > 0 {
			// Create a dynamic nested type
			nestedTypeName := sanitizeTypeName(schemaName) + sanitizeTypeName(fieldName)
			nestedType := createNestedObjectType(nestedTypeName, fieldInfo.NestedFields)
			fieldType = graphql.NewList(nestedType)
		} else {
			// Regular field (not nested)
			fieldType = fieldInfo.GraphQLType
		}

		fields[fieldName] = &graphql.Field{
			Type:    fieldType,
			Resolve: createFieldResolver(fieldName),
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   sanitizeTypeName(schemaName),
		Fields: fields,
	})
}

// sanitizeTypeName converts a property type to a valid GraphQL type name
func sanitizeTypeName(propType string) string {
	// Remove dots and other invalid characters, capitalize words
	clean := alphanumericOnlyRE.ReplaceAllString(propType, "_")

	// Strip leading digits
	clean = leadingDigitsRE.ReplaceAllString(clean, "")

	parts := strings.Split(clean, "_")

	result := ""
	for _, part := range parts {
		if part != "" {
			result += strings.ToUpper(string(part[0])) + strings.ToLower(part[1:])
		}
	}

	if result == "" {
		result = "Unknown"
	}

	return result
}

// BuildDynamicGraphQLSchema creates a complete GraphQL schema from discovered structure
func BuildDynamicGraphQLSchema(catalogSchema *CatalogSchema, metasBySchema map[string][]*declcfg.Meta) (*DynamicSchema, error) {
	// Pre-parse all meta blobs to avoid unmarshaling on every query
	// This has minimal memory overhead (parsed objects ≈ raw blob size)
	// but eliminates expensive json.Unmarshal operations from the query path
	parsedObjects := make(map[string][]map[string]interface{})
	for schemaName, metas := range metasBySchema {
		parsedObjects[schemaName] = make([]map[string]interface{}, 0, len(metas))
		for _, meta := range metas {
			var obj map[string]interface{}
			if err := json.Unmarshal(meta.Blob, &obj); err != nil {
				continue // Skip malformed objects (same as runtime behavior)
			}
			parsedObjects[schemaName] = append(parsedObjects[schemaName], obj)
		}
	}

	// Build GraphQL object types for each discovered schema
	objectTypes := make(map[string]*graphql.Object)

	for schemaName, schemaInfo := range catalogSchema.Schemas {
		objectTypes[schemaName] = buildGraphQLObjectType(schemaName, schemaInfo)
	}

	// Pre-build field name to schema name lookup map for O(1) access in resolvers
	fieldNameToSchema := make(map[string]string)
	for schemaName := range catalogSchema.Schemas {
		sanitized := alphanumericOnlyRE.ReplaceAllString(schemaName, "")
		fieldName := strings.ToLower(sanitized) + "s" // e.g., "olmbundles", "olmpackages"
		fieldNameToSchema[fieldName] = schemaName
	}

	// Create root query fields
	queryFields := graphql.Fields{}

	for schemaName, objectType := range objectTypes {
		schemaName := schemaName // Capture loop variable
		objectType := objectType // Capture loop variable
		// Generate GraphQL field name from schema name
		// Convention: remove dots/special chars, lowercase, append 's' for pluralization
		// Examples: "olm.bundle" -> "olmbundles", "helm.chart" -> "helmcharts"
		// LIMITATION: Simple 's' appending doesn't follow English grammar rules or support
		// non-English languages. Schemas should use names that pluralize well with 's'.
		sanitized := alphanumericOnlyRE.ReplaceAllString(schemaName, "")
		fieldName := strings.ToLower(sanitized) + "s"

		queryFields[fieldName] = &graphql.Field{
			Type: graphql.NewList(objectType),
			Args: graphql.FieldConfigArgument{
				"limit": &graphql.ArgumentConfig{
					Type:         graphql.Int,
					DefaultValue: 100,
					Description:  "Maximum number of items to return",
				},
				"offset": &graphql.ArgumentConfig{
					Type:         graphql.Int,
					DefaultValue: 0,
					Description:  "Number of items to skip",
				},
			},
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				// O(1) lookup of schema name from pre-built map
				currentSchemaName, ok := fieldNameToSchema[p.Info.FieldName]
				if !ok {
					return nil, fmt.Errorf("unknown schema for field %s", p.Info.FieldName)
				}

				// Get pre-parsed objects for this schema (no unmarshaling needed!)
				objects, ok := parsedObjects[currentSchemaName]
				if !ok {
					return []interface{}{}, nil
				}

				// Parse arguments
				limit, _ := p.Args["limit"].(int)
				offset, _ := p.Args["offset"].(int)

				// Apply pagination to pre-parsed objects
				var results []interface{}
				for i, obj := range objects {
					if i < offset {
						continue
					}
					if len(results) >= limit {
						break
					}
					results = append(results, obj)
				}

				return results, nil
			},
		}
	}

	// Add summary field
	queryFields["summary"] = &graphql.Field{
		Type: graphql.NewObject(graphql.ObjectConfig{
			Name: "CatalogSummary",
			Fields: graphql.Fields{
				"totalSchemas": &graphql.Field{Type: graphql.Int},
				"schemas": &graphql.Field{
					Type: graphql.NewList(graphql.NewObject(graphql.ObjectConfig{
						Name: "SchemaSummary",
						Fields: graphql.Fields{
							"name":         &graphql.Field{Type: graphql.String},
							"totalObjects": &graphql.Field{Type: graphql.Int},
							"totalFields":  &graphql.Field{Type: graphql.Int},
						},
					})),
				},
			},
		}),
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			schemas := make([]interface{}, 0, len(catalogSchema.Schemas))
			for name, info := range catalogSchema.Schemas {
				schemas = append(schemas, map[string]interface{}{
					"name":         name,
					"totalObjects": info.TotalObjects,
					"totalFields":  len(info.Fields),
				})
			}

			return map[string]interface{}{
				"totalSchemas": len(catalogSchema.Schemas),
				"schemas":      schemas,
			}, nil
		},
	}

	// Create root query
	rootQuery := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Build the schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: rootQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL schema: %w", err)
	}

	return &DynamicSchema{
		Schema:        schema,
		CatalogSchema: catalogSchema,
		ParsedObjects: parsedObjects,
	}, nil
}

// LoadAndSummarizeCatalogDynamic loads FBC using WalkMetasReader and builds dynamic GraphQL schema
func LoadAndSummarizeCatalogDynamic(catalogFS fs.FS) (*DynamicSchema, error) {
	var metas []*declcfg.Meta

	// Collect all metas from the filesystem
	err := declcfg.WalkMetasFS(context.Background(), catalogFS, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}
		if meta != nil {
			metas = append(metas, meta)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking catalog metas: %w", err)
	}

	// Discover schema from collected metas
	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		return nil, fmt.Errorf("error discovering schema: %w", err)
	}

	// Organize metas by schema for resolvers
	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, meta := range metas {
		if meta.Schema != "" {
			metasBySchema[meta.Schema] = append(metasBySchema[meta.Schema], meta)
		}
	}

	// Build dynamic GraphQL schema
	dynamicSchema, err := BuildDynamicGraphQLSchema(catalogSchema, metasBySchema)
	if err != nil {
		return nil, fmt.Errorf("error building GraphQL schema: %w", err)
	}

	return dynamicSchema, nil
}

// PrintCatalogSummary prints a comprehensive summary of the discovered schema
func PrintCatalogSummary(dynamicSchema *DynamicSchema) {
	catalogSchema := dynamicSchema.CatalogSchema

	// Print comprehensive summary
	fmt.Printf("Dynamic GraphQL schema generation complete.\n")
	fmt.Printf("Total schemas discovered: %d\n", len(catalogSchema.Schemas))

	for schemaName, info := range catalogSchema.Schemas {
		fmt.Printf("\nSchema: %s\n", schemaName)
		fmt.Printf("  Objects: %d\n", info.TotalObjects)
		fmt.Printf("  Fields: %d\n", len(info.Fields))

		// Show sample fields
		if len(info.Fields) > 0 {
			fmt.Printf("  Sample fields: ")
			count := 0
			for fieldName := range info.Fields {
				if count > 0 {
					fmt.Printf(", ")
				}
				fmt.Printf("%s", fieldName)
				count++
				if count >= 5 { // Show first 5 fields
					if len(info.Fields) > 5 {
						fmt.Printf(", ...")
					}
					break
				}
			}
			fmt.Printf("\n")
		}
	}

	fmt.Printf("\nGraphQL endpoints available:\n")
	for schemaName := range catalogSchema.Schemas {
		fmt.Printf("  - %ss\n", strings.ToLower(schemaName))
	}
	fmt.Printf("  - summary\n")

	fmt.Printf("\nSample GraphQL query:\n")
	fmt.Printf("{\n")
	fmt.Printf("  summary {\n")
	fmt.Printf("    totalSchemas\n")
	fmt.Printf("    schemas { name totalObjects totalFields }\n")
	fmt.Printf("  }\n")
	if _, ok := catalogSchema.Schemas[declcfg.SchemaBundle]; ok {
		fmt.Printf("  bundles(limit: 5) { name package }\n")
	}
	if _, ok := catalogSchema.Schemas[declcfg.SchemaPackage]; ok {
		fmt.Printf("  packages(limit: 5) { name }\n")
	}
	fmt.Printf("}\n")
}
