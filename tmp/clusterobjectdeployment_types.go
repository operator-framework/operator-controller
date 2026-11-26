/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// ClusterObjectDeploymentKind is the kind of the ClusterObjectDeployment API.
	ClusterObjectDeploymentKind = "ClusterObjectDeployment"

	// ClusterObjectDeploymentTypeProgressing is the condition type that reports the
	// rollout of the latest revision.
	ClusterObjectDeploymentTypeProgressing = "Progressing"
	// ClusterObjectDeploymentTypeReady is the condition type that reports whether
	// the deployment's single active revision is Ready.
	ClusterObjectDeploymentTypeReady = "Ready"

	// ClusterObjectDeploymentReasonNewClusterObjectSetProgressed reports that the latest
	// revision has completed its rollout.
	ClusterObjectDeploymentReasonNewClusterObjectSetProgressed = "NewClusterObjectSetProgressed"
	// ClusterObjectDeploymentReasonNewClusterObjectSetProgressing reports that a new
	// revision is being created or adopted, or that the latest revision is rolling out.
	ClusterObjectDeploymentReasonNewClusterObjectSetProgressing = "NewClusterObjectSetProgressing"
	// ClusterObjectDeploymentReasonProgressDeadlineExceeded reports that the latest
	// revision did not complete its rollout within spec.progressDeadlineMinutes.
	ClusterObjectDeploymentReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	// ClusterObjectDeploymentReasonInvalidSpecification reports that spec.template.metadata
	// contains labels or annotations that are not valid on a ClusterObjectSet, so no
	// revision is created from the template.
	ClusterObjectDeploymentReasonInvalidSpecification = "InvalidSpecification"
	// ClusterObjectDeploymentReasonRevisionPreconditionsNotMet reports that the latest
	// revision is waiting on preconditions that are not yet met.
	ClusterObjectDeploymentReasonRevisionPreconditionsNotMet = "RevisionPreconditionsNotMet"
	// ClusterObjectDeploymentReasonRevisionObjectCollision reports that objects in the
	// latest revision are owned by another controller or already exist.
	ClusterObjectDeploymentReasonRevisionObjectCollision = "RevisionObjectCollision"
	// ClusterObjectDeploymentReasonRevisionInvalidSpecification reports that objects in
	// the latest revision are invalid.
	ClusterObjectDeploymentReasonRevisionInvalidSpecification = "RevisionInvalidSpecification"
	// ClusterObjectDeploymentReasonOwnerConflict reports that the newest active revision
	// in the deployment's group is controlled by another controller.
	ClusterObjectDeploymentReasonOwnerConflict = "OwnerConflict"
	// ClusterObjectDeploymentReasonUnmanagedRevision reports that the newest active
	// revision in the deployment's group has no controller owner and was not created by
	// this controller.
	ClusterObjectDeploymentReasonUnmanagedRevision = "UnmanagedRevision"

	// Ready condition reasons.
	// ClusterObjectDeploymentReasonRevisionReady reports that the deployment has
	// a single active revision and that revision's Ready condition is True.
	ClusterObjectDeploymentReasonRevisionReady = "RevisionReady"
	// ClusterObjectDeploymentReasonRevisionNotReady reports that the deployment
	// has a single active revision and that revision's Ready condition is False.
	ClusterObjectDeploymentReasonRevisionNotReady = "RevisionNotReady"
	// ClusterObjectDeploymentReasonMultipleActiveRevisions reports that the
	// deployment has more than one active revision, so readiness is not evaluated.
	ClusterObjectDeploymentReasonMultipleActiveRevisions = "MultipleActiveRevisions"
	// ClusterObjectDeploymentReasonUnknown reports that the deployment has a
	// single active revision whose readiness is not yet determined.
	ClusterObjectDeploymentReasonUnknown = "Unknown"
	// ClusterObjectDeploymentReasonNoActiveRevision reports that the deployment
	// controls no active revision.
	ClusterObjectDeploymentReasonNoActiveRevision = "NoActiveRevision"
)

// ClusterObjectDeploymentSpec defines the mutable desired state of a deployment.
type ClusterObjectDeploymentSpec struct {
	// revisionHistoryLimit is the maximum number of archived ClusterObjectSet
	// revisions to retain for inspecting rollout history. Active revisions are never
	// deleted and do not count against this limit. When more archived revisions exist
	// than this limit, the oldest ones are deleted once they have finished tearing
	// down. Archived revisions cannot be reactivated; to return to earlier content,
	// set spec.template to that content, which creates a new revision.
	//
	// A value of 0 retains no archived revisions. The minimum is 0, the maximum
	// is 50, and the default is 5.
	//
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=50
	// +kubebuilder:default=5
	// +optional
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`

	// progressDeadlineMinutes is the maximum number of minutes a revision may take
	// to roll out, measured from the ClusterObjectSet's creation timestamp. Time
	// spent waiting for the ClusterObjectSet to be created is not counted. The
	// deadline covers all phases together; completing a phase does not reset it.
	// Once a revision has completed, the deadline no longer applies, even if it
	// later becomes unhealthy. When omitted, no deadline is enforced. The minimum
	// period is 1 minute, and the maximum is 720 minutes (12 hours).
	//
	// The deadline is rollout policy, not revision content: changing it does not
	// create a new revision and applies immediately to the latest active revision.
	//
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=720
	// +optional
	// <opcon:experimental>
	ProgressDeadlineMinutes int32 `json:"progressDeadlineMinutes,omitempty"`

	// template is the desired content of newly created ClusterObjectSets.
	// Changes to template create a new revision; existing revisions remain immutable.
	// +required
	Template ClusterObjectDeploymentTemplate `json:"template,omitzero"`
}

// ClusterObjectDeploymentTemplate describes the caller-controlled part of a revision.
type ClusterObjectDeploymentTemplate struct {
	// metadata contains labels and annotations propagated to each ClusterObjectSet
	// revision created from this template. The labels
	// olm.operatorframework.io/template-hash and app.kubernetes.io/managed-by are
	// reserved; the controller always sets their values on each revision. Changes to
	// metadata create a new revision.
	// +optional
	Metadata ClusterObjectDeploymentTemplateMetadata `json:"metadata,omitzero"`

	// spec contains the desired revision content. Group, revision, lifecycle state,
	// and ownership are assigned by the controller and cannot be set here.
	// +required
	Spec ClusterObjectDeploymentTemplateSpec `json:"spec,omitzero"`
}

// ClusterObjectDeploymentTemplateMetadata contains labels and annotations
// propagated to each ClusterObjectSet revision created from the template.
// At least one of labels or annotations must be set.
// +kubebuilder:validation:MinProperties=1
type ClusterObjectDeploymentTemplateMetadata struct {
	// labels is an optional set of key/value pairs propagated to each revision's
	// metadata. When set, it must contain between 1 and 32 entries. Keys and values
	// must be valid Kubernetes labels; invalid labels are reported on the Progressing
	// condition with reason InvalidSpecification and block new revisions until
	// corrected.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=32
	Labels map[string]string `json:"labels,omitempty"`

	// annotations is an optional set of key/value pairs propagated to each
	// revision's metadata. When set, it must contain between 1 and 32 entries. Keys
	// and the total size must satisfy the Kubernetes annotation rules; invalid
	// annotations are reported on the Progressing condition with reason
	// InvalidSpecification and block new revisions until corrected.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=32
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ClusterObjectDeploymentTemplateSpec contains mutable ClusterObjectSet content.
type ClusterObjectDeploymentTemplateSpec struct {
	// ClusterObjectSetContent holds the mutable revision content that is
	// propagated to each ClusterObjectSet created from this template. Changes
	// to these fields create a new revision; existing revisions remain immutable.
	ClusterObjectSetContent `json:",inline"`
}

// ClusterObjectDeploymentStatus reports rollout progress.
type ClusterObjectDeploymentStatus struct {
	// conditions describes the rollout state and readiness of the deployment.
	//
	// Progressing reports the rollout of the latest revision:
	//   - True, NewClusterObjectSetProgressing: a new revision is being created or
	//     adopted, or the latest revision is rolling out. The message says which. If
	//     creating or adopting a revision fails, the controller keeps retrying.
	//   - False, NewClusterObjectSetProgressed: the latest revision has completed.
	//     Completion remains recorded even if the revision later becomes unhealthy.
	//   <opcon:experimental:description>
	//   - False, ProgressDeadlineExceeded: the latest revision did not complete its
	//     rollout within spec.progressDeadlineMinutes.
	//   </opcon:experimental:description>
	//   - True, RevisionPreconditionsNotMet: the latest revision is waiting on
	//     preconditions that are not yet met, such as RBAC or a CRD that has not yet
	//     propagated. The controller keeps retrying.
	//   - False, RevisionObjectCollision: objects in the latest revision are owned by
	//     another controller or already exist. The collision must be resolved.
	//   - False, RevisionInvalidSpecification: objects in the latest revision are
	//     invalid. Inspect the named ClusterObjectSet for details.
	//   - False, InvalidSpecification: spec.template.metadata contains invalid labels
	//     or annotations. The controller does not create a revision from the template
	//     while it is invalid; correct spec.template.metadata to resume.
	//   - False, OwnerConflict: the newest active ClusterObjectSet in this deployment's
	//     group is controlled by another controller. No new revision is created while
	//     the conflict persists.
	//   - False, UnmanagedRevision: the newest active ClusterObjectSet in this
	//     deployment's group has no controller owner and was not created by this
	//     controller. Remove it, or restore its
	//     app.kubernetes.io/managed-by=object-controller label, to resume.
	//
	// Ready reports whether the deployment's active revision is Ready. Only
	// active (non-archived), non-deleting revisions controlled by this deployment
	// are considered:
	//   - True, RevisionReady: the deployment has a single active revision and
	//     that revision's Ready condition is True.
	//   - False, RevisionNotReady: the deployment has a single active revision
	//     and that revision's Ready condition is False. Inspect that revision's
	//     Ready condition for the reason.
	//   - False, NoActiveRevision: the deployment controls no active revision.
	//   - Unknown, MultipleActiveRevisions: the deployment has more than one
	//     active revision, for example while a new revision rolls out or an older
	//     one is being archived. Readiness is not evaluated until a single
	//     active revision remains.
	//   - Unknown, Unknown: the deployment has a single active revision, but its
	//     readiness is not yet determined.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"` //nolint:kubeapilinter // This CRD uses structural list-map validation, not protobuf serialization.
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=cod
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name=Progressing,type=string,JSONPath=`.status.conditions[?(@.type=='Progressing')].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 52 && self.metadata.name.matches('^[a-z]([a-z0-9-]*[a-z0-9])?$')",message="name must be 1 to 52 lowercase alphanumeric or hyphen characters, starting with a letter and ending with an alphanumeric character"

// ClusterObjectDeployment is a cluster-scoped API that manages mutable desired
// content by creating immutable ClusterObjectSet revisions from a template.
type ClusterObjectDeployment struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata. The name must be 1 to 52 lowercase
	// alphanumeric or hyphen characters, start with a letter, and end with an
	// alphanumeric character. It is used as the group of each ClusterObjectSet
	// revision, whose names are <name>-<revision>.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of the deployment: the revision template and
	// the rollout policy.
	// +required
	Spec ClusterObjectDeploymentSpec `json:"spec,omitzero"`

	// status is the most recently observed state of the deployment.
	// +optional
	Status ClusterObjectDeploymentStatus `json:"status,omitzero"` //nolint:kubeapilinter // OpenShift APIs use value structs with omitzero for optional status.
}

// +kubebuilder:object:root=true

// ClusterObjectDeploymentList contains ClusterObjectDeployment objects.
type ClusterObjectDeploymentList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is the list of deployments.
	// +required
	Items []ClusterObjectDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ClusterObjectDeployment{}, &ClusterObjectDeploymentList{})
		return nil
	})
}
