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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	BundleGVK  = SchemeBuilder.GroupVersion.WithKind("Bundle")
	BundleKind = BundleGVK.Kind
)

type SourceType string

const (
	SourceTypeImage SourceType = "image"
	SourceTypeGit   SourceType = "git"

	TypeUnpacked = "Unpacked"

	ReasonUnpackPending             = "UnpackPending"
	ReasonUnpacking                 = "Unpacking"
	ReasonUnpackSuccessful          = "UnpackSuccessful"
	ReasonUnpackFailed              = "UnpackFailed"
	ReasonProcessingFinalizerFailed = "ProcessingFinalizerFailed"

	PhasePending   = "Pending"
	PhaseUnpacking = "Unpacking"
	PhaseFailing   = "Failing"
	PhaseUnpacked  = "Unpacked"
)

// BundleSpec defines the desired state of Bundle
type BundleSpec struct {
	// ProvisionerClassName sets the name of the provisioner that should reconcile this BundleInstance.
	ProvisionerClassName string `json:"provisionerClassName"`
	// Source defines the configuration for the underlying Bundle content.
	Source BundleSource `json:"source"`
}

type BundleSource struct {
	// Type defines the kind of Bundle content being sourced.
	Type SourceType `json:"type"`
	// Image is the bundle image that backs the content of this bundle.
	Image *ImageSource `json:"image,omitempty"`
	// Git is the git repository that backs the content of this Bundle.
	Git *GitSource `json:"git,omitempty"`
}

type ImageSource struct {
	// Ref contains the reference to a container image containing Bundle contents.
	Ref string `json:"ref"`
	// ImagePullSecretName contains the name of the image pull secret in the namespace that the provisioner is deployed.
	ImagePullSecretName string `json:"pullSecret,omitempty"`
}

type GitSource struct {
	// Repository is a URL link to the git repository containing the bundle.
	// Repository is required and the URL should be parsable by a standard git tool.
	Repository string `json:"repository"`
	// Directory refers to the location of the bundle within the git repository.
	// Directory is optional and if not set defaults to ./manifests.
	Directory string `json:"directory,omitempty"`
	// Ref configures the git source to clone a specific branch, tag, or commit
	// from the specified repo. Ref is required, and exactly one field within Ref
	// is required. Setting more than one field or zero fields will result in an
	// error.
	Ref GitRef `json:"ref"`
	// Auth configures the authorization method if necessary.
	Auth Authorization `json:"auth,omitempty"`
}

type GitRef struct {
	// Branch refers to the branch to checkout from the repository.
	// The Branch should contain the bundle manifests in the specified directory.
	Branch string `json:"branch,omitempty"`
	// Tag refers to the tag to checkout from the repository.
	// The Tag should contain the bundle manifests in the specified directory.
	Tag string `json:"tag,omitempty"`
	// Commit refers to the commit to checkout from the repository.
	// The Commit should contain the bundle manifests in the specified directory.
	Commit string `json:"commit,omitempty"`
}

type Authorization struct {
	// Secret contains reference to the secret that has authorization information for HTTPS protocol and is in the namespace that the provisioner is deployed.
	// The secret is expected to contain `data.username` and `data.password` for the username and password, respectively.
	Secret corev1.LocalObjectReference `json:"secret,omitempty"`
	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name. This
	// configuration is used only when cloning git repositories that use the "https" scheme. If InsecureSkipVerify
	// is true, the clone operation will accept any certificate presented by the server and any host name in that
	// certificate. In this mode, TLS is susceptible to machine-in-the-middle attacks unless custom verification is
	// used. This should be used only for testing.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

type ProvisionerID string

// BundleStatus defines the observed state of Bundle
type BundleStatus struct {
	Phase              string             `json:"phase,omitempty"`
	ResolvedSource     *BundleSource      `json:"resolvedSource,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ContentURL         string             `json:"contentURL,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name=Type,type=string,JSONPath=`.spec.source.type`
//+kubebuilder:printcolumn:name=Phase,type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
//+kubebuilder:printcolumn:name=Resolved Source,type=string,JSONPath=`.status.resolvedSource`,priority=1

// Bundle is the Schema for the bundles API
type Bundle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BundleSpec   `json:"spec"`
	Status BundleStatus `json:"status,omitempty"`
}

func (b *Bundle) ProvisionerClassName() string {
	return b.Spec.ProvisionerClassName
}

//+kubebuilder:object:root=true

// BundleList contains a list of Bundle
type BundleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bundle `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Bundle{}, &BundleList{})
}
