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
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/deppy/pkg/deppy"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

// BundleProvider provides the way to retrieve a list of Bundles from a source,
// generally from a catalog client of some kind.
type BundleProvider interface {
	Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error)
}

type VariableSource struct {
	client        client.Client
	catalogClient BundleProvider
}

func NewVariableSource(cl client.Client, catalogClient BundleProvider) *VariableSource {
	return &VariableSource{
		client:        cl,
		catalogClient: catalogClient,
	}
}

func (v *VariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	operatorList := operatorsv1alpha1.OperatorList{}
	if err := v.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}

	bundleDeploymentList := rukpakv1alpha1.BundleDeploymentList{}
	if err := v.client.List(ctx, &bundleDeploymentList); err != nil {
		return nil, err
	}

	allBundles, err := v.catalogClient.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	requiredPackages, err := variablesources.MakeRequiredPackageVariables(allBundles, operatorList.Items)
	if err != nil {
		return nil, err
	}

	installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, operatorList.Items, bundleDeploymentList.Items)
	if err != nil {
		return nil, err
	}

	bundles, err := variablesources.MakeBundleVariables(allBundles, requiredPackages, installedPackages)
	if err != nil {
		return nil, err
	}

	bundleUniqueness := variablesources.MakeBundleUniquenessVariables(bundles)

	result := []deppy.Variable{}
	for _, v := range requiredPackages {
		result = append(result, v)
	}
	for _, v := range installedPackages {
		result = append(result, v)
	}
	for _, v := range bundles {
		result = append(result, v)
	}
	for _, v := range bundleUniqueness {
		result = append(result, v)
	}
	return result, nil
}
