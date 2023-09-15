/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/deppy/pkg/deppy/input"

	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func NewVariableSource(cl client.Client, catalog catalogclient.CatalogClient) variablesources.NestedVariableSource {
	return variablesources.NestedVariableSource{
		func(inputVariableSource input.VariableSource) (input.VariableSource, error) {
			return variablesources.NewOperatorVariableSource(cl, catalog, inputVariableSource), nil
		},
		func(inputVariableSource input.VariableSource) (input.VariableSource, error) {
			return variablesources.NewBundleDeploymentVariableSource(cl, catalog, inputVariableSource), nil
		},
		func(inputVariableSource input.VariableSource) (input.VariableSource, error) {
			return variablesources.NewBundlesAndDepsVariableSource(catalog, inputVariableSource), nil
		},
		func(inputVariableSource input.VariableSource) (input.VariableSource, error) {
			return variablesources.NewCRDUniquenessConstraintsVariableSource(inputVariableSource), nil
		},
	}
}
