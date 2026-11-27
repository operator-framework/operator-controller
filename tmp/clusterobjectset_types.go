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
	// The revision progresses to the next phase only after all objects in the current phase pass their progression probes.
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
	ConditionEqual ConditionEqualProbe `json:"conditionEqual,omitzero"`

	// cel contains a boolean CEL rule evaluated against the object's live state.
	// A False result reports the configured message; an evaluation error produces
	// Unknown and reports the error instead. Compilation and boolean result-type
	// checking occur before evaluation, not during CR admission.
	// Required when type is "CEL", and forbidden otherwise.
	// +unionMember
	// +optional
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
	// are invalid configuration. CR admission validates only the configuration's
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
	Type string `json:"type,omitempty"`

	// status sets the expected condition status.
	//
	// Allowed values are "True" and "False".
	//
	// +kubebuilder:validation:Enum=True;False
	// +required
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
// complete only after all objects pass their progression probes.
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
	// When set to "ObjectSliceRef", a slice name and entry identity identify an object manifest in a cluster-scoped ClusterObjectSlice.
	//
	// +unionDiscriminator
	// +required
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
// ClusterObjectSlice by the slice's name and the manifest's target identity.
type ObjectSliceSourceRef struct {
	// name is the name of the referenced ClusterObjectSlice.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`!format.dns1123Subdomain().validate(self).hasValue()`,message="the value must consist of only lowercase alphanumeric characters, hyphens, and dots, and must start and end with an alphanumeric character."
	Name string `json:"name,omitempty"`

	// entry identifies the desired object manifest within the referenced
	// ClusterObjectSlice by its target identity: apiVersion, kind, name, and
	// namespace. The identity must match the selected slice entry's key
	// exactly. Omitted and empty namespaces identify the same cluster-scoped
	// entry. The slice entry's content is not validated against this identity
	// at admission.
	//
	// +required
	Entry ObjectSliceEntryRef `json:"entry,omitzero"`
}

// ObjectSliceEntryRef selects a ClusterObjectSlice entry by its target
// identity, matching the composite key under which entries are stored in the
// referenced slice. Field names and validation mirror the target-identity
// fields of the slice's stored manifests.
type ObjectSliceEntryRef struct {
	// apiVersion is the required API version of the referenced object
	// manifest, for example "v1" or "apps/v1". It must match the apiVersion
	// of the selected slice entry.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=317
	// +kubebuilder:validation:XValidation:rule=`self.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?[a-z]([-a-z0-9]*[a-z0-9])?$')`,message="apiVersion must be a version or group/version"
	APIVersion string `json:"apiVersion,omitempty"`

	// kind is the required kind of the referenced object manifest, for
	// example "ConfigMap". It must match the kind of the selected slice entry.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$')`,message="kind must start with a letter and contain only letters, digits, or hyphens, ending with an alphanumeric character"
	Kind string `json:"kind,omitempty"`

	// name is the required metadata.name of the referenced object manifest.
	// It must match the name of the selected slice entry.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`self != "." && self != ".." && !self.contains("/") && !self.contains("%")`,message="name must not be '.', '..', or contain '/' or '%'"
	Name string `json:"name,omitempty"`

	// namespace is the metadata.namespace of the referenced object manifest.
	// When omitted, it identifies a cluster-scoped entry; omitted and empty
	// namespaces identify the same entry. Nonempty values must be lowercase
	// DNS labels of at most 63 characters. Namespaced objects must declare
	// their namespace explicitly.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`self == "" || !format.dns1123Label().validate(self).hasValue()`,message="namespace must be empty or a valid lowercase DNS label"
	Namespace string `json:"namespace,omitempty"`
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
	//   - False with reason ObjectsNotReady: one or more objects are still being applied or have not yet passed their progression probes during rollout.
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

	// objectCounts reports counts of the revision's objects by state.
	// total is reported after every reconciliation; all other counts are
	// best-effort summaries of the observations available, not an atomic
	// inventory of live objects, and may be older than conditions.
	// Omitted before the first reconciliation.
	// +optional
	ObjectCounts ObjectCounts `json:"objectCounts,omitzero"`

	// phases reports content integrity and sparse diagnostics for phases in
	// spec.phases, correlated by name. The content digest pins the initially
	// resolved desired content; it does not describe live cluster state.
	// <opcon:experimental:description>
	// Each phase also reports its current lifecycle state through its status,
	// reason, and message.
	// </opcon:experimental:description>
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

// ObjectCounts reports counts of a ClusterObjectSet's objects by state.
// All counts are best-effort summaries of the observations available, not an
// atomic inventory of live objects. Revision- or phase-level errors may leave
// them unavailable, incomplete, or unchanged from earlier observations.
type ObjectCounts struct {
	// total is the number of object declarations across all phases, including
	// phases that fail validation. It remains the declared total during archival
	// and does not decrease as objects are removed. It is reported after every
	// reconciliation, even when errors prevent observation of the objects.
	// Omitted before the first reconciliation; an explicit zero is a reported count.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Total *int64 `json:"total,omitempty"`

	// present is the number of declared objects reported as present.
	// During reconciliation, this includes objects controlled by this revision
	// or a recognized sibling revision. Recognized siblings are other active,
	// non-deleting ClusterObjectSets with the same spec.group. Dry-run creations
	// and objects with ownership collisions are excluded.
	// During archival, this counts objects still owned by this revision and
	// awaiting teardown, including objects in phases waiting for their turn.
	// Objects controlled by another owner are excluded during archival, even if
	// they still exist on the cluster.
	// A reported zero does not establish that every declared object is absent.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Present *int64 `json:"present,omitempty"`

	// synced is the number of objects controlled by this revision whose observed
	// state matches this revision's desired state. Objects adopted by newer
	// revisions are excluded, even if their content matches this revision.
	// Omitted until observed and during archival, when synchronization is not
	// evaluated. Omission does not imply a zero count.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Synced *int64 `json:"synced,omitempty"`

	// superseded is the number of objects observed under the control of a
	// recognized sibling with a higher revision number. During reconciliation,
	// these objects also count as present, but are not synced. This describes
	// observed ownership, not a cumulative count of transfers.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Superseded *int64 `json:"superseded,omitempty"`

	// colliding is the number of desired objects whose adoption is refused by
	// the configured collision-protection policy. The conflicting resources
	// exist but do not count as present, synced, or superseded.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Colliding *int64 `json:"colliding,omitempty"`
}

// ClusterObjectSetPhaseStatus reports a phase's sparse diagnostics together
// with its pinned content digest and historical completion.
// <opcon:experimental:description>
// It also reports the phase's current lifecycle state via status, reason, and
// message.
// </opcon:experimental:description>
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
	// <opcon:experimental>
	Status PhaseStatus `json:"status,omitempty"`

	// reason is a machine-readable explanation of status:
	//   - NotEvaluated: no usable evaluation of the phase exists yet (status Unknown).
	//   - WaitingForPreviousPhase: application is waiting for an earlier phase to
	//     complete its rollout (status Pending).
	//   - WaitingForLaterPhase: teardown is waiting for a later phase to complete
	//     its teardown (status Pending).
	//   - ProgressionProbesPending: all objects are synced to this revision, but at
	//     least one progression-probe result is false or unknown (status Stabilizing).
	//   - AllObjectsReady: all objects are synced to this revision and pass their
	//     progression probes (status Ready).
	//   - InvalidSpecification: the phase's declared content or progression probes
	//     are malformed; correcting the configuration requires creating a
	//     replacement revision (status Blocked).
	//   - PreconditionsNotMet: a precondition for applying the phase is not yet
	//     satisfied, for example a rejected preflight dry-run; the revision keeps
	//     retrying and recovers once the precondition is met (status Blocked).
	//   - ContentUnavailable: referenced object content is unavailable or invalid
	//     (status Blocked).
	//   - ContentChanged: referenced object content changed after this phase's
	//     content was initially resolved, violating the pinned content digest
	//     (status Blocked).
	//   - ObjectCollision: one or more objects are controlled by another owner,
	//     or are unowned while collision protection prevents adoption
	//     (status Blocked).
	//   - ObjectsAdoptedByNewerRevision: at least one object is controlled by a
	//     recognized sibling revision with a higher revision number
	//     (status Superseded).
	//   - RemovingOwnedObjects: this is the active teardown phase and owned
	//     objects remain to be removed (status Archiving).
	//   - TeardownComplete: this revision's teardown responsibility for the phase
	//     is complete; objects adopted by newer revisions may still exist
	//     (status Archived).
	//
	// A Blocked status does not imply a terminal failure or a fixed retry policy.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:Pattern=`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`
	// +required
	// <opcon:experimental>
	Reason string `json:"reason,omitempty"`

	// completedAt is the timestamp when this phase was first observed Ready.
	// Omitted until that observation. Once recorded, it cannot change or be
	// removed, even if the phase later fails its progression probes or is
	// superseded.
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
	// <opcon:experimental>
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
	// <opcon:experimental>
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

// ObjectStatus identifies an object with current diagnostics, using the same
// target identity fields as the spec's object references (see
// ObjectSliceEntryRef), so status entries correlate directly with the
// declared objects. The object may be missing or controlled by a conflicting
// owner; it is not necessarily managed.
type ObjectStatus struct {
	// apiVersion is the API version of the object, for example "v1" or
	// "apps/v1". It matches the apiVersion of the declared object manifest.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=317
	// +kubebuilder:validation:XValidation:rule=`self.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?[a-z]([-a-z0-9]*[a-z0-9])?$')`,message="apiVersion must be a version or group/version"
	APIVersion string `json:"apiVersion,omitempty"`

	// kind is the kind of the object, for example "ConfigMap". It matches the
	// kind of the declared object manifest.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$')`,message="kind must start with a letter and contain only letters, digits, or hyphens, ending with an alphanumeric character"
	Kind string `json:"kind,omitempty"`

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

	// diagnostics categorizes current object-specific problems. InvalidSpecification
	// and PreconditionsNotMet are classified at phase level only. Diagnostics are
	// not an event history or individual assertion results. Messages sharing a
	// reason are grouped into one entry.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=18
	// +listType=map
	// +listMapKey=reason
	// +required
	Diagnostics []ObjectDiagnostic `json:"diagnostics,omitempty"`
}

// ObjectDiagnosticReasons are the known reason values for ObjectDiagnostic.reason.
const (
	// ObjectDiagnosticReasonProgressionProbeFailed indicates the object's
	// aggregate progression probes evaluated to false.
	ObjectDiagnosticReasonProgressionProbeFailed = "ProgressionProbeFailed"

	// ObjectDiagnosticReasonProgressionProbeUnknown indicates the object's
	// aggregate progression probes could not be determined, for example because
	// the object's status has not yet been reported or is outdated.
	ObjectDiagnosticReasonProgressionProbeUnknown = "ProgressionProbeUnknown"

	// ObjectDiagnosticReasonObjectCollision indicates the object exists but
	// this revision cannot take ownership of it under the configured collision
	// protection policy.
	ObjectDiagnosticReasonObjectCollision = "ObjectCollision"

	// ObjectDiagnosticReasonAwaitingDeletion indicates the object is owned by
	// this revision, has been requested for deletion, and is not yet gone from
	// the cluster.
	ObjectDiagnosticReasonAwaitingDeletion = "AwaitingDeletion"
)

// ObjectDiagnostic categorizes an observed object problem without exposing
// implementation-specific actions or claiming individual assertion outcomes.
type ObjectDiagnostic struct {
	// reason identifies the diagnostic category:
	//   - ProgressionProbeFailed: the object's aggregate progression probes
	//     evaluated to false.
	//   - ProgressionProbeUnknown: the object's aggregate progression probes
	//     could not be determined, for example because the object's status has
	//     not yet been reported or is outdated.
	//   - ObjectCollision: the object exists but this revision cannot take
	//     ownership of it under the configured collision protection policy.
	//   - AwaitingDeletion: the object is owned by this revision, has been
	//     requested for deletion, and is not yet gone from the cluster.
	//
	// Retryability and terminality must not be inferred from the presence of a
	// diagnostic alone.
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

// Phase status reasons are the known reason values for
// ClusterObjectSetPhaseStatus.reason.
const (
	// PhaseStatusReasonNotEvaluated indicates no usable evaluation of the phase
	// exists yet (status Unknown).
	PhaseStatusReasonNotEvaluated = "NotEvaluated"

	// PhaseStatusReasonWaitingForPreviousPhase indicates application is waiting
	// for an earlier phase to complete its rollout (status Pending).
	PhaseStatusReasonWaitingForPreviousPhase = "WaitingForPreviousPhase"

	// PhaseStatusReasonWaitingForLaterPhase indicates teardown is waiting for a
	// later phase to complete its teardown (status Pending).
	PhaseStatusReasonWaitingForLaterPhase = "WaitingForLaterPhase"

	// PhaseStatusReasonProgressionProbesPending indicates all objects are synced
	// to this revision, but at least one progression-probe result is false or
	// unknown (status Stabilizing).
	PhaseStatusReasonProgressionProbesPending = "ProgressionProbesPending"

	// PhaseStatusReasonAllObjectsReady indicates all objects are synced to this
	// revision and pass their progression probes (status Ready).
	PhaseStatusReasonAllObjectsReady = "AllObjectsReady"

	// PhaseStatusReasonInvalidSpecification indicates the phase's declared content
	// or progression probes are malformed; correcting the configuration requires
	// creating a replacement revision (status Blocked).
	PhaseStatusReasonInvalidSpecification = "InvalidSpecification"

	// PhaseStatusReasonPreconditionsNotMet indicates a precondition for applying
	// the phase is not yet satisfied, for example a rejected preflight dry-run;
	// the revision keeps retrying and recovers once the precondition is met
	// (status Blocked).
	PhaseStatusReasonPreconditionsNotMet = "PreconditionsNotMet"

	// PhaseStatusReasonContentUnavailable indicates referenced object content is
	// unavailable or invalid (status Blocked).
	PhaseStatusReasonContentUnavailable = "ContentUnavailable"

	// PhaseStatusReasonContentChanged indicates referenced object content changed
	// after this phase's content was initially resolved, violating the pinned
	// content digest (status Blocked).
	PhaseStatusReasonContentChanged = "ContentChanged"

	// PhaseStatusReasonObjectCollision indicates one or more objects are controlled
	// by another owner, or are unowned while collision protection prevents
	// adoption (status Blocked).
	PhaseStatusReasonObjectCollision = "ObjectCollision"

	// PhaseStatusReasonObjectsAdoptedByNewerRevision indicates at least one object
	// is controlled by a recognized sibling revision with a higher revision
	// number (status Superseded).
	PhaseStatusReasonObjectsAdoptedByNewerRevision = "ObjectsAdoptedByNewerRevision"

	// PhaseStatusReasonRemovingOwnedObjects indicates this is the active teardown
	// phase and owned objects remain to be removed (status Archiving).
	PhaseStatusReasonRemovingOwnedObjects = "RemovingOwnedObjects"

	// PhaseStatusReasonTeardownComplete indicates this revision's teardown
	// responsibility for the phase is complete; objects adopted by newer
	// revisions may still exist (status Archived).
	PhaseStatusReasonTeardownComplete = "TeardownComplete"
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
// +kubebuilder:printcolumn:name="Total",type=integer,JSONPath=`.status.objectCounts.total`
// +kubebuilder:printcolumn:name="Present",type=integer,JSONPath=`.status.objectCounts.present`
// +kubebuilder:printcolumn:name="Synced",type=integer,JSONPath=`.status.objectCounts.synced`
// +kubebuilder:printcolumn:name="Superseded",type=integer,JSONPath=`.status.objectCounts.superseded`,priority=1
// +kubebuilder:printcolumn:name="Colliding",type=integer,JSONPath=`.status.objectCounts.colliding`,priority=1
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
	// objectCounts.total is reported after every reconciliation. All other object
	// counts are best-effort summaries of the observations available, not an
	// atomic inventory of live objects. Revision- or phase-level errors may leave
	// those counts unavailable, incomplete, or unchanged from earlier observations,
	// so they may be older than conditions. A reported zero is not proof that all
	// objects were evaluated.
	// During archival, objectCounts.synced is omitted.
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
