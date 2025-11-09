package bundle

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
)

const (
	BundleConfigWatchNamespaceKey = "watchNamespace"
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

// buildBundleConfigSchema creates validation rules based on what the operator supports.
//
// Examples of how install modes affect validation:
//   - AllNamespaces only: user can't set watchNamespace (operator watches everything)
//   - OwnNamespace only: user must set watchNamespace to the install namespace
//   - SingleNamespace only: user must set watchNamespace to a different namespace
//   - AllNamespaces + OwnNamespace: user can optionally set watchNamespace
func buildBundleConfigSchema(installModes sets.Set[v1alpha1.InstallMode]) (map[string]any, error) {
	schema := map[string]any{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"type":                 "object",
		"additionalProperties": false, // Reject unknown fields (catches typos and misconfigurations)
	}

	properties := map[string]any{}
	var required []any

	// Add watchNamespace property if the bundle supports it
	if isWatchNamespaceConfigurable(installModes) {
		watchNSProperty, isRequired := buildWatchNamespaceProperty(installModes)
		properties["watchNamespace"] = watchNSProperty
		if isRequired {
			required = append(required, "watchNamespace")
		}
	}

	schema["properties"] = properties
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema, nil
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
	watchNSProperty := map[string]any{
		"description": "The namespace that the operator should watch for custom resources",
	}

	hasOwnNamespace := installModes.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true})
	hasSingleNamespace := installModes.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true})

	format := selectNamespaceFormat(hasOwnNamespace, hasSingleNamespace)

	if isWatchNamespaceConfigRequired(installModes) {
		watchNSProperty["type"] = "string"
		if format != "" {
			watchNSProperty["format"] = format
		}
		return watchNSProperty, true
	}

	// allow null or valid namespace string
	stringSchema := map[string]any{
		"type": "string",
	}
	if format != "" {
		stringSchema["format"] = format
	}
	// Convert to []any for JSON schema compatibility
	watchNSProperty["anyOf"] = []any{
		map[string]any{"type": "null"},
		stringSchema,
	}

	return watchNSProperty, false
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
