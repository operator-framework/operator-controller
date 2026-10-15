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
	ClusterObjectDeploymentKind = "ClusterObjectDeployment"

	// Condition types.
	ClusterObjectDeploymentTypeAvailable   = "Available"
	ClusterObjectDeploymentTypeProgressing = "Progressing"

	// Condition reasons.
	ClusterObjectDeploymentReasonAvailable                      = "Available"
	ClusterObjectDeploymentReasonNotAvailable                   = "NotAvailable"
	ClusterObjectDeploymentReasonProgressing                    = "Progressing"
	ClusterObjectDeploymentReasonNewClusterObjectSetProgressed  = "NewClusterObjectSetProgressed"
	ClusterObjectDeploymentReasonNewClusterObjectSetProgressing = "NewClusterObjectSetProgressing"
	ClusterObjectDeploymentReasonProgressDeadlineExceeded       = "ProgressDeadlineExceeded"
)

// ClusterObjectDeploymentSpec defines the mutable desired state of a deployment.
type ClusterObjectDeploymentSpec struct {
	// template is the desired metadata and content of newly created ClusterObjectSets.
	// Changes to template create a new revision; existing revisions remain immutable.
	// +required
	Template ClusterObjectDeploymentTemplate `json:"template,omitzero"`

	// progressDeadlineMinutes is the maximum number of minutes without phase progress
	// before an incomplete rollout reports ProgressDeadlineExceeded. Omission disables
	// the deadline. A supplied value must be positive. Deadline changes do not create
	// revisions, and this value is not copied into the legacy ClusterObjectSet deadline.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ProgressDeadlineMinutes *int32 `json:"progressDeadlineMinutes,omitempty"` //nolint:kubeapilinter // A pointer distinguishes an omitted deadline from an invalid explicit zero.
}

// ClusterObjectDeploymentTemplate describes the caller-controlled part of a revision.
type ClusterObjectDeploymentTemplate struct {
	// metadata contains labels and annotations copied onto each ClusterObjectSet,
	// rather than onto the objects it manages. Root deployment metadata is not copied.
	// +optional
	Metadata ClusterObjectDeploymentTemplateMetadata `json:"metadata,omitzero"` //nolint:kubeapilinter // OpenShift APIs use value structs with omitzero for optional metadata.

	// spec contains the desired revision content. Group, revision, lifecycle state,
	// and ownership are assigned by the controller and cannot be set here.
	// +required
	Spec ClusterObjectDeploymentTemplateSpec `json:"spec,omitzero"`
}

// ClusterObjectDeploymentTemplateMetadata contains bounded revision metadata.
type ClusterObjectDeploymentTemplateMetadata struct {
	// labels contains up to 32 Kubernetes labels copied onto each revision.
	// The olm.operatorframework.io/template-hash label is reserved for the controller.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	// +kubebuilder:validation:XValidation:rule="self.all(k, !format.qualifiedName().validate(k).hasValue())",message="label keys must be valid Kubernetes qualified names"
	// +kubebuilder:validation:XValidation:rule="self.all(k, !format.labelValue().validate(self[k]).hasValue())",message="label values must be valid Kubernetes label values"
	// +kubebuilder:validation:XValidation:rule="!('olm.operatorframework.io/template-hash' in self)",message="olm.operatorframework.io/template-hash is reserved for the controller"
	Labels map[string]ClusterObjectDeploymentLabelValue `json:"labels,omitempty"`

	// annotations contains up to 32 Kubernetes annotations copied onto each revision.
	// Each value and the aggregate size of all keys and values must fit within 256 KiB.
	// Any controller-added annotations must also fit in that aggregate budget.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	// +kubebuilder:validation:XValidation:rule="self.all(k, !format.qualifiedName().validate(k.lowerAscii()).hasValue())",message="annotation keys must be valid Kubernetes qualified names"
	// +kubebuilder:validation:XValidation:rule="self.all(k, bytes(self[k]).size() <= 262144)",message="annotation values must not exceed 256 KiB"
	// +kubebuilder:validation:XValidation:rule="self.map(k, bytes(k).size() + bytes(self[k]).size()).sum() <= 262144",message="annotation keys and values must not exceed 256 KiB in total"
	Annotations map[string]ClusterObjectDeploymentAnnotationValue `json:"annotations,omitempty"`
}

// ClusterObjectDeploymentLabelValue is a bounded Kubernetes label value.
// Its length bounds the cost of label validation.
// +kubebuilder:validation:MaxLength=63
type ClusterObjectDeploymentLabelValue string

// ClusterObjectDeploymentAnnotationValue is a bounded Kubernetes annotation value.
// The aggregate byte limit is validated on the containing annotations map.
// +kubebuilder:validation:MaxLength=262144
type ClusterObjectDeploymentAnnotationValue string

// ClusterObjectDeploymentTemplateSpec contains mutable ClusterObjectSet content.
type ClusterObjectDeploymentTemplateSpec struct {
	// phases is an ordered list of 1 to 20 uniquely named phases. Each phase contains
	// at most 50 objects using the existing inline object or immutable Secret reference
	// representation. Phase, object, and probe order participate in template identity.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +listType=map
	// +listMapKey=name
	Phases []ClusterObjectSetPhase `json:"phases"` //nolint:kubeapilinter // These are ordered groups of objects, not an enum describing lifecycle state.

	// progressionProbes supplies custom readiness probes for objects in every phase.
	// The existing ClusterObjectSet selector and assertion semantics apply.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +listType=atomic
	ProgressionProbes []ProgressionProbe `json:"progressionProbes,omitempty"`

	// collisionProtection is the default strategy for objects in the revision.
	// Objects and phases may override it. When omitted the controller uses Prevent
	// in the generated ClusterObjectSet. Omission remains distinct from explicit
	// Prevent for template identity; admission does not default this field.
	// +optional
	// +kubebuilder:validation:Enum=Prevent;IfNoController;None
	CollisionProtection CollisionProtection `json:"collisionProtection,omitempty"`
}

// ClusterObjectDeploymentStatus summarizes owned active revisions and rollout progress.
type ClusterObjectDeploymentStatus struct {
	// conditions describes installation health and progress for the observed generation.
	// Available is True for one Ready active revision, False for none or one unready
	// revision, and Unknown with reason Progressing for multiple active revisions.
	// Progressing is True with reason NewClusterObjectSetProgressing while the desired
	// rollout is incomplete, or False with reason NewClusterObjectSetProgressed after
	// completion or ProgressDeadlineExceeded after a deadline expires. Completion
	// remains recorded even if the completed revision later becomes unhealthy.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"` //nolint:kubeapilinter // This CRD uses structural list-map validation, not protobuf serialization.

	// activeRevisions summarizes owned active, non-deleting ClusterObjectSets,
	// including a newly created revision, ordered by increasing int64 revision.
	// +optional
	// +listType=map
	// +listMapKey=name
	ActiveRevisions []ClusterObjectDeploymentRevision `json:"activeRevisions,omitempty"`

	// objectCounts reports counts from the highest owned active revision, rather than
	// summing overlapping revisions. Omitted when that revision has not reported
	// counts or no active revision exists. Available corresponds to the child's ready count.
	// +optional
	ObjectCounts ClusterObjectDeploymentObjectCounts `json:"objectCounts,omitzero"`
}

// ClusterObjectDeploymentRevision summarizes the current conditions of an active revision.
type ClusterObjectDeploymentRevision struct {
	// name is the name of the ClusterObjectSet. It may contain the full positive int64
	// revision suffix; it is not restricted to a uint32 suffix or a 63-character label.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name,omitempty"`

	// conditions contains the revision's current status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"` //nolint:kubeapilinter // This CRD uses structural list-map validation, not protobuf serialization.
}

// ClusterObjectDeploymentObjectCounts reports object states in the highest active revision.
type ClusterObjectDeploymentObjectCounts struct {
	// total is the total number of objects in the revision. Its presence distinguishes
	// reported zero counts from an omitted objectCounts summary.
	// +required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Total *int64 `json:"total,omitempty"`

	// present is the number of objects found or created on the cluster.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Present int64 `json:"present,omitempty"` //nolint:kubeapilinter // Omission and zero both mean no objects are present.

	// synced is the number of objects matching their desired state.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Synced int64 `json:"synced,omitempty"` //nolint:kubeapilinter // Omission and zero both mean no objects are synced.

	// available is the number of synced objects passing their readiness assertions.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Available int64 `json:"available,omitempty"` //nolint:kubeapilinter // Omission and zero both mean no objects are available.
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=cod
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Available,type=string,JSONPath=`.status.conditions[?(@.type=='Available')].status`
// +kubebuilder:printcolumn:name=Progressing,type=string,JSONPath=`.status.conditions[?(@.type=='Progressing')].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 52 && self.metadata.name.matches('^[a-z]([a-z0-9-]*[a-z0-9])?$')",message="name must be 1 to 52 lowercase alphanumeric or hyphen characters, starting with a letter and ending with an alphanumeric character"

// ClusterObjectDeployment manages mutable desired content through immutable
// ClusterObjectSet revisions. It is an experimental, cluster-scoped API.
type ClusterObjectDeployment struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata. Names are valid COS groups of at
	// most 52 characters; generated child names also include an int64 revision suffix.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired deployment template and progress deadline.
	// +required
	Spec ClusterObjectDeploymentSpec `json:"spec,omitzero"`

	// status defines the observed state and is written through the status subresource.
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
