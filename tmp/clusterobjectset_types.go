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
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	ClusterObjectSetKind = "ClusterObjectSet"

	// ClusterObjectSetMaxRevision is the largest revision whose canonical name fits
	// in a 63-character label value with a 52-character group and a separating hyphen.
	// Keep the revision validation maximum below in sync with this value.
	ClusterObjectSetMaxRevision int64 = 9999999999

	// Condition Types
	ClusterObjectSetTypeReady = "Ready"

	// Condition Reasons
	ClusterObjectSetReasonPending              = "Pending"
	ClusterObjectSetReasonArchived             = "Archived"
	ClusterObjectSetReasonArchiving            = "Archiving"
	ClusterObjectSetReasonObjectsNotReady      = "ObjectsNotReady"
	ClusterObjectSetReasonSuperseded           = "Superseded"
	ClusterObjectSetReasonObjectCollision      = "ObjectCollision"
	ClusterObjectSetReasonPreconditionsNotMet  = "PreconditionsNotMet"
	ClusterObjectSetReasonInvalidSpecification = "InvalidSpecification"
	ClusterObjectSetReasonAllObjectsReady      = "AllObjectsReady"
)

// ClusterObjectSetSpec defines the desired state of ClusterObjectSet.
// The content fields (phases, collisionProtection) are
// immutable from creation and may only be changed by creating a new revision.
// +kubebuilder:validation:XValidation:rule="self.phases == oldSelf.phases",message="phases is immutable"
// +kubebuilder:validation:XValidation:rule="self.collisionProtection == oldSelf.collisionProtection",message="collisionProtection is immutable"
type ClusterObjectSetSpec struct {
	// group is a required, immutable identifier that links related revisions together.
	// All revisions sharing the same group form an ordered sequence, regardless of owner references.
	// The value must be 1 to 52 characters long, start with a lowercase letter,
	// contain only lowercase letters, digits or hyphens, and end with a letter or digit.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=52
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[a-z]([a-z0-9-]*[a-z0-9])?$')`,message="group must start with a lowercase letter, contain only lowercase letters, digits or hyphens, and end with a letter or digit"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="group is immutable"
	Group string `json:"group,omitempty"`

	// lifecycleState specifies the lifecycle state of the ClusterObjectSet.
	//
	// When set to "Active", the revision is actively managed and reconciled.
	// When set to "Archived", the revision is inactive and any resources not managed by a subsequent revision are deleted.
	// The revision is removed from the owner list of all objects previously under management.
	// All objects that did not transition to a succeeding revision are deleted.
	//
	// Once a revision is set to "Archived", it cannot be un-archived.
	//
	// More than one revision may be "Active" during a rollout. For revisions owned
	// by a ClusterObjectDeployment, its controller requests archival of older
	// active revisions based on a newer owned, active revision's recorded rollout
	// completion, even if that newer revision is no longer Ready.
	// Standalone callers are responsible for setting lifecycleState to "Archived".
	//
	// +required
	// +kubebuilder:validation:XValidation:rule="oldSelf == 'Active' || oldSelf == 'Archived' && oldSelf == self", message="cannot un-archive"
	LifecycleState ClusterObjectSetLifecycleState `json:"lifecycleState,omitempty"`

	// revision is a required, immutable sequence number identifying a specific
	// ClusterObjectSet within a sequence of related revisions.
	//
	// The revision field must be an integer from 1 to 9999999999, inclusive.
	// Each ClusterObjectSet in the same group must have a unique revision number.
	//
	// +required
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=9999999999
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="revision is immutable"
	Revision int64 `json:"revision,omitempty"`

	// ClusterObjectSetContent holds the immutable revision content shared with
	// the ClusterObjectDeployment template. Immutability of these fields is
	// enforced at the ClusterObjectSetSpec level, not here, so that the same
	// structure can be embedded mutably in a ClusterObjectDeployment template.
	ClusterObjectSetContent `json:",inline"`
}

// ClusterObjectSetContent holds the revision content shared between
// ClusterObjectSet.spec and a ClusterObjectDeployment's template spec.
type ClusterObjectSetContent struct {
	// phases is a required list of phases that group objects to be applied together.
	//
	// All objects in a phase are applied in no particular order.
	// The revision progresses to the next phase only after all objects in the current phase pass their readiness probes.
	//
	// Each phase in the list must have a unique name. The maximum number of phases is 20.
	//
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +listType=map
	// +listMapKey=name
	// +required
	Phases []ClusterObjectSetPhase `json:"phases,omitempty"`

	// collisionProtection is a required field that specifies the default collision protection
	// strategy for all objects in this content. Individual phases or objects can override this value.
	//
	// This value is used as the default for any phase or object that does not explicitly specify
	// its own collisionProtection. The resolution order is: object > phase > content.
	//
	// +required
	CollisionProtection CollisionProtection `json:"collisionProtection,omitempty"`
}

// ProbeType defines the type of progression probe.
// +enum
type ProbeType string

const (
	ProbeTypeConditionEqual ProbeType = "ConditionEqual"
	ProbeTypeCEL            ProbeType = "CEL"
)

// Probe is a discriminated union defining a progression check for an object.
// +union
// +kubebuilder:validation:XValidation:rule="self.type == 'ConditionEqual' ?has(self.conditionEqual) : !has(self.conditionEqual)",message="conditionEqual is required when type is ConditionEqual, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'CEL' ?has(self.cel) : !has(self.cel)",message="cel is required when type is CEL, and forbidden otherwise"
type Probe struct {
	// type is a required field which specifies the type of probe to use.
	//
	// The allowed probe types are "ConditionEqual" and "CEL".
	//
	// When set to "ConditionEqual", the probe checks objects that have reached a condition of specified type and status.
	// When set to "CEL", the probe evaluates a boolean CEL rule against the object.
	//
	// +unionDiscriminator
	// +kubebuilder:validation:Enum=ConditionEqual;CEL
	// +required
	// <opcon:experimental>
	Type ProbeType `json:"type,omitempty"`

	// conditionEqual checks the requested condition in the object's status.conditions.
	// The probe result is True when the matching condition's status equals the
	// configured status, and False when it does not. Missing or malformed condition
	// lists, unreadable condition entries, or a missing requested condition produce
	// Unknown. If the matching condition has an integer observedGeneration that
	// differs from metadata.generation, the result is Unknown. An absent
	// observedGeneration does not by itself prevent the probe from passing.
	// Required when type is "ConditionEqual", and forbidden otherwise.
	//
	// +unionMember
	// +optional
	// <opcon:experimental>
	ConditionEqual ConditionEqualProbe `json:"conditionEqual,omitzero"`

	// cel contains a boolean CEL rule evaluated against the object's live state.
	// A False result reports the configured message; an evaluation error produces
	// Unknown and reports the error instead. Compilation and boolean result-type
	// checking occur before evaluation, not during CRD admission.
	// Required when type is "CEL", and forbidden otherwise.
	// +unionMember
	// +optional
	// <opcon:experimental>
	CEL ProbeCELSpec `json:"cel,omitzero"`
}

// ProbeCELSpec uses Common Expression Language (CEL) to probe an object.
// CEL rules must evaluate to a boolean to be valid. The variable self refers to
// the live Kubernetes resource described by the enclosing phase-object entry,
// including its status.
// See:
// https://kubernetes.io/docs/reference/using-api/cel
// https://github.com/google/cel-go
type ProbeCELSpec struct {
	// rule is the CEL expression to evaluate. The dynamically typed variable self
	// refers to the live Kubernetes resource described by the enclosing phase-object
	// entry, including its status, regardless of the desired manifest's source.
	//
	// The controller compiles and type-checks rule before evaluation. Its inferred
	// result type must be boolean; compilation failures and non-boolean result types
	// are invalid configuration. CRD admission validates only the configuration's
	// structure and string bounds, not CEL syntax or types.
	//
	// True passes the probe. False fails it and reports message. An evaluation
	// error, such as an unguarded missing-field access, produces Unknown and reports
	// the error instead of message.
	//
	// See https://kubernetes.io/docs/reference/using-api/cel and
	// https://github.com/google/cel-go.
	// +kubebuilder:example="self.metadata.name == \"Hans\""
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +required
	Rule string `json:"rule,omitempty"`

	// message is the human-readable explanation reported only when rule evaluates
	// to False. Evaluation errors produce Unknown and report the error instead.
	// Consumers must not parse this message.
	// +kubebuilder:example="Object must be named Hans"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +required
	Message string `json:"message,omitempty"`
}

// ConditionEqualProbe defines the condition type and status required for the probe to succeed.
type ConditionEqualProbe struct {
	// type sets the expected condition type, i.e. "Ready".
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=200
	// +required
	// <opcon:experimental>
	Type string `json:"type,omitempty"`

	// status sets the expected condition status.
	//
	// Allowed values are "True" and "False".
	//
	// +kubebuilder:validation:Enum=True;False
	// +required
	// <opcon:experimental>
	Status string `json:"status,omitempty"`
}

// ClusterObjectSetLifecycleState specifies the lifecycle state of the ClusterObjectSet.
// +kubebuilder:validation:Enum=Active;Archived
type ClusterObjectSetLifecycleState string

const (
	// ClusterObjectSetLifecycleStateActive / "Active" is the default lifecycle state.
	ClusterObjectSetLifecycleStateActive ClusterObjectSetLifecycleState = "Active"
	// ClusterObjectSetLifecycleStateArchived / "Archived" archives the revision for historical or auditing purposes.
	// The revision is removed from the owner list of all other objects previously under management and all objects
	// that did not transition to a succeeding revision are deleted.
	ClusterObjectSetLifecycleStateArchived ClusterObjectSetLifecycleState = "Archived"
)

// ClusterObjectSetPhase represents a group of objects that are applied together. The phase is considered
// complete only after all objects pass their status probes.
type ClusterObjectSetPhase struct {
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
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`!format.dns1123Label().validate(self).hasValue()`,message="the value must consist of only lowercase alphanumeric characters and hyphens, and must start and end with an alphanumeric character."
	Name string `json:"name,omitempty"`

	// objects is a required list of all Kubernetes objects that belong to this phase.
	//
	// All objects in this list are applied to the cluster in no particular order. The maximum number of objects per phase is 50.
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=50
	Objects []ClusterObjectSetObject `json:"objects"`

	// collisionProtection specifies the default collision protection strategy for all objects
	// in this phase. Individual objects can override this value.
	//
	// When set, this value is used as the default for any object in this phase that does not
	// explicitly specify its own collisionProtection.
	//
	// When omitted, objects in this phase that do not specify their own collisionProtection inherit the
	// enclosing spec default: the ClusterObjectSet's .spec.collisionProtection or, for a
	// ClusterObjectDeployment, the .spec.template.spec.collisionProtection.
	//
	// +optional
	CollisionProtection CollisionProtection `json:"collisionProtection,omitempty"`
}

// ObjectType defines the type of objects provided in the Objects set.
// +kubebuilder:validation:Enum=InlineManifest;ObjectSliceRef
type ObjectType string

const (
	ObjectTypeInlineManifest ObjectType = "InlineManifest"
	ObjectTypeObjectSliceRef ObjectType = "ObjectSliceRef"
)

// ClusterObjectSetObject represents a Kubernetes object to be applied as part
// of a phase, along with its progression probes and collision protection settings.
//
// Exactly one of inlineManifest or objectSliceRef must be set.
//
// +union
// +kubebuilder:validation:XValidation:rule="self.type == 'InlineManifest' ?has(self.inlineManifest) : !has(self.inlineManifest)",message="inlineManifest is required when type is InlineManifest, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'ObjectSliceRef' ?has(self.objectSliceRef) : !has(self.objectSliceRef)",message="objectSliceRef is required when type is ObjectSliceRef, and forbidden otherwise"
type ClusterObjectSetObject struct {
	// type is a required field which specifies the type of object to use.
	//
	// The allowed object types are "InlineManifest" and "ObjectSliceRef".
	//
	// When set to "InlineManifest", object manifests are supplied inline, as valid Kubernetes resources with apiVersion, kind, and metadata fields.
	// When set to "ObjectSliceRef", a name and key identify an object manifest in a cluster-scoped ClusterObjectSlice.
	//
	// +unionDiscriminator
	// +required
	// <opcon:experimental>
	Type ObjectType `json:"type,omitempty"`

	// inlineManifest is an optional embedded Kubernetes object to be applied.
	//
	// Exactly one of inlineManifest or objectSliceRef must be set.
	//
	// This object must be a valid Kubernetes resource with apiVersion, kind, and metadata fields.
	//
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:XValidation:rule="has(self.apiVersion) && has(self.kind) && has(self.metadata)",message="inlineManifest must be a valid Kubernetes resource with apiVersion, kind, and metadata"
	// +kubebuilder:validation:XValidation:rule="self.apiVersion != '' && self.kind != ''",message="apiVersion and kind must be non-empty"
	// +kubebuilder:validation:XValidation:rule="has(self.metadata.name) && self.metadata.name != ''",message="metadata.name is required"
	// +unionMember
	// +optional
	InlineManifest unstructured.Unstructured `json:"inlineManifest,omitempty,omitzero"`

	// objectSliceRef is an optional reference to an object manifest within a
	// cluster-scoped ClusterObjectSlice. No namespace is needed for this reference.
	//
	// Exactly one of inlineManifest or objectSliceRef must be set.
	//
	// +unionMember
	// +optional
	ObjectSliceRef ObjectSliceSourceRef `json:"objectSliceRef,omitzero"`

	// progressionProbes is an optional list of checks evaluated against this
	// object's live state, regardless of the desired manifest's source.
	// All configured probes must pass for the object to satisfy its progression
	// requirements. Failed or unknown results may block advancement to later phases.
	// When omitted, the object has no progression checks; this does not establish
	// application availability. Each object may specify at most 20 probes.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +listType=atomic
	// +optional
	// <opcon:experimental>
	ProgressionProbes []Probe `json:"progressionProbes,omitempty"`

	// collisionProtection controls whether the operator can adopt and modify objects
	// that already exist on the cluster.
	//
	// Allowed values are: "Prevent", "IfNoController", and "None".
	//
	// When set to "Prevent", the operator only manages objects it created itself.
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
	// When omitted, the value is inherited from the phase, then spec.
	//
	// +optional
	CollisionProtection CollisionProtection `json:"collisionProtection,omitempty"`
}

// ObjectSliceSourceRef identifies one object manifest within a cluster-scoped
// ClusterObjectSlice by name and key.
type ObjectSliceSourceRef struct {
	// name is the name of the referenced ClusterObjectSlice.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`!format.dns1123Subdomain().validate(self).hasValue()`,message="the value must consist of only lowercase alphanumeric characters, hyphens, and dots, and must start and end with an alphanumeric character."
	Name string `json:"name,omitempty"`

	// key identifies the desired object manifest within the referenced
	// ClusterObjectSlice.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[-._a-zA-Z0-9]+$')`,message="must consist only of alphanumeric characters, '-', '_', or '.'"
	Key string `json:"key,omitempty"`
}

// CollisionProtection specifies if and how ownership collisions are prevented.
// +kubebuilder:validation:Enum=Prevent;IfNoController;None
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

// ClusterObjectSetStatus defines the observed state of a ClusterObjectSet.
//
// The completedAt removal guard lives here at the parent level because a
// field-level transition rule is skipped when the field is absent from an
// update, which would otherwise allow the timestamp to be cleared and re-set.
//
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.completedAt) || has(self.completedAt)",message="completedAt cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.phases) || oldSelf.phases.all(p, (!has(p.contentDigest) && !has(p.completedAt)) || (has(self.phases) && self.phases.exists(n, n.name == p.name)))",message="phases with recorded contentDigest or completedAt cannot be removed"
type ClusterObjectSetStatus struct {
	// conditions is an optional list of status conditions describing the state of the
	// ClusterObjectSet.
	//
	// The Ready condition represents the state of the revision.
	// True means this revision's desired state is realized and its progression
	// requirements are satisfied. False means this revision does not currently
	// satisfy its readiness requirements; the reason explains why. Unknown is
	// the initial state, before the first reconciliation has evaluated the revision.
	//   - Unknown with reason Pending: the revision has not yet been evaluated (initial state, set when the condition is first created).
	//   - True with reason AllObjectsReady: the revision has rolled out and all objects are at the desired state.
	//   - False with reason ObjectsNotReady: one or more objects are still being applied or have not yet passed their readiness probes during rollout.
	//   - False with reason Superseded: one or more objects have been adopted by a newer revision of the object set.
	//   - False with reason ObjectCollision: one or more objects are controlled by another owner, or are unowned while collision protection prevents adoption, so this revision cannot take ownership of them.
	//   - False with reason PreconditionsNotMet: a precondition for applying the revision is not yet satisfied (referenced object content is unavailable, invalid, or changed, or a preflight dry-run was rejected, e.g. an immutable field, a conflicting object, or RBAC/dependency CRDs not yet propagated); the revision keeps retrying and recovers once the precondition is met.
	//   - False with reason InvalidSpecification: the revision spec is invalid (a malformed revision/phase object or progression probe); correcting the configuration requires creating a replacement revision.
	//   - False with reason Archiving: the revision is being archived and its owned objects are still being torn down.
	//   - False with reason Archived: the revision has been archived and its objects have been torn down.
	//
	// Rollout completion is recorded separately by status.completedAt.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"` //nolint:kubeapilinter // This CRD uses structural list-map validation, not protobuf serialization.

	// totalObjects is the number of object declarations across all phases,
	// including phases that fail validation. It remains the declared total during
	// archival and does not decrease as objects are removed. It is reported after
	// every reconciliation, even when errors prevent observation of the objects.
	// Omitted before the first reconciliation; an explicit zero is a reported count.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	TotalObjects *int64 `json:"totalObjects,omitempty"`

	// presentObjects is the number of declared objects reported as present.
	// During reconciliation, this includes objects controlled by this revision
	// or a recognized sibling revision. Recognized siblings are other active,
	// non-deleting ClusterObjectSets with the same spec.group. Dry-run creations
	// and objects with ownership collisions are excluded.
	// During archival, this counts objects still owned by this revision and
	// awaiting teardown, including objects in phases waiting for their turn.
	// Objects controlled by another owner are excluded during archival, even if
	// they still exist on the cluster.
	// Reported on a best-effort basis. Revision- or phase-level errors may leave
	// this count unavailable, incomplete, or unchanged from an earlier observation.
	// A reported zero does not establish that every declared object is absent.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	PresentObjects *int64 `json:"presentObjects,omitempty"`

	// syncedObjects is the number of objects controlled by this revision whose
	// observed state matches this revision's desired state. Objects adopted by
	// newer revisions are excluded, even if their content matches this revision.
	// Reported on a best-effort basis; revision- or phase-level errors may leave
	// this count incomplete or unchanged from an earlier observation.
	// Omitted until available and during archival, when synchronization is not
	// evaluated. Omission does not imply a zero count.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	SyncedObjects *int64 `json:"syncedObjects,omitempty"`

	// healthyObjects is the number of present objects whose aggregate progression
	// probes from this revision evaluate to true. With no configured progression
	// probes, a present object is considered healthy. Synchronization to this
	// revision is not required: objects awaiting changes in paused phases or
	// controlled by recognized sibling revisions may be healthy without being
	// synced. This is probe-only health, not a count of completed reconciliations or
	// a guarantee of application availability, and does not make this revision Ready.
	// Reported on a best-effort basis; revision- or phase-level errors may leave
	// this count incomplete or unchanged from an earlier observation.
	// Omitted until available and during archival, when health is not evaluated.
	// Omission does not imply a zero count.
	//
	// Objects with an undetermined probe state are counted as neither healthy nor
	// unhealthy (see unhealthyObjects), and objects that have not yet been created
	// by the revision or have ownership collisions are excluded from both counts.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	HealthyObjects *int64 `json:"healthyObjects,omitempty"`

	// unhealthyObjects is the number of present objects whose aggregate progression
	// probes from this revision evaluate to false. Objects with an undetermined
	// probe state, for example because their status has not yet been reported or is
	// outdated, are counted as neither healthy nor unhealthy, so healthyObjects +
	// unhealthyObjects may be less than presentObjects. Objects that have not yet
	// been created by the revision or have ownership collisions are excluded from
	// both counts, so their sum may also be less than totalObjects.
	// Reported on a best-effort basis; revision- or phase-level errors may leave
	// this count incomplete or unchanged from an earlier observation.
	// Omitted until available and during archival, when health is not evaluated.
	// Omission does not imply a zero count.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	UnhealthyObjects *int64 `json:"unhealthyObjects,omitempty"`

	// supersededObjects is the number of objects observed under the control of a
	// recognized sibling with a higher revision number. During reconciliation,
	// these objects also count as present and may be healthy, but are not synced.
	// This describes observed ownership, not a cumulative count of transfers.
	// Reported on a best-effort basis. Revision- or phase-level errors may leave
	// this count unavailable, incomplete, or unchanged from an earlier observation.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	SupersededObjects *int64 `json:"supersededObjects,omitempty"`

	// collidingObjects is the number of desired objects whose adoption is refused
	// by the configured collision-protection policy. The conflicting resources
	// exist but do not count as present, synced, healthy, or superseded.
	// Reported on a best-effort basis. Revision- or phase-level errors may leave
	// this count unavailable, incomplete, or unchanged from an earlier observation.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	CollidingObjects *int64 `json:"collidingObjects,omitempty"`

	// phases reports lifecycle state, content integrity, and sparse diagnostics
	// for phases in spec.phases, correlated by name. The content digest pins the
	// initially resolved desired content; it does not describe live cluster state.
	// +kubebuilder:validation:MaxItems=20
	// +listType=map
	// +listMapKey=name
	// +optional
	Phases []ClusterObjectSetPhaseStatus `json:"phases,omitempty"`

	// completedAt is the timestamp at which the revision was first observed to be
	// ready, meaning it had successfully rolled out and all of its objects passed
	// their probes. It is set once and is immutable thereafter.
	//
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || oldSelf == null",message="completedAt is immutable, once set"
	// +optional
	CompletedAt metav1.Time `json:"completedAt,omitempty,omitzero"`
}

// ClusterObjectSetPhaseStatus reports a phase's current lifecycle state and
// diagnostics together with its pinned content digest and historical completion.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.completedAt) || has(self.completedAt)",message="completedAt cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.contentDigest) || has(self.contentDigest)",message="contentDigest cannot be removed once set"
type ClusterObjectSetPhaseStatus struct {
	// name is the phase name matching a phase in spec.phases.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`!format.dns1123Label().validate(self).hasValue()`,message="the value must consist of only lowercase alphanumeric characters and hyphens, and must start and end with an alphanumeric character."
	Name string `json:"name,omitempty"`

	// status summarizes the current lifecycle state of this phase:
	//   - Unknown: no usable evaluation of the phase exists yet.
	//   - Pending: application is waiting for an earlier phase, or teardown is
	//     waiting for a later phase.
	//   - Stabilizing: all objects are synced to this revision, but at least one
	//     progression-probe result is false or unknown.
	//   - Ready: all objects are synced to this revision and pass their progression probes.
	//   - Blocked: an identified blocker prevents progress; this does not imply a terminal failure.
	//   - Superseded: at least one object is controlled by a recognized sibling
	//     with a higher revision number.
	//   - Archiving: this is the active teardown phase and owned objects remain to be removed.
	//   - Archived: this revision's teardown responsibility for the phase is complete;
	//     objects adopted by newer revisions may still exist.
	//
	// Superseded does not imply that progression probes pass. Failing or unknown
	// probes may still prevent later phases from advancing.
	// +required
	Status PhaseStatus `json:"status,omitempty"`

	// reason is a machine-readable explanation of status. Known reasons include
	// NotEvaluated, WaitingForPreviousPhase, WaitingForLaterPhase,
	// ProgressionProbesPending, AllObjectsReady, InvalidSpecification,
	// PreconditionsNotMet, ContentUnavailable, ContentChanged, ObjectCollision,
	// ObjectsAdoptedByNewerRevision, RemovingOwnedObjects, and TeardownComplete.
	// Reasons may be extended; consumers must tolerate unrecognized values.
	// A Blocked status does not imply a terminal failure or a fixed retry policy.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:Pattern=`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`
	// +required
	Reason string `json:"reason,omitempty"`

	// completedAt is the timestamp when this phase was first observed Ready.
	// Omitted until that observation. Once recorded, it cannot change or be
	// removed, even if the phase later becomes unhealthy or superseded.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || oldSelf == null",message="completedAt is immutable, once set"
	CompletedAt metav1.Time `json:"completedAt,omitempty,omitzero"`

	// message provides human-readable context, such as a phase-wide validation
	// failure, content mismatch, or the name of another phase blocking progress.
	// Consumers must not parse this message. At most 1024 characters;
	// longer messages are truncated by the controller.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Message string `json:"message,omitempty"`

	// contentDigest pins the phase's initially resolved desired object content,
	// before controller mutations, in the format "<algorithm>:<hex>". Currently
	// sha256 is the only supported algorithm. Omitted until content is resolved;
	// once recorded, it cannot change or be removed. Matching content permits
	// reconciliation but does not establish live-object synchronization or health.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="contentDigest cannot change once set"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[a-z0-9]+:[a-f0-9]+$`
	// +optional
	ContentDigest string `json:"contentDigest,omitempty"`

	// lastTransitionTime is the timestamp when the current status was first
	// reported or last changed. Changes to reason or message alone do not update
	// this timestamp.
	// +required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty,omitzero"`

	// objectDetails contains sparse current diagnostics, not a full object
	// inventory. Include failed or unknown progression probes, ownership
	// collisions (including independently actionable collisions in paused phases),
	// and objects awaiting deletion in the active teardown phase. Superseded
	// objects remain eligible for probe diagnostics when they block progression.
	// Omit objects with no reportable diagnostics, expected pending work in paused
	// phases, and copies of phase-wide failures. Entries are removed when their
	// diagnostics resolve.
	// At most 50 entries, matching the maximum number of objects per phase.
	// +kubebuilder:validation:MaxItems=50
	// +listType=atomic
	// +optional
	ObjectDetails []ObjectStatus `json:"objectDetails,omitempty"`
}

// ObjectStatus identifies an object with current diagnostics. The object may be
// missing or controlled by a conflicting owner; it is not necessarily managed.
type ObjectStatus struct {
	// group is the API group of the object. Omitted for resources in the core API
	// group. At most 253 characters (DNS subdomain max).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`!format.dns1123Subdomain().validate(self).hasValue()`,message="the value must be a valid DNS subdomain: lowercase alphanumeric characters, hyphens, and dots, starting and ending with an alphanumeric character."
	// +optional
	Group string `json:"group,omitempty"`

	// resource is the lowercase, plural resource type of the object
	// (for example, "pods" or "configmaps"). Must be between 1 and 63
	// characters.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`!format.dns1035Label().validate(self).hasValue()`,message="the value must consist of only lowercase alphanumeric characters and hyphens, must start with a letter, and must end with an alphanumeric character."
	// +required
	Resource string `json:"resource,omitempty"`

	// namespace is the namespace of the object. Omitted for cluster-scoped
	// resources. At most 63 characters (DNS label max).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`!format.dns1123Label().validate(self).hasValue()`,message="the value must consist of only lowercase alphanumeric characters and hyphens, and must start and end with an alphanumeric character."
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// name is the name of the object. Must be between 1 and 253
	// characters (DNS subdomain max).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`self != "." && self != ".." && !self.contains("/") && !self.contains("%")`,message="name must be a valid object name: it may not be '.' or '..' or contain '/' or '%'"
	// +required
	Name string `json:"name,omitempty"`

	// diagnostics categorizes current object-specific problems. Known reasons
	// are ProgressionProbeFailed, ProgressionProbeUnknown, ObjectCollision, and
	// AwaitingDeletion. InvalidSpecification and PreconditionsNotMet are classified
	// at phase level only. Diagnostics are not an event history or individual
	// assertion results. Messages sharing a reason are grouped into one entry.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=18
	// +listType=map
	// +listMapKey=reason
	// +required
	Diagnostics []ObjectDiagnostic `json:"diagnostics,omitempty"`
}

// ObjectDiagnostic categorizes an observed object problem without exposing
// implementation-specific actions or claiming individual assertion outcomes.
type ObjectDiagnostic struct {
	// reason identifies the diagnostic category. Reasons may be extended;
	// consumers must tolerate unrecognized values. Retryability and terminality
	// must not be inferred from the presence of a diagnostic alone.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:Pattern=`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`
	// +required
	Reason string `json:"reason,omitempty"`

	// messages provides bounded human-readable explanations of this diagnostic.
	// Consumers must not parse these messages. Probe messages describe an aggregate
	// result; their number is not the number of failed assertions. AwaitingDeletion
	// establishes that teardown is incomplete, not necessarily its underlying cause.
	// +kubebuilder:validation:MaxItems=18
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=1024
	// +listType=atomic
	// +optional
	Messages []string `json:"messages,omitempty"`
}

// PhaseStatus describes the current state of a phase in the rollout.
//
// +kubebuilder:validation:Enum=Unknown;Pending;Stabilizing;Ready;Blocked;Superseded;Archiving;Archived
type PhaseStatus string

const (
	// PhaseStatusBlocked indicates an established blocker prevents progress.
	// Reason identifies the cause; Blocked does not imply a terminal failure.
	PhaseStatusBlocked PhaseStatus = "Blocked"

	// PhaseStatusPending indicates writes are waiting for an earlier application
	// phase or a later teardown phase, as identified by the reason.
	PhaseStatusPending PhaseStatus = "Pending"

	// PhaseStatusStabilizing indicates all objects in this phase are synced to
	// this revision, but at least one progression-probe result is false or unknown.
	PhaseStatusStabilizing PhaseStatus = "Stabilizing"

	// PhaseStatusReady indicates all objects in this phase are synced to this
	// revision and pass their progression probes.
	PhaseStatusReady PhaseStatus = "Ready"

	// PhaseStatusUnknown indicates no usable evaluation of this phase exists yet.
	PhaseStatusUnknown PhaseStatus = "Unknown"

	// PhaseStatusSuperseded indicates at least one object in this phase has been
	// adopted by a recognized newer revision. Failing or unknown progression
	// probes may still prevent later phases from advancing and remain visible
	// in objectDetails.
	PhaseStatusSuperseded PhaseStatus = "Superseded"

	// PhaseStatusArchiving indicates this is the active teardown phase and
	// owned objects remain to be removed.
	PhaseStatusArchiving PhaseStatus = "Archiving"

	// PhaseStatusArchived indicates this revision's teardown responsibility for
	// the phase is complete. Objects adopted by newer revisions may still exist.
	PhaseStatusArchived PhaseStatus = "Archived"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == '%s-%d'.format([self.spec.group, self.spec.revision])",message="metadata.name must be <spec.group>-<spec.revision>"
// +kubebuilder:selectablefield:JSONPath=`.spec.group`
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=`.spec.group`
// +kubebuilder:printcolumn:name="Revision",type=integer,JSONPath=`.spec.revision`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Total",type=integer,JSONPath=`.status.totalObjects`
// +kubebuilder:printcolumn:name="Present",type=integer,JSONPath=`.status.presentObjects`
// +kubebuilder:printcolumn:name="Synced",type=integer,JSONPath=`.status.syncedObjects`
// +kubebuilder:printcolumn:name="Healthy",type=integer,JSONPath=`.status.healthyObjects`
// +kubebuilder:printcolumn:name="Unhealthy",type=integer,JSONPath=`.status.unhealthyObjects`
// +kubebuilder:printcolumn:name="Superseded",type=integer,JSONPath=`.status.supersededObjects`,priority=1
// +kubebuilder:printcolumn:name="Colliding",type=integer,JSONPath=`.status.collidingObjects`,priority=1
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterObjectSet represents an immutable snapshot of Kubernetes objects to
// apply and manage on the cluster. Each revision contains objects organized into
// phases that roll out sequentially. The same object can only be managed by a
// single revision at a time. Ownership of objects is transitioned from one revision
// to the next as new revisions are rolled out.
//
// For revisions owned by a ClusterObjectDeployment, that deployment's controller
// requests archival of older active revisions once a newer owned, non-deleting,
// active revision has recorded rollout completion in status.completedAt. This
// decision uses historical completion, not current readiness, and does not require
// the newest revision to have completed. The ClusterObjectSet controller performs
// teardown when spec.lifecycleState is set to "Archived"; it does not request
// archival of sibling revisions. Standalone callers manage archival explicitly.
// Archived revisions owned by a ClusterObjectDeployment may be pruned after
// teardown according to its spec.revisionHistoryLimit.
//
// Set metadata.name explicitly to <spec.group>-<spec.revision>, using decimal
// notation without leading zeros. This ensures revision uniqueness within a group.
// Name generation through metadata.generateName is not supported.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.status) || has(self.status)",message="status cannot be removed once set"
type ClusterObjectSet struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of the ClusterObjectSet.
	// +required
	Spec ClusterObjectSetSpec `json:"spec,omitzero"`

	// status is optional and defines the observed state of the ClusterObjectSet.
	// totalObjects is reported after every reconciliation. All other object
	// counts are best-effort summaries of the observations available, not an
	// atomic inventory of live objects. Revision- or phase-level errors may leave
	// those counts unavailable, incomplete, or unchanged from earlier observations,
	// so they may be older than conditions. A reported zero is not proof that all
	// objects were evaluated.
	// During archival, syncedObjects and healthyObjects are omitted.
	// Cannot be removed once set; this is to prevent immutable fields such as completedAt
	// from being cleared by an update which omits the status.
	// +optional
	Status ClusterObjectSetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterObjectSetList contains a list of ClusterObjectSet
type ClusterObjectSetList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is a required list of ClusterObjectSet objects.
	//
	// +required
	Items []ClusterObjectSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ClusterObjectSet{}, &ClusterObjectSetList{})
		return nil
	})
}
