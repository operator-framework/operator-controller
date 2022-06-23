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

// InputSpec defines the desired state of Input
type InputSpec struct {
	InputClassName string       `json:"inputClassName"`
	Constraints    []Constraint `json:"constraints,omitempty"`
	Properties     []Property   `json:"properties,omitempty"`
}

type Constraint struct {
	Type string `json:"type"`
	//+kubebuilder:validation:Schemaless
	//+kubebuilder:validation:XPreserveUnknownFields
	Value map[string]string `json:"value"`
}

type Property struct {
	Type string `json:"type"`
	//+kubebuilder:validation:Schemaless
	//+kubebuilder:validation:XPreserveUnknownFields
	Value map[string]string `json:"value"`
}

// InputStatus defines the observed state of Input
type InputStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// Input is the Schema for the inputs API
type Input struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InputSpec   `json:"spec,omitempty"`
	Status InputStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// InputList contains a list of Input
type InputList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Input `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Input{}, &InputList{})
}
