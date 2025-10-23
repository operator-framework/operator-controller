package bundle_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_UnmarshalConfig(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		rawConfig             []byte
		supportedInstallModes []v1alpha1.InstallModeType
		installNamespace      string
		expectedErrMessage    string
		expectedConfig        *bundle.Config
	}{
		{
			name:           "returns nil for nil config",
			rawConfig:      nil,
			expectedConfig: nil,
		},
		{
			name:                  "accepts json config",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "accepts yaml config",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`watchNamespace: some-namespace`),
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "rejects invalid json",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"hello`),
			expectedErrMessage:    `error unmarshalling registry+v1 configuration: found unexpected end of stream`,
		},
		{
			name:                  "rejects valid json that isn't of object type",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`true`),
			expectedErrMessage:    `error unmarshalling registry+v1 configuration: input is not a valid JSON object`,
		},
		{
			name:                  "rejects additional fields",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`somekey: somevalue`),
			expectedErrMessage:    `error unmarshalling registry+v1 configuration: unknown field "somekey"`,
		},
		{
			name:                  "rejects valid json but invalid registry+v1",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": {"hello": "there"}}`),
			expectedErrMessage:    `error unmarshalling registry+v1 configuration: invalid value type for field "watchNamespace": expected "string" but got "object"`,
		},
		{
			name:                  "rejects bad namespace format",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "bad-Namespace-"}`),
			expectedErrMessage:    "invalid 'watchNamespace' \"bad-Namespace-\": namespace must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character",
		},
		{
			name:                  "rejects with unknown field when install modes {AllNamespaces}",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedErrMessage:    "unknown field \"watchNamespace\"",
		},
		{
			name:                  "rejects with unknown field when install modes {MultiNamespace}",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedErrMessage:    "unknown field \"watchNamespace\"",
		},
		{
			name:                  "reject with unknown field when install modes {AllNamespaces, MultiNamespace}",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedErrMessage:    "unknown field \"watchNamespace\"",
		},
		{
			name:                  "reject with required field when install modes {OwnNamespace} and watchNamespace is null",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "reject with required field when install modes {OwnNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "reject with required field when install modes {MultiNamespace, OwnNamespace} and watchNamespace is null",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "reject with required field when install modes {MultiNamespace, OwnNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "accepts when install modes {SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "accepts when install modes {AllNamespaces, SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "accepts when install modes {MultiNamespace, SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "accepts when install modes {OwnNamespace, SingleNamespace} and watchNamespace != install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "not-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "rejects when install modes {SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedErrMessage:    "invalid 'watchNamespace' \"some-namespace\": must not be install namespace (some-namespace)",
		},
		{
			name:                  "rejects when install modes {AllNamespaces, SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedErrMessage:    "invalid 'watchNamespace' \"some-namespace\": must not be install namespace (some-namespace)",
		},
		{
			name:                  "rejects when install modes {MultiNamespace, SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeMultiNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedErrMessage:    "invalid 'watchNamespace' \"some-namespace\": must not be install namespace (some-namespace)",
		},
		{
			name:                  "accepts when install modes {AllNamespaces, OwnNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "accepts when install modes {OwnNamespace, SingleNamespace} and watchNamespace == install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "some-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: ptr.To("some-namespace"),
			},
		},
		{
			name:                  "rejects when install modes {AllNamespaces, OwnNamespace} and watchNamespace != install namespace",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": "some-namespace"}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "invalid 'watchNamespace' \"some-namespace\": must be install namespace (not-some-namespace)",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, OwnNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, OwnNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, MultiNamespace} and watchNamespace is missing",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "rejects with required field error when install modes {SingleNamespace, OwnNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "required field \"watchNamespace\" is missing",
		},
		{
			name:                  "accepts null watchNamespace when install modes {AllNamespaces, OwnNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: nil,
			},
		},
		{
			name:                  "accepts null watchNamespace when install modes {AllNamespaces, OwnNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{"watchNamespace": null}`),
			installNamespace:      "not-some-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: nil,
			},
		},
		{
			name:                  "accepts no watchNamespace when install modes {AllNamespaces, OwnNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: nil,
			},
		},
		{
			name:                  "accepts no watchNamespace when install modes {AllNamespaces, OwnNamespace, MultiNamespace} and watchNamespace is nil",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace, v1alpha1.InstallModeTypeMultiNamespace},
			rawConfig:             []byte(`{}`),
			installNamespace:      "not-some-namespace",
			expectedConfig: &bundle.Config{
				WatchNamespace: nil,
			},
		},
		{
			name:                  "rejects with format error when install modes are {SingleNamespace, OwnNamespace} and watchNamespace is ''",
			supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeSingleNamespace, v1alpha1.InstallModeTypeOwnNamespace},
			rawConfig:             []byte(`{"watchNamespace": ""}`),
			installNamespace:      "not-some-namespace",
			expectedErrMessage:    "invalid 'watchNamespace' \"\": namespace must consist of lower case alphanumeric characters",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rv1 bundle.RegistryV1
			if tc.supportedInstallModes != nil {
				rv1 = bundle.RegistryV1{
					CSV: clusterserviceversion.Builder().
						WithName("test-operator").
						WithInstallModeSupportFor(tc.supportedInstallModes...).
						Build(),
				}
			}

			config, err := bundle.UnmarshalConfig(tc.rawConfig, rv1, tc.installNamespace)
			require.Equal(t, tc.expectedConfig, config)
			if tc.expectedErrMessage != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
