package v1

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

const crdFilePath = "../../config/base/crd/bases/olm.operatorframework.io_clustercatalogs.yaml"

func TestImageSourceCELValidationRules(t *testing.T) {
	validators := fieldValidatorsFromFile(t, crdFilePath)
	pth := "openAPIV3Schema.properties.spec.properties.source.properties.image"
	validator, found := validators[GroupVersion.Version][pth]
	require.True(t, found)

	for name, tc := range map[string]struct {
		spec     ImageSource
		wantErrs []string
	}{
		"valid digest based image ref, poll interval not allowed, poll interval specified": {
			spec: ImageSource{
				Ref:                 "docker.io/test-image@sha256:abcdef123456789abcdef123456789abc",
				PollIntervalMinutes: ptr.To(1),
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image: Invalid value: \"object\": cannot specify pollIntervalMinutes while using digest-based image",
			},
		},
		"valid digest based image ref, poll interval not allowed, poll interval not specified": {
			spec: ImageSource{
				Ref: "docker.io/test-image@sha256:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{},
		},
		"invalid digest based image ref, invalid domain": {
			spec: ImageSource{
				Ref: "-quay+docker/foo/bar@sha256:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": must start with a valid domain. valid domains must be alphanumeric characters (lowercase and uppercase) separated by the \".\" character.",
			},
		},
		"invalid digest based image ref, invalid name": {
			spec: ImageSource{
				Ref: "docker.io/FOO/BAR@sha256:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": a valid name is required. valid names must contain lowercase alphanumeric characters separated only by the \".\", \"_\", \"__\", \"-\" characters.",
			},
		},
		"invalid digest based image ref, invalid digest algorithm": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar@99-problems:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": digest algorithm is not valid. valid algorithms must start with an uppercase or lowercase alpha character followed by alphanumeric characters and may contain the \"-\", \"_\", \"+\", and \".\" characters.",
			},
		},
		"invalid digest based image ref, too short digest encoding": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar@sha256:abcdef123456789",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": digest is not valid. the encoded string must be at least 32 characters",
			},
		},
		"invalid digest based image ref, invalid characters in digest encoding": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar@sha256:XYZxy123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": digest is not valid. the encoded string must only contain hex characters (A-F, a-f, 0-9)",
			},
		},
		"invalid image ref, no tag or digest": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": must end with a digest or a tag",
			},
		},
		"invalid tag based image ref, tag too long": {
			spec: ImageSource{
				Ref: fmt.Sprintf("docker.io/foo/bar:%s", strings.Repeat("x", 128)),
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": tag is invalid. the tag must not be more than 127 characters",
			},
		},
		"invalid tag based image ref, tag contains invalid characters": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar:-foo_bar-",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": tag is invalid. valid tags must begin with a word character (alphanumeric + \"_\") followed by word characters or \".\", and \"-\" characters",
			},
		},
		"valid tag based image ref": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar:v1.0.0",
			},
			wantErrs: []string{},
		},
		"valid tag based image ref, pollIntervalMinutes specified": {
			spec: ImageSource{
				Ref:                 "docker.io/foo/bar:v1.0.0",
				PollIntervalMinutes: ptr.To(5),
			},
			wantErrs: []string{},
		},
		"invalid image ref, only domain with port": {
			spec: ImageSource{
				Ref: "docker.io:8080",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.spec.properties.source.properties.image.ref: Invalid value: \"string\": a valid name is required. valid names must contain lowercase alphanumeric characters separated only by the \".\", \"_\", \"__\", \"-\" characters.",
			},
		},
		"valid image ref, domain with port": {
			spec: ImageSource{
				Ref: "my-subdomain.docker.io:8080/foo/bar:latest",
			},
			wantErrs: []string{},
		},
		"valid image ref, tag ends with hyphen": {
			spec: ImageSource{
				Ref: "my-subdomain.docker.io:8080/foo/bar:latest-",
			},
			wantErrs: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tc.spec) //nolint:gosec
			require.NoError(t, err)
			errs := validator(obj, nil)
			require.Equal(t, len(tc.wantErrs), len(errs), "want", tc.wantErrs, "got", errs)
			for i := range tc.wantErrs {
				got := errs[i].Error()
				assert.Equal(t, tc.wantErrs[i], got)
			}
		})
	}
}

func TestResolvedImageSourceCELValidation(t *testing.T) {
	validators := fieldValidatorsFromFile(t, crdFilePath)
	pth := "openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref"
	validator, found := validators[GroupVersion.Version][pth]
	require.True(t, found)

	for name, tc := range map[string]struct {
		spec     ImageSource
		wantErrs []string
	}{
		"valid digest based image ref": {
			spec: ImageSource{
				Ref: "docker.io/test-image@sha256:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{},
		},
		"invalid digest based image ref, invalid domain": {
			spec: ImageSource{
				Ref: "-quay+docker/foo/bar@sha256:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": must start with a valid domain. valid domains must be alphanumeric characters (lowercase and uppercase) separated by the \".\" character.",
			},
		},
		"invalid digest based image ref, invalid name": {
			spec: ImageSource{
				Ref: "docker.io/FOO/BAR@sha256:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": a valid name is required. valid names must contain lowercase alphanumeric characters separated only by the \".\", \"_\", \"__\", \"-\" characters.",
			},
		},
		"invalid digest based image ref, invalid digest algorithm": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar@99-problems:abcdef123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": digest algorithm is not valid. valid algorithms must start with an uppercase or lowercase alpha character followed by alphanumeric characters and may contain the \"-\", \"_\", \"+\", and \".\" characters.",
			},
		},
		"invalid digest based image ref, too short digest encoding": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar@sha256:abcdef123456789",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": digest is not valid. the encoded string must be at least 32 characters",
			},
		},
		"invalid digest based image ref, invalid characters in digest encoding": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar@sha256:XYZxy123456789abcdef123456789abc",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": digest is not valid. the encoded string must only contain hex characters (A-F, a-f, 0-9)",
			},
		},
		"invalid image ref, no digest": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": must end with a digest",
			},
		},
		"invalid image ref, only domain with port": {
			spec: ImageSource{
				Ref: "docker.io:8080",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": a valid name is required. valid names must contain lowercase alphanumeric characters separated only by the \".\", \"_\", \"__\", \"-\" characters.",
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": must end with a digest",
			},
		},
		"invalid image ref, tag-based ref": {
			spec: ImageSource{
				Ref: "docker.io/foo/bar:latest",
			},
			wantErrs: []string{
				"openAPIV3Schema.properties.status.properties.resolvedSource.properties.image.properties.ref: Invalid value: \"string\": must end with a digest",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			errs := validator(tc.spec.Ref, nil)
			require.Equal(t, len(tc.wantErrs), len(errs), "want", tc.wantErrs, "got", errs)
			for i := range tc.wantErrs {
				got := errs[i].Error()
				assert.Equal(t, tc.wantErrs[i], got)
			}
		})
	}
}

func TestClusterCatalogURLsCELValidation(t *testing.T) {
	validators := fieldValidatorsFromFile(t, crdFilePath)
	pth := "openAPIV3Schema.properties.status.properties.urls.properties.base"
	validator, found := validators[GroupVersion.Version][pth]
	require.True(t, found)
	for name, tc := range map[string]struct {
		urls     ClusterCatalogURLs
		wantErrs []string
	}{
		"base is valid": {
			urls: ClusterCatalogURLs{
				Base: "https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio",
			},
			wantErrs: []string{},
		},
		"base is invalid, scheme is not one of http or https": {
			urls: ClusterCatalogURLs{
				Base: "file://somefilepath",
			},
			wantErrs: []string{
				fmt.Sprintf("%s: Invalid value: \"string\": scheme must be either http or https", pth),
			},
		},
		"base is invalid": {
			urls: ClusterCatalogURLs{
				Base: "notevenarealURL",
			},
			wantErrs: []string{
				fmt.Sprintf("%s: Invalid value: \"string\": must be a valid URL", pth),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			errs := validator(tc.urls.Base, nil)
			fmt.Println(errs)
			require.Equal(t, len(tc.wantErrs), len(errs))
			for i := range tc.wantErrs {
				got := errs[i].Error()
				assert.Equal(t, tc.wantErrs[i], got)
			}
		})
	}
}

func TestSourceCELValidation(t *testing.T) {
	validators := fieldValidatorsFromFile(t, crdFilePath)
	pth := "openAPIV3Schema.properties.spec.properties.source"
	validator, found := validators[GroupVersion.Version][pth]
	require.True(t, found)
	for name, tc := range map[string]struct {
		source   CatalogSource
		wantErrs []string
	}{
		"image source missing required image field": {
			source: CatalogSource{
				Type: SourceTypeImage,
			},
			wantErrs: []string{
				fmt.Sprintf("%s: Invalid value: \"object\": image is required when source type is %s, and forbidden otherwise", pth, SourceTypeImage),
			},
		},
		"image source with required image field": {
			source: CatalogSource{
				Type: SourceTypeImage,
				Image: &ImageSource{
					Ref: "docker.io/foo/bar:latest",
				},
			},
			wantErrs: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tc.source) //nolint:gosec
			require.NoError(t, err)
			errs := validator(obj, nil)
			fmt.Println(errs)
			require.Equal(t, len(tc.wantErrs), len(errs))
			for i := range tc.wantErrs {
				got := errs[i].Error()
				assert.Equal(t, tc.wantErrs[i], got)
			}
		})
	}
}

func TestResolvedSourceCELValidation(t *testing.T) {
	validators := fieldValidatorsFromFile(t, crdFilePath)
	pth := "openAPIV3Schema.properties.status.properties.resolvedSource"
	validator, found := validators[GroupVersion.Version][pth]

	require.True(t, found)
	for name, tc := range map[string]struct {
		source   ResolvedCatalogSource
		wantErrs []string
	}{
		"image source missing required image field": {
			source: ResolvedCatalogSource{
				Type: SourceTypeImage,
			},
			wantErrs: []string{
				fmt.Sprintf("%s: Invalid value: \"object\": image is required when source type is %s, and forbidden otherwise", pth, SourceTypeImage),
			},
		},
		"image source with required image field": {
			source: ResolvedCatalogSource{
				Type: SourceTypeImage,
				Image: &ResolvedImageSource{
					Ref: "docker.io/foo/bar@sha256:abcdef123456789abcdef123456789abc",
				},
			},
			wantErrs: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tc.source) //nolint:gosec
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
// nolint:unparam
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
