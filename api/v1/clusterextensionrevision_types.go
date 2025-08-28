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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ClusterExtensionRevisionKind = "ClusterExtensionRevision"

	// Condition Types
	ClusterExtensionRevisionTypeAvailable = "Available"
	ClusterExtensionRevisionTypeSucceeded = "Succeeded"

	// Condition Reasons
	ClusterExtensionRevisionReasonAvailable                 = "Available"
	ClusterExtensionRevisionReasonReconcileFailure          = "ReconcileFailure"
	ClusterExtensionRevisionReasonRevisionValidationFailure = "RevisionValidationFailure"
	ClusterExtensionRevisionReasonPhaseValidationError      = "PhaseValidationError"
	ClusterExtensionRevisionReasonObjectCollisions          = "ObjectCollisions"
	ClusterExtensionRevisionReasonRolloutSuccess            = "RolloutSuccess"
	ClusterExtensionRevisionReasonProbeFailure              = "ProbeFailure"
	ClusterExtensionRevisionReasonIncomplete                = "Incomplete"
	ClusterExtensionRevisionReasonProgressing               = "Progressing"
)

// ClusterExtensionRevisionSpec defines the desired state of ClusterExtensionRevision.
type ClusterExtensionRevisionSpec struct {
	// Specifies the lifecycle state of the ClusterExtensionRevision.
	// +kubebuilder:default="Active"
	// +kubebuilder:validation:Enum=Active;Paused;Archived
	// +kubebuilder:validation:XValidation:rule="oldSelf == 'Active' || oldSelf == 'Paused' || oldSelf == 'Archived' && oldSelf == self", message="can not un-archive"
	LifecycleState ClusterExtensionRevisionLifecycleState `json:"lifecycleState,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="revision is immutable"
	Revision int64 `json:"revision"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || oldSelf.size() == 0", message="phases is immutable"
	Phases []ClusterExtensionRevisionPhase `json:"phases"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="previous is immutable"
	Previous []ClusterExtensionRevisionPrevious `json:"previous,omitempty"`
}

// ClusterExtensionRevisionLifecycleState specifies the lifecycle state of the ClusterExtensionRevision.
type ClusterExtensionRevisionLifecycleState string

const (
	// ClusterExtensionRevisionLifecycleStateActive / "Active" is the default lifecycle state.
	ClusterExtensionRevisionLifecycleStateActive ClusterExtensionRevisionLifecycleState = "Active"
	// ClusterExtensionRevisionLifecycleStatePaused / "Paused" disables reconciliation of the ClusterExtensionRevision.
	// Only Status updates will still propagated, but object changes will not be reconciled.
	ClusterExtensionRevisionLifecycleStatePaused ClusterExtensionRevisionLifecycleState = "Paused"
	// ClusterExtensionRevisionLifecycleStateArchived / "Archived" disables reconciliation while also "scaling to zero",
	// which deletes all objects that are not excluded via the pausedFor property and
	// removes itself from the owner list of all other objects previously under management.
	ClusterExtensionRevisionLifecycleStateArchived ClusterExtensionRevisionLifecycleState = "Archived"
)

type ClusterExtensionRevisionPhase struct {
	Name    string                           `json:"name"`
	Objects []ClusterExtensionRevisionObject `json:"objects"`
	Slices  []string                         `json:"slices,omitempty"`
}

type ClusterExtensionRevisionObject struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Object unstructured.Unstructured `json:"object"`
}

type ClusterExtensionRevisionPrevious struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	UID types.UID `json:"uid"`
}

// ClusterExtensionRevisionStatus defines the observed state of a ClusterExtensionRevision.
type ClusterExtensionRevisionStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

// ClusterExtensionRevision is the Schema for the clusterextensionrevisions API
type ClusterExtensionRevision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is an optional field that defines the desired state of the ClusterExtension.
	// +optional
	Spec ClusterExtensionRevisionSpec `json:"spec,omitempty"`

	// status is an optional field that defines the observed state of the ClusterExtension.
	// +optional
	Status ClusterExtensionRevisionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterExtensionRevisionList contains a list of ClusterExtensionRevision
type ClusterExtensionRevisionList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is a required list of ClusterExtensionRevision objects.
	//
	// +kubebuilder:validation:Required
	Items []ClusterExtensionRevision `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterExtensionRevision{}, &ClusterExtensionRevisionList{})
}
