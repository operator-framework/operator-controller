package v1alpha1

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"
)

func TestPollIntervalCELValidationRules(t *testing.T) {
	validators := fieldValidatorsFromFile(t, "../../../config/base/crd/bases/catalogd.operatorframework.io_clustercatalogs.yaml")
	pth := "openAPIV3Schema.properties.spec"
	validator, found := validators["v1alpha1"][pth]
	assert.True(t, found)

	for name, tc := range map[string]struct {
		spec     ClusterCatalogSpec
		wantErrs []string
	}{
		"digest based image ref, poll interval not allowed, poll interval specified": {
			spec: ClusterCatalogSpec{
				Source: CatalogSource{
					Type: SourceTypeImage,
					Image: &ImageSource{
						Ref:          "docker.io/test-image@sha256:asdf98234sd",
						PollInterval: &metav1.Duration{Duration: time.Minute},
					},
				},
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec: Invalid value: \"object\": cannot specify PollInterval while using digest-based image",
			},
		},
		"digest based image ref, poll interval not allowed, poll interval not specified": {
			spec: ClusterCatalogSpec{
				Source: CatalogSource{
					Type: SourceTypeImage,
					Image: &ImageSource{
						Ref: "docker.io/example/test-catalog@sha256:asdf123",
					},
				},
			},
			wantErrs: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tc.spec) //nolint:gosec
			require.NoError(t, err)
			errs := validator(obj, nil)
			require.Equal(t, len(tc.wantErrs), len(errs))
			for i := range tc.wantErrs {
				got := errs[i].Error()
				assert.Equal(t, tc.wantErrs[i], got)
			}
		})
	}
}

// fieldValidatorsFromFile extracts the CEL validators by version and JSONPath from a CRD file and returns
// a validator func for testing against samples.
func fieldValidatorsFromFile(t *testing.T, crdFilePath string) map[string]map[string]CELValidateFunc {
	data, err := os.ReadFile(crdFilePath)
	require.NoError(t, err)

	var crd apiextensionsv1.CustomResourceDefinition
	err = yaml.Unmarshal(data, &crd)
	require.NoError(t, err)

	ret := map[string]map[string]CELValidateFunc{}
	for _, v := range crd.Spec.Versions {
		var internalSchema apiextensions.JSONSchemaProps
		err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v.Schema.OpenAPIV3Schema, &internalSchema, nil)
		require.NoError(t, err, "failed to convert JSONSchemaProps for version %s: %v", v.Name, err)
		structuralSchema, err := schema.NewStructural(&internalSchema)
		require.NoError(t, err, "failed to create StructuralSchema for version %s: %v", v.Name, err)

		versionVals, err := findCEL(structuralSchema, true, field.NewPath("openAPIV3Schema"))
		require.NoError(t, err, "failed to find CEL for version %s: %v", v.Name, err)
		ret[v.Name] = versionVals
	}
	return ret
}

// CELValidateFunc tests a sample object against a CEL validator.
type CELValidateFunc func(obj, old interface{}) field.ErrorList

func findCEL(s *schema.Structural, root bool, pth *field.Path) (map[string]CELValidateFunc, error) {
	ret := map[string]CELValidateFunc{}

	if len(s.XValidations) > 0 {
		s := *s
		pth := *pth
		ret[pth.String()] = func(obj, old interface{}) field.ErrorList {
			errs, _ := cel.NewValidator(&s, root, celconfig.PerCallLimit).Validate(context.TODO(), &pth, &s, obj, old, celconfig.RuntimeCELCostBudget)
			return errs
		}
	}

	for k, v := range s.Properties {
		v := v
		sub, err := findCEL(&v, false, pth.Child("properties").Child(k))
		if err != nil {
			return nil, err
		}

		for pth, val := range sub {
			ret[pth] = val
		}
	}
	if s.Items != nil {
		sub, err := findCEL(s.Items, false, pth.Child("items"))
		if err != nil {
			return nil, err
		}
		for pth, val := range sub {
			ret[pth] = val
		}
	}
	if s.AdditionalProperties != nil && s.AdditionalProperties.Structural != nil {
		sub, err := findCEL(s.AdditionalProperties.Structural, false, pth.Child("additionalProperties"))
		if err != nil {
			return nil, err
		}
		for pth, val := range sub {
			ret[pth] = val
		}
	}

	return ret, nil
}
