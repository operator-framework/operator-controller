package convert_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func Test_BundleValidatorHasAllValidationFns(t *testing.T) {
	expectedValidationFns := []func(v1 *convert.RegistryV1) []error{
		convert.CheckDeploymentSpecUniqueness,
		convert.CheckCRDResourceUniqueness,
		convert.CheckOwnedCRDExistence,
		convert.CheckWebhookDeploymentReferentialIntegrity,
		convert.CheckWebhookNameUniqueness,
		convert.CheckConversionWebhookCRDReferences,
	}
	actualValidationFns := convert.RegistryV1BundleValidator

	require.Equal(t, len(expectedValidationFns), len(actualValidationFns))
	for i := range expectedValidationFns {
		require.Equal(t, reflect.ValueOf(expectedValidationFns[i]).Pointer(), reflect.ValueOf(actualValidationFns[i]).Pointer(), "bundle validator has unexpected validation function")
	}
}

func Test_BundleValidatorCallsAllValidationFnsInOrder(t *testing.T) {
	actual := ""
	validator := convert.BundleValidator{
		func(v1 *convert.RegistryV1) []error {
			actual += "h"
			return nil
		},
		func(v1 *convert.RegistryV1) []error {
			actual += "i"
			return nil
		},
	}
	require.NoError(t, validator.Validate(nil))
	require.Equal(t, "hi", actual)
}

func Test_CheckDeploymentSpecUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with unique deployment strategy spec names",
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
					),
				),
			},
		}, {
			name: "rejects bundles with duplicate deployment strategy spec names",
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			errs := convert.CheckDeploymentSpecUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CRDResourceUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with unique custom resource definition resources",
			bundle: &convert.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{
					{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "b.crd.something"}},
				},
			},
		}, {
			name: "rejects bundles with duplicate custom resource definition resources",
			bundle: &convert.RegistryV1{CRDs: []apiextensionsv1.CustomResourceDefinition{
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
			}},
			expectedErrs: []error{
				errors.New("bundle contains duplicate custom resource definition 'a.crd.something'"),
			},
		}, {
			name: "errors are ordered by custom resource definition name",
			bundle: &convert.RegistryV1{CRDs: []apiextensionsv1.CustomResourceDefinition{
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
			err := convert.CheckCRDResourceUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, err)
		})
	}
}

func Test_CheckOwnedCRDExistence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with existing owned custom resource definition resources",
			bundle: &convert.RegistryV1{
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
		}, {
			name: "rejects bundles with missing owned custom resource definition resources",
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			errs := convert.CheckOwnedCRDExistence(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckWebhookDeploymentReferentialIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles where webhook definitions reference existing strategy deployment specs",
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
				errors.New("webhook 'test-webhook' of type 'ValidatingAdmissionWebhook' references non-existent deployment 'test-deployment-two'"),
			},
		}, {
			name: "errors are ordered by deployment strategy spec name, webhook type, and webhook name",
			bundle: &convert.RegistryV1{
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
				errors.New("webhook 'test-mute-webhook-a' of type 'MutatingAdmissionWebhook' references non-existent deployment 'test-deployment-a'"),
				errors.New("webhook 'test-conv-webhook-b' of type 'ConversionWebhook' references non-existent deployment 'test-deployment-b'"),
				errors.New("webhook 'test-conv-webhook-c-a' of type 'ConversionWebhook' references non-existent deployment 'test-deployment-c'"),
				errors.New("webhook 'test-conv-webhook-c-b' of type 'ConversionWebhook' references non-existent deployment 'test-deployment-c'"),
				errors.New("webhook 'test-mute-webhook-c' of type 'MutatingAdmissionWebhook' references non-existent deployment 'test-deployment-c'"),
				errors.New("webhook 'test-val-webhook-c' of type 'ValidatingAdmissionWebhook' references non-existent deployment 'test-deployment-c'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := convert.CheckWebhookDeploymentReferentialIntegrity(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckWebhookNameUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles without webhook definitions",
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(),
			},
		}, {
			name: "accepts bundles with unique webhook names",
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			errs := convert.CheckWebhookNameUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckConversionWebhookCRDReferences(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name:   "accepts bundles without webhook definitions",
			bundle: &convert.RegistryV1{},
		}, {
			name: "accepts bundles without conversion webhook definitions",
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			bundle: &convert.RegistryV1{
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
			errs := convert.CheckConversionWebhookCRDReferences(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func WithWebhookDefinitions(webhookDefinitions ...v1alpha1.WebhookDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.WebhookDefinitions = webhookDefinitions
	}
}

type CSVOption func(version *v1alpha1.ClusterServiceVersion)

func WithStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = strategyDeploymentSpecs
	}
}

func WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.CustomResourceDefinitions.Owned = crdDesc
	}
}

func MakeCSV(opts ...CSVOption) v1alpha1.ClusterServiceVersion {
	csv := v1alpha1.ClusterServiceVersion{}
	for _, opt := range opts {
		opt(&csv)
	}
	return csv
}
