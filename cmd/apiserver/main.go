/*
Copyright 2022.

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

package main

import (
	"k8s.io/klog"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"

	// +kubebuilder:scaffold:resource-imports

	corev1beta1 "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
)

// TODO: We can't properly set the version for the APIServer using the apiserver-runtime
// package because they hardcode the version here: https://github.com/kubernetes-sigs/apiserver-runtime/blob/33c90185692756252ad3e36c5a940167d0de8f41/internal/sample-apiserver/pkg/apiserver/apiserver.go#L86-L89
// To be able to update this we would need to create a PR to fix it OR create the apiserver w/o using the apiserver-runtime tooling

func main() {
	err := builder.APIServer.
		// +kubebuilder:scaffold:resource-register
		WithResource(&corev1beta1.Package{}).
		WithResource(&corev1beta1.BundleMetadata{}).
		WithResource(&corev1beta1.CatalogSource{}).
		Execute()
	if err != nil {
		klog.Fatal(err)
	}
}
