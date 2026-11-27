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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const ClusterObjectSliceKind = "ClusterObjectSlice"

// ClusterObjectSliceType defines the type of content stored in a ClusterObjectSlice.
// +kubebuilder:validation:Enum=InlineManifests
type ClusterObjectSliceType string

const (
	// ClusterObjectSliceTypeInlineManifests stores Kubernetes object manifests
	// inline in the slice.
	ClusterObjectSliceTypeInlineManifests ClusterObjectSliceType = "InlineManifests"
)

// ClusterObjectSliceSpec defines an immutable group of stored manifests.
// List order has no meaning; create a new slice to change content.
//
// +union
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
// +kubebuilder:validation:XValidation:rule="self.type == 'InlineManifests' ? has(self.manifests) : !has(self.manifests)",message="manifests is required when type is InlineManifests, and forbidden otherwise"
type ClusterObjectSliceSpec struct {
	// type is a required field which specifies the type of content stored in
	// the slice.
	//
	// The allowed content type is "InlineManifests".
	//
	// When set to "InlineManifests", the slice stores Kubernetes object
	// manifests inline in spec.manifests.
	//
	// +unionDiscriminator
	// +kubebuilder:validation:Enum=InlineManifests
	// +required
	Type ClusterObjectSliceType `json:"type,omitempty"`

	// manifests is a required list of 1 to 256 Kubernetes object manifests
	// stored inline in the slice. Each manifest must be a valid Kubernetes
	// resource with a non-empty apiVersion, kind, and metadata.name. Admission
	// validates only this structure; it does not validate a manifest against
	// the schema of its apiVersion and kind, does not verify resource scope or
	// namespace correctness, and does not enforce uniqueness of the manifests'
	// target identities (apiVersion, kind, name, and namespace).
	//
	// Each manifest is identified by its own target identity: apiVersion,
	// kind, metadata.name, and metadata.namespace. Consumers select a manifest
	// by that identity.
	//
	// List order has no rollout meaning.
	//
	// +unionMember
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=256
	// +listType=atomic
	// +kubebuilder:validation:items:XEmbeddedResource
	// +kubebuilder:validation:items:XPreserveUnknownFields
	// +kubebuilder:validation:items:XValidation:rule="has(self.apiVersion) && has(self.kind) && has(self.metadata)",message="manifests items must be a valid Kubernetes resource with apiVersion, kind, and metadata"
	// +kubebuilder:validation:items:XValidation:rule="self.apiVersion != '' && self.kind != ''",message="apiVersion and kind must be non-empty"
	// +kubebuilder:validation:items:XValidation:rule="has(self.metadata.name) && self.metadata.name != ''",message="metadata.name is required"
	Manifests []unstructured.Unstructured `json:"manifests,omitzero"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=cosl
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterObjectSlice stores immutable Kubernetes object manifests independently
// of rollout intent. It is an experimental, cluster-scoped API. Creating a slice
// stores content only; it does not apply objects or report rollout readiness.
type ClusterObjectSlice struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata. Labels, annotations, and
	// owner references can change independently of the immutable content.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the required, immutable content of the slice.
	// +required
	Spec ClusterObjectSliceSpec `json:"spec,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterObjectSliceList contains ClusterObjectSlice objects.
type ClusterObjectSliceList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is the list of slices.
	// +required
	Items []ClusterObjectSlice `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ClusterObjectSlice{}, &ClusterObjectSliceList{})
		return nil
	})
}
