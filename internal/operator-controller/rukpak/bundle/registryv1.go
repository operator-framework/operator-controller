package bundle

import (
	_ "embed"
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
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

// GetConfigSchema returns the validation schema for registry+v1 bundle configuration.
func (rv1 *RegistryV1) GetConfigSchema() (map[string]any, error) {
	var schemaMap map[string]any
	if err := json.Unmarshal(bundleConfigSchemaJSON, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bundle config schema: %w", err)
	}
	return schemaMap, nil
}
