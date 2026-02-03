// Package config validates configuration for different package format types.
//
// How it works:
//
// Each package format type (like registry+v1 or Helm) knows what configuration it accepts.
// When a user provides configuration, we validate it before creating a Config object.
// Once created, a Config is guaranteed to be valid - you never need to check it again.
//
// The validation uses JSON Schema:
//  1. Bundle provides its schema (what config is valid)
//  2. We validate the user's config against that schema
//  3. If valid, we create a Config object
//  4. If invalid, we return a helpful error message
//
// Design choices:
//
//   - Validation happens once, when creating the Config. There's no Validate() method
//     because once you have a Config, it's already been validated.
//
//   - Config doesn't hold onto the schema. We only need the schema during validation,
//     not after the Config is created.
//
//   - You can't create a Config directly. You must go through UnmarshalConfig so that
//     validation always happens.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

const (
	// configSchemaID is a name we use to identify the config schema when compiling it.
	// Think of it like a file name - it just needs to be consistent.
	configSchemaID = "config-schema.json"

	// FormatOwnNamespaceInstallMode defines the format check to ensure that
	// the watchNamespace must equal install namespace
	FormatOwnNamespaceInstallMode = "ownNamespaceInstallMode"
	// FormatSingleNamespaceInstallMode defines the format check to ensure that
	// the watchNamespace must differ from install namespace
	FormatSingleNamespaceInstallMode = "singleNamespaceInstallMode"
)

// DeploymentConfig is a type alias for v1alpha1.SubscriptionConfig
// to maintain clear naming in the OLMv1 context while reusing the v0 type.
type DeploymentConfig = v1alpha1.SubscriptionConfig

// SchemaProvider lets each package format type describe what configuration it accepts.
//
// Different package format types provide schemas in different ways:
//   - registry+v1: builds schema from the operator's install modes
//   - Helm: reads schema from values.schema.json in the chart
//   - registry+v2: (future) will have its own way
type SchemaProvider interface {
	// GetConfigSchema returns a JSON Schema describing what configuration is valid.
	// Returns nil if this package format type doesn't need configuration validation.
	GetConfigSchema() (map[string]any, error)
}

// Config holds validated configuration data from a ClusterExtension.
//
// Different package format types have different configuration options, so we store
// the data in a flexible format and provide accessor methods to get values out.
//
// Why there's no Validate() method:
// We validate configuration when creating a Config. If you have a Config object,
// it's already been validated - you don't need to check it again. You can't create
// a Config directly; you have to use UnmarshalConfig, which does the validation.
type Config map[string]any

// newConfig creates a Config from already-validated data.
// This is unexported so all Configs must be created through UnmarshalConfig,
// which ensures validation always happens.
func newConfig(data map[string]any) *Config {
	cfg := Config(data)
	return &cfg
}

// GetWatchNamespace returns the watchNamespace value if present in the configuration.
// Returns nil if watchNamespace is not set or is explicitly set to null.
func (c *Config) GetWatchNamespace() *string {
	if c == nil || *c == nil {
		return nil
	}
	val, exists := (*c)["watchNamespace"]
	if !exists {
		return nil
	}
	// User set watchNamespace: null - treat as "not configured"
	if val == nil {
		return nil
	}
	// Convert value to string. Schema validation ensures this is a string,
	// but fmt.Sprintf handles edge cases defensively.
	str := fmt.Sprintf("%v", val)
	return &str
}

// GetDeploymentConfig returns the deploymentConfig value if present in the configuration.
// Returns nil if deploymentConfig is not set or is explicitly set to null.
// The returned value is a generic map[string]any that can be marshaled to JSON
// for validation or conversion to specific types (like v1alpha1.SubscriptionConfig).
//
// Returns a defensive deep copy so callers can't mutate the internal Config state.
func (c *Config) GetDeploymentConfig() map[string]any {
	if c == nil || *c == nil {
		return nil
	}
	val, exists := (*c)["deploymentConfig"]
	if !exists {
		return nil
	}
	// User set deploymentConfig: null - treat as "not configured"
	if val == nil {
		return nil
	}
	// Schema validation ensures this is an object (map)
	dcMap, ok := val.(map[string]any)
	if !ok {
		return nil
	}

	// Return a defensive deep copy so callers can't mutate the internal Config state.
	// We use JSON marshal/unmarshal because the data is already JSON-compatible and
	// this handles nested structures correctly.
	data, err := json.Marshal(dcMap)
	if err != nil {
		// This should never happen since the map came from validated JSON/YAML,
		// but return nil as a safe fallback
		return nil
	}
	var copied map[string]any
	if err := json.Unmarshal(data, &copied); err != nil {
		// This should never happen for valid JSON
		return nil
	}
	return copied
}

// UnmarshalConfig takes user configuration, validates it, and creates a Config object.
// This is the only way to create a Config.
//
// What it does:
//  1. Checks the user's configuration against the schema (if provided)
//  2. If valid, creates a Config object
//  3. If invalid, returns an error explaining what's wrong
//
// Parameters:
//   - bytes: the user's configuration in YAML or JSON. If nil, we treat it as empty ({})
//   - schema: describes what configuration is valid. If nil, we skip validation
//   - installNamespace: the namespace where the operator will be installed. We use this
//     to validate namespace constraints (e.g., OwnNamespace mode requires watchNamespace
//     to equal installNamespace)
//
// If the user doesn't provide any configuration but the package format type requires some fields
// (like watchNamespace), validation will fail with a helpful error.
func UnmarshalConfig(bytes []byte, schema map[string]any, installNamespace string) (*Config, error) {
	// nil config becomes {} so we can validate required fields
	if bytes == nil {
		bytes = []byte("{}")
	}

	// Step 1: Validate against the schema if provided
	if schema != nil {
		if err := validateConfigWithSchema(bytes, schema, installNamespace); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	// Step 2: Parse into Config struct
	// We use yaml.Unmarshal to parse the validated config into an opaque map.
	// Schema validation has already ensured correctness.
	var configData map[string]any
	if err := yaml.Unmarshal(bytes, &configData); err != nil {
		return nil, fmt.Errorf("error unmarshalling configuration: %w", formatUnmarshalError(err))
	}

	return newConfig(configData), nil
}

// validateConfigWithSchema checks if the user's config matches the schema.
//
// We create a fresh validator each time because the namespace constraints depend on
// which namespace this specific ClusterExtension is being installed into. Each
// ClusterExtension might have a different installNamespace, so we can't reuse validators.
func validateConfigWithSchema(configBytes []byte, schema map[string]any, installNamespace string) error {
	var configData interface{}
	if err := yaml.Unmarshal(configBytes, &configData); err != nil {
		return formatUnmarshalError(err)
	}

	compiler := jsonschema.NewCompiler()

	compiler.RegisterFormat(&jsonschema.Format{
		Name: FormatOwnNamespaceInstallMode,
		Validate: func(value interface{}) error {
			// Check it equals install namespace (if installNamespace is set)
			// If installNamespace is empty, we can't validate the constraint properly,
			// so we skip validation and accept any value. This is a fallback for edge
			// cases where the install namespace isn't known yet.
			if installNamespace == "" {
				return nil
			}
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("value must be a string")
			}
			if str != installNamespace {
				return fmt.Errorf("invalid value %q: must be %q (the namespace where the operator is installed) because this operator only supports OwnNamespace install mode", str, installNamespace)
			}
			return nil
		},
	})
	compiler.RegisterFormat(&jsonschema.Format{
		Name: FormatSingleNamespaceInstallMode,
		Validate: func(value interface{}) error {
			// Check it does NOT equal install namespace (if installNamespace is set)
			// If installNamespace is empty, we can't validate the constraint properly,
			// so we skip validation and accept any value. This is a fallback for edge
			// cases where the install namespace isn't known yet.
			if installNamespace == "" {
				return nil
			}
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("value must be a string")
			}
			if str == installNamespace {
				return fmt.Errorf("invalid value %q: must be different from %q (the install namespace) because this operator uses SingleNamespace install mode to watch a different namespace", str, installNamespace)
			}
			return nil
		},
	})

	if err := compiler.AddResource(configSchemaID, schema); err != nil {
		return fmt.Errorf("failed to load schema: %w", err)
	}

	compiledSchema, err := compiler.Compile(configSchemaID)
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	if err := compiledSchema.Validate(configData); err != nil {
		return formatSchemaError(err)
	}

	return nil
}

// formatSchemaError converts JSON schema validation errors into user-friendly messages.
// If multiple validation errors exist, it combines them into a single error message.
func formatSchemaError(err error) error {
	ve := &jsonschema.ValidationError{}
	ok := errors.As(err, &ve)
	if !ok {
		// Not a ValidationError, return as-is
		// Caller (UnmarshalConfig) will add "invalid configuration:" prefix
		return err
	}

	// Use BasicOutput() to get structured error information
	// This is more robust than parsing error strings
	output := ve.BasicOutput()
	if output == nil || len(output.Errors) == 0 {
		// No structured errors available, fallback to error message
		// Note: Using errors.New since ve.Error() is already a formatted string
		return errors.New(ve.Error())
	}

	// Collect all error messages
	var errorMessages []string
	for _, errUnit := range output.Errors {
		msg := formatSingleError(errUnit)
		if msg != "" {
			errorMessages = append(errorMessages, msg)
		}
	}

	if len(errorMessages) == 0 {
		return fmt.Errorf("invalid configuration: %w", ve)
	}

	// Single error - return it directly
	if len(errorMessages) == 1 {
		return errors.New(errorMessages[0])
	}

	// Multiple errors - combine them
	return fmt.Errorf("multiple errors found:\n  - %s", strings.Join(errorMessages, "\n  - "))
}

// formatSingleError formats a single validation error from the schema library.
func formatSingleError(errUnit jsonschema.OutputUnit) string {
	if errUnit.Error == nil {
		return ""
	}

	// Check the keyword location to identify the error type
	switch errKind := errUnit.Error.Kind.(type) {
	case *kind.Required:
		// Missing required field
		fieldName := extractFieldNameFromMessage(errUnit.Error)
		if fieldName != "" {
			return fmt.Sprintf("required field %q is missing", fieldName)
		}
		return "required field is missing"

	case *kind.AdditionalProperties:
		// Unknown/additional field
		fieldName := extractFieldNameFromMessage(errUnit.Error)
		if fieldName != "" {
			return fmt.Sprintf("unknown field %q", fieldName)
		}
		return "unknown field"

	case *kind.Type:
		// Type mismatch (e.g., got null, want string)
		fieldPath := buildFieldPath(errUnit.InstanceLocation)
		if fieldPath != "" {
			// Check if this is a "null instead of required value" case
			if errUnit.Error != nil && strings.Contains(errUnit.Error.String(), "got null") {
				return fmt.Sprintf("required field %q is missing", fieldPath)
			}
			return fmt.Sprintf("invalid type for field %q: %s", fieldPath, errUnit.Error.String())
		}
		return fmt.Sprintf("invalid type: %s", errUnit.Error.String())

	case *kind.Format:
		fieldPath := buildFieldPath(errUnit.InstanceLocation)
		if fieldPath != "" {
			return fmt.Sprintf("invalid format for field %q: %s", fieldPath, errUnit.Error.String())
		}
		return fmt.Sprintf("invalid format: %s", errUnit.Error.String())

	case *kind.AnyOf:
		// anyOf validation failed - could be null or wrong type
		// This happens when a field accepts [null, string] but got something else
		fieldPath := buildFieldPath(errUnit.InstanceLocation)
		if fieldPath != "" {
			return fmt.Sprintf("invalid value for field %q", fieldPath)
		}
		return "invalid value"

	case *kind.MaxLength:
		fieldPath := buildFieldPath(errUnit.InstanceLocation)
		if fieldPath != "" {
			return fmt.Sprintf("field %q must have maximum length of %d (len=%d)", fieldPath, errKind.Want, errKind.Got)
		}
		return errUnit.Error.String()

	case *kind.MinLength:
		fieldPath := buildFieldPath(errUnit.InstanceLocation)
		if fieldPath != "" {
			return fmt.Sprintf("field %q must have minimum length of %d (len=%d)", fieldPath, errKind.Want, errKind.Got)
		}
		return errUnit.Error.String()

	case *kind.Pattern:
		fieldPath := buildFieldPath(errUnit.InstanceLocation)
		if fieldPath != "" {
			return fmt.Sprintf("field %q must match pattern %q", fieldPath, errKind.Want)
		}
		return errUnit.Error.String()

	default:
		// Unhandled error type - return the library's error message
		// This serves as a fallback for future schema features we haven't customized yet
		return errUnit.Error.String()
	}
}

// extractFieldNameFromMessage extracts the field name from error messages.
// Example: "missing property 'watchNamespace'" -> "watchNamespace"
// Example: "additional properties 'unknownField' not allowed" -> "unknownField"
func extractFieldNameFromMessage(errOutput *jsonschema.OutputError) string {
	if errOutput == nil {
		return ""
	}
	msg := errOutput.String()

	// Look for field names in single quotes (library's format)
	if idx := strings.Index(msg, "'"); idx != -1 {
		remaining := msg[idx+1:]
		if endIdx := strings.Index(remaining, "'"); endIdx != -1 {
			return remaining[:endIdx]
		}
	}

	return ""
}

// buildFieldPath constructs a field path from instance location array.
// Example: ["watchNamespace"] -> "watchNamespace"
// Example: ["spec", "namespace"] -> "spec.namespace"
func buildFieldPath(location string) string {
	// Instance location comes as a JSON pointer like "/watchNamespace"
	if location == "" || location == "/" {
		return ""
	}
	// Remove leading slash
	path := strings.TrimPrefix(location, "/")
	// Replace JSON pointer slashes with dots for readability
	path = strings.ReplaceAll(path, "/", ".")
	return path
}

// formatUnmarshalError makes YAML/JSON parsing errors easier to understand.
func formatUnmarshalError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field == "" {
			return errors.New("input is not a valid JSON object")
		}
		return fmt.Errorf("invalid value type for field %q: expected %q but got %q",
			typeErr.Field, typeErr.Type.String(), typeErr.Value)
	}

	// Unwrap to core error and strip "json:" or "yaml:" prefix
	current := err
	for {
		unwrapped := errors.Unwrap(current)
		if unwrapped == nil {
			parts := strings.Split(current.Error(), ":")
			coreMessage := strings.TrimSpace(parts[len(parts)-1])
			return errors.New(coreMessage)
		}
		current = unwrapped
	}
}
