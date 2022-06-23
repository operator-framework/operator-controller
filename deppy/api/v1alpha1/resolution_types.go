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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResolutionSpec defines the desired state of Resolution
type ResolutionSpec struct {
	Constraints []Constraint `json:"constraints,omitempty"`
}

// ResolutionStatus defines the observed state of Resolution
type ResolutionStatus struct {
	// +nullable
	// +optional
	IDs []string `json:"ids,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// Resolution is the Schema for the resolutions API
type Resolution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResolutionSpec   `json:"spec,omitempty"`
	Status ResolutionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ResolutionList contains a list of Resolution
type ResolutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Resolution `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Resolution{}, &ResolutionList{})
}
