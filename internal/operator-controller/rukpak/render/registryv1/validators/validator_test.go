package validators_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/validators"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

func Test_CheckDeploymentSpecUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with unique deployment strategy spec names",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
					),
				),
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with duplicate deployment strategy spec names",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service version contains duplicate strategy deployment spec 'test-deployment-one'"),
			},
		}, {
			name: "errors are ordered by deployment strategy spec name",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-a"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-b"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-c"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-b"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-a"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service version contains duplicate strategy deployment spec 'test-deployment-a'"),
				errors.New("cluster service version contains duplicate strategy deployment spec 'test-deployment-b'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckDeploymentSpecUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckDeploymentNameIsDNS1123SubDomain(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts valid deployment strategy spec names",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
					),
				),
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with invalid deployment strategy spec names - errors are sorted by name",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "-bad-name"},
						v1alpha1.StrategyDeploymentSpec{Name: "b-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long"},
						v1alpha1.StrategyDeploymentSpec{Name: "a-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long-and-bad-"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("invalid cluster service version strategy deployment name '-bad-name': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"),
				errors.New("invalid cluster service version strategy deployment name 'a-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long-and-bad-': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'), must be no more than 253 characters"),
				errors.New("invalid cluster service version strategy deployment name 'b-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long': must be no more than 253 characters"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckDeploymentNameIsDNS1123SubDomain(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CRDResourceUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with unique custom resource definition resources",
			bundle: &bundle.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{
					{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "b.crd.something"}},
				},
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with duplicate custom resource definition resources",
			bundle: &bundle.RegistryV1{CRDs: []apiextensionsv1.CustomResourceDefinition{
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
			}},
			expectedErrs: []error{
				errors.New("bundle contains duplicate custom resource definition 'a.crd.something'"),
			},
		}, {
			name: "errors are ordered by custom resource definition name",
			bundle: &bundle.RegistryV1{CRDs: []apiextensionsv1.CustomResourceDefinition{
				{ObjectMeta: metav1.ObjectMeta{Name: "c.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "c.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
			}},
			expectedErrs: []error{
				errors.New("bundle contains duplicate custom resource definition 'a.crd.something'"),
				errors.New("bundle contains duplicate custom resource definition 'c.crd.something'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validators.CheckCRDResourceUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, err)
		})
	}
}

func Test_CheckOwnedCRDExistence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with existing owned custom resource definition resources",
			bundle: &bundle.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{
					{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "b.crd.something"}},
				},
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "a.crd.something"},
						v1alpha1.CRDDescription{Name: "b.crd.something"},
					),
				),
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with missing owned custom resource definition resources",
			bundle: &bundle.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{},
				CSV: MakeCSV(
					WithOwnedCRDs(v1alpha1.CRDDescription{Name: "a.crd.something"}),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service definition references owned custom resource definition 'a.crd.something' not found in bundle"),
			},
		}, {
			name: "errors are ordered by owned custom resource definition name",
			bundle: &bundle.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{},
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "a.crd.something"},
						v1alpha1.CRDDescription{Name: "c.crd.something"},
						v1alpha1.CRDDescription{Name: "b.crd.something"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service definition references owned custom resource definition 'a.crd.something' not found in bundle"),
				errors.New("cluster service definition references owned custom resource definition 'b.crd.something' not found in bundle"),
				errors.New("cluster service definition references owned custom resource definition 'c.crd.something' not found in bundle"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckOwnedCRDExistence(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckPackageNameNotEmpty(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with non-empty package name",
			bundle: &bundle.RegistryV1{
				PackageName: "not-empty",
			},
		}, {
			name:   "rejects bundles with empty package name",
			bundle: &bundle.RegistryV1{},
			expectedErrs: []error{
				errors.New("package name is empty"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckPackageNameNotEmpty(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckWebhookSupport(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with validating webhook definitions when they only support AllNamespaces install mode",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type: v1alpha1.ValidatingAdmissionWebhook,
						},
					),
				),
			},
		},
		{
			name: "accepts bundles with mutating webhook definitions when they only support AllNamespaces install mode",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type: v1alpha1.MutatingAdmissionWebhook,
						},
					),
				),
			},
		},
		{
			name: "accepts bundles with conversion webhook definitions when they only support AllNamespaces install mode",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type: v1alpha1.ConversionWebhook,
						},
					),
				),
			},
		},
		{
			name: "rejects bundles with validating webhook definitions when they support more modes than AllNamespaces install mode",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type: v1alpha1.ValidatingAdmissionWebhook,
						},
					),
				),
			},
			expectedErrs: []error{errors.New("bundle contains webhook definitions but supported install modes beyond AllNamespaces")},
		},
		{
			name: "accepts bundles with mutating webhook definitions when they support more modes than AllNamespaces install mode",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type: v1alpha1.MutatingAdmissionWebhook,
						},
					),
				),
			},
			expectedErrs: []error{errors.New("bundle contains webhook definitions but supported install modes beyond AllNamespaces")},
		},
		{
			name: "accepts bundles with conversion webhook definitions when they support more modes than AllNamespaces install mode",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type: v1alpha1.ConversionWebhook,
						},
					),
				),
			},
			expectedErrs: []error{errors.New("bundle contains webhook definitions but supported install modes beyond AllNamespaces")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckWebhookSupport(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckWebhookDeploymentReferentialIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles where webhook definitions reference existing strategy deployment specs",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							GenerateName:   "test-webhook",
							DeploymentName: "test-deployment-one",
						},
					),
				),
			},
		}, {
			name: "rejects bundles with webhook definitions that reference non-existing strategy deployment specs",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							GenerateName:   "test-webhook",
							DeploymentName: "test-deployment-two",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("webhook of type 'ValidatingAdmissionWebhook' with name 'test-webhook' references non-existent deployment 'test-deployment-two'"),
			},
		}, {
			name: "errors are ordered by deployment strategy spec name, webhook type, and webhook name",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							GenerateName:   "test-val-webhook-c",
							DeploymentName: "test-deployment-c",
						},
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							GenerateName:   "test-mute-webhook-a",
							DeploymentName: "test-deployment-a",
						},
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							GenerateName:   "test-conv-webhook-b",
							DeploymentName: "test-deployment-b",
						}, v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							GenerateName:   "test-mute-webhook-c",
							DeploymentName: "test-deployment-c",
						},
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							GenerateName:   "test-conv-webhook-c-b",
							DeploymentName: "test-deployment-c",
						}, v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							GenerateName:   "test-conv-webhook-c-a",
							DeploymentName: "test-deployment-c",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("webhook of type 'MutatingAdmissionWebhook' with name 'test-mute-webhook-a' references non-existent deployment 'test-deployment-a'"),
				errors.New("webhook of type 'ConversionWebhook' with name 'test-conv-webhook-b' references non-existent deployment 'test-deployment-b'"),
				errors.New("webhook of type 'ConversionWebhook' with name 'test-conv-webhook-c-a' references non-existent deployment 'test-deployment-c'"),
				errors.New("webhook of type 'ConversionWebhook' with name 'test-conv-webhook-c-b' references non-existent deployment 'test-deployment-c'"),
				errors.New("webhook of type 'MutatingAdmissionWebhook' with name 'test-mute-webhook-c' references non-existent deployment 'test-deployment-c'"),
				errors.New("webhook of type 'ValidatingAdmissionWebhook' with name 'test-val-webhook-c' references non-existent deployment 'test-deployment-c'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckWebhookDeploymentReferentialIntegrity(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckWebhookNameUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles without webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(),
			},
		}, {
			name: "accepts bundles with unique webhook names",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-webhook-one",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-webhook-two",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-three",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-webhook-four",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-webhook-five",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-six",
						},
					),
				),
			},
		}, {
			name: "accepts bundles with webhooks with the same name but different types",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-webhook",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-webhook",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
						},
					),
				),
			},
		}, {
			name: "rejects bundles with duplicate validating webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-webhook",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-webhook",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("duplicate webhook 'test-webhook' of type 'ValidatingAdmissionWebhook'"),
			},
		}, {
			name: "rejects bundles with duplicate mutating webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-webhook",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-webhook",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("duplicate webhook 'test-webhook' of type 'MutatingAdmissionWebhook'"),
			},
		}, {
			name: "rejects bundles with duplicate conversion webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("duplicate webhook 'test-webhook' of type 'ConversionWebhook'"),
			},
		}, {
			name: "orders errors by webhook type and name",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-val-webhook-b",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-val-webhook-a",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-val-webhook-a",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-val-webhook-b",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-conv-webhook-b",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-conv-webhook-a",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-conv-webhook-a",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-conv-webhook-b",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-mute-webhook-b",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-mute-webhook-a",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-mute-webhook-a",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-mute-webhook-b",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("duplicate webhook 'test-conv-webhook-a' of type 'ConversionWebhook'"),
				errors.New("duplicate webhook 'test-conv-webhook-b' of type 'ConversionWebhook'"),
				errors.New("duplicate webhook 'test-mute-webhook-a' of type 'MutatingAdmissionWebhook'"),
				errors.New("duplicate webhook 'test-mute-webhook-b' of type 'MutatingAdmissionWebhook'"),
				errors.New("duplicate webhook 'test-val-webhook-a' of type 'ValidatingAdmissionWebhook'"),
				errors.New("duplicate webhook 'test-val-webhook-b' of type 'ValidatingAdmissionWebhook'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckWebhookNameUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckConversionWebhooksReferenceOwnedCRDs(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name:   "accepts bundles without webhook definitions",
			bundle: &bundle.RegistryV1{},
		}, {
			name: "accepts bundles without conversion webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-val-webhook",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-mute-webhook",
						},
					),
				),
			},
		}, {
			name: "accepts bundles with conversion webhooks that reference owned CRDs",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "some.crd.something"},
						v1alpha1.CRDDescription{Name: "another.crd.something"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
							ConversionCRDs: []string{
								"some.crd.something",
								"another.crd.something",
							},
						},
					),
				),
			},
		}, {
			name: "rejects bundles with conversion webhooks that reference existing CRDs that are not owned",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "some.crd.something"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
							ConversionCRDs: []string{
								"some.crd.something",
								"another.crd.something",
							},
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("conversion webhook 'test-webhook' references custom resource definition 'another.crd.something' not owned bundle"),
			},
		}, {
			name: "errors are ordered by webhook name and CRD name",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "b.crd.something"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-b",
							ConversionCRDs: []string{
								"b.crd.something",
							},
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-a",
							ConversionCRDs: []string{
								"c.crd.something",
								"a.crd.something",
							},
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-c",
							ConversionCRDs: []string{
								"a.crd.something",
								"d.crd.something",
							},
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("conversion webhook 'test-webhook-a' references custom resource definition 'a.crd.something' not owned bundle"),
				errors.New("conversion webhook 'test-webhook-a' references custom resource definition 'c.crd.something' not owned bundle"),
				errors.New("conversion webhook 'test-webhook-c' references custom resource definition 'a.crd.something' not owned bundle"),
				errors.New("conversion webhook 'test-webhook-c' references custom resource definition 'd.crd.something' not owned bundle"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckConversionWebhooksReferenceOwnedCRDs(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckConversionWebhookCRDReferenceUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name:         "accepts bundles without webhook definitions",
			bundle:       &bundle.RegistryV1{},
			expectedErrs: []error{},
		},
		{
			name: "accepts bundles without conversion webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "test-val-webhook",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "test-mute-webhook",
						},
					),
				),
			},
			expectedErrs: []error{},
		},
		{
			name: "accepts bundles with conversion webhooks that reference different CRDs",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "some.crd.something"},
						v1alpha1.CRDDescription{Name: "another.crd.something"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
							ConversionCRDs: []string{
								"some.crd.something",
							},
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-2",
							ConversionCRDs: []string{
								"another.crd.something",
							},
						},
					),
				),
			},
			expectedErrs: []error{},
		},
		{
			name: "rejects bundles with conversion webhooks that reference the same CRD",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "some.crd.something"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook",
							ConversionCRDs: []string{
								"some.crd.something",
							},
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-two",
							ConversionCRDs: []string{
								"some.crd.something",
							},
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("conversion webhooks [test-webhook,test-webhook-two] reference same custom resource definition 'some.crd.something'"),
			},
		},
		{
			name: "errors are ordered by CRD name and webhook names",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithOwnedCRDs(
						v1alpha1.CRDDescription{Name: "b.crd.something"},
					),
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-b",
							ConversionCRDs: []string{
								"b.crd.something",
								"a.crd.something",
							},
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-a",
							ConversionCRDs: []string{
								"d.crd.something",
								"a.crd.something",
								"b.crd.something",
							},
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "test-webhook-c",
							ConversionCRDs: []string{
								"b.crd.something",
								"d.crd.something",
							},
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("conversion webhooks [test-webhook-a,test-webhook-b] reference same custom resource definition 'a.crd.something'"),
				errors.New("conversion webhooks [test-webhook-a,test-webhook-b,test-webhook-c] reference same custom resource definition 'b.crd.something'"),
				errors.New("conversion webhooks [test-webhook-a,test-webhook-c] reference same custom resource definition 'd.crd.something'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckConversionWebhookCRDReferenceUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckWebhookNameIsDNS1123SubDomain(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *bundle.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles without webhook definitions",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(),
			},
		}, {
			name: "rejects bundles with invalid webhook definitions names and orders errors by webhook type and name",
			bundle: &bundle.RegistryV1{
				CSV: MakeCSV(
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "b-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long-and-bad-",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "a-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ValidatingAdmissionWebhook,
							GenerateName: "-bad-name",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "b-bad-name-",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "b-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long-and-bad-",
						}, v1alpha1.WebhookDescription{
							Type:         v1alpha1.MutatingAdmissionWebhook,
							GenerateName: "a-bad-name-",
						},
						v1alpha1.WebhookDescription{
							Type:         v1alpha1.ConversionWebhook,
							GenerateName: "a-bad-name-",
						},
					),
				),
			},
			expectedErrs: []error{
				errors.New("webhook of type 'ConversionWebhook' has invalid name 'a-bad-name-': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"),
				errors.New("webhook of type 'ConversionWebhook' has invalid name 'b-bad-name-': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"),
				errors.New("webhook of type 'MutatingAdmissionWebhook' has invalid name 'a-bad-name-': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"),
				errors.New("webhook of type 'MutatingAdmissionWebhook' has invalid name 'b-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long-and-bad-': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'),must be no more than 253 characters"),
				errors.New("webhook of type 'ValidatingAdmissionWebhook' has invalid name '-bad-name': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"),
				errors.New("webhook of type 'ValidatingAdmissionWebhook' has invalid name 'a-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long': must be no more than 253 characters"),
				errors.New("webhook of type 'ValidatingAdmissionWebhook' has invalid name 'b-name-is-waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long-and-bad-': a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'),must be no more than 253 characters"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validators.CheckWebhookNameIsDNS1123SubDomain(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}
