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

// CatalogCache
// +k8s:openapi-gen=true
type CatalogCache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CatalogCacheSpec   `json:"spec,omitempty"`
	Status CatalogCacheStatus `json:"status,omitempty"`
}

// CatalogCacheList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CatalogCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []CatalogCache `json:"items"`
}

// APIKey
type APIKey struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// Operator
type Operator struct {
	Name         string   `json:"name"`
	Package      string   `json:"package"`
	Version      string   `json:"version"`
	ProvidedAPIs []APIKey `json:"providedapis"`
	RequiredAPIs []APIKey `json:"requiredapis,omitempty"`
	BundlePath   string   `json:"bundlepath"`
}

// CatalogCacheSpec defines the desired state of CatalogCache
type CatalogCacheSpec struct {
	Operators []Operator `json:"operators,omitempty"`
}

var _ resource.Object = &CatalogCache{}
var _ resourcestrategy.Validater = &CatalogCache{}

func (in *CatalogCache) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *CatalogCache) NamespaceScoped() bool {
	return false
}

func (in *CatalogCache) New() runtime.Object {
	return &CatalogCache{}
}

func (in *CatalogCache) NewList() runtime.Object {
	return &CatalogCacheList{}
}

func (in *CatalogCache) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "core.rukpak.io",
		Version:  "v1beta1",
		Resource: "catalogcaches",
	}
}

func (in *CatalogCache) IsStorageVersion() bool {
	return true
}

func (in *CatalogCache) Validate(ctx context.Context) field.ErrorList {
	// TODO(user): Modify it, adding your API validation here.
	return nil
}

var _ resource.ObjectList = &CatalogCacheList{}

func (in *CatalogCacheList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

// CatalogCacheStatus defines the observed state of CatalogCache
type CatalogCacheStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (in CatalogCacheStatus) SubResourceName() string {
	return "status"
}

// CatalogCache implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &CatalogCache{}

func (in *CatalogCache) GetStatus() resource.StatusSubResource {
	return in.Status
}

// CatalogCacheStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &CatalogCacheStatus{}

func (in CatalogCacheStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*CatalogCache).Status = in
}
