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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterExtensionRevisionSpec defines the desired state of ClusterExtensionRevision
type ClusterExtensionRevisionSpec struct {
	// clusterExtensionRef is a required reference to the ClusterExtension
	// that this revision represents an available upgrade for.
	//
	// +kubebuilder:validation:Required
	ClusterExtensionRef ClusterExtensionReference `json:"clusterExtensionRef"`

	// version is a required field that specifies the exact version of the bundle
	// that represents this available upgrade.
	//
	// version follows the semantic versioning standard as defined in https://semver.org/.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.matches(\"^([0-9]+)(\\\\.[0-9]+)?(\\\\.[0-9]+)?(-([-0-9A-Za-z]+(\\\\.[-0-9A-Za-z]+)*))?(\\\\+([-0-9A-Za-z]+(-\\\\.[-0-9A-Za-z]+)*))?\")",message="version must be well-formed semver"
	Version string `json:"version"`
	// bundleMetadata contains the complete metadata for the bundle that represents
	// this available upgrade.
	//
	// +kubebuilder:validation:Required
	BundleMetadata BundleMetadata `json:"bundleMetadata"`

	// availableSince indicates when this upgrade revision was first detected
	// as being available. This helps track how long an upgrade has been pending.
	//
	// +kubebuilder:validation:Required
	AvailableSince metav1.Time `json:"availableSince"`

	// approved indicates whether this upgrade revision has been approved for execution.
	// When set to true, the controller will automatically update the corresponding
	// ClusterExtension to trigger the upgrade to this version.
	//
	// +optional
	Approved bool `json:"approved,omitempty"`

	// approvedAt indicates when this upgrade revision was approved for execution.
	// This field is set automatically when the approved field changes from false to true.
	//
	// +optional
	ApprovedAt *metav1.Time `json:"approvedAt,omitempty"`
}

// ClusterExtensionReference identifies a ClusterExtension
type ClusterExtensionReference struct {
	// name is the name of the ClusterExtension
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength:=253
	// +kubebuilder:validation:XValidation:rule="self.matches(\"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$\")",message="name must be a valid DNS1123 subdomain"
	Name string `json:"name"`
}

// ClusterExtensionRevisionStatus defines the observed state of ClusterExtensionRevision
type ClusterExtensionRevisionStatus struct {
	// conditions represent the latest available observations of the ClusterExtensionRevision's current state.
	//
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Extension",type=string,JSONPath=".spec.clusterExtensionRef.name"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version"
// +kubebuilder:printcolumn:name="Approved",type=boolean,JSONPath=".spec.approved"
// +kubebuilder:printcolumn:name="Approved At",type=date,JSONPath=".spec.approvedAt"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ClusterExtensionRevision represents an available upgrade for a ClusterExtension.
// It is created automatically by the operator-controller when new versions become
// available in catalogs that represent valid upgrade paths for installed ClusterExtensions.
type ClusterExtensionRevision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the available upgrade revision details.
	//
	// +kubebuilder:validation:Required
	Spec ClusterExtensionRevisionSpec `json:"spec"`

	// status represents the current status of this ClusterExtensionRevision.
	//
	// +optional
	Status ClusterExtensionRevisionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterExtensionRevisionList contains a list of ClusterExtensionRevision
type ClusterExtensionRevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterExtensionRevision `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterExtensionRevision{}, &ClusterExtensionRevisionList{})
}
