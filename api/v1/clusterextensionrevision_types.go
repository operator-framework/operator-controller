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
)

const (
	ClusterExtensionRevisionKind = "ClusterExtensionRevision"

	// Condition Types
	ClusterExtensionRevisionTypeReady = "Ready"

	// Condition Reasons
	ClusterExtensionRevisionReasonReady                    = "Ready"
	ClusterExtensionRevisionReasonReconciling              = "Reconciling"
	ClusterExtensionRevisionReasonProbeFailure             = "ProbeFailure"
	ClusterExtensionRevisionReasonValidationFailed         = "ValidationFailed"
	ClusterExtensionRevisionReasonObjectCollision          = "ObjectCollision"
	ClusterExtensionRevisionReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	ClusterExtensionRevisionReasonArchived                 = "Archived"
	ClusterExtensionRevisionReasonRollingOut               = "RollingOut"
	ClusterExtensionRevisionReasonTransitioning            = "Transitioning"
)

// ClusterExtensionRevisionSpec defines the desired state of ClusterExtensionRevision.
type ClusterExtensionRevisionSpec struct {
	// lifecycleState specifies the lifecycle state of the ClusterExtensionRevision.
	//
	// When set to "Active" (the default), the revision is actively managed and reconciled.
	// When set to "Archived", the revision is inactive and any resources not managed by a subsequent revision are deleted.
	// The revision is removed from the owner list of all objects previously under management.
	// All objects that did not transition to a succeeding revision are deleted.
	//
	// Once a revision is set to "Archived", it cannot be un-archived.
	//
	// +kubebuilder:default="Active"
	// +kubebuilder:validation:Enum=Active;Archived
	// +kubebuilder:validation:XValidation:rule="oldSelf == 'Active' || oldSelf == 'Archived' && oldSelf == self", message="cannot un-archive"
	LifecycleState ClusterExtensionRevisionLifecycleState `json:"lifecycleState,omitempty"`

	// revision is a required, immutable sequence number representing a specific revision
	// of the parent ClusterExtension.
	//
	// The revision field must be a positive integer.
	// Each ClusterExtensionRevision belonging to the same parent ClusterExtension must have a unique revision number.
	// The revision number must always be the previous revision number plus one, or 1 for the first revision.
	//
	// +required
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="revision is immutable"
	Revision int64 `json:"revision"`

	// phases is an optional, immutable list of phases that group objects to be applied together.
	//
	// Objects are organized into phases based on their Group-Kind. Common phases include:
	//   - namespaces: Namespace objects
	//   - policies: ResourceQuota, LimitRange, NetworkPolicy objects
	//   - rbac: ServiceAccount, Role, RoleBinding, ClusterRole, ClusterRoleBinding objects
	//   - crds: CustomResourceDefinition objects
	//   - storage: PersistentVolume, PersistentVolumeClaim, StorageClass objects
	//   - deploy: Deployment, StatefulSet, DaemonSet, Service, ConfigMap, Secret objects
	//   - publish: Ingress, APIService, Route, Webhook objects
	//
	// All objects in a phase are applied in no particular order.
	// The revision progresses to the next phase only after all objects in the current phase pass their readiness probes.
	//
	// Once set, even if empty, the phases field is immutable.
	//
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || oldSelf.size() == 0", message="phases is immutable"
	// +listType=map
	// +listMapKey=name
	// +optional
	Phases []ClusterExtensionRevisionPhase `json:"phases,omitempty"`
}

// ClusterExtensionRevisionLifecycleState specifies the lifecycle state of the ClusterExtensionRevision.
type ClusterExtensionRevisionLifecycleState string

const (
	// ClusterExtensionRevisionLifecycleStateActive / "Active" is the default lifecycle state.
	ClusterExtensionRevisionLifecycleStateActive ClusterExtensionRevisionLifecycleState = "Active"
	// ClusterExtensionRevisionLifecycleStateArchived / "Archived" archives the revision for historical or auditing purposes.
	// The revision is removed from the owner list of all other objects previously under management and all objects
	// that did not transition to a succeeding revision are deleted.
	ClusterExtensionRevisionLifecycleStateArchived ClusterExtensionRevisionLifecycleState = "Archived"
)

// ClusterExtensionRevisionPhase represents a group of objects that are applied together. The phase is considered
// complete only after all objects pass their status probes.
type ClusterExtensionRevisionPhase struct {
	// name is a required identifier for this phase.
	//
	// phase names must follow the DNS label standard as defined in [RFC 1123].
	// They must contain only lowercase alphanumeric characters or hyphens (-),
	// start and end with an alphanumeric character, and be no longer than 63 characters.
	//
	// Common phase names include: namespaces, policies, rbac, crds, storage, deploy, publish.
	//
	// [RFC 1123]: https://tools.ietf.org/html/rfc1123
	//
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// objects is a required list of all Kubernetes objects that belong to this phase.
	//
	// All objects in this list are applied to the cluster in no particular order.
	Objects []ClusterExtensionRevisionObject `json:"objects"`
}

// ClusterExtensionRevisionObject represents a Kubernetes object to be applied as part
// of a phase, along with its collision protection settings.
type ClusterExtensionRevisionObject struct {
	// object is a required embedded Kubernetes object to be applied.
	//
	// This object must be a valid Kubernetes resource with apiVersion, kind, and metadata fields.
	//
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Object unstructured.Unstructured `json:"object"`

	// collisionProtection controls whether the operator can adopt and modify objects
	// that already exist on the cluster.
	//
	// When set to "Prevent" (the default), the operator only manages objects it created itself.
	// This prevents ownership collisions.
	//
	// When set to "IfNoController", the operator can adopt and modify pre-existing objects
	// that are not owned by another controller.
	// This is useful for taking over management of manually-created resources.
	//
	// When set to "None", the operator can adopt and modify any pre-existing object, even if
	// owned by another controller.
	// Use this setting with extreme caution as it may cause multiple controllers to fight over
	// the same resource, resulting in increased load on the API server and etcd.
	//
	// +kubebuilder:default="Prevent"
	// +kubebuilder:validation:Enum=Prevent;IfNoController;None
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

// ClusterExtensionRevisionPhaseState represents the state of a phase during rollout.
type ClusterExtensionRevisionPhaseState string

const (
	// ClusterExtensionRevisionPhaseStateApplied indicates all objects in the phase have been successfully applied.
	ClusterExtensionRevisionPhaseStateApplied ClusterExtensionRevisionPhaseState = "Applied"
	// ClusterExtensionRevisionPhaseStateProgressing indicates the phase is actively being reconciled and probes are being evaluated.
	ClusterExtensionRevisionPhaseStateProgressing ClusterExtensionRevisionPhaseState = "Progressing"
	// ClusterExtensionRevisionPhaseStateFailed indicates the phase has failed due to validation errors or collisions.
	ClusterExtensionRevisionPhaseStateFailed ClusterExtensionRevisionPhaseState = "Failed"
	// ClusterExtensionRevisionPhaseStatePending indicates the phase is waiting for previous phases to complete.
	ClusterExtensionRevisionPhaseStatePending ClusterExtensionRevisionPhaseState = "Pending"
	// ClusterExtensionRevisionPhaseStateTransitioning indicates objects in the phase are transitioning to a newer revision.
	ClusterExtensionRevisionPhaseStateTransitioning ClusterExtensionRevisionPhaseState = "Transitioning"
)

// ClusterExtensionRevisionProbeFailure describes a failing probe for an object in a phase.
type ClusterExtensionRevisionProbeFailure struct {
	// kind is the Kind of the object failing probes.
	// +required
	Kind string `json:"kind"`

	// name is the name of the object failing probes.
	// +required
	Name string `json:"name"`

	// namespace is the namespace of the object failing probes.
	// Empty for cluster-scoped objects.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// message contains the joined probe failure messages.
	// +required
	Message string `json:"message"`
}

// ClusterExtensionRevisionPhaseStatus describes the status of a single phase.
type ClusterExtensionRevisionPhaseStatus struct {
	// name is the name of the phase.
	// +required
	Name string `json:"name"`

	// state represents the current state of the phase.
	// +required
	// +kubebuilder:validation:Enum=Applied;Progressing;Failed;Pending;Transitioning
	State ClusterExtensionRevisionPhaseState `json:"state"`

	// message provides additional context about the phase state.
	// This may include error messages for failed phases.
	// +optional
	Message string `json:"message,omitempty"`

	// lastTransitionTime is the last time the phase state changed.
	// This only updates when the state, message, or failingProbes change.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// failingProbes lists objects in this phase that are failing their readiness probes.
	// Only populated when state is Progressing and probes are failing.
	// +optional
	// +listType=atomic
	FailingProbes []ClusterExtensionRevisionProbeFailure `json:"failingProbes,omitempty"`
}

// ClusterExtensionRevisionStatus defines the observed state of a ClusterExtensionRevision.
type ClusterExtensionRevisionStatus struct {
	// conditions is an optional list of status conditions describing the state of the
	// ClusterExtensionRevision.
	//
	// The Ready condition represents whether the revision has been successfully rolled out and is ready:
	//   - When status is True and reason is Ready, all ClusterExtensionRevision resources have been applied and all progression probes are successful.
	//   - When status is False and reason is RollingOut, the ClusterExtensionRevision rollout is actively making progress and objects are being applied.
	//   - When status is False and reason is Reconciling, the ClusterExtensionRevision is being reconciled or retrying after an error.
	//   - When status is False and reason is ProbeFailure, one or more objects are failing their readiness probes.
	//   - When status is False and reason is ValidationFailed, the revision failed preflight validation checks.
	//   - When status is False and reason is ObjectCollision, the revision encountered object ownership collisions.
	//   - When status is False and reason is ProgressDeadlineExceeded, the revision has not completed within the specified progress deadline.
	//   - When status is False and reason is Transitioning, the revision's objects are being transitioned to a newer revision.
	//   - When status is False and reason is Archived, the ClusterExtensionRevision is archived and not being actively reconciled.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phaseStatuses provides detailed status information for each phase in the revision.
	// Each phase status includes the phase name, current state, any error messages,
	// and details about failing probes if applicable.
	//
	// +listType=map
	// +listMapKey=name
	// +optional
	PhaseStatuses []ClusterExtensionRevisionPhaseStatus `json:"phaseStatuses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterExtensionRevision represents an immutable snapshot of Kubernetes objects
// for a specific version of a ClusterExtension. Each revision contains objects
// organized into phases that roll out sequentially. The same object can only be managed by a single revision
// at a time. Ownership of objects is transitioned from one revision to the next as the extension is upgraded
// or reconfigured. Once the latest revision has rolled out successfully, previous active revisions are archived for
// posterity.
type ClusterExtensionRevision struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of the ClusterExtensionRevision.
	// +optional
	Spec ClusterExtensionRevisionSpec `json:"spec,omitempty"`

	// status is optional and defines the observed state of the ClusterExtensionRevision.
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
	// +required
	Items []ClusterExtensionRevision `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterExtensionRevision{}, &ClusterExtensionRevisionList{})
}
