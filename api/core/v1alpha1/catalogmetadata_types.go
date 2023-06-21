/*
Copyright 2023.

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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CatalogMetadataSpec defines the desired state of CatalogMetadata.
// It is based on the `Meta` schema defined in Operator Registry (https://olm.operatorframework.io/docs/reference/file-based-catalogs/#schema)
// and it adheres to the format of `Meta` schema that contains fields such as `Schema` (Required), `Package` (Optional), `Name` (Optional) and `Blob`.
// The `CatalogMetadataSpec` is an extension of the `Meta` schema that additionally contains a `Catalog` field which references the Catalog and a `Content` field
// which is a JSON representation of the File-Based Catalog blob.
type CatalogMetadataSpec struct {
	Catalog corev1.LocalObjectReference `json:"catalog"`
	Schema  string                      `json:"schema"`
	Package string                      `json:"package,omitempty"`
	Name    string                      `json:"name,omitempty"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	Content json.RawMessage `json:"content,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster

// CatalogMetadata is the Schema for the catalogs API
type CatalogMetadata struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CatalogMetadataSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// CatalogMetadataList contains a list of CatalogMetadata
type CatalogMetadataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CatalogMetadata `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CatalogMetadata{}, &CatalogMetadataList{})
}
