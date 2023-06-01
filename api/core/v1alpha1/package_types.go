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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster

// Package is the Schema for the packages API
type Package struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PackageSpec   `json:"spec,omitempty"`
	Status PackageStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PackageList contains a list of Package
type PackageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Package `json:"items"`
}

// PackageSpec defines the desired state of Package
type PackageSpec struct {
	// Catalog is the name of the Catalog this package belongs to
	Catalog corev1.LocalObjectReference `json:"catalog"`

	// Name is the name of the package, ala `etcd`.
	Name string `json:"packageName"`

	// Description is the description of the package
	Description string `json:"description"`

	// Channels are the declared channels for the package, ala `stable` or `alpha`.
	Channels []PackageChannel `json:"channels"`

	//Icon is the Base64data image of the package for console display
	Icon *Icon `json:"icon,omitempty"`

	// DefaultChannel is, if specified, the name of the default channel for the package. The
	// default channel will be installed if no other channel is explicitly given. If the package
	// has a single channel, then that channel is implicitly the default.
	DefaultChannel string `json:"defaultChannel"`
}

// PackageChannel defines a single channel under a package, pointing to a version of that
// package.
type PackageChannel struct {
	// Name is the name of the channel, e.g. `alpha` or `stable`
	Name string `json:"name"`

	// Entries is all the channel entries within a channel
	Entries []ChannelEntry `json:"entries"`
}

type ChannelEntry struct {
	Name      string   `json:"name"`
	Replaces  string   `json:"replaces,omitempty"`
	Skips     []string `json:"skips,omitempty"`
	SkipRange string   `json:"skipRange,omitempty"`
}

// Icon defines a base64 encoded icon and media type
type Icon struct {
	Data      []byte `json:"data,omitempty"`
	MediaType string `json:"mediatype,omitempty"`
}

// PackageStatus defines the observed state of Package
type PackageStatus struct{}

func init() {
	SchemeBuilder.Register(&Package{}, &PackageList{})
}
