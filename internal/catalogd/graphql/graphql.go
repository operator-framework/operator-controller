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
	"k8s.io/klog/v2"

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
	OriginalName string // The original JSON key before remapping
	GraphQLType  graphql.Type
	JSONType     reflect.Kind
	IsArray      bool
	NestedFields map[string]*FieldInfo // For array-of-objects, stores object structure
}

// SchemaInfo holds discovered schema information
type SchemaInfo struct {
	Fields       map[string]*FieldInfo
	TotalObjects int
}

// CatalogSchema holds the complete discovered schema
type CatalogSchema struct {
	Schemas map[string]*SchemaInfo // schema name -> info
}

// serializableFieldInfo is a JSON-friendly representation of FieldInfo
type serializableFieldInfo struct {
	Name            string                            `json:"name"`
	OriginalName    string                            `json:"originalName"`
	JSONType        string                            `json:"jsonType"`
	IsArray         bool                              `json:"isArray"`
	GraphQLTypeName string                            `json:"graphqlTypeName,omitempty"`
	NestedFields    map[string]*serializableFieldInfo `json:"nestedFields,omitempty"`
}

// serializableSchemaInfo is a JSON-friendly representation of SchemaInfo
type serializableSchemaInfo struct {
	Fields       map[string]*serializableFieldInfo `json:"fields"`
	TotalObjects int                               `json:"totalObjects"`
}

// serializableCatalogSchema is a JSON-friendly representation of CatalogSchema
type serializableCatalogSchema struct {
	Schemas map[string]*serializableSchemaInfo `json:"schemas"`
}

func kindToString(k reflect.Kind) string {
	switch k {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "uint"
	case reflect.Float32, reflect.Float64:
		return "float64"
	default:
		return "string"
	}
}

func stringToKind(s string) reflect.Kind {
	switch s {
	case "string":
		return reflect.String
	case "bool":
		return reflect.Bool
	case "int":
		return reflect.Int
	case "uint":
		return reflect.Uint
	case "float64":
		return reflect.Float64
	default:
		return reflect.String
	}
}

func graphqlTypeName(t graphql.Type) string {
	if list, ok := t.(*graphql.List); ok {
		return graphqlTypeName(list.OfType)
	}
	return t.Name()
}

func graphqlTypeFromName(name string, isArray bool) graphql.Type {
	var base graphql.Type
	switch name {
	case "Int":
		base = graphql.Int
	case "Float":
		base = graphql.Float
	case "Boolean":
		base = graphql.Boolean
	default:
		base = graphql.String
	}
	if isArray {
		return graphql.NewList(base)
	}
	return base
}

func fieldInfoToSerializable(fi *FieldInfo) *serializableFieldInfo {
	sfi := &serializableFieldInfo{
		Name:            fi.Name,
		OriginalName:    fi.OriginalName,
		JSONType:        kindToString(fi.JSONType),
		IsArray:         fi.IsArray,
		GraphQLTypeName: graphqlTypeName(fi.GraphQLType),
	}
	if len(fi.NestedFields) > 0 {
		sfi.NestedFields = make(map[string]*serializableFieldInfo)
		for k, v := range fi.NestedFields {
			sfi.NestedFields[k] = fieldInfoToSerializable(v)
		}
	}
	return sfi
}

func serializableToFieldInfo(sfi *serializableFieldInfo) *FieldInfo {
	k := stringToKind(sfi.JSONType)
	var gqlType graphql.Type
	if sfi.GraphQLTypeName != "" {
		gqlType = graphqlTypeFromName(sfi.GraphQLTypeName, sfi.IsArray)
	} else {
		gqlType = jsonTypeToGraphQL(k, sfi.IsArray, nil)
	}
	fi := &FieldInfo{
		Name:         sfi.Name,
		OriginalName: sfi.OriginalName,
		JSONType:     k,
		IsArray:      sfi.IsArray,
		GraphQLType:  gqlType,
	}
	if len(sfi.NestedFields) > 0 {
		fi.NestedFields = make(map[string]*FieldInfo)
		for kk, v := range sfi.NestedFields {
			fi.NestedFields[kk] = serializableToFieldInfo(v)
		}
	}
	return fi
}

// MarshalCatalogSchema serializes a CatalogSchema to JSON bytes
func MarshalCatalogSchema(cs *CatalogSchema) ([]byte, error) {
	scs := &serializableCatalogSchema{
		Schemas: make(map[string]*serializableSchemaInfo),
	}
	for name, info := range cs.Schemas {
		si := &serializableSchemaInfo{
			Fields:       make(map[string]*serializableFieldInfo),
			TotalObjects: info.TotalObjects,
		}
		for fname, finfo := range info.Fields {
			si.Fields[fname] = fieldInfoToSerializable(finfo)
		}
		scs.Schemas[name] = si
	}
	return json.Marshal(scs)
}

// UnmarshalCatalogSchema deserializes a CatalogSchema from JSON bytes
func UnmarshalCatalogSchema(data []byte) (*CatalogSchema, error) {
	var scs serializableCatalogSchema
	if err := json.Unmarshal(data, &scs); err != nil {
		return nil, err
	}
	cs := &CatalogSchema{
		Schemas: make(map[string]*SchemaInfo),
	}
	for name, si := range scs.Schemas {
		info := &SchemaInfo{
			Fields:       make(map[string]*FieldInfo),
			TotalObjects: si.TotalObjects,
		}
		for fname, sfi := range si.Fields {
			info.Fields[fname] = serializableToFieldInfo(sfi)
		}
		cs.Schemas[name] = info
	}
	return cs, nil
}

// ObjectLoader reads FBC objects for a given schema from disk with pagination.
// It is called by the root resolver at query time instead of holding all
// parsed objects in memory.
type ObjectLoader func(schemaName string, offset, limit int) ([]map[string]interface{}, error)

// DynamicSchema holds the generated GraphQL schema and metadata
type DynamicSchema struct {
	Schema        graphql.Schema
	CatalogSchema *CatalogSchema
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

// isIntegerValue checks if a value is a float64 representing an integer
func isIntegerValue(value interface{}) bool {
	if f, ok := value.(float64); ok {
		return f == float64(int64(f))
	}
	return false
}

// jsonTypeToGraphQL maps JSON types to GraphQL types
func jsonTypeToGraphQL(jsonType reflect.Kind, isArray bool, sampleValue interface{}) graphql.Type {
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
		// JSON unmarshaling always produces float64 for numbers
		// Check if the sample value is actually an integer
		if isIntegerValue(sampleValue) {
			baseType = graphql.Int
		} else {
			baseType = graphql.Float
		}
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

// maxSampleElements limits how many slice elements are examined when inferring
// GraphQL types from catalog data, bounding work on large arrays.
const maxSampleElements = 10

// analyzeFieldValue analyzes a field value and returns type info, sample value, and nested fields.
// For slices, it examines up to maxSampleElements to detect heterogeneous element types
// and build a union of nested field structures across multiple elements.
func analyzeFieldValue(value interface{}) (reflect.Kind, bool, interface{}, map[string]*FieldInfo) {
	if value == nil {
		return reflect.String, false, value, nil
	}

	valueType := reflect.TypeOf(value)
	if valueType.Kind() != reflect.Slice {
		return valueType.Kind(), false, value, nil
	}

	slice := reflect.ValueOf(value)
	if slice.Len() == 0 {
		return reflect.String, true, value, nil
	}

	// Scan up to maxSampleElements to determine element type and nested structure
	var dominantType reflect.Kind
	var sampleValue interface{}
	var nestedFields map[string]*FieldInfo
	heterogeneous := false
	limit := slice.Len()
	if limit > maxSampleElements {
		limit = maxSampleElements
	}

	for i := 0; i < limit; i++ {
		elem := slice.Index(i).Interface()
		if elem == nil {
			continue
		}

		elemKind := reflect.TypeOf(elem).Kind()

		if sampleValue == nil {
			dominantType = elemKind
			sampleValue = elem
		} else if elemKind != dominantType {
			heterogeneous = true
		}

		// For map elements, merge nested structure from each sampled element
		if elemKind == reflect.Map {
			if elemObj, ok := elem.(map[string]interface{}); ok {
				elemFields := analyzeNestedObject(elemObj)
				if nestedFields == nil {
					nestedFields = elemFields
				} else {
					mergeNestedFields(nestedFields, elemFields)
				}
			}
		}
	}

	if sampleValue == nil {
		return reflect.String, true, value, nil
	}

	// Heterogeneous primitive arrays (string mixed with int, etc.) fall back to String
	if heterogeneous && nestedFields == nil {
		return reflect.String, true, sampleValue, nil
	}

	return dominantType, true, sampleValue, nestedFields
}

// analyzeNestedObject analyzes a nested object and returns its field structure
func analyzeNestedObject(obj map[string]interface{}) map[string]*FieldInfo {
	fields := make(map[string]*FieldInfo)

	for key, value := range obj {
		fieldName := remapFieldName(key)
		jsonType, isArray := determineFieldType(value)

		// For arrays, get the first element as sample for type detection
		sampleValue := value
		if isArray {
			if slice := reflect.ValueOf(value); slice.Kind() == reflect.Slice && slice.Len() > 0 {
				sampleValue = slice.Index(0).Interface()
			}
		}

		fields[fieldName] = &FieldInfo{
			Name:         fieldName,
			OriginalName: key,
			GraphQLType:  jsonTypeToGraphQL(jsonType, isArray, sampleValue),
			JSONType:     jsonType,
			IsArray:      isArray,
		}
	}

	return fields
}

// mergeNestedFields merges discovered nested fields into existing ones
func mergeNestedFields(existing, new map[string]*FieldInfo) {
	for fieldName, newInfo := range new {
		if _, ok := existing[fieldName]; !ok {
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
				OriginalName: key,
				GraphQLType:  jsonTypeToGraphQL(jsonType, isArray, sampleValue),
				JSONType:     jsonType,
				IsArray:      isArray,
				NestedFields: nestedFields,
			}
			continue
		}

		// Different original keys mapping to the same GraphQL field name is a collision
		if existing.OriginalName != key {
			klog.V(2).InfoS("field name collision: different JSON keys map to same GraphQL field",
				"graphqlField", fieldName, "existingKey", existing.OriginalName, "newKey", key)
		}

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

// processMetaIntoSchema parses one meta blob and updates the catalog schema.
// TotalObjects is incremented only on successful parse so pagination counts
// reflect objects that can actually be returned by the ObjectLoader.
func processMetaIntoSchema(catalogSchema *CatalogSchema, meta *declcfg.Meta) {
	if meta.Schema == "" {
		return
	}
	if catalogSchema.Schemas[meta.Schema] == nil {
		catalogSchema.Schemas[meta.Schema] = &SchemaInfo{
			Fields: make(map[string]*FieldInfo),
		}
	}
	info := catalogSchema.Schemas[meta.Schema]

	var obj map[string]interface{}
	if err := json.Unmarshal(meta.Blob, &obj); err != nil {
		klog.V(4).InfoS("skipping malformed meta blob during schema discovery",
			"schema", meta.Schema, "name", meta.Name, "error", err)
		return
	}

	info.TotalObjects++
	analyzeJSONObject(obj, info)
}

// DiscoverSchemaFromMetas analyzes Meta objects to discover schema structure.
func DiscoverSchemaFromMetas(metas []*declcfg.Meta) (*CatalogSchema, error) {
	catalogSchema := &CatalogSchema{Schemas: make(map[string]*SchemaInfo)}
	for _, meta := range metas {
		processMetaIntoSchema(catalogSchema, meta)
	}
	return catalogSchema, nil
}

// DiscoverSchemaFromChannel performs streaming schema discovery, processing
// one meta at a time through a channel. Each meta's blob is parsed, analyzed,
// and then goes out of scope — avoiding accumulation of all blobs in memory.
func DiscoverSchemaFromChannel(metasChan <-chan *declcfg.Meta) (*CatalogSchema, error) {
	catalogSchema := &CatalogSchema{Schemas: make(map[string]*SchemaInfo)}
	for meta := range metasChan {
		processMetaIntoSchema(catalogSchema, meta)
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
				// Marshal complex values (maps/slices) to JSON strings
				// to match the graphql.String type declared in the schema
				return marshalComplexValue(value), nil
			}
		}
		return nil, nil
	}
}

// createNestedFieldResolver creates a resolver that returns the raw value
// so GraphQL can iterate over array-of-object fields (e.g. properties, entries).
func createNestedFieldResolver(fieldName string) graphql.FieldResolveFn {
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
		var resolver graphql.FieldResolveFn
		// Check if this field has nested structure (array of objects)
		if len(fieldInfo.NestedFields) > 0 {
			// Create a dynamic nested type
			nestedTypeName := sanitizeTypeName(schemaName) + sanitizeTypeName(fieldName)
			nestedType := createNestedObjectType(nestedTypeName, fieldInfo.NestedFields)
			fieldType = graphql.NewList(nestedType)
			resolver = createNestedFieldResolver(fieldName)
		} else {
			// Regular field (not nested)
			fieldType = fieldInfo.GraphQLType
			resolver = createFieldResolver(fieldName)
		}

		fields[fieldName] = &graphql.Field{
			Type:    fieldType,
			Resolve: resolver,
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

// BuildDynamicGraphQLSchema creates a complete GraphQL schema from discovered structure.
// The loader is called at query time to read objects from disk with pagination.
func BuildDynamicGraphQLSchema(catalogSchema *CatalogSchema, loader ObjectLoader) (*DynamicSchema, error) {
	// Detect type name collisions (distinct schemas that sanitize to the same GraphQL type name)
	typeNameToOriginal := make(map[string]string)
	for schemaName := range catalogSchema.Schemas {
		sanitized := sanitizeTypeName(schemaName)
		if existing, ok := typeNameToOriginal[sanitized]; ok && existing != schemaName {
			return nil, fmt.Errorf("type name collision: schemas %q and %q both sanitize to GraphQL type %q", existing, schemaName, sanitized)
		}
		typeNameToOriginal[sanitized] = schemaName
	}

	// Detect root query field name collisions
	fieldNameToOriginal := make(map[string]string)
	for schemaName := range catalogSchema.Schemas {
		sanitized := alphanumericOnlyRE.ReplaceAllString(schemaName, "")
		fieldName := strings.ToLower(sanitized) + "s"
		if existing, ok := fieldNameToOriginal[fieldName]; ok && existing != schemaName {
			return nil, fmt.Errorf("query field collision: schemas %q and %q both map to field %q", existing, schemaName, fieldName)
		}
		fieldNameToOriginal[fieldName] = schemaName
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
		// Examples: "olm.bundle" -> "olmbundles"
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
				currentSchemaName, ok := fieldNameToSchema[p.Info.FieldName]
				if !ok {
					return nil, fmt.Errorf("unknown schema for field %s", p.Info.FieldName)
				}

				limit, _ := p.Args["limit"].(int)
				if limit <= 0 || limit > 100 {
					limit = 100
				}
				offset, _ := p.Args["offset"].(int)
				if offset < 0 {
					offset = 0
				}

				objects, err := loader(currentSchemaName, offset, limit)
				if err != nil {
					return nil, fmt.Errorf("error loading objects for schema %s: %w", currentSchemaName, err)
				}
				results := make([]interface{}, len(objects))
				for i, obj := range objects {
					results[i] = obj
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
	}, nil
}

// NewInMemoryObjectLoader creates an ObjectLoader from pre-parsed metas.
// Used by the demo server and tests where disk-backed loading is not needed.
func NewInMemoryObjectLoader(metasBySchema map[string][]*declcfg.Meta) ObjectLoader {
	parsed := make(map[string][]map[string]interface{})
	for schema, metas := range metasBySchema {
		for _, meta := range metas {
			var obj map[string]interface{}
			if err := json.Unmarshal(meta.Blob, &obj); err != nil {
				continue
			}
			parsed[schema] = append(parsed[schema], obj)
		}
	}
	return func(schemaName string, offset, limit int) ([]map[string]interface{}, error) {
		objects := parsed[schemaName]
		if offset >= len(objects) {
			return nil, nil
		}
		end := offset + limit
		if end > len(objects) {
			end = len(objects)
		}
		return objects[offset:end], nil
	}
}

// LoadAndSummarizeCatalogDynamic loads FBC from a filesystem and builds a dynamic GraphQL schema.
// Uses in-memory object loading — suitable for demos and CLI tools, not production serving.
func LoadAndSummarizeCatalogDynamic(catalogFS fs.FS) (*DynamicSchema, error) {
	var metas []*declcfg.Meta

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

	catalogSchema, err := DiscoverSchemaFromMetas(metas)
	if err != nil {
		return nil, fmt.Errorf("error discovering schema: %w", err)
	}

	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, meta := range metas {
		if meta.Schema != "" {
			metasBySchema[meta.Schema] = append(metasBySchema[meta.Schema], meta)
		}
	}

	loader := NewInMemoryObjectLoader(metasBySchema)
	return BuildDynamicGraphQLSchema(catalogSchema, loader)
}

// PrintCatalogSummary prints a comprehensive summary of the discovered schema
func PrintCatalogSummary(dynamicSchema *DynamicSchema) {
	catalogSchema := dynamicSchema.CatalogSchema

	// Print comprehensive summary
	fmt.Println("Dynamic GraphQL schema generation complete.")
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
		sanitized := alphanumericOnlyRE.ReplaceAllString(schemaName, "")
		fmt.Printf("  - %ss\n", strings.ToLower(sanitized))
	}
	fmt.Printf("  - summary\n")

	fmt.Printf("\nSample GraphQL query:\n")
	fmt.Printf("{\n")
	fmt.Printf("  summary {\n")
	fmt.Printf("    totalSchemas\n")
	fmt.Printf("    schemas { name totalObjects totalFields }\n")
	fmt.Printf("  }\n")
	if _, ok := catalogSchema.Schemas[declcfg.SchemaBundle]; ok {
		fmt.Printf("  olmbundles(limit: 5) { name package }\n")
	}
	if _, ok := catalogSchema.Schemas[declcfg.SchemaPackage]; ok {
		fmt.Printf("  olmpackages(limit: 5) { name }\n")
	}
	fmt.Printf("}\n")
}
