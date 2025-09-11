package bundlecfg

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
	schemavalidation "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
)

const (
	dns1123SubdomainFormat = "Namespace"
	notOwnNamespaceFormat  = "NotOwnNamespace"
)

var (
	//go:embed bundle_config_schema.json
	// bundleConfigBaseSchema is the base jsonschema for a registry+v1 bundle configuration
	// The rool level properties (e.g. watchNamespaces) must match the attribute names in the
	// Config struct's properties' json annotations. The final schema can be mutated to respect
	// bundle specific settings, e.g. the particular install mode support (i.e. if the bundle
	// only supports AllNamespaces install mode, it doesn't need a 'watchNamespace' parameter).
	// TODO: when we are ready to develop the SubscriptionConfig support, update Config with
	//   the *v1alpha1.SubscriptionConfig parameter, and update the base schema with the json value
	//   of the 'config' parameter in the SubscriptionConfig CRD found here:
	//   https://github.com/operator-framework/api/blob/master/crds/operators.coreos.com_subscriptions.yaml#L70
	bundleConfigBaseSchema []byte

	// supportedBundleInstallModes is a set of install modes supported by OLMv1
	supportedBundleInstallModes = sets.New[v1alpha1.InstallModeType](
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeSingleNamespace,
		v1alpha1.InstallModeTypeOwnNamespace,
	)

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

// ConfigSchema
type ConfigSchema struct{}

// Unmarshall returns a validated Config struct from the values given in rawConfig.
// The applied validation will be determined by the install modes supported by the bundle
func Unmarshall(rv1 bundle.RegistryV1, installNamespace string, rawConfig map[string]interface{}) (*Config, error) {
	if len(rawConfig) == 0 {
		return nil, nil
	}

	rawSchema, err := bundleConfigSchema(rv1, installNamespace)
	if err != nil {
		return nil, fmt.Errorf("error generating bundle config schema: %v", err)
	}

	// custom formats used for field validation
	// for instance kubernetes namespace name.
	// Also used for value validation, e.g. when a watchNamespace cannot be the install namespace
	// because more control over the error message can be given
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
func bundleConfigSchema(rv1 bundle.RegistryV1, installNamespace string) ([]byte, error) {
	schema := &jsonschema.Schema{}
	if err := json.Unmarshal(bundleConfigBaseSchema, schema); err != nil {
		return nil, err
	}

	// apply bundle rawConfig based mutations for watchNamespace
	if err := configureWatchNamespaceProperty(rv1, installNamespace, schema); err != nil {
		return nil, err
	}

	// return schema
	out, err := schema.MarshalJSON()
	if err != nil {
		panic(err)
	}
	return out, err
}

// configureWatchNamespaceProperty modifies schema to configure the watchNamespace config property based on
// the install modes supported by the bundle marking the field required or optional, or restricting the possible values
// it can take
func configureWatchNamespaceProperty(rv1 bundle.RegistryV1, installNamespace string, schema *jsonschema.Schema) error {
	bundleInstallModes := sets.New[v1alpha1.InstallModeType]()
	for _, im := range rv1.CSV.Spec.InstallModes {
		if im.Supported {
			bundleInstallModes.Insert(im.Type)
		}
	}

	supportedInstallModes := bundleInstallModes.Intersection(supportedBundleInstallModes)

	if len(supportedInstallModes) == 0 {
		//bundleModes := slices.Sorted(slices.Values(bundleInstallModes.UnsortedList()))
		supportedModes := slices.Sorted(slices.Values(supportedBundleInstallModes.UnsortedList()))
		return fmt.Errorf("bundle does not support any of the allowable install modes %v", supportedModes)
	}

	allSupported := supportedInstallModes.Has(v1alpha1.InstallModeTypeAllNamespaces)
	singleSupported := supportedInstallModes.Has(v1alpha1.InstallModeTypeSingleNamespace)
	ownSupported := supportedInstallModes.Has(v1alpha1.InstallModeTypeOwnNamespace)

	// no watchNamespace rawConfig parameter if bundle only supports AllNamespaces or OwnNamespace install modes
	if len(supportedInstallModes) == 1 && (allSupported || ownSupported) {
		schema.Properties.Delete("watchNamespace")
		return nil
	}

	watchNamespaceProperty, ok := schema.Properties.Get("watchNamespace")
	if !ok {
		return errors.New("watchNamespace not found in schema")
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
	return nil
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
	bytes, err := json.Marshal(rawConfig)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	err = json.Unmarshal(bytes, cfg)
	return cfg, err
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

// notOwnNamespaceFmt returns a dynamically generated format specifically for the case where
// a bundle does not support own namespace installation but a watch namespace can be optionally given
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
