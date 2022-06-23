package crd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/rukpak/internal/unit"
	"github.com/operator-framework/rukpak/test/testutil"
)

func TestValidate(t *testing.T) {
	// Setup envtest client
	kubeclient, err := unit.SetupClient()
	require.NoError(t, err, "failed to create kube client")

	// Setup defaults for eventually calls
	defaultWaitPeriod := time.Second * 15
	defaultTick := time.Second * 1

	for _, tt := range []struct {
		name                string
		errString           string
		existingCrVersion   string
		existingCrdVersions []apiextensionsv1.CustomResourceDefinitionVersion
		newCrdVersions      []apiextensionsv1.CustomResourceDefinitionVersion
	}{
		{
			name:              "validates a safe crd upgrade",
			errString:         "",
			existingCrVersion: "v1alpha1",
			existingCrdVersions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
						},
					},
				},
			},
			newCrdVersions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
						},
					},
				},
				{
					Name:    "v1alpha2",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
						},
					},
				},
			},
		},
		{
			name:              "invalidates a crd upgrade that removes a stored version",
			errString:         "cannot remove stored versions",
			existingCrVersion: "v1alpha1",
			existingCrdVersions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
						},
					},
				},
			},
			newCrdVersions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha2",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
						},
					},
				},
			},
		},
		{
			name:              "invalidates a crd upgrade that breaks an existing cr",
			errString:         "failed validation for new schema",
			existingCrVersion: "v1alpha1",
			existingCrdVersions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"sampleProperty": {Type: "string"},
							},
						},
					},
				},
			},
			newCrdVersions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:        "object",
							Description: "my crd schema",
							Required:    []string{"sampleProperty"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"sampleProperty": {Type: "string"},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			defer ctx.Done()

			// Create needed structs for testing using uniquely generated names
			existingCrd := testutil.NewTestingCRD("", testutil.DefaultGroup, tt.existingCrdVersions)
			uniqueName := existingCrd.Spec.Names.Singular
			existingCr := testutil.NewTestingCR(testutil.DefaultCrName, testutil.DefaultGroup, tt.existingCrVersion, uniqueName)
			newCrd := testutil.NewTestingCRD(uniqueName, testutil.DefaultGroup, tt.newCrdVersions)

			// Create existing CRD and wait for it to be ready
			err := kubeclient.Create(ctx, existingCrd)
			require.NoError(t, err, "failed to create initial crd for testing")
			require.Eventuallyf(t, func() bool {
				if err := kubeclient.Get(ctx, client.ObjectKeyFromObject(existingCrd), existingCrd); err != nil {
					return false
				}
				return testutil.CrdReady(&existingCrd.Status)
			}, defaultWaitPeriod, defaultTick, "failed to get initial crd for testing: %v", err)

			// Creating existing CR and wait for it to be created
			err = kubeclient.Create(ctx, existingCr)
			require.NoError(t, err, "failed to create initial cr for testing")
			require.Eventuallyf(t, func() bool {
				return kubeclient.Get(ctx, client.ObjectKeyFromObject(existingCr), existingCr) == nil
			}, defaultWaitPeriod, defaultTick, "failed to get initial cr for testing: %v", err)

			// Run Validate and check that the error is not nil and we were not expecting it to be nil
			err = Validate(ctx, kubeclient, newCrd)
			if err != nil && tt.errString != "" {
				require.ErrorContains(t, err, tt.errString)
			}

			// Cleanup resources
			err = kubeclient.Delete(ctx, existingCr)
			require.NoError(t, err, "failed to delete initial cr for testing")
			err = kubeclient.Delete(ctx, existingCrd)
			require.NoError(t, err, "failed to delete initial crd for testing")
		})
	}
}
