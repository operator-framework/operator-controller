// Originally copied from https://github.com/carvel-dev/kapp/tree/d7fc2e15439331aa3a379485bb124e91a0829d2e
// Attribution:
//   Copyright 2024 The Carvel Authors.
//   SPDX-License-Identifier: Apache-2.0

package crdupgradesafety

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestValidator(t *testing.T) {
	for _, tc := range []struct {
		name        string
		validations []Validation
		shouldErr   bool
	}{
		{
			name:        "no validators, no error",
			validations: []Validation{},
		},
		{
			name: "passing validator, no error",
			validations: []Validation{
				NewValidationFunc("pass", func(_, _ apiextensionsv1.CustomResourceDefinition) error {
					return nil
				}),
			},
		},
		{
			name: "failing validator, error",
			validations: []Validation{
				NewValidationFunc("fail", func(_, _ apiextensionsv1.CustomResourceDefinition) error {
					return errors.New("boom")
				}),
			},
			shouldErr: true,
		},
		{
			name: "passing+failing validator, error",
			validations: []Validation{
				NewValidationFunc("pass", func(_, _ apiextensionsv1.CustomResourceDefinition) error {
					return nil
				}),
				NewValidationFunc("fail", func(_, _ apiextensionsv1.CustomResourceDefinition) error {
					return errors.New("boom")
				}),
			},
			shouldErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Validator{
				Validations: tc.validations,
			}
			var o, n apiextensionsv1.CustomResourceDefinition

			err := v.Validate(o, n)
			require.Equal(t, tc.shouldErr, err != nil)
		})
	}
}

func TestNoScopeChange(t *testing.T) {
	for _, tc := range []struct {
		name        string
		old         apiextensionsv1.CustomResourceDefinition
		new         apiextensionsv1.CustomResourceDefinition
		shouldError bool
	}{
		{
			name: "no scope change, no error",
			old: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Scope: apiextensionsv1.ClusterScoped,
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Scope: apiextensionsv1.ClusterScoped,
				},
			},
		},
		{
			name: "scope change, error",
			old: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Scope: apiextensionsv1.ClusterScoped,
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Scope: apiextensionsv1.NamespaceScoped,
				},
			},
			shouldError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NoScopeChange(tc.old, tc.new)
			require.Equal(t, tc.shouldError, err != nil)
		})
	}
}

func TestNoStoredVersionRemoved(t *testing.T) {
	for _, tc := range []struct {
		name        string
		old         apiextensionsv1.CustomResourceDefinition
		new         apiextensionsv1.CustomResourceDefinition
		shouldError bool
	}{
		{
			name: "no stored versions, no error",
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
						},
					},
				},
			},
			old: apiextensionsv1.CustomResourceDefinition{},
		},
		{
			name: "stored versions, no stored version removed, no error",
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
						},
						{
							Name: "v1alpha2",
						},
					},
				},
			},
			old: apiextensionsv1.CustomResourceDefinition{
				Status: apiextensionsv1.CustomResourceDefinitionStatus{
					StoredVersions: []string{
						"v1alpha1",
					},
				},
			},
		},
		{
			name: "stored versions, stored version removed, error",
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha2",
						},
					},
				},
			},
			old: apiextensionsv1.CustomResourceDefinition{
				Status: apiextensionsv1.CustomResourceDefinitionStatus{
					StoredVersions: []string{
						"v1alpha1",
					},
				},
			},
			shouldError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NoStoredVersionRemoved(tc.old, tc.new)
			require.Equal(t, tc.shouldError, err != nil)
		})
	}
}

func TestNoExistingFieldRemoved(t *testing.T) {
	for _, tc := range []struct {
		name        string
		new         apiextensionsv1.CustomResourceDefinition
		old         apiextensionsv1.CustomResourceDefinition
		shouldError bool
	}{
		{
			name: "no existing field removed, no error",
			old: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "existing field removed, error",
			old: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
										"fieldTwo": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
			shouldError: true,
		},
		{
			name: "new version is added with the field removed, no error",
			old: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
										"fieldTwo": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
										"fieldTwo": {
											Type: "string",
										},
									},
								},
							},
						},
						{
							Name: "v1alpha2",
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"fieldOne": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NoExistingFieldRemoved(tc.old, tc.new)
			assert.Equal(t, tc.shouldError, err != nil)
		})
	}
}
