package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"
	schemavalidation "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

const (
	dns1123SubdomainFormat = "RFC-1123"

	// the format name is injected into the error
	notOwnNamespaceFormat = "watchNamespace"
)

var (
	// unsupportedInstallModes set of unsupported ClusterServiceVersion install modes
	unsupportedInstallModes = sets.New[v1alpha1.InstallModeType](v1alpha1.InstallModeTypeMultiNamespace)

	// dnsFormat checks conformity to RFC1213 lowercase dns subdomain format by any field with format 'RFC-1123'
	dnsFormat = &schemavalidation.Format{
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
				return fmt.Errorf("%q is not a valid namespace name: %s", v, strings.Join(errs, ", "))
			}
			return nil
		},
	}
)

// Config is a registry+v1 bundle configuration surface
type Config struct {
	// WatchNamespace is supported for certain bundles to allow the user to configure installation in Single- or OwnNamespace modes
	// The validation behavior of this field is determined by the install modes supported by the bundle, e.g.:
	// - If a bundle only supports AllNamespaces mode (or only OwnNamespace mode): this field will be unknown
	// - If a bundle supports AllNamespaces and SingleNamespace install modes: this field is optional
	// - If a bundle supports AllNamespaces and OwnNamespace: this field is optional, but if set must be equal to the install namespace
	WatchNamespace string `json:"watchNamespace,omitempty"`
}

// ValidatedBundleConfigFromRaw returns a validated Config struct from the values given in rawConfig.
// The applied validation will be determined by the install modes supported by the bundle
func ValidatedBundleConfigFromRaw(rv1 RegistryV1, installNamespace string, rawConfig map[string]interface{}) (*Config, error) {
	if len(rawConfig) == 0 {
		return nil, nil
	}

	rawSchema := bundleConfigSchema(rv1, installNamespace)
	customFormats := []*schemavalidation.Format{
		dnsFormat,
		notOwnNamespaceFmt(installNamespace),
	}

	if err := validateBundleConfig(rawSchema, customFormats, rawConfig); err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	return toConfig(rawConfig)
}

// bundleConfigSchema generates a jsonschema used to validate bundle configuration
func bundleConfigSchema(rv1 RegistryV1, installNamespace string) []byte {
	// configure reflector
	r := new(jsonschema.Reflector)
	r.ExpandedStruct = true
	r.AllowAdditionalProperties = false

	// generate base schema
	schema := r.Reflect(&Config{})

	// apply bundle rawConfig based mutations for watchNamespace
	configureWatchNamespaceProperty(rv1, installNamespace, schema)

	// return schema
	out, err := schema.MarshalJSON()
	if err != nil {
		panic(err)
	}
	return out
}

// configureWatchNamespaceProperty modifies schema to configure the watchNamespace config property based on
// the install modes supported by the bundle marking the field required or optional, or restricting the possible values
// it can take
func configureWatchNamespaceProperty(rv1 RegistryV1, installNamespace string, schema *jsonschema.Schema) {
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

	// no watchNamespace rawConfig parameter if bundle only supports AllNamespaces or OwnNamespace install modes
	if len(supportedInstallModes) == 1 && (allSupported || ownSupported) {
		schema.Properties.Delete("watchNamespace")
		return
	}

	watchNamespaceProperty, ok := schema.Properties.Get("watchNamespace")
	if !ok {
		panic("watchNamespace not found in schema")
	}

	watchNamespaceProperty.Format = dns1123SubdomainFormat

	// required or optional
	if !allSupported && singleSupported {
		schema.Required = append(schema.Required, "watchNamespace")
	} else {
		// note: the library currently doesn't support jsonschema.Types
		// this is the current workaround for declaring optional/nullable fields
		// https://github.com/invopop/jsonschema/issues/115
		watchNamespaceProperty.Extras = map[string]any{
			"type": []string{"string", "null"},
		}
		if !ownSupported {
			// if own namespace is not supported validate that it is not being used
			watchNamespaceProperty.Format = notOwnNamespaceFormat
		}
	}

	// must be the install namespace
	if allSupported && ownSupported && !singleSupported {
		watchNamespaceProperty.Enum = []any{
			installNamespace,
			nil,
		}
	}
}

// validateBundleConfig validates the bundle rawConfig
func validateBundleConfig(rawSchema []byte, customFormats []*schemavalidation.Format, rawConfig map[string]interface{}) error {
	schema, err := schemavalidation.UnmarshalJSON(strings.NewReader(string(rawSchema)))
	if err != nil {
		return err
	}

	compiler := schemavalidation.NewCompiler()
	for _, format := range customFormats {
		compiler.RegisterFormat(format)
	}
	compiler.AssertFormat()
	if err := compiler.AddResource("schema.json", schema); err != nil {
		return err
	}
	compiledSchema, err := compiler.Compile("schema.json")
	if err != nil {
		return err
	}

	return formatJSONSchemaValidationError(compiledSchema.Validate(rawConfig))
}

// toConfig converts rawConfig into a Config struct
func toConfig(rawConfig map[string]interface{}) (*Config, error) {
	cfg := Config{}
	dataBytes, err := json.Marshal(rawConfig)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(dataBytes, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// formatJSONSchemaValidationError extracts and formats the jsonschema validation errors given by the underlying library
func formatJSONSchemaValidationError(err error) error {
	var validationErr *schemavalidation.ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	var errs []error
	for _, cause := range validationErr.Causes {
		if cause == nil || cause.ErrorKind == nil {
			continue
		}

		var errMsg string
		switch e := cause.ErrorKind.(type) {
		case *kind.Format:
			errMsg = e.Err.Error()
		default:
			errMsg = cause.Error()
		}

		instanceLocation := "." + strings.Join(cause.InstanceLocation, ".")
		if instanceLocation == "." {
			errs = append(errs, fmt.Errorf("%v", errMsg))
		} else {
			errs = append(errs, fmt.Errorf("at path %q: %s", instanceLocation, errMsg))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return err
}

func notOwnNamespaceFmt(installNamespace string) *schemavalidation.Format {
	return &schemavalidation.Format{
		Name: notOwnNamespaceFormat,
		Validate: func(v any) error {
			if err := dnsFormat.Validate(v); err != nil {
				return err
			}
			if v == installNamespace {
				return fmt.Errorf("unsupported value %q, watchNamespace cannot be install namespace", v)
			}
			return nil
		},
	}
}
