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

var (
	TypeInstalled = "Installed"

	ReasonSourceFailed  = "SourceFailed"
	ReasonUnpackPending = "UnpackPending"

	ReasonInstallFailed     = "InstallFailed"
	ReasonInstallSuccessful = "InstallSuccessful"
	ReasonInstallPending    = "InstallPending"
)

// SetActiveBundleDeployment is responsible for populating the status.ActiveBundleDeployment
// structure with the Operator resource the Operator controller is currently managing.
func SetActiveBundleDeployment(o *Operator, name string) {
	if o == nil {
		panic("input specified is nil")
	}
	o.Status.ActiveBundleDeployment = ActiveBundleDeployment{
		Name: name,
	}
}

// SetSourceInfo is responsible for populating the status.SourceInfo
// structure with the Operator resource the Operator controller is currently managing.
func SetSourceInfo(o *Operator, info SourceInfo) {
	if o == nil {
		panic("input specified is nil")
	}
	o.Status.SourceInfo = info
}
