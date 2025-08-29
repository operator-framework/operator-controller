package bundlecfg_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/bundlecfg"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

const (
	installNamespace = "install-namespace"
)

var (
	requiredConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeSingleNamespace,
	}

	optionalNotOwnNamespaceConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeSingleNamespace,
	}

	optionalOwnNamespaceConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeOwnNamespace,
	}

	optionalConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeOwnNamespace,
		v1alpha1.InstallModeTypeSingleNamespace,
	}

	notRequiredConfigSet = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
	}
)

func Test_Unmarshall_WatchNamespace_Configuration(t *testing.T) {
	// The behavior of watchNamespace is dynamic and depends on the install modes supported by
	// the bundle (declared in its ClusterServiceVersion). For instance, a bundle that only
	// supports AllNamespaces or only supports OwnNamespace mode does not need a watchNamespace configuration, or
	// a bundle that supports AllNamespaces and OwnNamespace install modes will have an optional watchNamespace
	// configuration, however when set, the value must be equal to the install namespace
	for _, tt := range []struct {
		name                   string
		supportedInstallModes  []v1alpha1.InstallModeType
		rawConfig              map[string]interface{}
		expectedErrMsgFragment string
		expectedConfig         *bundlecfg.Config
	}{
		{
			name:                  "bundle does not have a valid install mode",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
			expectedErrMsgFragment: "bundle does not support any of the allowable install modes [AllNamespaces OwnNamespace SingleNamespace]",
		},
		{
			name:                  "watchNamespace is required and provided",
			supportedInstallModes: requiredConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
			expectedConfig: &bundlecfg.Config{
				WatchNamespace: "some-namespace",
			},
		},
		{
			name:                  "watchNamespace is required and provided but invalid",
			supportedInstallModes: requiredConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "invalid-",
			},
			expectedErrMsgFragment: "is not a valid namespace name",
		},
		{
			name:                  "watchNamespace is required and provided but empty",
			supportedInstallModes: requiredConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "",
			},
			expectedErrMsgFragment: "is not a valid namespace name",
		},
		{
			name:                  "watchNamespace is required and not provided (nil)",
			supportedInstallModes: requiredConfigSet,
			rawConfig:             nil,
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is required and not provided (empty config)",
			supportedInstallModes: requiredConfigSet,
			rawConfig:             map[string]interface{}{},
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is optional and provided",
			supportedInstallModes: optionalConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
			expectedConfig: &bundlecfg.Config{
				WatchNamespace: "some-namespace",
			},
		},
		{
			name:                  "watchNamespace is optional and provided but invalid",
			supportedInstallModes: optionalConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "not a valid namespace name",
			},
			expectedErrMsgFragment: "not a valid namespace name",
		},
		{
			name:                  "watchNamespace is optional and not provided (nil config)",
			supportedInstallModes: optionalConfigSet,
			rawConfig:             nil,
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is optional and not provided (empty config)",
			supportedInstallModes: optionalConfigSet,
			rawConfig:             map[string]interface{}{},
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is optional and not provided (nil)",
			supportedInstallModes: optionalConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": nil,
			},
			expectedConfig: &bundlecfg.Config{},
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and correctly provided",
			supportedInstallModes: optionalOwnNamespaceConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "install-namespace",
			},
			expectedConfig: &bundlecfg.Config{
				WatchNamespace: "install-namespace",
			},
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and incorrectly provided",
			supportedInstallModes: optionalOwnNamespaceConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "not-install-namespace",
			},
			expectedErrMsgFragment: "value must be one of 'install-namespace', <nil>",
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and not provided (nil)",
			supportedInstallModes: optionalOwnNamespaceConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": nil,
			},
			expectedConfig: &bundlecfg.Config{},
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and not provided (nil config)",
			supportedInstallModes: optionalOwnNamespaceConfigSet,
			rawConfig:             nil,
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is optional and restricted to install-namespace and not provided (empty config)",
			supportedInstallModes: optionalOwnNamespaceConfigSet,
			rawConfig:             map[string]interface{}{},
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is optional and cannot be install-namespace but is install namespace",
			supportedInstallModes: optionalNotOwnNamespaceConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "install-namespace",
			},
			expectedConfig: &bundlecfg.Config{
				WatchNamespace: "install-namespace",
			},
			expectedErrMsgFragment: "watchNamespace cannot be install namespace",
		},
		{
			name:                  "watchNamespace is optional and cannot be install-namespace and it not install namespace",
			supportedInstallModes: optionalNotOwnNamespaceConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "not-install-namespace",
			},
			expectedConfig: &bundlecfg.Config{
				WatchNamespace: "not-install-namespace",
			},
		},
		{
			name:                  "watchNamespace is optional and cannot be install-namespace and not provided (nil)",
			supportedInstallModes: optionalNotOwnNamespaceConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": nil,
			},
			expectedConfig: &bundlecfg.Config{},
		},
		{
			name:                  "watchNamespace is optional and cannot be install-namespace and not provided (nil config)",
			supportedInstallModes: optionalNotOwnNamespaceConfigSet,
			rawConfig:             nil,
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is optional and cannot be install-namespace and not provided (empty config)",
			supportedInstallModes: optionalNotOwnNamespaceConfigSet,
			rawConfig:             map[string]interface{}{},
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is not a config option and it is set",
			supportedInstallModes: notRequiredConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": "some-namespace",
			},
			expectedErrMsgFragment: "additional properties 'watchNamespace' not allowed",
		},
		{
			name:                  "watchNamespace is not a config option and it is not set (nil)",
			supportedInstallModes: notRequiredConfigSet,
			rawConfig: map[string]interface{}{
				"watchNamespace": nil,
			},
			expectedErrMsgFragment: "additional properties 'watchNamespace' not allowed",
		},
		{
			name:                  "watchNamespace is not a config option and it is not set (config empty)",
			supportedInstallModes: notRequiredConfigSet,
			rawConfig:             map[string]interface{}{},
			expectedConfig:        nil,
		},
		{
			name:                  "watchNamespace is not a config option and it is not set (config nil)",
			supportedInstallModes: notRequiredConfigSet,
			rawConfig:             nil,
			expectedConfig:        nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rv1 := bundle.RegistryV1{
				CSV: MakeCSV(WithInstallModeSupportFor(tt.supportedInstallModes...)),
			}

			cfg, err := bundlecfg.Unmarshall(rv1, installNamespace, tt.rawConfig)

			if tt.expectedErrMsgFragment == "" {
				require.NoError(t, err)
				require.Equal(t, tt.expectedConfig, cfg)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrMsgFragment)
			}
		})
	}
}
