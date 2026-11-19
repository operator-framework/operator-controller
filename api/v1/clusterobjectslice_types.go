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

const ClusterObjectSliceKind = "ClusterObjectSlice"

// ClusterObjectSliceEncoding identifies the representation of a stored manifest.
// +kubebuilder:validation:Enum=JSON;GzipJSON
type ClusterObjectSliceEncoding string

const (
	// ClusterObjectSliceEncodingJSON stores a JSON-serialized manifest.
	ClusterObjectSliceEncodingJSON ClusterObjectSliceEncoding = "JSON"
	// ClusterObjectSliceEncodingGzipJSON stores a gzip-compressed JSON manifest.
	ClusterObjectSliceEncodingGzipJSON ClusterObjectSliceEncoding = "GzipJSON"
)

// ClusterObjectSliceSpec defines an immutable group of stored manifests.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type ClusterObjectSliceSpec struct {
	// objects is a required list of 1 to 256 stored manifests. Each entry must
	// have a unique combination of apiVersion, kind, name, and namespace.
	// List order has no rollout meaning. Create a new slice to change content.
	//
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=256
	// +listType=map
	// +listMapKey=apiVersion
	// +listMapKey=kind
	// +listMapKey=name
	// +listMapKey=namespace
	Objects []ClusterObjectSliceEntry `json:"objects,omitempty"`
}

// ClusterObjectSliceEntry stores a manifest with an explicit target identity.
// Admission validates the envelope, not the encoded manifest. Publishers must
// ensure that the decoded manifest agrees with the declared identity.
type ClusterObjectSliceEntry struct {
	// apiVersion is the required API version of the stored object, for example
	// "v1" or "apps/v1". It must be 1 to 317 characters, with an optional
	// lowercase DNS group followed by "/" and a lowercase API version.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=317
	// +kubebuilder:validation:XValidation:rule=`self.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?[a-z]([-a-z0-9]*[a-z0-9])?$')`,message="apiVersion must be a version or group/version"
	APIVersion string `json:"apiVersion,omitempty"`

	// kind is the required kind of the stored object, for example "ConfigMap".
	// It must be 1 to 63 characters, start with a letter, end with an
	// alphanumeric character, and contain only letters, digits, or hyphens.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$')`,message="kind must start with a letter and contain only letters, digits, or hyphens, ending with an alphanumeric character"
	Kind string `json:"kind,omitempty"`

	// name is the required metadata.name of the stored object. It must be
	// 1 to 253 characters, cannot be "." or "..", and cannot contain "/" or "%".
	// Resource-specific naming constraints are checked when the object is applied.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule=`self != "." && self != ".." && !self.contains("/") && !self.contains("%")`,message="name must not be '.', '..', or contain '/' or '%'"
	Name string `json:"name,omitempty"`

	// namespace is the metadata.namespace of the stored object. When omitted,
	// it defaults to the empty string for a cluster-scoped object. Nonempty
	// values must be lowercase DNS labels of at most 63 characters.
	// Namespaced objects must declare their namespace explicitly.
	//
	// +optional
	// +kubebuilder:default=""
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule=`self == "" || !format.dns1123Label().validate(self).hasValue()`,message="namespace must be empty or a valid lowercase DNS label"
	Namespace string `json:"namespace,omitempty"` //nolint:kubeapilinter // Omitted and empty namespaces intentionally identify the same cluster-scoped key.

	// encoding is the required representation of content. "JSON" means one
	// JSON-serialized Kubernetes object; "GzipJSON" means gzip-compressed JSON.
	// Base64 wire encoding applies to both representations.
	//
	// +required
	Encoding ClusterObjectSliceEncoding `json:"encoding,omitempty"`

	// content is the required manifest payload, base64-encoded in JSON and YAML.
	// Its base64 representation must contain 1 to 1048576 characters. The limit
	// does not bound expanded gzip content or the total serialized slice size.
	// Admission does not parse JSON, decompress gzip, or verify that the payload
	// matches apiVersion, kind, name, and namespace.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1048576
	Content []byte `json:"content,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=cosl
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterObjectSlice stores immutable, encoded Kubernetes manifests independently
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
