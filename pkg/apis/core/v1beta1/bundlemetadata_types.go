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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster

// BundleMetadata is the Schema for the bundlemetadata API
type BundleMetadata struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BundleMetadataSpec   `json:"spec,omitempty"`
	Status BundleMetadataStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BundleMetadataList contains a list of BundleMetadata
type BundleMetadataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []BundleMetadata `json:"items"`
}

// BundleMetadataSpec defines the desired state of BundleMetadata
type BundleMetadataSpec struct {
	// CatalogSource is the name of the Catalog that provides this bundle
	CatalogSource string `json:"catalogSource"`

	// Package is the name of the package that provides this bundle
	Package string `json:"package"`

	// Image is a reference to the image that provides the bundle contents
	Image string `json:"image"`

	// Properties is a string of references to property objects that are part of the bundle
	Properties []Property `json:"properties"`

	// RelatedImages are the RelatedImages in the bundle
	RelatedImages []RelatedImage `json:"relatedImages"`
}

type Property struct {
	Type string `json:"type"`

	// +kubebuilder:pruning:PreserveUnknownFields
	Value runtime.RawExtension `json:"value"`
}

// TODO: In the future we should remove this in favor of using `declcfg.RelatedImage` (or similar) from
// https://pkg.go.dev/github.com/operator-framework/operator-registry@v1.26.3/alpha/declcfg#RelatedImage
// This will likely require some changes to the `declcfg.RelatedImage` type
// to make it suitable for usage within the Spec for a CustomResource
type RelatedImage struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// BundleMetadataStatus defines the observed state of BundleMetadata
type BundleMetadataStatus struct{}

func init() {
	SchemeBuilder.Register(&BundleMetadata{}, &BundleMetadataList{})
}
