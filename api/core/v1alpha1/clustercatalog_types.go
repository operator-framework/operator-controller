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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +enum
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

	MetadataNameLabel = "olm.operatorframework.io/metadata.name"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name=LastUnpacked,type=date,JSONPath=`.status.lastUnpacked`
//+kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterCatalog is the Schema for the ClusterCatalogs API
type ClusterCatalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec ClusterCatalogSpec `json:"spec"`
	// +optional
	Status ClusterCatalogStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterCatalogList contains a list of ClusterCatalog
type ClusterCatalogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ClusterCatalog `json:"items"`
}

// ClusterCatalogSpec defines the desired state of ClusterCatalog
// +kubebuilder:validation:XValidation:rule="!has(self.source.image.pollInterval) || (self.source.image.ref.find('@sha256:') == \"\")",message="cannot specify PollInterval while using digest-based image"
type ClusterCatalogSpec struct {
	// source is the source of a Catalog that contains catalog metadata in the FBC format
	// https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs
	Source CatalogSource `json:"source"`

	// priority is used as the tie-breaker between bundles selected from different catalogs; a higher number means higher priority.
	// +kubebuilder:default:=0
	// +optional
	Priority int32 `json:"priority,omitempty"`
}

// ClusterCatalogStatus defines the observed state of ClusterCatalog
type ClusterCatalogStatus struct {
	// conditions store the status conditions of the ClusterCatalog instances
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// resolvedSource contains information about the resolved source
	// +optional
	ResolvedSource *ResolvedCatalogSource `json:"resolvedSource,omitempty"`
	// contentURL is a cluster-internal address that on-cluster components
	// can read the content of a catalog from
	// +optional
	ContentURL string `json:"contentURL,omitempty"`
	// observedGeneration is the most recent generation observed for this ClusterCatalog. It corresponds to the
	// ClusterCatalog's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastUnpacked represents the time when the
	// ClusterCatalog object was last unpacked.
	// +optional
	LastUnpacked metav1.Time `json:"lastUnpacked,omitempty"`
}

// CatalogSource contains the sourcing information for a Catalog
type CatalogSource struct {
	// type defines the kind of Catalog content being sourced.
	// +unionDiscriminator
	// +kubebuilder:validation:Enum:="image"
	// +kubebuilder:validation:Required
	Type SourceType `json:"type"`
	// image is the catalog image that backs the content of this catalog.
	// +optional
	Image *ImageSource `json:"image,omitempty"`
}

// ResolvedCatalogSource contains the information about a sourced Catalog
type ResolvedCatalogSource struct {
	// type defines the kind of Catalog content that was sourced.
	// +unionDiscriminator
	// +kubebuilder:validation:Enum:="image"
	// +kubebuilder:validation:Required
	Type SourceType `json:"type"`
	// image is the catalog image that backs the content of this catalog.
	Image *ResolvedImageSource `json:"image"`
}

// ResolvedImageSource contains information about the sourced Catalog
type ResolvedImageSource struct {
	// ref contains the reference to a container image containing Catalog contents.
	Ref string `json:"ref"`
	// resolvedRef contains the resolved sha256 image ref containing Catalog contents.
	ResolvedRef string `json:"resolvedRef"`
	// lastPollAtempt is the time when the source resolved was last polled for new content.
	LastPollAttempt metav1.Time `json:"lastPollAttempt"`
	// LastUnpacked is the time when the catalog contents were successfully unpacked.
	LastUnpacked metav1.Time `json:"lastUnpacked"`
}

// ImageSource contains information required for sourcing a Catalog from an OCI image
type ImageSource struct {
	// ref contains the reference to a container image containing Catalog contents.
	Ref string `json:"ref"`
	// pollInterval indicates the interval at which the image source should be polled for new content,
	// specified as a duration (e.g., "5m", "1h", "24h", "etc".). Note that PollInterval may not be
	// specified for a catalog image referenced by a sha256 digest.
	// +kubebuilder:validation:Format:=duration
	// +optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ClusterCatalog{}, &ClusterCatalogList{})
}
