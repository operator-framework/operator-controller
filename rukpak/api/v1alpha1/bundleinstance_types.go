/*
Copyright 2021.

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

var (
	BundleInstanceGVK  = SchemeBuilder.GroupVersion.WithKind("BundleInstance")
	BundleInstanceKind = BundleInstanceGVK.Kind
)

const (
	TypeHasValidBundle       = "HasValidBundle"
	TypeInvalidBundleContent = "InvalidBundleContent"
	TypeInstalled            = "Installed"

	ReasonBundleLookupFailed         = "BundleLookupFailed"
	ReasonBundleLoadFailed           = "BundleLoadFailed"
	ReasonReadingContentFailed       = "ReadingContentFailed"
	ReasonErrorGettingClient         = "ErrorGettingClient"
	ReasonErrorGettingReleaseState   = "ErrorGettingReleaseState"
	ReasonInstallFailed              = "InstallFailed"
	ReasonUpgradeFailed              = "UpgradeFailed"
	ReasonReconcileFailed            = "ReconcileFailed"
	ReasonCreateDynamicWatchFailed   = "CreateDynamicWatchFailed"
	ReasonInstallationSucceeded      = "InstallationSucceeded"
	ReasonMaxGeneratedBundlesReached = "MaxGenerationReached"
)

// BundleInstanceSpec defines the desired state of BundleInstance
type BundleInstanceSpec struct {
	// ProvisionerClassName sets the name of the provisioner that should reconcile this BundleInstance.
	ProvisionerClassName string `json:"provisionerClassName"`
	// Template describes the generated Bundle that this instance will manage.
	Template *BundleTemplate `json:"template"`
}

// BundleTemplate defines the desired state of a Bundle resource
type BundleTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of the desired behavior of the Bundle.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec BundleSpec `json:"spec"`
}

// BundleInstanceStatus defines the observed state of BundleInstance
type BundleInstanceStatus struct {
	Conditions          []metav1.Condition `json:"conditions,omitempty"`
	InstalledBundleName string             `json:"installedBundleName,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName={"bi","bis"}
//+kubebuilder:printcolumn:name="Installed Bundle",type=string,JSONPath=`.status.installedBundleName`
//+kubebuilder:printcolumn:name="Install State",type=string,JSONPath=`.status.conditions[?(.type=="Installed")].reason`
//+kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// BundleInstance is the Schema for the bundleinstances API
type BundleInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BundleInstanceSpec   `json:"spec"`
	Status BundleInstanceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BundleInstanceList contains a list of BundleInstance
type BundleInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BundleInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BundleInstance{}, &BundleInstanceList{})
}
