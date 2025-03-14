package crdupgradesafety_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/preflights/crdupgradesafety"
)

func TestServedVersionValidator(t *testing.T) {
	validationErr1 := errors.New(`version "v1alpha1", field "^" has unknown change, refusing to determine that change is safe`)
	validationErr2 := errors.New(`version upgrade "v1alpha1" to "v1alpha2", field "^": fail`)

	for _, tc := range []struct {
		name                   string
		servedVersionValidator *crdupgradesafety.ServedVersionValidator
		new                    apiextensionsv1.CustomResourceDefinition
		expectedError          error
	}{
		{
			name: "no changes, no error",
			servedVersionValidator: &crdupgradesafety.ServedVersionValidator{
				Validations: []crdupgradesafety.ChangeValidation{
					func(_ crdupgradesafety.FieldDiff) (bool, error) {
						return false, errors.New("should not run")
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
					},
				},
			},
		},
		{
			name: "changes, validation successful, change is fully handled, no error",
			servedVersionValidator: &crdupgradesafety.ServedVersionValidator{
				Validations: []crdupgradesafety.ChangeValidation{
					func(_ crdupgradesafety.FieldDiff) (bool, error) {
						return true, nil
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "changes, validation successful, change not fully handled, error",
			servedVersionValidator: &crdupgradesafety.ServedVersionValidator{
				Validations: []crdupgradesafety.ChangeValidation{
					func(_ crdupgradesafety.FieldDiff) (bool, error) {
						return false, nil
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
			expectedError: validationErr1,
		},
		{
			name: "changes, validation failed, change fully handled, error",
			servedVersionValidator: &crdupgradesafety.ServedVersionValidator{
				Validations: []crdupgradesafety.ChangeValidation{
					func(_ crdupgradesafety.FieldDiff) (bool, error) {
						return true, errors.New("fail")
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
			expectedError: validationErr2,
		},
		{
			name: "changes, validation failed, change not fully handled, ordered error",
			servedVersionValidator: &crdupgradesafety.ServedVersionValidator{
				Validations: []crdupgradesafety.ChangeValidation{
					func(_ crdupgradesafety.FieldDiff) (bool, error) {
						return false, errors.New("fail")
					},
					func(_ crdupgradesafety.FieldDiff) (bool, error) {
						return false, errors.New("error")
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
			expectedError: fmt.Errorf("%w\n%s\n%w", validationErr2, `version upgrade "v1alpha1" to "v1alpha2", field "^": error`, validationErr1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.servedVersionValidator.Validate(apiextensionsv1.CustomResourceDefinition{}, tc.new)
			if tc.expectedError != nil {
				assert.EqualError(t, err, tc.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
