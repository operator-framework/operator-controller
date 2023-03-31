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

package v1beta1

import (
	"context"

	"github.com/operator-framework/api/pkg/lib/version"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource/resourcestrategy"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Package
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Cluster
type Package struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PackageSpec   `json:"spec,omitempty"`
	Status PackageStatus `json:"status,omitempty"`
}

// PackageList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PackageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Package `json:"items"`
}

// PackageSpec defines the desired state of Package
type PackageSpec struct {
	// CatalogSource is the name of the CatalogSource this package belongs to
	CatalogSource            string `json:"catalogSource"`
	CatalogSourceDisplayName string `json:"catalogSourceDisplayName,omitempty"`
	CatalogSourcePublisher   string `json:"catalogSourcePublisher,omitempty"`

	// TODO(everettraven): can we remove this? Can the package metadata.name can be used instead?
	// // PackageName is the name of the overall package, ala `etcd`.
	// PackageName string `json:"packageName"`

	// Description is the description of the package
	Description string `json:"description"`

	// Channels are the declared channels for the package, ala `stable` or `alpha`.
	Channels []PackageChannel `json:"channels"`

	//Icon is the Base64data image of the package for console display
	Icon Icon `json:"icon,omitempty"`

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
	Base64Data string `json:"base64data,omitempty"`
	Mediatype  string `json:"mediatype,omitempty"`
}

var _ resource.Object = &Package{}
var _ resourcestrategy.Validater = &Package{}

func (in *Package) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Package) NamespaceScoped() bool {
	return false
}

func (in *Package) New() runtime.Object {
	return &Package{}
}

func (in *Package) NewList() runtime.Object {
	return &PackageList{}
}

func (in *Package) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "catalogd.operatorframework.io",
		Version:  "v1beta1",
		Resource: "packages",
	}
}

func (in *Package) IsStorageVersion() bool {
	return true
}

func (in *Package) Validate(ctx context.Context) field.ErrorList {
	// TODO(user): Modify it, adding your API validation here.
	return nil
}

var _ resource.ObjectList = &PackageList{}

func (in *PackageList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

// TODO: Audit this section
// ---- START AUDIT SECTION ----

// AppLink defines a link to an application
type AppLink struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Maintainer defines a project maintainer
type Maintainer struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// Description
type Description struct {
	// DisplayName
	DisplayName string `json:"displayName,omitempty"`

	// Icon is the base64 encoded icon
	// +listType=set
	Icon []Icon `json:"icon,omitempty"`

	// Version
	Version version.OperatorVersion `json:"version,omitempty"`

	// Provider
	Provider AppLink `json:"provider,omitempty"`
	// // +listType=map
	// Annotations map[string]string `json:"annotations,omitempty"`
	// +listType=set
	Keywords []string `json:"keywords,omitempty"`
	// +listType=set
	Links []AppLink `json:"links,omitempty"`
	// +listType=set
	Maintainers []Maintainer `json:"maintainers,omitempty"`
	Maturity    string       `json:"maturity,omitempty"`

	// LongDescription
	LongDescription string `json:"description,omitempty"`

	// InstallModes specify supported installation types
	// +listType=set
	InstallModes []operatorv1alpha1.InstallMode `json:"installModes,omitempty"`

	CustomResourceDefinitions operatorv1alpha1.CustomResourceDefinitions `json:"customresourcedefinitions,omitempty"`
	APIServiceDefinitions     operatorv1alpha1.APIServiceDefinitions     `json:"apiservicedefinitions,omitempty"`
	NativeAPIs                []metav1.GroupVersionKind                  `json:"nativeApis,omitempty"`

	// Minimum Kubernetes version for operator installation
	MinKubeVersion string `json:"minKubeVersion,omitempty"`

	// List of related images
	RelatedImages []string `json:"relatedImages,omitempty"`
}

// ---- END AUDIT SECTION ----

// PackageStatus defines the observed state of Package
type PackageStatus struct{}

func (in PackageStatus) SubResourceName() string {
	return "status"
}

// Package implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &Package{}

func (in *Package) GetStatus() resource.StatusSubResource {
	return in.Status
}

// PackageStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &PackageStatus{}

func (in PackageStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*Package).Status = in
}
