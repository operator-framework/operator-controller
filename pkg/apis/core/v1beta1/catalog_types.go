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
)

const (
	TypeReady = "Ready"

	ReasonContentsAvailable = "ContentsAvailable"
	ReasonUnpackError       = "UnpackError"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status

// Catalog is the Schema for the Catalogs API
type Catalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CatalogSpec   `json:"spec,omitempty"`
	Status CatalogStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CatalogList contains a list of Catalog
type CatalogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Catalog `json:"items"`
}

// CatalogSpec defines the desired state of Catalog
type CatalogSpec struct {

	// Image is the Catalog image that contains Operators' metadata in the FBC format
	// https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs
	Image string `json:"image"`

	// PollingInterval is used to determine the time interval between checks of the
	// latest index image version. The image is polled to see if a new version of the
	// image is available. If available, the latest image is pulled and the cache is
	// updated to contain the new content.
	PollingInterval *metav1.Duration `json:"pollingInterval,omitempty"`
}

// CatalogStatus defines the observed state of Catalog
type CatalogStatus struct {
	// Conditions store the status conditions of the Catalog instances
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func init() {
	SchemeBuilder.Register(&Catalog{}, &CatalogList{})
}
