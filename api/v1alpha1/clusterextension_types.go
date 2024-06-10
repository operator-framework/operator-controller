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

const (
	SourceTypePackage = "package"
)

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

// +kubebuilder:validation:XValidation:rule="self.sourceType=='package' && has(self.__package__)",message="sourceType must match populated union field"
//
// ClusterExtensionSource defines the source for this ClusterExtensionSource, right now, only a package is supported.
type ClusterExtensionSource struct {
	//+kubebuilder:validation:Enum:=package
	//+kubebuilder:validation:Required
	// sourceType is the discriminator for the source type
	SourceType string `json:"sourceType"`

	// package defines a reference for a bundle in a catalog defined by a name and a version and/or channel
	Package *ClusterExtensionSourcePackage `json:"package,omitempty"`
}

type ClusterExtensionSourcePackage struct {
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

	// InsecureSkipTLSVerify indicates that TLS certificate validation should be skipped.
	// If this option is specified, the HTTPS protocol will still be used to
	// fetch the specified image reference.
	// This should not be used in a production environment.
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
}

// ClusterExtensionSpec defines the desired state of ClusterExtension
type ClusterExtensionSpec struct {
	// source of Extension to be installed
	Source ClusterExtensionSource `json:"source"`

	//+kubebuilder:validation:Pattern:=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	//+kubebuilder:validation:MaxLength:=63
	//
	// installNamespace is the namespace where the bundle should be installed. However, note that
	// the bundle may contain resources that are cluster-scoped or that are
	// installed in a different namespace. This namespace is expected to exist.
	InstallNamespace string `json:"installNamespace"`
}

const (
	// TODO(user): add more Types, here and into init()
	TypeInstalled      = "Installed"
	TypeResolved       = "Resolved"
	TypeHasValidBundle = "HasValidBundle"

	// TypeDeprecated is a rollup condition that is present when
	// any of the deprecated conditions are present.
	TypeDeprecated        = "Deprecated"
	TypePackageDeprecated = "PackageDeprecated"
	TypeChannelDeprecated = "ChannelDeprecated"
	TypeBundleDeprecated  = "BundleDeprecated"
	TypeUnpacked          = "Unpacked"

	ReasonErrorGettingClient = "ErrorGettingClient"
	ReasonBundleLoadFailed   = "BundleLoadFailed"

	ReasonInstallationFailed        = "InstallationFailed"
	ReasonInstallationStatusUnknown = "InstallationStatusUnknown"
	ReasonInstallationSucceeded     = "InstallationSucceeded"
	ReasonResolutionFailed          = "ResolutionFailed"

	ReasonSuccess               = "Success"
	ReasonDeprecated            = "Deprecated"
	ReasonUpgradeFailed         = "UpgradeFailed"
	ReasonHasValidBundleUnknown = "HasValidBundleUnknown"

	ReasonUnpackPending = "UnpackPending"
	ReasonUnpackSuccess = "UnpackSuccess"
	ReasonUnpackFailed  = "UnpackFailed"
	ReasonUnpacking     = "Unpacking"

	ReasonErrorGettingReleaseState = "ErrorGettingReleaseState"
	ReasonCreateDynamicWatchFailed = "CreateDynamicWatchFailed"
)

func init() {
	// TODO(user): add Types from above
	conditionsets.ConditionTypes = append(conditionsets.ConditionTypes,
		TypeInstalled,
		TypeResolved,
		TypeHasValidBundle,
		TypeDeprecated,
		TypePackageDeprecated,
		TypeChannelDeprecated,
		TypeBundleDeprecated,
		TypeUnpacked,
	)
	// TODO(user): add Reasons from above
	conditionsets.ConditionReasons = append(conditionsets.ConditionReasons,
		ReasonInstallationSucceeded,
		ReasonResolutionFailed,
		ReasonInstallationFailed,
		ReasonSuccess,
		ReasonDeprecated,
		ReasonUpgradeFailed,
		ReasonBundleLoadFailed,
		ReasonErrorGettingClient,
		ReasonInstallationStatusUnknown,
		ReasonHasValidBundleUnknown,
		ReasonUnpackPending,
		ReasonUnpackSuccess,
		ReasonUnpacking,
		ReasonUnpackFailed,
		ReasonErrorGettingReleaseState,
		ReasonCreateDynamicWatchFailed,
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
