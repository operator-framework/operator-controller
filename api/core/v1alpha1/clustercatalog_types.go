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

// SourceType defines the type of source used for catalogs.
// +enum
type SourceType string

const (
	SourceTypeImage SourceType = "Image"

	TypeProgressing = "Progressing"
	TypeServing     = "Serving"

	// Serving reasons
	ReasonAvailable   = "Available"
	ReasonUnavailable = "Unavailable"

	// Progressing reasons
	ReasonSucceeded = "Succeeded"
	ReasonRetrying  = "Retrying"
	ReasonBlocked   = "Blocked"

	MetadataNameLabel = "olm.operatorframework.io/metadata.name"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name=LastUnpacked,type=date,JSONPath=`.status.lastUnpacked`
//+kubebuilder:printcolumn:name="Serving",type=string,JSONPath=`.status.conditions[?(@.type=="Serving")].status`
//+kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterCatalog enables users to make File-Based Catalog (FBC) catalog data available to the cluster.
// For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs
type ClusterCatalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   ClusterCatalogSpec   `json:"spec"`
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
	// source is a required field that allows the user to define the source of a Catalog that contains catalog metadata in the File-Based Catalog (FBC) format.
	//
	// Below is a minimal example of a ClusterCatalogSpec that sources a catalog from an image:
	//
	//  source:
	//    type: Image
	//    image:
	//      ref: quay.io/operatorhubio/catalog:latest
	//
	// For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs
	Source CatalogSource `json:"source"`

	// priority is an optional field that allows the user to define a priority for a ClusterCatalog.
	// A ClusterCatalog's priority is used by clients as a tie-breaker between ClusterCatalogs that meet the client's requirements.
	// For example, in the case where multiple ClusterCatalogs provide the same bundle.
	// A higher number means higher priority. Negative number as also accepted.
	// When omitted, the default priority is 0.
	// +kubebuilder:default:=0
	// +optional
	Priority int32 `json:"priority"`
}

// ClusterCatalogStatus defines the observed state of ClusterCatalog
type ClusterCatalogStatus struct {
	// conditions is a representation of the current state for this ClusterCatalog.
	// The status is represented by a set of "conditions".
	//
	// Each condition is generally structured in the following format:
	//   - Type: a string representation of the condition type. More or less the condition "name".
	//   - Status: a string representation of the state of the condition. Can be one of ["True", "False", "Unknown"].
	//   - Reason: a string representation of the reason for the current state of the condition. Typically useful for building automation around particular Type+Reason combinations.
	//   - Message: a human-readable message that further elaborates on the state of the condition.
	//
	// The current set of condition types are:
	//   - "Unpacked", epresents whether, or not, the catalog contents have been successfully unpacked.
	//   - "Deleted", represents whether, or not, the catalog contents have been successfully deleted.
	//
	// The current set of reasons are:
	//   - "UnpackPending", this reason is set on the "Unpack" condition when unpacking the catalog has not started.
	//   - "Unpacking", this reason is set on the "Unpack" condition when the catalog is being unpacked.
	//   - "UnpackSuccessful", this reason is set on the "Unpack" condition when unpacking the catalog is successful and the catalog metadata is available to the cluster.
	//   - "FailedToStore", this reason is set on the "Unpack" condition when an error has been encountered while storing the contents of the catalog.
	//   - "FailedToDelete", this reason is set on the "Delete" condition when an error has been encountered while deleting the contents of the catalog.
	//
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// resolvedSource contains information about the resolved source based on the source type.
	//
	// Below is an example of a resolved source for an image source:
	// resolvedSource:
	//
	//  image:
	//    lastPollAttempt: "2024-09-10T12:22:13Z"
	//    lastUnpacked: "2024-09-10T12:22:13Z"
	//    ref: quay.io/operatorhubio/catalog:latest
	//    resolvedRef: quay.io/operatorhubio/catalog@sha256:c7392b4be033da629f9d665fec30f6901de51ce3adebeff0af579f311ee5cf1b
	//  type: Image
	// +optional
	ResolvedSource *ResolvedCatalogSource `json:"resolvedSource,omitempty"`
	// contentURL is a cluster-internal URL from which on-cluster components
	// can read the content of a catalog
	// +optional
	ContentURL string `json:"contentURL,omitempty"`

	// lastUnpacked represents the time when the
	// ClusterCatalog object was last unpacked successfully.
	// +optional
	LastUnpacked metav1.Time `json:"lastUnpacked,omitempty"`
}

// CatalogSource is a discriminated union of possible sources for a Catalog.
// CatalogSource contains the sourcing information for a Catalog
// +union
// +kubebuilder:validation:XValidation:rule="self.type == 'Image' && has(self.image)",message="source type 'Image' requires image field"
type CatalogSource struct {
	// type is a required reference to the type of source the catalog is sourced from.
	//
	// Allowed values are ["Image"]
	//
	// When this field is set to "Image", the ClusterCatalog content will be sourced from an OCI image.
	// When using an image source, the image field must be set and must be the only field defined for this type.
	//
	// +unionDiscriminator
	// +kubebuilder:validation:Enum:="Image"
	// +kubebuilder:validation:Required
	Type SourceType `json:"type"`
	// image is used to configure how catalog contents are sourced from an OCI image. This field must be set when type is set to "Image" and must be the only field defined for this type.
	// +optional
	Image *ImageSource `json:"image,omitempty"`
}

// ResolvedCatalogSource is a discriminated union of resolution information for a Catalog.
// ResolvedCatalogSource contains the information about a sourced Catalog
// +union
// +kubebuilder:validation:XValidation:rule="self.type == 'Image' && has(self.image)",message="source type 'Image' requires image field"
type ResolvedCatalogSource struct {
	// type is a reference to the type of source the catalog is sourced from.
	//
	// It will be set to one of the following values: ["Image"].
	//
	// When this field is set to "Image", information about the resolved image source will be set in the 'image' field.
	//
	// +unionDiscriminator
	// +kubebuilder:validation:Enum:="Image"
	// +kubebuilder:validation:Required
	Type SourceType `json:"type"`
	// image is a field containing resolution information for a catalog sourced from an image.
	Image *ResolvedImageSource `json:"image"`
}

// ResolvedImageSource provides information about the resolved source of a Catalog sourced from an image.
type ResolvedImageSource struct {
	// ref contains the resolved sha256 image ref containing Catalog contents.
	Ref string `json:"ref"`
	// lastSuccessfulPollAttempt is the time when the resolved source was last successfully polled for new content.
	LastSuccessfulPollAttempt metav1.Time `json:"lastSuccessfulPollAttempt"`
}

// ImageSource enables users to define the information required for sourcing a Catalog from an OCI image
type ImageSource struct {
	// ref is a required field that allows the user to define the reference to a container image containing Catalog contents.
	// Examples:
	//   ref: quay.io/operatorhubio/catalog:latest # image reference
	//   ref: quay.io/operatorhubio/catalog@sha256:c7392b4be033da629f9d665fec30f6901de51ce3adebeff0af579f311ee5cf1b # image reference with sha256 digest
	Ref string `json:"ref"`
	// pollInterval is an optional field that allows the user to set the interval at which the image source should be polled for new content.
	// It must be specified as a duration.
	// It must not be specified for a catalog image referenced by a sha256 digest.
	// Examples:
	//   pollInterval: 1h # poll the image source every hour
	//   pollInterval: 30m # poll the image source every 30 minutes
	//   pollInterval: 1h30m # poll the image source every 1 hour and 30 minutes
	//
	// When omitted, the image will not be polled for new content.
	// +kubebuilder:validation:Format:=duration
	// +optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ClusterCatalog{}, &ClusterCatalogList{})
}
