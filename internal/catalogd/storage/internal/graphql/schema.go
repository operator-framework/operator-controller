package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"regexp"

	"github.com/graphql-go/graphql"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// GenerateSchema processes FBC meta objects and returns a GraphQL schema that represents them.
func GenerateSchema(ctx context.Context, metas iter.Seq2[*declcfg.Meta, error]) (*graphql.Schema, error) {
	schemaFieldData := make(map[string]map[string]interface{})
	for meta, iterErr := range metas {
		if iterErr != nil {
			return nil, fmt.Errorf("error walking FBC data: %v", iterErr)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Parse the blob content
		var blobContent map[string]interface{}
		if err := json.Unmarshal(meta.Blob, &blobContent); err != nil {
			return nil, err
		}

		// Initialize schema field data if not exists
		if schemaFieldData[meta.Schema] == nil {
			schemaFieldData[meta.Schema] = map[string]interface{}{}
		}

		// Process each field in the blob - only merge data, no GraphQL object creation
		for fieldName, fieldValue := range blobContent {
			// Deep merge field values to capture all possible nested fields
			if existingValue, exists := schemaFieldData[meta.Schema][fieldName]; exists {
				mergedValue, err := deepMerge(existingValue, fieldValue, fmt.Sprintf("%s.%s", meta.Schema, fieldName))
				if err != nil {
					return nil, fmt.Errorf("failed to merge field '%s' in schema '%s': %w",
						fieldName, meta.Schema, err)
				}
				schemaFieldData[meta.Schema][fieldName] = mergedValue
			} else {
				schemaFieldData[meta.Schema][fieldName] = fieldValue
			}
		}
	}
	return generateSchema(schemaFieldData)
}

func deepMergeMap(existingMap, newMap map[string]interface{}, fieldPath string) (map[string]interface{}, error) {
	for key, newValue := range newMap {
		if existingValue, exists := existingMap[key]; exists {
			// Recursively merge the field
			mergedValue, err := deepMerge(existingValue, newValue, fieldPath+"."+key)
			if err != nil {
				return nil, err
			}
			existingMap[key] = mergedValue
		} else {
			// Field doesn't exist in existing, add it
			existingMap[key] = newValue
		}
	}
	return existingMap, nil
}

func deepMergeArray(existingArray, newArray []interface{}, fieldPath string) ([]interface{}, error) {
	mergeInto := func(element interface{}, itemsSlices ...[]interface{}) ([]interface{}, error) {
		var err error
		elementType := getValueTypeName(element)

		for _, items := range itemsSlices {
			for _, item := range items {
				itemType := getValueTypeName(item)
				if itemType != elementType {
					return nil, fmt.Errorf("cannot merge arrays at %s: different element types (%s vs %s)",
						fieldPath, elementType, itemType)
				}

				element, err = deepMerge(element, item, fmt.Sprintf("%s[%s]", fieldPath, elementType))
				if err != nil {
					return nil, err
				}
			}
		}
		return []interface{}{element}, nil
	}

	if isPropertiesPattern(existingArray) && isPropertiesPattern(newArray) {
		// Property objects always have the same structure: {type: string, value: anything}
		// No need to merge - just return the canonical structure
		return []interface{}{
			map[string]interface{}{
				"type":  "",
				"value": nil,
			},
		}, nil
	}

	switch {
	case len(existingArray) == 0 && len(newArray) == 0:
		return []interface{}{}, nil
	case len(existingArray) == 0:
		return mergeInto(newArray[0], newArray[1:])
	case len(newArray) == 0:
		return mergeInto(existingArray[0], existingArray[1:])
	default:
		return mergeInto(existingArray[0], existingArray[1:], newArray)
	}
}

// deepMerge recursively merges two interface{} values, prioritizing comprehensive field coverage
// Returns an error if there are type mismatches between existing and new values
func deepMerge(existing, new interface{}, fieldPath string) (interface{}, error) {
	// If existing is nil, return new
	if existing == nil {
		return new, nil
	}

	// If new is nil, return existing
	if new == nil {
		return existing, nil
	}

	// Check for type mismatches
	existingType := getValueTypeName(existing)
	newType := getValueTypeName(new)

	if existingType != newType {
		return nil, fmt.Errorf("type mismatch for field '%s': existing type '%s' conflicts with new type '%s'",
			fieldPath, existingType, newType)
	}

	// If both are maps, merge them.
	existingMap, existingIsMap := existing.(map[string]interface{})
	newMap, newIsMap := new.(map[string]interface{})
	if existingIsMap && newIsMap {
		return deepMergeMap(existingMap, newMap, fieldPath)
	}

	// If both are arrays, merge them:
	//   1. Validate that all elements have the same type
	//   2. Merge all elements into a single representative element
	//   3. Return the merged element as a single-element array
	existingArray, existingIsArray := existing.([]interface{})
	newArray, newIsArray := new.([]interface{})
	if existingIsArray && newIsArray {
		return deepMergeArray(existingArray, newArray, fieldPath)
	}

	// For primitives of the same type, prefer the existing value
	// (we already have a sample, both are valid examples)
	return existing, nil
}

// GenerateFBCSchema generates a complete GraphQL schema from accumulated data - Phase 2: Schema Generation
func generateSchema(schemaFieldData map[string]map[string]interface{}) (*graphql.Schema, error) {
	if len(schemaFieldData) == 0 {
		return nil, fmt.Errorf("no schema data available - no Meta objects have been processed")
	}

	// Build GraphQL types for each schema
	graphqlNamer := newNamer()
	rootFields := make(graphql.Fields, len(schemaFieldData))
	for schema, protoObj := range schemaFieldData {
		// Create GraphQL type for this schema using accumulated field data
		gqlType := generateTypeForSchema(schema, protoObj, graphqlNamer)

		// Create field name from schema (e.g., "olm.package" -> "olmPackage")
		fieldName := graphqlNamer.FieldNameForSchema(schema)

		// Add field to root query
		rootFields[fieldName] = &graphql.Field{
			Type: graphql.NewList(gqlType),
			Args: graphql.FieldConfigArgument{
				"name": &graphql.ArgumentConfig{
					Type:        graphql.String,
					Description: "Filter by name field",
				},
				"package": &graphql.ArgumentConfig{
					Type:        graphql.String,
					Description: "Filter by package field",
				},
			},
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				file, err := fileFromContext(p.Context)
				if err != nil {
					return nil, err
				}

				idx, err := indexFromContext(p.Context)
				if err != nil {
					return nil, err
				}

				// Get filter arguments
				nameFilter, _ := p.Args["name"].(string)
				packageFilter, _ := p.Args["package"].(string)

				// Parse the data using streaming JSON decoder
				reader := idx.Get(file, schema, packageFilter, nameFilter)
				decoder := json.NewDecoder(reader)
				result := make([]map[string]interface{}, 0)

				for {
					var parsedContent map[string]interface{}
					if err := decoder.Decode(&parsedContent); err != nil {
						if err == io.EOF {
							break
						}
						return nil, fmt.Errorf("failed to parse JSON: %w", err)
					}
					result = append(result, parsedContent)
				}

				return result, nil
			},
		}
	}

	// Create root query type
	rootQuery := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: rootFields,
	})

	// Create and return the schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: rootQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL schema: %w", err)
	}

	return &schema, nil
}

// generateTypeForSchema creates a GraphQL type for a specific schema using stored field information
func generateTypeForSchema(schema string, schemaFields map[string]interface{}, namer *namer) *graphql.Object {
	typeName := namer.TypeNameForSchema(schema)

	// Create GraphQL fields based on discovered fields
	gqlFields := make(graphql.Fields, len(schemaFields))
	for fieldName, fieldValue := range schemaFields {
		// Determine GraphQL type based on the field value with context
		gqlType := inferGraphQLTypeFromValueWithContext(typeName, fieldName, fieldValue, namer)

		var field *graphql.Field
		if fieldName == "properties" && isPropertiesPattern(fieldValue) {
			field = &graphql.Field{
				Type: gqlType,
				Args: graphql.FieldConfigArgument{
					"type": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Filter properties by type field",
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return filterProperties(p.Source.(map[string]interface{})[fieldName].([]interface{}), p.Args), nil
				},
			}
		} else {
			field = &graphql.Field{
				Type: gqlType,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return p.Source.(map[string]interface{})[fieldName], nil
				},
			}
		}

		gqlFields[fieldName] = field
	}

	// Create the object type
	objectType := graphql.NewObject(graphql.ObjectConfig{
		Name:   typeName,
		Fields: gqlFields,
	})

	return objectType
}

// isPropertiesPattern checks if a value matches the OLM properties pattern.
func isPropertiesPattern(value interface{}) bool {
	arr, ok := value.([]interface{})
	if !ok || len(arr) == 0 {
		return false
	}

	// Check if all elements have the expected structure for properties
	for _, item := range arr {
		if itemMap, ok := item.(map[string]interface{}); !ok || !isPropertyObject(itemMap) {
			return false
		}
	}
	return true
}

// isPropertyObject checks if this looks like a property object (has type(String) and value(Any) fields)
func isPropertyObject(obj map[string]interface{}) bool {
	typeVal, hasType := obj["type"]
	_, typeIsString := typeVal.(string)
	_, hasValue := obj["value"]
	return hasType && typeIsString && hasValue && len(obj) == 2
}

// createPropertiesType creates a properties type using JSON scalar for arbitrary data
func createPropertiesType(parentName, name string, namer *namer) *graphql.Object {
	// Generate a type name for properties
	typeName := namer.TypeNameForField(parentName, name)

	objectType := graphql.NewObject(graphql.ObjectConfig{
		Name: typeName,
		Fields: graphql.Fields{
			"type": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return p.Source.(map[string]interface{})["type"], nil
				},
			},
			"value": newJSONScalarField("value"),
		},
	})

	return objectType
}

// filterProperties applies filtering to properties arrays based on arguments
func filterProperties(properties []interface{}, args map[string]interface{}) []interface{} {
	// Get the type filter argument
	typeFilter, _ := args["type"].(string)

	// If no type filter, return all properties
	if typeFilter == "" {
		return properties
	}

	// Filter properties by type
	var filtered []interface{}
	for _, prop := range properties {
		if propObj, ok := prop.(map[string]interface{}); ok {
			if propType, ok := propObj["type"].(string); ok && propType == typeFilter {
				filtered = append(filtered, prop)
			}
		}
	}

	return filtered
}

// isValidGraphQLFieldName checks if a string is a valid GraphQL field name
// GraphQL field names must match /^[_a-zA-Z][_a-zA-Z0-9]*$/
func isValidGraphQLFieldName(name string) bool {
	return regexp.MustCompile(`^[_a-zA-Z][_a-zA-Z0-9]*$`).MatchString(name)
}

// hasNoValidGraphQLFields checks if a map contains no valid GraphQL field names
func hasAllValidGraphQLFields(mapValue map[string]interface{}) bool {
	for key := range mapValue {
		if !isValidGraphQLFieldName(key) {
			return false
		}
	}
	return true
}

// inferGraphQLTypeFromValueWithContext infers a GraphQL type from a value with context
func inferGraphQLTypeFromValueWithContext(parentName, name string, value interface{}, namer *namer) graphql.Type {
	switch v := value.(type) {
	case string:
		return graphql.String
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return graphql.Int
	case float32, float64:
		return graphql.Float
	case bool:
		return graphql.Boolean
	case []interface{}:
		// Check if this matches the OLM properties pattern
		if isPropertiesPattern(value) {
			return graphql.NewList(createPropertiesType(parentName, name, namer))
		}

		// For regular arrays, create a list type using the first element
		if len(v) > 0 {
			elementType := inferGraphQLTypeFromValueWithContext(namer.TypeNameForField(parentName, name), "Item", v[0], namer)
			return graphql.NewList(elementType)
		}
		return graphql.NewList(jsonScalar)
	case map[string]interface{}:
		// Check if this map is empty or has no valid GraphQL fields
		if len(v) == 0 || !hasAllValidGraphQLFields(v) {
			return jsonScalar
		}
		return generateObjectTypeFromValue(namer.TypeNameForField(parentName, name), v, namer)
	case nil:
		return graphql.String
	default:
		return graphql.String // Default fallback
	}
}

// generateObjectTypeFromValue creates a GraphQL object type from a value using JSON scalar approach
func generateObjectTypeFromValue(name string, value map[string]interface{}, namer *namer) *graphql.Object {
	// Create fields for this object
	fields := make(graphql.Fields, len(value))

	for fieldName, fieldValue := range value {
		// Use JSON scalar for all property values to handle mixed primitive/object types
		fields[fieldName] = &graphql.Field{
			Name: fieldName,
			Type: inferGraphQLTypeFromValueWithContext(name, fieldName, fieldValue, namer),
		}
	}

	// Create the object type
	objectType := graphql.NewObject(graphql.ObjectConfig{
		Name:   name,
		Fields: fields,
	})

	return objectType
}

// getValueTypeName returns a type name for a value for consistency checking
func getValueTypeName(value interface{}) string {
	switch value.(type) {
	case string:
		return "string"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case float32, float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	case nil:
		return "null"
	default:
		panic(fmt.Sprintf("unknown value type: %T", value))
	}
}
