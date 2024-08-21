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

	"github.com/operator-framework/operator-controller/internal/conditionsets"
)

var (
	ClusterExtensionGVK  = SchemeBuilder.GroupVersion.WithKind("ClusterExtension")
	ClusterExtensionKind = ClusterExtensionGVK.Kind
)

type UpgradeConstraintPolicy string
type CRDUpgradeSafetyPolicy string

const (
	// The extension will only upgrade if the new version satisfies
	// the upgrade constraints set by the package author.
	UpgradeConstraintPolicyEnforce UpgradeConstraintPolicy = "Enforce"

	// Unsafe option which allows an extension to be
	// upgraded or downgraded to any available version of the package and
	// ignore the upgrade path designed by package authors.
	// This assumes that users independently verify the outcome of the changes.
	// Use with caution as this can lead to unknown and potentially
	// disastrous results such as data loss.
	UpgradeConstraintPolicyIgnore UpgradeConstraintPolicy = "Ignore"
)

// ClusterExtensionSpec defines the desired state of ClusterExtension
type ClusterExtensionSpec struct {
	//+kubebuilder:validation:MaxLength:=48
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+(-[a-z0-9]+)*$
	//+kubebuilder:validation:XValidation:rule="self == oldSelf",message="packageName is immutable"
	PackageName string `json:"packageName"`

	//+kubebuilder:validation:MaxLength:=64
	//+kubebuilder:validation:Pattern=`^(\s*(=||!=|>|<|>=|=>|<=|=<|~|~>|\^)\s*(v?(0|[1-9]\d*|[x|X|\*])(\.(0|[1-9]\d*|x|X|\*]))?(\.(0|[1-9]\d*|x|X|\*))?(-([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?(\+([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?)\s*)((?:\s+|,\s*|\s*\|\|\s*)(=||!=|>|<|>=|=>|<=|=<|~|~>|\^)\s*(v?(0|[1-9]\d*|x|X|\*])(\.(0|[1-9]\d*|x|X|\*))?(\.(0|[1-9]\d*|x|X|\*]))?(-([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?(\+([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?)\s*)*$`
	//+kubebuilder:Optional
	// Version is an optional semver constraint on the package version. If not specified, the latest version available of the package will be installed.
	// If specified, the specific version of the package will be installed so long as it is available in any of the content sources available.
	// Examples: 1.2.3, 1.0.0-alpha, 1.0.0-rc.1
	//
	// For more information on semver, please see https://semver.org/
	Version string `json:"version,omitempty"`

	//+kubebuilder:validation:MaxLength:=48
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+([\.-][a-z0-9]+)*$
	// Channel constraint definition
	Channel string `json:"channel,omitempty"`

	//+kubebuilder:optional
	// +optional
	// CatalogSelector by label
	CatalogSelector metav1.LabelSelector `json:"catalogSelector,omitempty"`

	//+kubebuilder:validation:Enum:=Enforce;Ignore
	//+kubebuilder:default:=Enforce
	//+kubebuilder:Optional
	//
	// Defines the policy for how to handle upgrade constraints
	UpgradeConstraintPolicy UpgradeConstraintPolicy `json:"upgradeConstraintPolicy,omitempty"`

	//+kubebuilder:validation:Pattern:=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	//+kubebuilder:validation:MaxLength:=63
	//+kubebuilder:validation:XValidation:rule="self == oldSelf",message="installNamespace is immutable"
	//
	// installNamespace is the namespace where the bundle should be installed. However, note that
	// the bundle may contain resources that are cluster-scoped or that are
	// installed in a different namespace. This namespace is expected to exist.
	InstallNamespace string `json:"installNamespace"`

	//+kubebuilder:Optional
	// Preflight defines the configuration of preflight checks.
	Preflight *PreflightConfig `json:"preflight,omitempty"`

	// ServiceAccount is used to install and manage resources.
	// The service account is expected to exist in the InstallNamespace.
	ServiceAccount ServiceAccountReference `json:"serviceAccount"`
}

// ServiceAccountReference references a serviceAccount.
type ServiceAccountReference struct {
	// name is the metadata.name of the referenced serviceAccount object.
	//+kubebuilder:validation:MaxLength:=253
	//+kubebuilder:validation:Pattern:=^[a-z0-9]+([.|-][a-z0-9]+)*$
	//+kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name"`
}

// PreflightConfig holds the configuration for the preflight checks.
type PreflightConfig struct {
	//+kubebuilder:Required
	// CRDUpgradeSafety holds necessary configuration for the CRD Upgrade Safety preflight checks.
	CRDUpgradeSafety *CRDUpgradeSafetyPreflightConfig `json:"crdUpgradeSafety"`
}

// CRDUpgradeSafetyPreflightConfig is the configuration for CRD upgrade safety preflight check.
type CRDUpgradeSafetyPreflightConfig struct {
	//+kubebuilder:Required
	//+kubebuilder:validation:Enum:="Enabled";"Disabled"
	//+kubebuilder:default:=Enabled
	// policy represents the state of the CRD upgrade safety preflight check. Allowed values are "Enabled", and Disabled".
	Policy CRDUpgradeSafetyPolicy `json:"policy"`
}

const (
	// TODO(user): add more Types, here and into init()
	TypeInstalled = "Installed"
	TypeResolved  = "Resolved"

	// TypeDeprecated is a rollup condition that is present when
	// any of the deprecated conditions are present.
	TypeDeprecated        = "Deprecated"
	TypePackageDeprecated = "PackageDeprecated"
	TypeChannelDeprecated = "ChannelDeprecated"
	TypeBundleDeprecated  = "BundleDeprecated"
	TypeUnpacked          = "Unpacked"

	ReasonErrorGettingClient = "ErrorGettingClient"
	ReasonBundleLoadFailed   = "BundleLoadFailed"

	ReasonInstallationFailed = "InstallationFailed"
	ReasonResolutionFailed   = "ResolutionFailed"

	ReasonSuccess       = "Success"
	ReasonDeprecated    = "Deprecated"
	ReasonUpgradeFailed = "UpgradeFailed"

	ReasonUnpackSuccess = "UnpackSuccess"
	ReasonUnpackFailed  = "UnpackFailed"

	ReasonErrorGettingReleaseState = "ErrorGettingReleaseState"

	CRDUpgradeSafetyPolicyEnabled  CRDUpgradeSafetyPolicy = "Enabled"
	CRDUpgradeSafetyPolicyDisabled CRDUpgradeSafetyPolicy = "Disabled"
)

func init() {
	// TODO(user): add Types from above
	conditionsets.ConditionTypes = append(conditionsets.ConditionTypes,
		TypeInstalled,
		TypeResolved,
		TypeDeprecated,
		TypePackageDeprecated,
		TypeChannelDeprecated,
		TypeBundleDeprecated,
		TypeUnpacked,
	)
	// TODO(user): add Reasons from above
	conditionsets.ConditionReasons = append(conditionsets.ConditionReasons,
		ReasonResolutionFailed,
		ReasonInstallationFailed,
		ReasonSuccess,
		ReasonDeprecated,
		ReasonUpgradeFailed,
		ReasonBundleLoadFailed,
		ReasonErrorGettingClient,
		ReasonUnpackSuccess,
		ReasonUnpackFailed,
		ReasonErrorGettingReleaseState,
	)
}

type BundleMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClusterExtensionStatus defines the observed state of ClusterExtension.
type ClusterExtensionStatus struct {
	// InstalledBundle should only be modified when a new bundle is successfully installed. This ensures that if there
	//  is a previously successfully installed a bundle, and an upgrade fails, it is still communicated that there is
	//  still a bundle that is currently installed and owned by the ClusterExtension.
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

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status

// ClusterExtension is the Schema for the clusterextensions API
type ClusterExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterExtensionSpec   `json:"spec,omitempty"`
	Status ClusterExtensionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterExtensionList contains a list of ClusterExtension
type ClusterExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterExtension `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterExtension{}, &ClusterExtensionList{})
}
