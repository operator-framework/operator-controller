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

package rukpakapi

type SourceType string

const (
	SourceTypeImage SourceType = "image"

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

type BundleSource struct {
	// Type defines the kind of Bundle content being sourced.
	Type SourceType `json:"type"`
	// Image is the bundle image that backs the content of this bundle.
	Image ImageSource `json:"image,omitempty"`
	// Git is the git repository that backs the content of this Bundle.
}

type ImageSource struct {
	// Ref contains the reference to a container image containing Bundle contents.
	Ref string `json:"ref"`
	// ImagePullSecretName contains the name of the image pull secret in the namespace that the provisioner is deployed.
	ImagePullSecretName string `json:"pullSecret,omitempty"`
}
