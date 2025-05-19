package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

// ClusterObjectSlice is the Schema for the clusterobjectslices API
type ClusterObjectSlice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="objects is immutable"
	// +kubebuilder:MaxItems=32
	Objects []ClusterExtensionRevisionObject `json:"objects"`
}

// +kubebuilder:object:root=true

// ClusterObjectSliceList contains a list of ClusterObjectSlice
type ClusterObjectSliceList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is a required list of ClusterObjectSlice objects.
	//
	// +kubebuilder:validation:Required
	Items []ClusterObjectSlice `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterObjectSlice{}, &ClusterObjectSliceList{})
}
