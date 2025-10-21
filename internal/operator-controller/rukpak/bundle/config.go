package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

type Config struct {
	WatchNamespace *string `json:"watchNamespace"`
}

// UnmarshallConfig returns a deserialized and validated *bundle.Config based on bytes and validated
// against rv1 and the desired install namespaces. It will error if:
// - rv is nil
// - bytes is not a valid YAML/JSON object
// - bytes is a valid YAML/JSON object but does not follow the registry+v1 schema
// if bytes is nil a nil bundle.Config is returned
func UnmarshallConfig(bytes []byte, rv1 RegistryV1, installNamespace string) (*Config, error) {
	if bytes == nil {
		return nil, nil
	}

	bundleConfig := &Config{}
	if err := yaml.UnmarshalStrict(bytes, bundleConfig); err != nil {
		return nil, fmt.Errorf("error unmarshalling registry+v1 configuration: %w", formatUnmarshallError(err))
	}

	// collect bundle install modes
	bundleInstallModeSet := sets.New(rv1.CSV.Spec.InstallModes...)

	if err := validateConfig(bundleConfig, installNamespace, bundleInstallModeSet); err != nil {
		return nil, fmt.Errorf("error unmarshalling registry+v1 configuration: %w", err)
	}

	return bundleConfig, nil
}

// validateConfig validates a *bundle.Config against the bundle's supported install modes and the user-give installNamespace.
func validateConfig(config *Config, installNamespace string, bundleInstallModeSet sets.Set[v1alpha1.InstallMode]) error {
	// no config, no problem
	if config == nil {
		return nil
	}

	// if the bundle does not support the watchNamespace configuration and it is set, treat it like any unknown field
	if config.WatchNamespace != nil && !isWatchNamespaceConfigSupported(bundleInstallModeSet) {
		return errors.New(`unknown field "watchNamespace"`)
	}

	// if watchNamespace is required then ensure that it is set
	if config.WatchNamespace == nil && isWatchNamespaceConfigRequired(bundleInstallModeSet) {
		return errors.New(`required field "watchNamespace" is missing`)
	}

	// if watchNamespace is set then ensure it is a valid namespace
	if config.WatchNamespace != nil {
		if errs := validation.IsDNS1123Subdomain(*config.WatchNamespace); len(errs) > 0 {
			return fmt.Errorf("invalid 'watchNamespace' %q: namespace must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character", *config.WatchNamespace)
		}
	}

	// only accept install namespace if OwnNamespace install mode is supported
	if config.WatchNamespace != nil && *config.WatchNamespace == installNamespace &&
		!bundleInstallModeSet.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true}) {
		return fmt.Errorf("invalid 'watchNamespace' %q: must not be install namespace (%s)", *config.WatchNamespace, installNamespace)
	}

	// only accept non-install namespace is SingleNamespace is supported
	if config.WatchNamespace != nil && *config.WatchNamespace != installNamespace &&
		!bundleInstallModeSet.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true}) {
		return fmt.Errorf("invalid 'watchNamespace' %q: must be install namespace (%s)", *config.WatchNamespace, installNamespace)
	}

	return nil
}

// isWatchNamespaceConfigSupported returns true when the bundle exposes a watchNamespace configuration. This happens when:
// - SingleNamespace install more is supported, or
// - OwnNamespace and AllNamespaces install modes are supported
func isWatchNamespaceConfigSupported(bundleInstallModeSet sets.Set[v1alpha1.InstallMode]) bool {
	return bundleInstallModeSet.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true}) ||
		bundleInstallModeSet.HasAll(
			v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
			v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true})
}

// isWatchNamespaceConfigRequired returns true if the watchNamespace configuration is required. This happens when
// AllNamespaces install mode is not supported and SingleNamespace is supported
func isWatchNamespaceConfigRequired(bundleInstallModeSet sets.Set[v1alpha1.InstallMode]) bool {
	return !bundleInstallModeSet.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}) &&
		bundleInstallModeSet.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true})
}

// formatUnmarshallError format JSON unmarshal errors to be more readable
func formatUnmarshallError(err error) error {
	var unmarshalErr *json.UnmarshalTypeError
	if errors.As(err, &unmarshalErr) {
		if unmarshalErr.Field == "" {
			return errors.New("input is not a valid JSON object")
		} else {
			return fmt.Errorf("invalid value type for field %q: expected %q but got %q", unmarshalErr.Field, unmarshalErr.Type.String(), unmarshalErr.Value)
		}
	}

	// unwrap error until the core and process it
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			// usually the errors present in the form json: <message> or yaml: <message>
			// we want to extract <message> if we can
			errMessageComponents := strings.Split(err.Error(), ":")
			coreErrMessage := strings.TrimSpace(errMessageComponents[len(errMessageComponents)-1])
			return errors.New(coreErrMessage)
		}
		err = unwrapped
	}
}
