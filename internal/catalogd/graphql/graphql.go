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

// FieldInfo represents discovered field information
type FieldInfo struct {
	Name         string
	GraphQLType  graphql.Type
	JSONType     reflect.Kind
	IsArray      bool
	SampleValues []interface{}
}

// SchemaInfo holds discovered schema information
type SchemaInfo struct {
	Fields        map[string]*FieldInfo
	PropertyTypes map[string]map[string]*FieldInfo // For bundle properties: type -> field -> info
	TotalObjects  int
	SampleObject  map[string]interface{}
}

// CatalogSchema holds the complete discovered schema
type CatalogSchema struct {
	Schemas map[string]*SchemaInfo // schema name -> info
}

// DynamicSchema holds the generated GraphQL schema and metadata
type DynamicSchema struct {
	Schema        graphql.Schema
	CatalogSchema *CatalogSchema
	MetasBySchema map[string][]*declcfg.Meta // For resolvers
}

// remapFieldName converts field names to valid GraphQL camelCase identifiers
func remapFieldName(name string) string {
	// Handle empty names
	if name == "" {
		return "value"
	}

	// Replace invalid characters with underscores
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	clean := re.ReplaceAllString(name, "_")

	// Collapse multiple consecutive underscores
	clean = regexp.MustCompile(`_+`).ReplaceAllString(clean, "_")

	// Trim leading underscores only (keep trailing to detect them)
	clean = strings.TrimLeft(clean, "_")

	// Split on underscores and camelCase
	parts := strings.Split(clean, "_")
	result := ""
	hasContent := false
	for i, part := range parts {
		if part == "" {
			// If we have an empty part after having content, it means there was a trailing separator
			// Add a capitalized version of the last word
			if hasContent && i == len(parts)-1 {
				// Get the base word (first non-empty part)
				for _, p := range parts {
					if p != "" {
						result += strings.ToUpper(string(p[0])) + strings.ToLower(p[1:])
						break
					}
				}
			}
			continue
		}
		hasContent = true
		if i == 0 || result == "" {
			// For the first part, check if it's all uppercase
			if strings.ToUpper(part) == part {
				// If all uppercase, convert entirely to lowercase
				result = strings.ToLower(part)
			} else {
				// Otherwise, make only the first character lowercase
				result = strings.ToLower(string(part[0])) + part[1:]
			}
		} else {
			// For subsequent parts, capitalize first letter, lowercase rest
			result += strings.ToUpper(string(part[0])) + strings.ToLower(part[1:])
		}
	}

	// Ensure it starts with a letter
	if result == "" || !regexp.MustCompile(`^[a-zA-Z]`).MatchString(result) {
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

// analyzeJSONObject analyzes a JSON object and extracts field information
func analyzeJSONObject(obj map[string]interface{}, info *SchemaInfo) {
	if info.Fields == nil {
		info.Fields = make(map[string]*FieldInfo)
	}

	for key, value := range obj {
		fieldName := remapFieldName(key)

		// Determine type and array status
		isArray := false
		var jsonType reflect.Kind
		var sampleValue interface{} = value

		if value == nil {
			jsonType = reflect.String // Default for null values
		} else {
			valueType := reflect.TypeOf(value)
			if valueType.Kind() == reflect.Slice {
				isArray = true
				slice := reflect.ValueOf(value)
				if slice.Len() > 0 {
					firstElem := slice.Index(0).Interface()
					if firstElem != nil {
						jsonType = reflect.TypeOf(firstElem).Kind()
						sampleValue = firstElem
					} else {
						jsonType = reflect.String
					}
				} else {
					jsonType = reflect.String
				}
			} else {
				jsonType = valueType.Kind()
			}
		}

		// Update or create field info
		if existing, ok := info.Fields[fieldName]; ok {
			// Add sample value if not already present
			existing.SampleValues = appendUnique(existing.SampleValues, sampleValue)
		} else {
			info.Fields[fieldName] = &FieldInfo{
				Name:         fieldName,
				GraphQLType:  jsonTypeToGraphQL(jsonType, isArray),
				JSONType:     jsonType,
				IsArray:      isArray,
				SampleValues: []interface{}{sampleValue},
			}
		}
	}
}

// analyzeBundleProperties analyzes bundle properties for union type creation
func analyzeBundleProperties(obj map[string]interface{}, info *SchemaInfo) {
	if info.PropertyTypes == nil {
		info.PropertyTypes = make(map[string]map[string]*FieldInfo)
	}

	properties, ok := obj["properties"]
	if !ok {
		return
	}

	propsSlice, ok := properties.([]interface{})
	if !ok {
		return
	}

	for _, prop := range propsSlice {
		propObj, ok := prop.(map[string]interface{})
		if !ok {
			continue
		}

		propType, ok := propObj["type"].(string)
		if !ok {
			continue
		}

		value, ok := propObj["value"]
		if !ok {
			continue
		}

		// Analyze the value structure for this property type
		if valueObj, ok := value.(map[string]interface{}); ok {
			if info.PropertyTypes[propType] == nil {
				info.PropertyTypes[propType] = make(map[string]*FieldInfo)
			}

			for key, val := range valueObj {
				fieldName := remapFieldName(key)
				isArray := false
				var jsonType reflect.Kind

				if val == nil {
					jsonType = reflect.String
				} else {
					valType := reflect.TypeOf(val)
					if valType.Kind() == reflect.Slice {
						isArray = true
						slice := reflect.ValueOf(val)
						if slice.Len() > 0 {
							firstElem := slice.Index(0).Interface()
							if firstElem != nil {
								jsonType = reflect.TypeOf(firstElem).Kind()
							} else {
								jsonType = reflect.String
							}
						} else {
							jsonType = reflect.String
						}
					} else {
						jsonType = valType.Kind()
					}
				}

				if existing, ok := info.PropertyTypes[propType][fieldName]; ok {
					existing.SampleValues = appendUnique(existing.SampleValues, val)
				} else {
					info.PropertyTypes[propType][fieldName] = &FieldInfo{
						Name:         fieldName,
						GraphQLType:  jsonTypeToGraphQL(jsonType, isArray),
						JSONType:     jsonType,
						IsArray:      isArray,
						SampleValues: []interface{}{val},
					}
				}
			}
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
				Fields:        make(map[string]*FieldInfo),
				PropertyTypes: make(map[string]map[string]*FieldInfo),
				TotalObjects:  0,
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

		// Analyze general fields
		analyzeJSONObject(obj, info)

		// Special handling for bundle properties
		if meta.Schema == declcfg.SchemaBundle {
			analyzeBundleProperties(obj, info)
		}
	}

	return catalogSchema, nil
}

// buildGraphQLObjectType creates a GraphQL object type from discovered field info
func buildGraphQLObjectType(schemaName string, info *SchemaInfo) *graphql.Object {
	fields := graphql.Fields{}

	// Add discovered fields
	for fieldName, fieldInfo := range info.Fields {
		fields[fieldName] = &graphql.Field{
			Type: fieldInfo.GraphQLType,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				if source, ok := p.Source.(map[string]interface{}); ok {
					// Find the original JSON key for this GraphQL field
					for origKey, value := range source {
						if remapFieldName(origKey) == fieldName {
							return value, nil
						}
					}
				}
				return nil, nil
			},
		}
	}

	// Special handling for bundle properties
	if schemaName == declcfg.SchemaBundle && len(info.PropertyTypes) > 0 {
		fields["properties"] = &graphql.Field{
			Type: graphql.NewList(createBundlePropertyType(info.PropertyTypes)),
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				if source, ok := p.Source.(map[string]interface{}); ok {
					if props, ok := source["properties"]; ok {
						return props, nil
					}
				}
				return nil, nil
			},
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   sanitizeTypeName(schemaName),
		Fields: fields,
	})
}

// createBundlePropertyType creates a GraphQL type for bundle properties with union values
func createBundlePropertyType(propertyTypes map[string]map[string]*FieldInfo) *graphql.Object {
	// Create union type for property values
	var unionTypes []*graphql.Object
	unionTypesMap := make(map[string]*graphql.Object)

	for propType, fields := range propertyTypes {
		typeName := fmt.Sprintf("PropertyValue%s", sanitizeTypeName(propType))

		valueFields := graphql.Fields{}
		for fieldName, fieldInfo := range fields {
			valueFields[fieldName] = &graphql.Field{
				Type: fieldInfo.GraphQLType,
			}
		}

		objType := graphql.NewObject(graphql.ObjectConfig{
			Name:   typeName,
			Fields: valueFields,
		})

		unionTypes = append(unionTypes, objType)
		unionTypesMap[propType] = objType
	}

	// Create union of all property value types
	var valueUnion *graphql.Union
	if len(unionTypes) > 0 {
		valueUnion = graphql.NewUnion(graphql.UnionConfig{
			Name:  "PropertyValue",
			Types: unionTypes,
			ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
				// Try to determine the type from the parent property's type field
				if valueMap, ok := p.Value.(map[string]interface{}); ok {
					// Look for type in parent context (property object should have type field)
					if parent, ok := p.Context.Value("propertyType").(string); ok {
						if objType, ok := unionTypesMap[parent]; ok {
							return objType
						}
					}
					// Fallback: use the first matching type
					for _, objType := range unionTypesMap {
						if len(valueMap) > 0 {
							return objType
						}
					}
				}
				// Default to first type if available
				if len(unionTypes) > 0 {
					return unionTypes[0]
				}
				return nil
			},
		})
	}

	// Create the bundle property object type
	propertyFields := graphql.Fields{
		"type": &graphql.Field{Type: graphql.String},
	}

	if valueUnion != nil {
		propertyFields["value"] = &graphql.Field{Type: valueUnion}
	} else {
		// Fallback to string if no union types
		propertyFields["value"] = &graphql.Field{Type: graphql.String}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   "BundleProperty",
		Fields: propertyFields,
	})
}

// sanitizeTypeName converts a property type to a valid GraphQL type name
func sanitizeTypeName(propType string) string {
	// Remove dots and other invalid characters, capitalize words
	re := regexp.MustCompile(`[^a-zA-Z0-9]`)
	clean := re.ReplaceAllString(propType, "_")

	// Strip leading digits
	clean = regexp.MustCompile(`^[0-9]+`).ReplaceAllString(clean, "")

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
	// Build GraphQL object types for each discovered schema
	objectTypes := make(map[string]*graphql.Object)

	for schemaName, schemaInfo := range catalogSchema.Schemas {
		objectTypes[schemaName] = buildGraphQLObjectType(schemaName, schemaInfo)
	}

	// Create root query fields
	queryFields := graphql.Fields{}

	for schemaName, objectType := range objectTypes {
		// Sanitize schema name by removing dots and special characters for GraphQL field name
		sanitized := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(schemaName, "")
		fieldName := strings.ToLower(sanitized) + "s" // e.g., "olmbundles", "olmpackages"

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
				// Get the schema name from the field name
				currentSchemaName := ""
				for sn := range catalogSchema.Schemas {
					sanitized := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(sn, "")
					if strings.ToLower(sanitized)+"s" == p.Info.FieldName {
						currentSchemaName = sn
						break
					}
				}

				if currentSchemaName == "" {
					return nil, fmt.Errorf("unknown schema for field %s", p.Info.FieldName)
				}

				// Get metas for this schema
				metas, ok := metasBySchema[currentSchemaName]
				if !ok {
					return []interface{}{}, nil
				}

				// Parse arguments
				limit, _ := p.Args["limit"].(int)
				offset, _ := p.Args["offset"].(int)

				// Convert metas to JSON objects and apply pagination
				var results []interface{}
				for i, meta := range metas {
					if i < offset {
						continue
					}
					if len(results) >= limit {
						break
					}

					var obj map[string]interface{}
					if err := json.Unmarshal(meta.Blob, &obj); err != nil {
						continue // Skip malformed objects
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
			schemas := []interface{}{}
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
		MetasBySchema: metasBySchema,
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

		if schemaName == declcfg.SchemaBundle && len(info.PropertyTypes) > 0 {
			fmt.Printf("  Property types: %d\n", len(info.PropertyTypes))
			for propType, fields := range info.PropertyTypes {
				fmt.Printf("    - %s (%d fields)\n", propType, len(fields))
			}
		}

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
