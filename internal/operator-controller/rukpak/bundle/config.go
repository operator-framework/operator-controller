package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

const (
	dns1123SubdomainFormat = "RFC-1123"
)

var dnsFormat = &jsonschema.Format{
	Name: dns1123SubdomainFormat,
	Validate: func(v any) error {
		if v == nil {
			return nil
		}
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("invalid type %T, expected string", v)
		}
		errs := validation.IsDNS1123Subdomain(s)
		if len(errs) > 0 {
			return errors.New(strings.Join(errs, ", "))
		}
		return nil
	},
}
var unsupportedInstallModes = sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeMultiNamespace)

type Config struct {
	WatchNamespace *string `json:"watchNamespace,omitempty"`
}
type JSONSchema []byte
type JSONPropertySchema struct {
	Schema     json.RawMessage
	Name       string
	IsRequired bool
}

func WatchNamespaceSchemaProperty(rv1 RegistryV1, installNamespace string) JSONPropertySchema {
	supportedInstallModes := sets.New[v1alpha1.InstallModeType]()
	for _, im := range rv1.CSV.Spec.InstallModes {
		if im.Supported && !unsupportedInstallModes.Has(im.Type) {
			supportedInstallModes.Insert(im.Type)
		}
	}

	allSupported := supportedInstallModes.Has(v1alpha1.InstallModeTypeAllNamespaces)
	singleSupported := supportedInstallModes.Has(v1alpha1.InstallModeTypeSingleNamespace)
	ownSupported := supportedInstallModes.Has(v1alpha1.InstallModeTypeOwnNamespace)

	if len(supportedInstallModes) == 0 {
		panic("bundle does not support any supported install modes")
	}

	// no watchNamespace config parameter if bundle only supports AllNamespaces or OwnNamespace install modes
	if len(supportedInstallModes) == 1 && (allSupported || ownSupported) {
		return JSONPropertySchema{}
	}

	// schema settings
	isRequired := false
	restrictNamespaceTo := ""

	// required
	if !allSupported && singleSupported {
		isRequired = true
	}

	// optional must be install namespace
	if allSupported && ownSupported && !singleSupported {
		restrictNamespaceTo = installNamespace
	}

	return JSONPropertySchema{
		Schema:     watchNamespaceSchema(isRequired, restrictNamespaceTo),
		Name:       "watchNamespace",
		IsRequired: isRequired,
	}
}

func GetValidatedBundleConfigFromRaw(rv1 RegistryV1, installNamespace string, config map[string]interface{}) (*Config, error) {
	rawSchema := BuildConfigSchemaWithProperties([]JSONPropertySchema{
		WatchNamespaceSchemaProperty(rv1, installNamespace),
	})

	if err := ValidateBundleConfig(rawSchema, config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	cfg, err := unmarshalConfig(config)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func unmarshalConfig(config map[string]interface{}) (*Config, error) {
	cfg := Config{}
	dataBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(dataBytes, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func ValidateBundleConfig(rawSchema []byte, config map[string]interface{}) error {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(string(rawSchema)))
	if err != nil {
		return err
	}

	compiler := jsonschema.NewCompiler()
	compiler.RegisterFormat(dnsFormat)
	compiler.AssertFormat()
	if err := compiler.AddResource("schema.json", schema); err != nil {
		return err
	}
	compiledSchema, err := compiler.Compile("schema.json")
	if err != nil {
		return err
	}

	return formatJSONSchemaValidationError(compiledSchema.Validate(config))
}

func formatJSONSchemaValidationError(err error) error {
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	var errs []error
	for _, cause := range validationErr.Causes {
		if cause == nil || cause.BasicOutput() == nil {
			continue
		}

		output := cause.BasicOutput()
		instanceLocation := strings.ReplaceAll(output.InstanceLocation, "/", ".")
		if instanceLocation == "" {
			errs = append(errs, fmt.Errorf("%v", output.Error))
		} else {
			errs = append(errs, fmt.Errorf("at path %q: %s", instanceLocation, output.Error))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return err
}

func BuildConfigSchemaWithProperties(properties []JSONPropertySchema) JSONSchema {
	var requiredProperties []string
	props := map[string]json.RawMessage{}
	for _, prop := range properties {
		if prop.Name == "" {
			continue
		}
		if prop.IsRequired {
			requiredProperties = append(requiredProperties, prop.Name)
		}
		props[prop.Name] = prop.Schema
	}

	schema := struct {
		Schema               string                     `json:"$schema"`
		Title                string                     `json:"title"`
		Description          string                     `json:"description"`
		Type                 string                     `json:"type"`
		Properties           map[string]json.RawMessage `json:"properties,omitempty"`
		RequiredProperties   []string                   `json:"required,omitempty"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}{
		Schema:               "https://json-schema.org/draft/2020-12/schema",
		Title:                "BundleConfig",
		Description:          "registry+v1 bundle configuration",
		Type:                 "object",
		Properties:           props,
		RequiredProperties:   requiredProperties,
		AdditionalProperties: false,
	}

	out, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal JSON schema: %v", err))
	}
	return out
}

func watchNamespaceSchemaDescription(isRequired bool, restrictedInput string) string {
	var description = strings.Builder{}

	description.WriteString("The Kubernetes namespace to watch for resources.")

	if isRequired {
		description.WriteString(" This field is required.")
	} else {
		description.WriteString(" If not specified, all namespaces are watched.")
	}

	if restrictedInput != "" {
		description.WriteString(fmt.Sprintf(" If specified, the value must be %q.", restrictedInput))
	}

	return description.String()
}

func watchNamespaceSchema(isRequired bool, restrictedInput string) []byte {
	schema := struct {
		Type        []string  `json:"type"`
		Description string    `json:"description"`
		Enum        []*string `json:"enum,omitempty"`
		Format      string    `json:"format,omitempty"`
		Pattern     string    `json:"pattern,omitempty"`
	}{
		Type:        []string{"string"},
		Description: watchNamespaceSchemaDescription(isRequired, restrictedInput),
		Format:      dns1123SubdomainFormat,
	}
	if restrictedInput != "" {
		schema.Enum = []*string{ptr.To(restrictedInput), nil}
	}
	if !isRequired {
		schema.Type = append(schema.Type, "null")
	}
	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic(fmt.Errorf("failed to marshal JSON: %s", err))
	}
	return out
}
