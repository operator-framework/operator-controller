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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ClusterExtensionRevisionKind = "ClusterExtensionRevision"

	// Condition Types
	ClusterExtensionRevisionTypeAvailable   = "Available"
	ClusterExtensionRevisionTypeSucceeded   = "Succeeded"
	ClusterExtensionRevisionTypeProgressing = "Progressing"

	// Condition Reasons
	ClusterExtensionRevisionReasonAvailable                 = "Available"
	ClusterExtensionRevisionReasonReconcileFailure          = "ReconcileFailure"
	ClusterExtensionRevisionReasonRevisionValidationFailure = "RevisionValidationFailure"
	ClusterExtensionRevisionReasonPhaseValidationError      = "PhaseValidationError"
	ClusterExtensionRevisionReasonObjectCollisions          = "ObjectCollisions"
	ClusterExtensionRevisionReasonRolloutSuccess            = "RolloutSuccess"
	ClusterExtensionRevisionReasonProbeFailure              = "ProbeFailure"
	ClusterExtensionRevisionReasonProbesSucceeded           = "ProbesSucceeded"
	ClusterExtensionRevisionReasonIncomplete                = "Incomplete"
	ClusterExtensionRevisionReasonProgressing               = "Progressing"
	ClusterExtensionRevisionReasonArchived                  = "Archived"
	ClusterExtensionRevisionReasonRolloutInProgress         = "RollingOut"
	ClusterExtensionRevisionReasonRolloutError              = "RolloutError"
	ClusterExtensionRevisionReasonRolledOut                 = "RolledOut"
)

// ClusterExtensionRevisionSpec defines the desired state of ClusterExtensionRevision.
type ClusterExtensionRevisionSpec struct {
	// Specifies the lifecycle state of the ClusterExtensionRevision.
	//
	// +kubebuilder:default="Active"
	// +kubebuilder:validation:Enum=Active;Paused;Archived
	// +kubebuilder:validation:XValidation:rule="oldSelf == 'Active' || oldSelf == 'Paused' || oldSelf == 'Archived' && oldSelf == self", message="can not un-archive"
	LifecycleState ClusterExtensionRevisionLifecycleState `json:"lifecycleState,omitempty"`
	// Revision is a sequence number representing a specific revision of the ClusterExtension instance.
	// Must be positive. Each ClusterExtensionRevision of the same parent ClusterExtension needs to have
	// a unique value assigned. It is immutable after creation. The new revision number must always be previous revision +1.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="revision is immutable"
	Revision int64 `json:"revision"`
	// Phases are groups of objects that will be applied at the same time.
	// All objects in the phase will have to pass their probes in order to progress to the next phase.
	//
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || oldSelf.size() == 0", message="phases is immutable"
	// +listType=map
	// +listMapKey=name
	// +optional
	Phases []ClusterExtensionRevisionPhase `json:"phases,omitempty"`
	// Previous references previous revisions that objects can be adopted from.
	//
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

// ClusterExtensionRevisionPhase are groups of objects that will be applied at the same time.
// All objects in the a phase will have to pass their probes in order to progress to the next phase.
type ClusterExtensionRevisionPhase struct {
	// Name identifies this phase.
	//
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
	// Objects are a list of all the objects within this phase.
	Objects []ClusterExtensionRevisionObject `json:"objects"`
}

// ClusterExtensionRevisionObject contains an object and settings for it.
type ClusterExtensionRevisionObject struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Object unstructured.Unstructured `json:"object"`
	// CollisionProtection controls whether OLM can adopt and modify objects
	// already existing on the cluster or even owned by another controller.
	//
	// +kubebuilder:default="Prevent"
	// +optional
	CollisionProtection CollisionProtection `json:"collisionProtection,omitempty"`
}

// CollisionProtection specifies if and how ownership collisions are prevented.
type CollisionProtection string

const (
	// CollisionProtectionPrevent prevents owner collisions entirely
	// by only allowing to work with objects itself has created.
	CollisionProtectionPrevent CollisionProtection = "Prevent"
	// CollisionProtectionIfNoController allows to patch and override
	// objects already present if they are not owned by another controller.
	CollisionProtectionIfNoController CollisionProtection = "IfNoController"
	// CollisionProtectionNone allows to patch and override objects
	// already present and owned by other controllers.
	// Be careful! This setting may cause multiple controllers to fight over a resource,
	// causing load on the API server and etcd.
	CollisionProtectionNone CollisionProtection = "None"
)

type ClusterExtensionRevisionPrevious struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	UID types.UID `json:"uid"`
}

// ClusterExtensionRevisionStatus defines the observed state of a ClusterExtensionRevision.
type ClusterExtensionRevisionStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

// ClusterExtensionRevision is the Schema for the clusterextensionrevisions API
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=`.status.conditions[?(@.type=='Available')].status`
// +kubebuilder:printcolumn:name="Progressing",type=string,JSONPath=`.status.conditions[?(@.type=='Progressing')].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
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

func (cer *ClusterExtensionRevision) MarkAsProgressing(reason, message string) {
	meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ClusterExtensionRevisionTypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func (cer *ClusterExtensionRevision) MarkAsNotProgressing(reason, message string) {
	meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ClusterExtensionRevisionTypeProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func (cer *ClusterExtensionRevision) MarkAsAvailable(reason, message string) {
	meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ClusterExtensionRevisionTypeAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func (cer *ClusterExtensionRevision) MarkAsUnavailable(reason, message string) {
	meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ClusterExtensionRevisionTypeAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
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
