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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource/resourcestrategy"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BundleMetadata
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Cluster
type BundleMetadata struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BundleMetadataSpec   `json:"spec,omitempty"`
	Status BundleMetadataStatus `json:"status,omitempty"`
}

// BundleMetadataList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type BundleMetadataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []BundleMetadata `json:"items"`
}

// BundleMetadataSpec defines the desired state of BundleMetadata
type BundleMetadataSpec struct {
	// CatalogSource is the name of the CatalogSource that provides this bundle
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

// TODO: In the future we should remove this in favor of using `property.Property` from
// https://pkg.go.dev/github.com/operator-framework/operator-registry@v1.26.3/alpha/property#Property
// This will likely require some changes to the `property.Property` type to
// make it suitable for usage within the Spec for a CustomResource
type Property struct {
	Type  string `json:"type"`
	Value []byte `json:"value"`
}

// TODO: In the future we should remove this in favor of using `model.RelatedImage` (or similar) from
// https://pkg.go.dev/github.com/operator-framework/operator-registry@v1.26.3/alpha/model#RelatedImage
// This will likely require some changes to the `model.RelatedImage` type
// to make it suitable for usage within the Spec for a CustomResource
type RelatedImage struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

var _ resource.Object = &BundleMetadata{}
var _ resourcestrategy.Validater = &BundleMetadata{}

func (in *BundleMetadata) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *BundleMetadata) NamespaceScoped() bool {
	return false
}

func (in *BundleMetadata) New() runtime.Object {
	return &BundleMetadata{}
}

func (in *BundleMetadata) NewList() runtime.Object {
	return &BundleMetadataList{}
}

func (in *BundleMetadata) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "catalogd.operatorframework.io",
		Version:  "v1beta1",
		Resource: "bundlemetadata",
	}
}

func (in *BundleMetadata) IsStorageVersion() bool {
	return true
}

func (in *BundleMetadata) Validate(ctx context.Context) field.ErrorList {
	// TODO(user): Modify it, adding your API validation here.
	return nil
}

var _ resource.ObjectList = &BundleMetadataList{}

func (in *BundleMetadataList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

// BundleMetadataStatus defines the observed state of BundleMetadata
type BundleMetadataStatus struct{}

func (in BundleMetadataStatus) SubResourceName() string {
	return "status"
}

// BundleMetadata implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &BundleMetadata{}

func (in *BundleMetadata) GetStatus() resource.StatusSubResource {
	return in.Status
}

// BundleMetadataStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &BundleMetadataStatus{}

func (in BundleMetadataStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*BundleMetadata).Status = in
}
