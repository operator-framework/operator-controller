package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// BundleConfig ...
// +kubebuilder:object:root=true
type BundleConfig struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata"`

	// spec is the desired state of the BundleConfig.
	// spec is required.
	Spec BundleConfigSpec `json:"spec"`
}

type BundleConfigSpec struct {
	// watchNamespace configures what Namespace the Operator should watch.
	// setting it to the same value as the install namespace corresponds to OwnNamespace mode,
	// setting it to a different value corresponds to SingleNamespace mode.
	WatchNamespace string `json:"watchNamespace,omitempty"`
}

func init() {
	SchemeBuilder.Register(&BundleConfig{})
}
