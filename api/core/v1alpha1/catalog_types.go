/*
Copyright 2022.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO: The source types, reason, etc. are all copy/pasted from the rukpak
//   repository. We should look into whether it is possible to share these.

type SourceType string

const (
	SourceTypeImage SourceType = "image"

	TypeUnpacked = "Unpacked"
	TypeDelete   = "Delete"

	ReasonUnpackPending       = "UnpackPending"
	ReasonUnpacking           = "Unpacking"
	ReasonUnpackSuccessful    = "UnpackSuccessful"
	ReasonUnpackFailed        = "UnpackFailed"
	ReasonStorageFailed       = "FailedToStore"
	ReasonStorageDeleteFailed = "FailedToDelete"

	PhasePending   = "Pending"
	PhaseUnpacking = "Unpacking"
	PhaseFailing   = "Failing"
	PhaseUnpacked  = "Unpacked"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// Catalog is the Schema for the Catalogs API
type Catalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CatalogSpec   `json:"spec,omitempty"`
	Status CatalogStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CatalogList contains a list of Catalog
type CatalogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Catalog `json:"items"`
}

// CatalogSpec defines the desired state of Catalog
// +kubebuilder:validation:XValidation:rule="!has(self.source.image.pollInterval) || (self.source.image.ref.find('@sha256:') == \"\")",message="cannot specify PollInterval while using digest-based image"
type CatalogSpec struct {
	// Source is the source of a Catalog that contains Operators' metadata in the FBC format
	// https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs
	Source CatalogSource `json:"source"`
}

// CatalogStatus defines the observed state of Catalog
type CatalogStatus struct {
	// Conditions store the status conditions of the Catalog instances
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// ResolvedSource contains information about the resolved source
	ResolvedSource *ResolvedCatalogSource `json:"resolvedSource,omitempty"`
	// Phase represents a human-readable status of resolution of the content source.
	// It is not appropriate to use for business logic determination.
	Phase string `json:"phase,omitempty"`
	// ContentURL is a cluster-internal address that on-cluster components
	// can read the content of a catalog from
	ContentURL string `json:"contentURL,omitempty"`
	// observedGeneration is the most recent generation observed for this Catalog. It corresponds to the
	// Catalog's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// CatalogSource contains the sourcing information for a Catalog
type CatalogSource struct {
	// Type defines the kind of Catalog content being sourced.
	// +kubebuilder:validation:Enum=image
	Type SourceType `json:"type"`
	// Image is the catalog image that backs the content of this catalog.
	Image *ImageSource `json:"image,omitempty"`
}

// ResolvedCatalogSource contains the information about a sourced Catalog
type ResolvedCatalogSource struct {
	// Type defines the kind of Catalog content that was sourced.
	Type SourceType `json:"type"`
	// Image is the catalog image that backs the content of this catalog.
	Image *ResolvedImageSource `json:"image,omitempty"`
}

// ResolvedImageSource contains information about the sourced Catalog
type ResolvedImageSource struct {
	// Ref contains the reference to a container image containing Catalog contents.
	Ref string `json:"ref"`
	// ResolvedRef contains the resolved sha256 image ref containing Catalog contents.
	ResolvedRef string `json:"resolvedRef"`
	// LastPollAtempt is the time when the source resolved was last polled for new content.
	LastPollAttempt metav1.Time `json:"lastPollAttempt"`
	// pullSecret exists to retain compatibility with the existing v1alpha1 APIs. It will be removed in v1alpha2.
	PullSecret string `json:"pullSecret,omitempty"`
}

// ImageSource contains information required for sourcing a Catalog from an OCI image
type ImageSource struct {
	// Ref contains the reference to a container image containing Catalog contents.
	Ref string `json:"ref"`
	// PullSecret contains the name of the image pull secret in the namespace that catalogd is deployed.
	PullSecret string `json:"pullSecret,omitempty"`
	// PollInterval indicates the interval at which the image source should be polled for new content,
	// specified as a duration (e.g., "5m", "1h", "24h", "etc".). Note that PollInterval may not be
	// specified for a catalog image referenced by a sha256 digest.
	// +kubebuilder:validation:Format:=duration
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`
	// InsecureSkipTLSVerify indicates that TLS certificate validation should be skipped.
	// If this option is specified, the HTTPS protocol will still be used to
	// fetch the specified image reference.
	// This should not be used in a production environment.
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Catalog{}, &CatalogList{})
}
