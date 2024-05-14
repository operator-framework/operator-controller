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

type UpgradeConstraintPolicy string

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

	//+kubebuilder:validation:Enum:=Enforce;Ignore
	//+kubebuilder:default:=Enforce
	//+kubebuilder:Optional
	//
	// Defines the policy for how to handle upgrade constraints
	UpgradeConstraintPolicy UpgradeConstraintPolicy `json:"upgradeConstraintPolicy,omitempty"`

	//+kubebuilder:validation:Pattern:=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	//+kubebuilder:validation:MaxLength:=63
	//
	// installNamespace is the namespace where the bundle should be installed. However, note that
	// the bundle may contain resources that are cluster-scoped or that are
	// installed in a different namespace. This namespace is expected to exist.
	InstallNamespace string `json:"installNamespace"`

	//+kubebuilder:Optional
	// Preflight defines the strategy for the preflight checks (i.e. as of now, the CRD upgrade safety checks) to be applied or skipped when attempting to install the cluster extension.
	Preflight PreflightConfig `json:"preflight,omitempty"`
}

// PreflightConfig holds the config for the preflight checks. Currently, this is implemented for the
// CRDUpgradeSafety preflight check and can be extended in the future to add other preflight checks.
type PreflightConfig struct {
	CRDUpgradeSafety CRDUpgradeSafetyPreflightConfig `json:"crdUpgradeSafety,omitempty"`
}

// CRDUpgradeSafetyPreflightConfig is a custom struct that holds the necessary configuration values to
// enable or disable the CRD Upgrade Safety validations. Currently, this holds Mode
// that sets the CRD Upgrade Safety validations to either Enabled/Disabled.
// By default, this is set to "Enabled".
type CRDUpgradeSafetyPreflightConfig struct {
	//+kubebuilder:validation:Enum:=Enabled;Disabled
	//+kubebuilder:default:=Enabled
	//+kubebuilder:Optional
	CRDUpgradeSafetyMode CRDUpgradeSafetyMode `json:"mode,omitempty"`
}

type CRDUpgradeSafetyMode string

const (
	// CRDUpgradeSafetyModeEnabled represents the default state for the
	// CRD Upgrade Safety validations by setting it to "Enabled".
	CRDUpgradeSafetyModeEnabled CRDUpgradeSafetyMode = "Enabled"

	// CRDUpgradeSafetyModeDisabled represents the option for the
	// CRD Upgrade Safety validations to be disabled by setting it to "Disabled".
	CRDUpgradeSafetyModeDisabled CRDUpgradeSafetyMode = "Disabled"
)

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

	ReasonBundleLookupFailed        = "BundleLookupFailed"
	ReasonInstallationFailed        = "InstallationFailed"
	ReasonInstallationStatusUnknown = "InstallationStatusUnknown"
	ReasonInstallationSucceeded     = "InstallationSucceeded"
	ReasonInvalidSpec               = "InvalidSpec"
	ReasonResolutionFailed          = "ResolutionFailed"
	ReasonResolutionUnknown         = "ResolutionUnknown"
	ReasonSuccess                   = "Success"
	ReasonDeprecated                = "Deprecated"
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
	)
	// TODO(user): add Reasons from above
	conditionsets.ConditionReasons = append(conditionsets.ConditionReasons,
		ReasonInstallationSucceeded,
		ReasonResolutionFailed,
		ReasonResolutionUnknown,
		ReasonBundleLookupFailed,
		ReasonInstallationFailed,
		ReasonInstallationStatusUnknown,
		ReasonInvalidSpec,
		ReasonSuccess,
		ReasonDeprecated,
	)
}

type BundleMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClusterExtensionStatus defines the observed state of ClusterExtension
type ClusterExtensionStatus struct {
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
