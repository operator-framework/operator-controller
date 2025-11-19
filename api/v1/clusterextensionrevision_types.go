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

	// ClusterExtensionRevisionTypeAvailable is the condition type that represents whether the
	// ClusterExtensionRevision is available and has been successfully rolled out.
	ClusterExtensionRevisionTypeAvailable = "Available"

	// ClusterExtensionRevisionTypeSucceeded is the condition type that represents whether the
	// ClusterExtensionRevision rollout has succeeded at least once.
	ClusterExtensionRevisionTypeSucceeded = "Succeeded"

	// ClusterExtensionRevisionReasonAvailable is set when the revision is available
	// and passes all probes after a successful rollout.
	// Condition: Available, Status: True
	ClusterExtensionRevisionReasonAvailable = "Available"

	// ClusterExtensionRevisionReasonReconcileFailure is set when the ClusterExtensionRevision
	// encounters a general reconciliation failure, such as errors ensuring finalizers or
	// establishing watches.
	// Condition: Available, Status: False
	ClusterExtensionRevisionReasonReconcileFailure = "ReconcileFailure"

	// ClusterExtensionRevisionReasonRevisionValidationFailure is set when the revision
	// fails preflight validation checks at the revision level.
	// Condition: Available, Status: False
	ClusterExtensionRevisionReasonRevisionValidationFailure = "RevisionValidationFailure"

	// ClusterExtensionRevisionReasonPhaseValidationError is set when a phase within
	// the revision fails preflight validation checks.
	// Condition: Available, Status: False
	ClusterExtensionRevisionReasonPhaseValidationError = "PhaseValidationError"

	// ClusterExtensionRevisionReasonObjectCollisions is set when objects in the revision
	// collide with existing objects on the cluster that cannot be adopted based on
	// the configured collision protection policy.
	// Condition: Available, Status: False
	ClusterExtensionRevisionReasonObjectCollisions = "ObjectCollisions"

	// ClusterExtensionRevisionReasonRolloutSuccess is set when the revision has
	// successfully completed its rollout for the first time.
	// Condition: Succeeded, Status: True
	ClusterExtensionRevisionReasonRolloutSuccess = "RolloutSuccess"

	// ClusterExtensionRevisionReasonProbeFailure is set when one or more objects
	// in the revision fail their readiness probes during rollout.
	// Condition: Available, Status: False
	ClusterExtensionRevisionReasonProbeFailure = "ProbeFailure"

	// ClusterExtensionRevisionReasonIncomplete is set when the revision rollout
	// has not completed but no specific probe failures have been detected.
	// Condition: Available, Status: False
	ClusterExtensionRevisionReasonIncomplete = "Incomplete"

	// ClusterExtensionRevisionReasonProgressing is set when the revision rollout
	// is actively making progress and is in transition.
	// Condition: Progressing, Status: True
	ClusterExtensionRevisionReasonProgressing = "Progressing"

	// ClusterExtensionRevisionReasonArchived is set when the revision has been
	// archived and its objects have been torn down.
	// Condition: Available, Status: Unknown
	ClusterExtensionRevisionReasonArchived = "Archived"

	// ClusterExtensionRevisionReasonMigrated is set when the revision was
	// migrated from an existing Helm release to a ClusterExtensionRevision.
	// Condition: Available, Status: Unknown
	ClusterExtensionRevisionReasonMigrated = "Migrated"
)

// ClusterExtensionRevisionSpec defines the desired state of ClusterExtensionRevision.
//
// A ClusterExtensionRevision represents a specific immutable snapshot of the objects
// to be installed for a ClusterExtension. Each revision is rolled out in phases,
// with objects organized by their Group-Kind into well-known phases such as namespaces,
// rbac, crds, and deploy.
type ClusterExtensionRevisionSpec struct {
	// lifecycleState specifies the lifecycle state of the ClusterExtensionRevision.
	//
	// When set to "Active" (the default), the revision is actively managed and reconciled.
	// When set to "Paused", reconciliation is disabled but status updates continue.
	// When set to "Archived", the revision is torn down and scaled to zero - the revision is removed from the owner
	// list of all other objects previously under management and all objects that did not transition to a
	// succeeding revision are deleted.
	//
	// Once a revision is set to "Archived", it cannot be un-archived.
	//
	// +kubebuilder:default="Active"
	// +kubebuilder:validation:Enum=Active;Paused;Archived
	// +kubebuilder:validation:XValidation:rule="oldSelf == 'Active' || oldSelf == 'Paused' || oldSelf == 'Archived' && oldSelf == self", message="can not un-archive"
	LifecycleState ClusterExtensionRevisionLifecycleState `json:"lifecycleState,omitempty"`

	// revision is a required, immutable sequence number representing a specific revision
	// of the parent ClusterExtension.
	//
	// Must be a positive integer. Each ClusterExtensionRevision belonging to the same
	// parent ClusterExtension must have a unique revision number. The revision number
	// must always be the previous revision number plus one, or 1 for the first revision.
	//
	// +kubebuilder:validation:Required
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
	// All objects within a phase are applied simultaneously. The revision only progresses
	// to the next phase once all objects in the current phase pass their readiness probes.
	//
	// Once set (even if empty), this field is immutable.
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
	// ClusterExtensionRevisionLifecycleStatePaused / "Paused" disables reconciliation of the ClusterExtensionRevision.
	// Object changes will not be reconciled. However, status updates will be propagated.
	ClusterExtensionRevisionLifecycleStatePaused ClusterExtensionRevisionLifecycleState = "Paused"
	// ClusterExtensionRevisionLifecycleStateArchived / "Archived" archives the revision for historical or auditing purposes.
	// The revision is removed from the owner list of all other objects previously under management and all objects
	// that did not transition to a succeeding revision are deleted.
	ClusterExtensionRevisionLifecycleStateArchived ClusterExtensionRevisionLifecycleState = "Archived"
)

// ClusterExtensionRevisionPhase represents a group of objects that are applied together.
//
// Objects within a phase are applied simultaneously, and all objects must pass their
// readiness probes before the revision progresses to the next phase. Phases are applied
// in a well-defined order based on the types of objects they contain.
type ClusterExtensionRevisionPhase struct {
	// name is a required identifier for this phase.
	//
	// Phase names must follow the DNS label standard as defined in [RFC 1123].
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

	// objects is a required list of all Kubernetes objects within this phase.
	//
	// All objects in this list will be applied to the cluster simultaneously.
	// The phase will only be considered complete once all objects pass their
	// readiness probes.
	Objects []ClusterExtensionRevisionObject `json:"objects"`
}

// ClusterExtensionRevisionObject represents a Kubernetes object to be applied as part
// of a ClusterExtensionRevision, along with its collision protection settings.
type ClusterExtensionRevisionObject struct {
	// object is a required embedded Kubernetes object to be applied.
	//
	// This object must be a valid Kubernetes resource with apiVersion, kind, and metadata
	// fields. Status fields are not permitted and will be removed if present.
	// Only specific metadata fields are preserved: name, namespace, labels, and annotations.
	//
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Object unstructured.Unstructured `json:"object"`

	// collisionProtection controls whether the operator can adopt and modify objects
	// that already exist on the cluster.
	//
	// When set to "Prevent" (the default), the operator will only manage objects it
	// has created itself, preventing any ownership collisions.
	//
	// When set to "IfNoController", the operator can adopt and modify pre-existing
	// objects that are not owned by another controller. This is useful for taking
	// over management of manually-created resources.
	//
	// When set to "None", the operator can adopt and modify any pre-existing object,
	// even if owned by another controller. Use this setting with extreme caution as
	// it may cause multiple controllers to fight over the same resource, resulting
	// in increased load on the API server and etcd.
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

// ClusterExtensionRevisionStatus defines the observed state of a ClusterExtensionRevision.
type ClusterExtensionRevisionStatus struct {
	// conditions is an optional list of status conditions describing the state of the
	// ClusterExtensionRevision.
	//
	// The Available condition represents whether the revision has been successfully rolled out
	// and is available:
	//   - When Available is True with Reason Available, the revision has been successfully
	//     rolled out and all objects pass their readiness probes.
	//   - When Available is False with Reason Incomplete, the revision rollout has not yet
	//     completed but no specific failures have been detected.
	//   - When Available is False with Reason ProbeFailure, one or more objects are failing
	//     their readiness probes during rollout.
	//   - When Available is False with Reason ReconcileFailure, the revision has encountered
	//     a general reconciliation failure.
	//   - When Available is False with Reason RevisionValidationFailure, the revision failed
	//     preflight validation checks.
	//   - When Available is False with Reason PhaseValidationError, a phase within the revision
	//     failed preflight validation checks.
	//   - When Available is False with Reason ObjectCollisions, objects in the revision collide
	//     with existing cluster objects that cannot be adopted.
	//   - When Available is Unknown with Reason Archived, the revision has been archived and
	//     its objects have been torn down.
	//   - When Available is Unknown with Reason Migrated, the revision was migrated
	//     from an existing release and object status probe results have not yet been observed.
	//
	// The Succeeded condition represents whether the revision has successfully completed its
	// rollout at least once:
	//   - When Succeeded is True with Reason RolloutSuccess, the revision has successfully
	//     completed its rollout. This condition is set once and persists even if the revision
	//     later becomes unavailable.
	//
	// The Progressing condition represents whether the revision is actively rolling out:
	//   - When Progressing is True with Reason Progressing, the revision rollout is actively
	//     making progress and is in transition.
	//   - When Progressing is not present, the revision is not currently in transition.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=`.status.conditions[?(@.type=='Available')].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterExtensionRevision is the schema for the ClusterExtensionRevisions API.
//
// A ClusterExtensionRevision represents an immutable snapshot of the Kubernetes objects
// to be installed for a specific version of a ClusterExtension. Each revision contains
// a set of objects organized into phases that are rolled out sequentially.
//
// ClusterExtensionRevisions are created and managed automatically by the
// ClusterExtension controller and should not be created directly by users. Each
// ClusterExtension may have multiple revisions over its lifetime as it is upgraded
// or reconfigured.
//
// The revision rollout follows a phased approach where objects are applied in a
// well-defined order based on their types (e.g., namespaces, then RBAC, then CRDs,
// then deployments). Within each phase, all objects are applied simultaneously and
// must pass their readiness probes before the rollout progresses to the next phase.
//
// Revisions have three lifecycle states:
//   - Active: The revision is actively managed and reconciled (default state)
//   - Paused: Reconciliation is disabled but status updates continue
//   - Archived: The revision is torn down, objects are deleted, and the revision
//     removes itself from the owner list of managed objects
type ClusterExtensionRevision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is an optional field that defines the desired state of the ClusterExtensionRevision.
	// +optional
	Spec ClusterExtensionRevisionSpec `json:"spec,omitempty"`

	// status is an optional field that defines the observed state of the ClusterExtensionRevision.
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
