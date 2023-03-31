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

// CatalogSource
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Cluster
type CatalogSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CatalogSourceSpec   `json:"spec,omitempty"`
	Status CatalogSourceStatus `json:"status,omitempty"`
}

// CatalogSourceList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CatalogSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []CatalogSource `json:"items"`
}

// CatalogSourceSpec defines the desired state of CatalogSource
type CatalogSourceSpec struct {

	// Image is the Catalog image that contains Operators' metadata in the FBC format
	// https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs
	Image string `json:"image"`

	// PollingInterval is used to determine the time interval between checks of the
	// latest index image version. The image is polled to see if a new version of the
	// image is available. If available, the latest image is pulled and the cache is
	// updated to contain the new content.
	PollingInterval *metav1.Duration `json:"pollingInterval,omitempty"`
}

var _ resource.Object = &CatalogSource{}
var _ resourcestrategy.Validater = &CatalogSource{}

func (in *CatalogSource) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *CatalogSource) NamespaceScoped() bool {
	return false
}

func (in *CatalogSource) New() runtime.Object {
	return &CatalogSource{}
}

func (in *CatalogSource) NewList() runtime.Object {
	return &CatalogSourceList{}
}

func (in *CatalogSource) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "catalogd.operatorframework.io",
		Version:  "v1beta1",
		Resource: "catalogsources",
	}
}

func (in *CatalogSource) IsStorageVersion() bool {
	return true
}

func (in *CatalogSource) Validate(ctx context.Context) field.ErrorList {
	// TODO(user): Modify it, adding your API validation here.
	return nil
}

var _ resource.ObjectList = &CatalogSourceList{}

func (in *CatalogSourceList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

// CatalogSourceStatus defines the observed state of CatalogSource
type CatalogSourceStatus struct {

	// The last time the image has been polled to ensure the image is up-to-date
	LatestImagePoll *metav1.Time `json:"latestImagePoll"`
}

func (in CatalogSourceStatus) SubResourceName() string {
	return "status"
}

// CatalogSource implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &CatalogSource{}

func (in *CatalogSource) GetStatus() resource.StatusSubResource {
	return in.Status
}

// CatalogSourceStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &CatalogSourceStatus{}

func (in CatalogSourceStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*CatalogSource).Status = in
}

// TODO: We should probably move this to a specific errors package
type UnpackPhaseError struct {
	message string
}

func NewUnpackPhaseError(message string) *UnpackPhaseError {
	return &UnpackPhaseError{
		message: message,
	}
}

func (upe *UnpackPhaseError) Error() string {
	return upe.message
}

func IsUnpackPhaseError(err error) bool {
	_, ok := err.(*UnpackPhaseError)
	return ok
}
