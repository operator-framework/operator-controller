package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	schemaID          = "https://operator-framework.io/schemas/registry-v1-bundle-config.json"
	schemaDraft       = "http://json-schema.org/draft-07/schema#"
	schemaTitle       = "Registry+v1 Bundle Configuration"
	schemaDescription = "Configuration schema for registry+v1 bundles. Includes watchNamespace for controlling operator scope and deploymentConfig for customizing operator deployment (environment variables, resource scheduling, storage, and pod placement). The deploymentConfig follows the same structure and behavior as OLM v0's SubscriptionConfig. Note: The 'selector' field from v0's SubscriptionConfig is not included as it was never used."
)

// OpenAPISpec represents the structure of Kubernetes OpenAPI v3 spec
type OpenAPISpec struct {
	Components struct {
		Schemas map[string]interface{} `json:"schemas"`
	} `json:"components"`
}

// Schema represents a JSON Schema Draft 7 document with OpenAPI v3 components
type Schema struct {
	Schema               string                  `json:"$schema"`
	ID                   string                  `json:"$id"`
	Title                string                  `json:"title"`
	Description          string                  `json:"description"`
	Type                 string                  `json:"type"`
	Properties           map[string]*SchemaField `json:"properties"`
	AdditionalProperties bool                    `json:"additionalProperties"`
	Components           map[string]interface{}  `json:"components,omitempty"`
}

// SchemaField represents a single field in a JSON Schema
type SchemaField struct {
	Type                 string                  `json:"type,omitempty"`
	Description          string                  `json:"description,omitempty"`
	Properties           map[string]*SchemaField `json:"properties,omitempty"`
	AdditionalProperties interface{}             `json:"additionalProperties,omitempty"`
	Items                interface{}             `json:"items,omitempty"`
	AnyOf                []*SchemaField          `json:"anyOf,omitempty"`
	AllOf                []*SchemaField          `json:"allOf,omitempty"`
	Ref                  string                  `json:"$ref,omitempty"`

	// Allow pass-through of unknown fields from OpenAPI schemas
	Extra map[string]interface{} `json:"-"`
}

// FieldInfo contains parsed information about a struct field
type FieldInfo struct {
	JSONName string
	TypeName string
	TypePkg  string
	IsSlice  bool
	IsPtr    bool
	IsMap    bool
}

// schemaCollector tracks schemas that need to be included for $ref resolution
type schemaCollector struct {
	openAPISpec      *OpenAPISpec
	collectedSchemas map[string]bool
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <k8s-openapi-spec-url> <subscription-types-file> <output-file>\n", os.Args[0])
		os.Exit(1)
	}

	k8sOpenAPISpecURL := os.Args[1]
	subscriptionTypesFile := os.Args[2]
	outputFile := os.Args[3]

	fmt.Printf("Fetching Kubernetes OpenAPI spec from %s...\n", k8sOpenAPISpecURL)

	// Fetch the Kubernetes OpenAPI spec
	openAPISpec, err := fetchOpenAPISpec(k8sOpenAPISpecURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Parsing SubscriptionConfig from %s...\n", subscriptionTypesFile)

	// Parse SubscriptionConfig structure
	fields, err := parseSubscriptionConfig(subscriptionTypesFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing SubscriptionConfig: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generating registry+v1 bundle configuration schema...\n")

	// Generate the schema
	schema := generateBundleConfigSchema(openAPISpec, fields)

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling schema: %v\n", err)
		os.Exit(1)
	}

	// Ensure output directory exists
	dir := filepath.Dir(outputFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Write to file
	if err := os.WriteFile(outputFile, data, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing schema file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated schema at %s\n", outputFile)
}

func fetchOpenAPISpec(url string) (*OpenAPISpec, error) {
	// Create HTTP client with timeout to prevent hanging
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}

	return &spec, nil
}

func parseSubscriptionConfig(filePath string) ([]FieldInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var fields []FieldInfo

	// Find the SubscriptionConfig struct
	ast.Inspect(node, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != "SubscriptionConfig" {
			return true
		}

		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		// Extract field information
		for _, field := range structType.Fields.List {
			if field.Names == nil {
				continue
			}

			fieldName := field.Names[0].Name

			// Skip Selector field
			if fieldName == "Selector" {
				continue
			}

			// Get JSON tag
			jsonName := extractJSONTag(field.Tag)
			if jsonName == "" || jsonName == "-" {
				continue
			}

			// Parse the field type
			fieldInfo := FieldInfo{
				JSONName: jsonName,
			}

			parseFieldType(field.Type, &fieldInfo)

			fields = append(fields, fieldInfo)
		}

		return false
	})

	return fields, nil
}

func extractJSONTag(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}

	tagValue := strings.Trim(tag.Value, "`")
	for _, part := range strings.Split(tagValue, " ") {
		if strings.HasPrefix(part, "json:") {
			jsonTag := strings.Trim(strings.TrimPrefix(part, "json:"), "\"")
			return strings.Split(jsonTag, ",")[0]
		}
	}

	return ""
}

func parseFieldType(expr ast.Expr, info *FieldInfo) {
	switch t := expr.(type) {
	case *ast.ArrayType:
		info.IsSlice = true
		parseFieldType(t.Elt, info)

	case *ast.StarExpr:
		info.IsPtr = true
		parseFieldType(t.X, info)

	case *ast.MapType:
		info.IsMap = true
		info.TypeName = "map[string]string" // Simplified for our use case

	case *ast.Ident:
		info.TypeName = t.Name

	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			info.TypePkg = pkg.Name
			info.TypeName = t.Sel.Name
		}
	}
}

func generateBundleConfigSchema(openAPISpec *OpenAPISpec, fields []FieldInfo) *Schema {
	schema := &Schema{
		Schema:               schemaDraft,
		ID:                   schemaID,
		Title:                schemaTitle,
		Description:          schemaDescription,
		Type:                 "object",
		Properties:           make(map[string]*SchemaField),
		AdditionalProperties: false,
	}

	// Track schemas we need to include (for resolving $ref dependencies)
	collector := &schemaCollector{
		openAPISpec:      openAPISpec,
		collectedSchemas: make(map[string]bool),
	}

	// Add watchNamespace property (base definition - will be modified at runtime)
	schema.Properties["watchNamespace"] = &SchemaField{
		Description: "The namespace that the operator should watch for custom resources. The meaning and validation of this field depends on the operator's install modes. This field may be optional or required, and may have format constraints, based on the operator's supported install modes.",
		AnyOf: []*SchemaField{
			{Type: "null"},
			{Type: "string"},
		},
	}

	// Create deploymentConfig property
	deploymentConfigProps := make(map[string]*SchemaField)

	// Build deploymentConfig properties from parsed fields
	for _, field := range fields {
		fieldSchema := mapFieldToOpenAPISchema(field, openAPISpec, collector)
		if fieldSchema != nil {
			deploymentConfigProps[field.JSONName] = fieldSchema
		}
	}

	schema.Properties["deploymentConfig"] = &SchemaField{
		Type:                 "object",
		Description:          "Configuration for customizing operator deployment (environment variables, resources, volumes, etc.)",
		Properties:           deploymentConfigProps,
		AdditionalProperties: false,
	}

	// Add all collected schemas to the components/schemas section
	// (OpenAPI v3 uses components/schemas for $ref resolution)
	if len(collector.collectedSchemas) > 0 {
		componentsSchemas := make(map[string]interface{})
		for schemaName := range collector.collectedSchemas {
			if s, ok := openAPISpec.Components.Schemas[schemaName]; ok {
				componentsSchemas[schemaName] = s
			}
		}

		schema.Components = map[string]interface{}{
			"schemas": componentsSchemas,
		}
	}

	return schema
}

func mapFieldToOpenAPISchema(field FieldInfo, openAPISpec *OpenAPISpec, collector *schemaCollector) *SchemaField {
	// Handle map types (nodeSelector, annotations)
	if field.IsMap {
		return &SchemaField{
			Type: "object",
			AdditionalProperties: &SchemaField{
				Type: "string",
			},
		}
	}

	// Get the OpenAPI schema for the base type
	openAPITypeName := getOpenAPITypeName(field)
	if openAPITypeName == "" {
		fmt.Fprintf(os.Stderr, "Warning: Could not map field %s (type: %s.%s) to OpenAPI schema\n",
			field.JSONName, field.TypePkg, field.TypeName)
		return nil
	}

	baseSchema, ok := openAPISpec.Components.Schemas[openAPITypeName]
	if !ok {
		fmt.Fprintf(os.Stderr, "Warning: Schema for %s not found in OpenAPI spec\n", openAPITypeName)
		return nil
	}

	// Collect this schema and all its dependencies
	collector.collectSchemaWithDependencies(openAPITypeName, baseSchema)

	// Use $ref to point to the schema in components/schemas.
	// This preserves all validation keywords (required, enum, format, pattern, etc.)
	// that would be lost if we copied the schema content via marshal/unmarshal.
	schemaRef := &SchemaField{
		Ref: fmt.Sprintf("#/components/schemas/%s", openAPITypeName),
	}

	// Wrap in array if it's a slice field
	if field.IsSlice {
		return &SchemaField{
			Type:  "array",
			Items: schemaRef,
		}
	}

	return schemaRef
}

// collectSchemaWithDependencies recursively collects a schema and all schemas it references via $ref
func (c *schemaCollector) collectSchemaWithDependencies(schemaName string, schema interface{}) {
	// Mark this schema as collected
	if c.collectedSchemas[schemaName] {
		return // Already processed
	}
	c.collectedSchemas[schemaName] = true

	// Recursively find all $ref references in this schema
	c.findReferences(schema)
}

// findReferences recursively walks a schema object to find all $ref pointers
func (c *schemaCollector) findReferences(obj interface{}) {
	switch v := obj.(type) {
	case map[string]interface{}:
		// Check if this is a $ref and process it
		c.processRef(v)

		// Recursively check all values in the map
		for _, val := range v {
			c.findReferences(val)
		}

	case []interface{}:
		// Recursively check all items in the array
		for _, item := range v {
			c.findReferences(item)
		}
	}
}

// processRef extracts and collects schema dependencies from a $ref pointer
func (c *schemaCollector) processRef(v map[string]interface{}) {
	ref, ok := v["$ref"].(string)
	if !ok {
		return
	}

	// Extract the schema name from the $ref
	// Format: "#/components/schemas/io.k8s.api.core.v1.NodeAffinity"
	if !strings.HasPrefix(ref, "#/components/schemas/") {
		return
	}

	schemaName := strings.TrimPrefix(ref, "#/components/schemas/")

	// Skip if already collected
	if c.collectedSchemas[schemaName] {
		return
	}

	// Collect the referenced schema recursively
	refSchema, ok := c.openAPISpec.Components.Schemas[schemaName]
	if ok {
		c.collectSchemaWithDependencies(schemaName, refSchema)
	}
}

func getOpenAPITypeName(field FieldInfo) string {
	// Map package names to OpenAPI prefixes
	pkgMap := map[string]string{
		"corev1": "io.k8s.api.core.v1",
		"v1":     "io.k8s.api.core.v1",
	}

	prefix, ok := pkgMap[field.TypePkg]
	if !ok {
		return ""
	}

	return fmt.Sprintf("%s.%s", prefix, field.TypeName)
}
