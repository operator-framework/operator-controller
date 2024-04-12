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

	"github.com/operator-framework/operator-controller/internal/conditionsets"
)

const (
	SourceTypePackage = "package"
)

type ExtensionSourcePackage struct {
	//+kubebuilder:validation:MaxLength:=48
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+(-[a-z0-9]+)*$
	// name specifies the name of the name of the package
	Name string `json:"name"`

	//+kubebuilder:validation:MaxLength:=64
	//+kubebuilder:validation:Pattern=`^(\s*(=||!=|>|<|>=|=>|<=|=<|~|~>|\^)\s*(v?(0|[1-9]\d*|[x|X|\*])(\.(0|[1-9]\d*|x|X|\*]))?(\.(0|[1-9]\d*|x|X|\*))?(-([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?(\+([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?)\s*)((?:\s+|,\s*|\s*\|\|\s*)(=||!=|>|<|>=|=>|<=|=<|~|~>|\^)\s*(v?(0|[1-9]\d*|x|X|\*])(\.(0|[1-9]\d*|x|X|\*))?(\.(0|[1-9]\d*|x|X|\*]))?(-([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?(\+([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?)\s*)*$`
	//+kubebuilder:Optional
	// Version is an optional semver constraint on the package version. If not specified, the latest version available of the package will be installed.
	// If specified, the specific version of the package will be installed so long as it is available in any of the content sources available.
	// Examples: 1.2.3, 1.0.0-alpha, 1.0.0-rc.1
	//
	// For more information on semver, please see https://semver.org/
	// version constraint definition
	Version string `json:"version,omitempty"`

	//+kubebuilder:validation:MaxLength:=48
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+([\.-][a-z0-9]+)*$
	// channel constraint definition
	Channel string `json:"channel,omitempty"`

	//+kubebuilder:validation:Enum:=Enforce;Ignore
	//+kubebuilder:default:=Enforce
	//+kubebuilder:Optional
	//
	// upgradeConstraintPolicy Defines the policy for how to handle upgrade constraints
	UpgradeConstraintPolicy UpgradeConstraintPolicy `json:"upgradeConstraintPolicy,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.sourceType=='package' && has(self.__package__)",message="sourceType must match populated union field"
//
// ExtensionSource defines the source for this Extension, right now, only a package is supported.
type ExtensionSource struct {
	//+kubebuilder:validation:Enum:=package
	//+kubebuilder:validation:Required
	// sourceType is the discriminator for the source type
	SourceType string `json:"sourceType"`

	// package defines a reference for a bundle in a catalog defined by a name and a version and/or channel
	Package *ExtensionSourcePackage `json:"package,omitempty"`
}

// ExtensionSpec defines the desired state of Extension
type ExtensionSpec struct {
	//+kubebuilder:Optional
	//
	// paused controls the management state of the extension. If the extension is paused, it will be ignored by the extension controller.
	Paused bool `json:"paused,omitempty"`

	//+kubebuilder:validation:MaxLength:=253
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+([\.-][a-z0-9]+)*$
	//
	// serviceAccountName is the name of a service account in the Extension's namespace that will be used to manage the installation and lifecycle of the extension.
	ServiceAccountName string `json:"serviceAccountName"`

	// source of Extension to be installed
	Source ExtensionSource `json:"source"`

	//+kubebuilder:Optional
	//
	// skipCRDUpgradeSafetyCheck specifies whether or not the CRD upgrade safety checks should be skipped when attempting to install the extension
	SkipCRDUpgradeSafetyCheck bool `json:"skipCRDUpgradeSafetyCheck,omitempty"`
}

// ExtensionStatus defines the observed state of Extension
type ExtensionStatus struct {
	// paused indicates the current reconciliation state of this extension
	Paused bool `json:"paused"`

	// +optional
	InstalledBundle *BundleMetadata `json:"installedBundle,omitempty"`
	// +optional
	ResolvedBundle *BundleMetadata `json:"resolvedBundle,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

type BundleMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Paused",type=string,JSONPath=`.status.paused`,description="The current reconciliation state of this extension"

// Extension is the Schema for the extensions API
type Extension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExtensionSpec   `json:"spec,omitempty"`
	Status ExtensionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ExtensionList contains a list of Extension
type ExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Extension `json:"items"`
}

const (
	// TypeProgressing indicates whether operator-controller is
	// reconciling, installing, updating or deleting an extension.
	TypeProgressing = "Progressing"

	ReasonProgressing                = "Progressing"
	ReasonFailedToReachDesiredIntent = "FailedToReachDesiredIntent"
	ReasonReachedDesiredIntent       = "ReachedDesiredIntent"
)

func init() {
	SchemeBuilder.Register(&Extension{}, &ExtensionList{})

	conditionsets.ExtensionConditionTypes = []string{
		TypeProgressing,
	}
	conditionsets.ExtensionConditionReasons = []string{
		ReasonProgressing,
		ReasonFailedToReachDesiredIntent,
		ReasonReachedDesiredIntent,
	}
}
