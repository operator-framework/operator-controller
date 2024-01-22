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
	"github.com/operator-framework/deppy/pkg/deppy"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func GenerateVariables(allBundles []*catalogmetadata.Bundle, clusterExtensions []ocv1alpha1.ClusterExtension, bundleDeployments []rukpakv1alpha2.BundleDeployment) ([]deppy.Variable, error) {
	requiredPackages, err := variablesources.MakeRequiredPackageVariables(allBundles, clusterExtensions)
	if err != nil {
		return nil, err
	}

	installedPackages, err := variablesources.MakeInstalledPackageVariables(allBundles, clusterExtensions, bundleDeployments)
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
