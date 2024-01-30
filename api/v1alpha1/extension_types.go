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
	"k8s.io/apimachinery/pkg/types"
)

type ExtensionManagedState string

const (
	// Peform reconcilliation of this Extension
	ManagedStateActive ExtensionManagedState = "Active"
	// Pause reconcilliation of this Extension
	ManagedStatePaused ExtensionManagedState = "Paused"
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

// ExtensionSource defines the source for this Extension, right now, only a package is supported.
type ExtensionSource struct {
	// A source package defined by a name, version and/or channel
	Package *ExtensionSourcePackage `json:"package,omitempty"`
}

// ExtensionSpec defines the desired state of Extension
type ExtensionSpec struct {
	//+kubebuilder:validation:Enum:=Active;Paused
	//+kubebuilder:default:=Active
	//+kubebuilder:Optional
	//
	// managed controls the management state of the extension. "Active" means this extension will be reconciled and "Paused" means this extension will be ignored.
	Managed ExtensionManagedState `json:"managed,omitempty"`

	//+kubebuilder:validation:MaxLength:=253
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+([\.-][a-z0-9]+)*$
	//
	// serviceAccountName is the name of a service account in the Extension's namespace that will be used to manage the installation and lifecycle of the extension.
	ServiceAccountName string `json:"serviceAccountName"`

	// source of Extension to be installed
	Source ExtensionSource `json:"source"`
}

// ExtensionStatus defines the observed state of Extension
type ExtensionStatus struct {
	// +optional
	InstalledBundleResource string `json:"installedBundleResource,omitempty"`
	// +optional
	ResolvedBundleResource string `json:"resolvedBundleResource,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Managed",type=string,JSONPath=`.spec.managed`,description="The current reconciliation state of this extension"

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

func init() {
	SchemeBuilder.Register(&Extension{}, &ExtensionList{})
}

func (r *Extension) GetPackageSpec() *ExtensionSourcePackage {
	return r.Spec.Source.Package.DeepCopy()
}

func (r *Extension) GetUID() types.UID {
	return r.ObjectMeta.GetUID()
}

func (r *Extension) GetUpgradeConstraintPolicy() UpgradeConstraintPolicy {
	if r.Spec.Source.Package != nil {
		return r.Spec.Source.Package.UpgradeConstraintPolicy
	}
	return ""
}
