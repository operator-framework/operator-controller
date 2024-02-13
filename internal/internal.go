/*
Copyright 2024.

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

package internal

import (
	"k8s.io/apimachinery/pkg/types"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type ExtensionInterface interface {
	GetPackageSpec() *ocv1alpha1.ExtensionSourcePackage
	GetUpgradeConstraintPolicy() ocv1alpha1.UpgradeConstraintPolicy
	GetUID() types.UID
}

func ExtensionArrayToInterface(in []ocv1alpha1.Extension) []ExtensionInterface {
	ei := make([]ExtensionInterface, len(in))
	for i := range in {
		ei[i] = &in[i]
	}
	return ei
}

func ClusterExtensionArrayToInterface(in []ocv1alpha1.ClusterExtension) []ExtensionInterface {
	ei := make([]ExtensionInterface, len(in))
	for i := range in {
		ei[i] = &in[i]
	}
	return ei
}
