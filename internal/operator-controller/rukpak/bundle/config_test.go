package bundle_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

const (
	installNamespace = "install-namespace"
)

var (
	watchNamespaceRequired = bundle.JSONPropertySchema{
		Name:       "watchNamespace",
		IsRequired: true,
		Schema: []byte(`{
  "type": [
    "string"
  ],
  "description": "The Kubernetes namespace to watch for resources. This field is required.",
  "format": "RFC-1123"
}`),
	}

	watchNamespaceOptionalWithRestrictions = bundle.JSONPropertySchema{
		Name:       "watchNamespace",
		IsRequired: false,
		Schema: []byte(`{
  "type": [
    "string",
    "null"
  ],
  "description": "The Kubernetes namespace to watch for resources. If not specified, all namespaces are watched. If specified, the value must be \"install-namespace\".",
  "enum": [
    "install-namespace",
    null
  ],
  "format": "RFC-1123"
}`),
	}

	watchNamespaceOptional = bundle.JSONPropertySchema{
		Name:       "watchNamespace",
		IsRequired: false,
		Schema: []byte(`{
  "type": [
    "string",
    "null"
  ],
  "description": "The Kubernetes namespace to watch for resources. If not specified, all namespaces are watched.",
  "format": "RFC-1123"
}`),
	}

	requiredConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeSingleNamespace,
	}

	optionalConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeSingleNamespace,
	}

	optionalRestrictedConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeOwnNamespace,
	}

	notRequiredConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
	}
)

func Test_ConfigSchema(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		supportedInstallModes  []v1alpha1.InstallModeType
		config                 map[string]interface{}
		expectedErrMsgFragment string
	}{
		{
			name:                  "watchNamespace is required and provided",
			supportedInstallModes: requiredConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
		},
		{
			name:                  "watchNamespace is required and provided but invalid",
			supportedInstallModes: requiredConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "not a valid namespace name",
			},
			expectedErrMsgFragment: "'not a valid namespace name' is not valid RFC-1123",
		},
		{
			name:                  "watchNamespace is required and provided but empty",
			supportedInstallModes: requiredConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "",
			},
			expectedErrMsgFragment: "'' is not valid RFC-1123",
		},
		{
			name:                   "watchNamespace is required and not provided (nil)",
			supportedInstallModes:  requiredConfigSet,
			config:                 nil,
			expectedErrMsgFragment: "missing property 'watchNamespace'",
		},
		{
			name:                   "watchNamespace is required and not provided (empty config)",
			supportedInstallModes:  requiredConfigSet,
			config:                 map[string]interface{}{},
			expectedErrMsgFragment: "missing property 'watchNamespace'",
		},
		{
			name:                  "watchNamespace is optional and provided",
			supportedInstallModes: optionalConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
		},
		{
			name:                  "watchNamespace is optional and provided but invalid",
			supportedInstallModes: optionalConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "not a valid namespace name",
			},
			expectedErrMsgFragment: "'not a valid namespace name' is not valid RFC-1123",
		},
		{
			name:                  "watchNamespace is optional and not provided (nil config)",
			supportedInstallModes: optionalConfigSet,
			config:                nil,
		},
		{
			name:                  "watchNamespace is optional and not provided (empty config)",
			supportedInstallModes: optionalConfigSet,
			config:                map[string]interface{}{},
		},
		{
			name:                  "watchNamespace is optional and not provided (nil)",
			supportedInstallModes: optionalConfigSet,
			config: map[string]interface{}{
				"watchNamespace": nil,
			},
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and correctly provided",
			supportedInstallModes: optionalRestrictedConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "install-namespace",
			},
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and incorrectly provided",
			supportedInstallModes: optionalRestrictedConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "not-install-namespace",
			},
			expectedErrMsgFragment: "value must be one of 'install-namespace', <nil>",
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and not provided (nil)",
			supportedInstallModes: optionalRestrictedConfigSet,
			config: map[string]interface{}{
				"watchNamespace": nil,
			},
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and not provided (nil config)",
			supportedInstallModes: optionalRestrictedConfigSet,
			config:                nil,
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and not provided (empty config)",
			supportedInstallModes: optionalRestrictedConfigSet,
			config:                map[string]interface{}{},
		},
		{
			name:                  "watchNamespace is not a config option and it is set",
			supportedInstallModes: notRequiredConfigSet,
			config: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
			expectedErrMsgFragment: "additional properties 'watchNamespace' not allowed",
		},
		{
			name:                  "watchNamespace is not a config option and it is not set (nil)",
			supportedInstallModes: notRequiredConfigSet,
			config: map[string]interface{}{
				"watchNamespace": nil,
			},
			expectedErrMsgFragment: "additional properties 'watchNamespace' not allowed",
		},
		{
			name:                  "watchNamespace is not a config option and it is not set (config empty)",
			supportedInstallModes: notRequiredConfigSet,
			config:                map[string]interface{}{},
		},
		{
			name:                  "watchNamespace is not a config option and it is not set (config nil)",
			supportedInstallModes: notRequiredConfigSet,
			config:                nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rv1 := bundle.RegistryV1{
				CSV: MakeCSV(WithInstallModeSupportFor(tt.supportedInstallModes...)),
			}

			cfg, err := bundle.GetValidatedBundleConfigFromRaw(rv1, installNamespace, tt.config)

			if tt.expectedErrMsgFragment == "" {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrMsgFragment)
			}
		})
	}
}

func Test_WatchNamespaceSchemaProperty(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		supportedInstallModes []v1alpha1.InstallModeType
		expectPanic           bool
		expectedSchema        bundle.JSONPropertySchema
		expectedIsRequired    bool
	}{
		{
			name:                  "no install modes - panic",
			supportedInstallModes: []v1alpha1.InstallModeType{},
			expectPanic:           true,
		},
		{
			name:                  "only MultiNamespace install mode - panic",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			expectPanic:           true,
		},
		{
			name:                  "only OwnNamespace - no property",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			expectedSchema:        bundle.JSONPropertySchema{},
		},
		{
			name:                  "OwnNamespace + MultiNamespace - no property",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        bundle.JSONPropertySchema{},
		},
		{
			name:                  "only SingleNamespace - required with no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			expectedSchema:        watchNamespaceRequired,
		},
		{
			name:                  "SingleNamespace + MultiNamespace - required with no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        watchNamespaceRequired,
		},
		{
			name:                  "SingleNamespace + OwnNamespace - required with no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			expectedSchema:        watchNamespaceRequired,
		},
		{
			name:                  "SingleNamespace + OwnNamespace + MultiNamespace - required with no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        watchNamespaceRequired,
		},
		{
			name:                  "Only AllNamespaces - no property",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			expectedSchema:        bundle.JSONPropertySchema{},
		},
		{
			name:                  "AllNamespaces + MultiNamespace - no property",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        bundle.JSONPropertySchema{},
		},
		{
			name:                  "AllNamespaces + OwnNamespace - optional with input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			expectedSchema:        watchNamespaceOptionalWithRestrictions,
		},
		{
			name:                  "AllNamespace + OwnNamespace + MultiNamespace - optional with input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        watchNamespaceOptionalWithRestrictions,
		},
		{
			name:                  "AllNamespaces + SingleNamespace - optional no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			expectedSchema:        watchNamespaceOptional,
		},
		{
			name:                  "AllNamespaces + SingleNamespace + MultiNamespace - optional no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        watchNamespaceOptional,
		},
		{
			name:                  "AllNamespaces + SingleNamespace + OwnNamespace - optional no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			expectedSchema:        watchNamespaceOptional,
		},
		{
			name:                  "All install modes supported - optional no input restriction",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			expectedSchema:        watchNamespaceOptional,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				require.Panics(t, func() {
					bundle.WatchNamespaceSchemaProperty(bundle.RegistryV1{
						CSV: MakeCSV(WithInstallModeSupportFor(tt.supportedInstallModes...)),
					}, installNamespace)
				})
			} else {
				schema := bundle.WatchNamespaceSchemaProperty(bundle.RegistryV1{
					CSV: MakeCSV(WithInstallModeSupportFor(tt.supportedInstallModes...)),
				}, installNamespace)
				require.Equal(t, string(tt.expectedSchema.Schema), string(schema.Schema))
			}
		})
	}
}
