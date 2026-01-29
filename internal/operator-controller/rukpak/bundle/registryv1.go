package bundle

import (
	_ "embed"
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
)

const (
	watchNamespaceConfigKey = "watchNamespace"
	namespaceNamePattern    = "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	namespaceNameMaxLength  = 63
)

var (
	//go:embed registryv1bundleconfig.json
	bundleConfigSchemaJSON []byte
)

type RegistryV1 struct {
	PackageName string
	CSV         v1alpha1.ClusterServiceVersion
	CRDs        []apiextensionsv1.CustomResourceDefinition
	Others      []unstructured.Unstructured
}

// GetConfigSchema builds a validation schema based on what install modes the operator supports.
//
// For registry+v1 bundles, we look at the CSV's install modes and generate a schema
// that matches. For example, if the operator only supports OwnNamespace mode, we'll
// require the user to provide a watchNamespace that equals the install namespace.
func (rv1 *RegistryV1) GetConfigSchema() (map[string]any, error) {
	installModes := sets.New(rv1.CSV.Spec.InstallModes...)
	return buildBundleConfigSchema(installModes)
}

// buildBundleConfigSchema loads the base bundle config schema and modifies it based on
// the operator's install modes.
//
// The base schema includes
// 1. watchNamespace
// 2. deploymentConfig properties.
// The watchNamespace property is modified based on what the operator supports:
//   - AllNamespaces only: remove watchNamespace (operator always watches everything)
//   - OwnNamespace only: make watchNamespace required, must equal install namespace
//   - SingleNamespace only: make watchNamespace required, must differ from install namespace
//   - AllNamespaces + OwnNamespace: make watchNamespace optional
func buildBundleConfigSchema(installModes sets.Set[v1alpha1.InstallMode]) (map[string]any, error) {
	// Load the base schema
	baseSchema, err := getBundleConfigSchemaMap()
	if err != nil {
		return nil, fmt.Errorf("failed to get base bundle config schema: %w", err)
	}

	// Get properties map from the schema
	properties, ok := baseSchema["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("base schema missing properties")
	}

	// Modify watchNamespace field based on install modes
	if isWatchNamespaceConfigurable(installModes) {
		// Replace the generic watchNamespace with install-mode-specific version
		watchNSProperty, isRequired := buildWatchNamespaceProperty(installModes)
		properties[watchNamespaceConfigKey] = watchNSProperty

		// Preserve existing required fields, only add/remove watchNamespace
		if isRequired {
			addToRequired(baseSchema, watchNamespaceConfigKey)
		} else {
			removeFromRequired(baseSchema, watchNamespaceConfigKey)
		}
	} else {
		// AllNamespaces only - remove watchNamespace property entirely
		// (operator always watches all namespaces, no configuration needed)
		delete(properties, watchNamespaceConfigKey)
		removeFromRequired(baseSchema, watchNamespaceConfigKey)
	}

	return baseSchema, nil
}

// addToRequired adds fieldName to the schema's required array if it's not already present.
// Preserves any existing required fields.
func addToRequired(schema map[string]any, fieldName string) {
	var required []any
	if existingRequired, ok := schema["required"].([]any); ok {
		// Check if field is already required
		for _, field := range existingRequired {
			if field == fieldName {
				return // Already required
			}
		}
		required = existingRequired
	}
	// Add the field to required list
	schema["required"] = append(required, fieldName)
}

// removeFromRequired removes fieldName from the schema's required array if present.
// Preserves all other required fields.
func removeFromRequired(schema map[string]any, fieldName string) {
	existingRequired, ok := schema["required"].([]any)
	if !ok {
		return // No required array
	}

	// Filter out the field
	filtered := make([]any, 0, len(existingRequired))
	for _, field := range existingRequired {
		if field != fieldName {
			filtered = append(filtered, field)
		}
	}

	// Update or delete the required array
	if len(filtered) > 0 {
		schema["required"] = filtered
	} else {
		delete(schema, "required")
	}
}

// buildWatchNamespaceProperty creates the validation rules for the watchNamespace field.
//
// The rules depend on what install modes are supported:
//   - AllNamespaces supported: watchNamespace is optional (can be null)
//   - Only Single/Own supported: watchNamespace is required
//   - Only OwnNamespace: must equal install namespace
//   - Only SingleNamespace: must be different from install namespace
//
// Returns the validation rules and whether the field is required.
func buildWatchNamespaceProperty(installModes sets.Set[v1alpha1.InstallMode]) (map[string]any, bool) {
	const description = "The namespace that the operator should watch for custom resources"

	hasOwnNamespace := installModes.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true})
	hasSingleNamespace := installModes.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true})

	format := selectNamespaceFormat(hasOwnNamespace, hasSingleNamespace)

	watchNamespaceSchema := map[string]any{
		"type":      "string",
		"minLength": 1,
		"maxLength": namespaceNameMaxLength,
		// kubernetes namespace name format
		"pattern": namespaceNamePattern,
	}
	if format != "" {
		watchNamespaceSchema["format"] = format
	}

	if isWatchNamespaceConfigRequired(installModes) {
		watchNamespaceSchema["description"] = description
		return watchNamespaceSchema, true
	}

	// allow null or valid namespace string
	return map[string]any{
		"description": description,
		"anyOf": []any{
			map[string]any{"type": "null"},
			watchNamespaceSchema,
		},
	}, false
}

// selectNamespaceFormat picks which namespace constraint to apply.
//
//   - OwnNamespace only: watchNamespace must equal install namespace
//   - SingleNamespace only: watchNamespace must be different from install namespace
//   - Both or neither: no constraint, any namespace name is valid
func selectNamespaceFormat(hasOwnNamespace, hasSingleNamespace bool) string {
	if hasOwnNamespace && !hasSingleNamespace {
		return config.FormatOwnNamespaceInstallMode
	}
	if hasSingleNamespace && !hasOwnNamespace {
		return config.FormatSingleNamespaceInstallMode
	}
	return "" // No format constraint needed for generic case
}

// isWatchNamespaceConfigurable checks if the user can set a watchNamespace.
//
// Returns true if:
//   - SingleNamespace is supported (user picks a namespace to watch)
//   - OwnNamespace is supported (user sets watchNamespace to the install namespace)
//
// Returns false if:
//   - Only AllNamespaces is supported (operator always watches everything)
func isWatchNamespaceConfigurable(installModes sets.Set[v1alpha1.InstallMode]) bool {
	return installModes.HasAny(
		v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
		v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
	)
}

// isWatchNamespaceConfigRequired checks if the user must provide a watchNamespace.
//
// Returns true (required) when:
//   - Only OwnNamespace is supported
//   - Only SingleNamespace is supported
//   - Both OwnNamespace and SingleNamespace are supported
//
// Returns false (optional) when:
//   - AllNamespaces is supported (user can leave it unset to watch all namespaces)
func isWatchNamespaceConfigRequired(installModes sets.Set[v1alpha1.InstallMode]) bool {
	return isWatchNamespaceConfigurable(installModes) &&
		!installModes.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true})
}

// getBundleConfigSchemaMap returns the complete registry+v1 bundle configuration schema
// as a map[string]any. This includes the following properties:
// 1. watchNamespace
// 2. deploymentConfig
// The schema can be modified at runtime based on operator install modes before validation.
func getBundleConfigSchemaMap() (map[string]any, error) {
	var schemaMap map[string]any
	if err := json.Unmarshal(bundleConfigSchemaJSON, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bundle config schema: %w", err)
	}
	return schemaMap, nil
}
